package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// return_to is the frontend path a login started from. It is validated on
// the server and only ever leads back into the frontend; anything doubtful
// is dropped (empty), never an error.
func TestSanitizeReturnTo(t *testing.T) {
	keep := []string{
		"/",
		"/status/demo",
		"/status/demo?tab=history&lang=de#incident-3",
		"/admin/pages/3/incidents",
		"/status/demo?q=a%20b",   // encoded space inside the query is fine
		"/status/demo?next=%2Fx", // an encoded slash later on is fine
		"/" + strings.Repeat("a", 2047),
		"/status/api", // /api only counts at the start
	}
	for _, raw := range keep {
		assert.Equal(t, raw, sanitizeReturnTo(raw), "%q must be kept", raw)
	}
	drop := map[string]string{
		"empty":                 "",
		"protocol-relative":     "//evil.example.org/x",
		"encoded second slash":  "/%2F/evil.example.org",
		"encoded upper case":    "/%2f/evil.example.org",
		"backslash":             "/\\evil.example.org",
		"encoded backslash":     "/%5Cevil.example.org",
		"backslash later":       "/status\\x",
		"absolute URL":          "https://evil.example.org/",
		"scheme-less host":      "evil.example.org/x",
		"javascript":            "javascript:alert(1)",
		"api path":              "/api/auth/provider/corp/login",
		"api root":              "/api",
		"api upper case":        "/API/x",
		"api prefix":            "/apidocs",
		"encoded api":           "/%61pi/x",
		"newline":               "/status\n/x",
		"encoded newline":       "/status%0d%0aSet-Cookie:x",
		"tab":                   "/status\tx",
		"NUL":                   "/status\x00",
		"DEL":                   "/status\x7f",
		"space":                 "/status demo",
		"too long":              "/" + strings.Repeat("a", 2048),
		"broken escape":         "/status%zz",
		"non-breaking space":    "/status x",
		"line separator":        "/status x",
		"invalid UTF-8":         "/status\xff",
		"only encoded slashes":  "/%2F%2Fevil.example.org",
		"triple slash":          "///evil.example.org",
		"dot segments to host?": "/\\/evil.example.org",
	}
	for name, raw := range drop {
		assert.Equal(t, "", sanitizeReturnTo(raw), "%s: %q must be dropped", name, raw)
	}
}

// exchangeFromRedirect redeems the one-time code of a login redirect via the
// real sso-exchange handler, with the binding cookie of the login start
// (start), and returns its response.
func exchangeFromRedirect(t *testing.T, h *Handler, w, start *httptest.ResponseRecorder) (int, map[string]string) {
	t.Helper()
	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/sso-exchange",
		strings.NewReader(`{"code":"`+loc.Query().Get("code")+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	if ck := bindingCookie(start); ck != nil {
		c.Request.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
	}
	h.SSOExchangeHandler(c)
	body := map[string]string{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestReturnTo_ProviderLoginCarriesTheTargetToTheExchange(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	target := "/status/demo?tab=history"
	w := serveProvider(f.h.ProviderLoginHandler, http.MethodGet,
		"/api/auth/provider/corp/login?return_to="+url.QueryEscape(target), "corp", nil, 0)
	require.Equal(t, http.StatusTemporaryRedirect, w.Code)
	var st models.OIDCLoginState
	state := f.rememberAuthorizeURL(t, w.Header().Get("Location"))
	require.NoError(t, f.db.Where("state = ?", state).First(&st).Error)
	assert.Equal(t, target, st.ReturnTo)

	cb := f.callback("corp", state, "good-code", stateCookie(w))
	code, body := exchangeFromRedirect(t, f.h, cb, w)
	require.Equal(t, http.StatusOK, code)
	assert.NotEmpty(t, body["token"])
	assert.Equal(t, target, body["return_to"])
}

func TestReturnTo_InvalidTargetIsDroppedNotRejected(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	w := serveProvider(f.h.ProviderLoginHandler, http.MethodGet,
		"/api/auth/provider/corp/login?return_to="+url.QueryEscape("//evil.example.org"), "corp", nil, 0)
	require.Equal(t, http.StatusTemporaryRedirect, w.Code)
	state := f.rememberAuthorizeURL(t, w.Header().Get("Location"))
	code, body := exchangeFromRedirect(t, f.h, f.callback("corp", state, "good-code", stateCookie(w)), w)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "", body["return_to"])
	assert.Contains(t, body, "return_to", "the field is always present")
}

func TestReturnTo_PageOIDCLoginAndSlugLogin(t *testing.T) {
	for name, start := range map[string]string{
		"oidc/login": "/api/auth/oidc/login?page=%d&return_to=",
		"sso/login":  "/api/auth/sso/login?slug=oidc-seite&return_to=",
	} {
		t.Run(name, func(t *testing.T) {
			f := newOIDCFixture(t)
			f.linkIdentity(t, f.user.ID)
			handler := f.h.OIDCLoginHandler
			if name == "sso/login" {
				handler = f.h.SSOLoginBySlugHandler
			}
			path := start
			if strings.Contains(path, "%d") {
				path = strings.Replace(path, "%d", "1", 1)
				path = strings.Replace(path, "page=1", f.pageQuery(), 1)
			}
			w := serveOIDC(handler, http.MethodGet, path+url.QueryEscape("/status/oidc-seite"), nil, 0)
			require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
			state := f.rememberAuthorizeURL(t, w.Header().Get("Location"))
			code, body := exchangeFromRedirect(t, f.h, f.callback(state, "good-code", stateCookie(w)), w)
			require.Equal(t, http.StatusOK, code)
			assert.Equal(t, "/status/oidc-seite", body["return_to"])
		})
	}
}

// Linking always returns to the profile (next=profile); a return_to on the
// link request is ignored.
func TestReturnTo_LinkKeepsTheProfileTarget(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	user := models.User{Email: "u@example.org", RoleID: f.viewer.ID}
	require.NoError(t, f.db.Create(&user).Error)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/provider/corp/link?return_to=%2Fstatus%2Fx", nil)
	c.Params = gin.Params{{Key: "id", Value: "corp"}}
	c.Set("userID", user.ID)
	f.h.ProviderLinkHandler(c)
	require.Equal(t, http.StatusOK, rec.Code)
	var link map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &link))
	state := f.rememberAuthorizeURL(t, link["url"])

	w := f.callback("corp", state, "good-code", stateCookie(rec))
	loc, _ := url.Parse(w.Header().Get("Location"))
	assert.Equal(t, "profile", loc.Query().Get("next"))
	_, body := exchangeFromRedirect(t, f.h, w, rec)
	assert.Equal(t, "", body["return_to"])
}

// A one-time code works exactly once, also with a return target.
func TestReturnTo_CodeIsSingleUse(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	start := serveProvider(f.h.ProviderLoginHandler, http.MethodGet, "/api/auth/provider/corp/login", "corp", nil, 0)
	w := f.callback("corp", f.rememberAuthorizeURL(t, start.Header().Get("Location")), "good-code", stateCookie(start))
	code, _ := exchangeFromRedirect(t, f.h, w, start)
	require.Equal(t, http.StatusOK, code)
	code, body := exchangeFromRedirect(t, f.h, w, start)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, "login_code_invalid", body["code"])
}
