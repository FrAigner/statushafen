package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Login CSRF: an attacker finishes a login in their own browser and sends the
// victim the frontend link with the attacker's one-time code. Redeeming it
// would sign the victim in to the attacker's account. The code is therefore
// bound to the browser that started the login: the start sets a random nonce
// in an HttpOnly cookie (path of the exchange), the state and then the code
// keep only its hash, and the exchange needs the matching cookie.

func bindingCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, ck := range w.Result().Cookies() {
		if ck.Name == loginBindingCookie {
			return ck
		}
	}
	return nil
}

// redeemOn redeems code on host (Origin of the frontend there) with the
// given binding cookie (nil: none).
func redeemOn(h *Handler, host, code string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := testutils.SetupRouter()
	r.POST("/api/auth/sso-exchange", h.SSOExchangeHandler)
	body, _ := json.Marshal(map[string]string{"code": code})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/sso-exchange", bytes.NewReader(body))
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func boundCodeOf(t *testing.T, w *httptest.ResponseRecorder, base string) string {
	t.Helper()
	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, base+"/auth/sso-callback", loc.Scheme+"://"+loc.Host+loc.Path)
	return loc.Query().Get("code")
}

func assertBindingCookie(t *testing.T, ck *http.Cookie) {
	t.Helper()
	require.NotNil(t, ck, "the login start sets the binding cookie")
	assert.NotEmpty(t, ck.Value)
	assert.Equal(t, "/api/auth/sso-exchange", ck.Path)
	assert.True(t, ck.HttpOnly)
	assert.True(t, ck.Secure)
	assert.Equal(t, http.SameSiteLaxMode, ck.SameSite)
	assert.Empty(t, ck.Domain, "host-only cookie")
}

func TestSSOExchange_RequiresTheBindingCookie(t *testing.T) {
	h := NewHandler(testutils.SetupTestDB())
	nonce := rand.Text()
	good := &http.Cookie{Name: loginBindingCookie, Value: nonce}

	for name, tc := range map[string]struct {
		binding string
		cookie  *http.Cookie
	}{
		"no cookie":             {binding: hashLoginBinding(nonce)},
		"other cookie":          {binding: hashLoginBinding(nonce), cookie: &http.Cookie{Name: loginBindingCookie, Value: rand.Text()}},
		"hash as cookie":        {binding: hashLoginBinding(nonce), cookie: &http.Cookie{Name: loginBindingCookie, Value: hashLoginBinding(nonce)}},
		"unbound code":          {binding: "", cookie: good},
		"unbound, cookie empty": {binding: "", cookie: &http.Cookie{Name: loginBindingCookie, Value: ""}},
	} {
		t.Run(name, func(t *testing.T) {
			code := storeSSOCode("tok", "", "", tc.binding)
			w := redeemOn(h, "localhost", code, tc.cookie)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Body.String(), `"code":"login_code_invalid"`)
			// The code is spent: the right cookie does not help afterwards.
			assert.Equal(t, http.StatusUnauthorized, redeemOn(h, "localhost", code, good).Code)
		})
	}

	t.Run("matching cookie", func(t *testing.T) {
		code := storeSSOCode("tok", "/x", "", hashLoginBinding(nonce))
		w := redeemOn(h, "localhost", code, good)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.JSONEq(t, `{"token":"tok","return_to":"/x"}`, w.Body.String())
		ck := bindingCookie(w)
		require.NotNil(t, ck, "the exchange clears the cookie")
		assert.Negative(t, ck.MaxAge)
		assert.Equal(t, "/api/auth/sso-exchange", ck.Path)
	})
}

// Every login start binds: global provider, page SSO and the slug entry.
func TestLoginStart_BindsTheCodeToTheBrowser(t *testing.T) {
	t.Run("global provider", func(t *testing.T) {
		f := newGlobalOIDCFixture(t)
		start := serveProvider(f.h.ProviderLoginHandler, http.MethodGet, "/api/auth/provider/corp/login", "corp", nil, 0)
		assertBindingCookie(t, bindingCookie(start))
		state := f.rememberAuthorizeURL(t, start.Header().Get("Location"))
		code := boundCodeOf(t, f.callback("corp", state, "good-code", stateCookie(start)), getFrontendURL())
		assert.Equal(t, http.StatusUnauthorized, redeemOn(f.h, "localhost", code, nil).Code, "victim browser without the cookie")

		start = serveProvider(f.h.ProviderLoginHandler, http.MethodGet, "/api/auth/provider/corp/login", "corp", nil, 0)
		state = f.rememberAuthorizeURL(t, start.Header().Get("Location"))
		code = boundCodeOf(t, f.callback("corp", state, "good-code", stateCookie(start)), getFrontendURL())
		assert.Equal(t, http.StatusOK, redeemOn(f.h, "localhost", code, bindingCookie(start)).Code)
	})

	for name, target := range map[string]func(f *oidcFixture) string{
		"page SSO":   func(f *oidcFixture) string { return "/api/auth/oidc/login?" + f.pageQuery() },
		"slug entry": func(*oidcFixture) string { return "/api/auth/sso/login?slug=oidc-seite" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newOIDCFixture(t)
			f.linkIdentity(t, f.user.ID)
			handler := f.h.OIDCLoginHandler
			if name == "slug entry" {
				handler = f.h.SSOLoginBySlugHandler
			}
			run := func() (string, *http.Cookie) {
				start := serveOIDC(handler, http.MethodGet, target(f), nil, 0)
				assertBindingCookie(t, bindingCookie(start))
				state := f.rememberAuthorizeURL(t, start.Header().Get("Location"))
				return boundCodeOf(t, f.callback(state, "good-code", stateCookie(start)), getFrontendURL()), bindingCookie(start)
			}
			code, _ := run()
			assert.Equal(t, http.StatusUnauthorized, redeemOn(f.h, "localhost", code, nil).Code)
			code, ck := run()
			assert.Equal(t, http.StatusOK, redeemOn(f.h, "localhost", code, ck).Code)
		})
	}
}

// Linking binds as well: a link code also hands out a session.
func TestLinkStart_BindsTheCodeToTheBrowser(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	u := f.member(t)
	w := serveProvider(f.h.ProviderLinkHandler, http.MethodPost, "/api/auth/provider/corp/link", "corp", nil, u.ID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertBindingCookie(t, bindingCookie(w))
}

// A login started on a custom domain sets a second binding cookie there
// before it moves to the BASE_URL host: the code is redeemed on the domain
// (consenting member) or on FRONTEND_URL (anyone else), so each place needs
// its own cookie.
func TestLoginStartOnCustomDomain_BindsBothHosts(t *testing.T) {
	for name, member := range map[string]bool{"member": true, "no member": false} {
		t.Run(name, func(t *testing.T) {
			f := newGlobalOIDCFixture(t)
			t.Setenv("FRONTEND_URL", "https://app.example.org")
			pages := customDomainPages(t, f.db)
			if member {
				u := f.member(t)
				grantPageRole(t, f.db, u.ID, pages[acmeHost].ID)
			}

			onDomain := serveOnHost(f.h.ProviderLoginHandler, acmeHost, "/api/auth/provider/corp/login?return_to=%2F", "corp")
			domainCookie := bindingCookie(onDomain)
			assertBindingCookie(t, domainCookie)
			moved, err := url.Parse(onDomain.Header().Get("Location"))
			require.NoError(t, err)
			assert.Equal(t, hashLoginBinding(domainCookie.Value), moved.Query().Get(loginBindingParam), "only the hash travels in the URL")
			assert.NotContains(t, moved.String(), domainCookie.Value)

			start := serveProvider(f.h.ProviderLoginHandler, http.MethodGet, moved.RequestURI(), "corp", nil, 0)
			baseCookie := bindingCookie(start)
			assertBindingCookie(t, baseCookie)
			state := f.rememberAuthorizeURL(t, start.Header().Get("Location"))
			cb := f.callback("corp", state, "good-code", stateCookie(start))

			if member {
				code := boundCodeOf(t, cb, "https://"+acmeHost)
				assert.Equal(t, http.StatusUnauthorized, redeemOn(f.h, acmeHost, code, baseCookie).Code, "the base cookie does not redeem on the domain")
				return
			}
			code := boundCodeOf(t, cb, "https://app.example.org")
			assert.Equal(t, http.StatusOK, redeemOn(f.h, "app.example.org", code, baseCookie).Code)
		})
	}
}

// A consenting member redeems on the custom domain with the domain's cookie.
func TestLoginStartOnCustomDomain_RedeemsWithTheDomainCookie(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	t.Setenv("FRONTEND_URL", "https://app.example.org")
	pages := customDomainPages(t, f.db)
	u := f.member(t)
	grantPageRole(t, f.db, u.ID, pages[acmeHost].ID)

	onDomain := serveOnHost(f.h.ProviderLoginHandler, acmeHost, "/api/auth/provider/corp/login", "corp")
	moved, err := url.Parse(onDomain.Header().Get("Location"))
	require.NoError(t, err)
	start := serveProvider(f.h.ProviderLoginHandler, http.MethodGet, moved.RequestURI(), "corp", nil, 0)
	state := f.rememberAuthorizeURL(t, start.Header().Get("Location"))
	code := boundCodeOf(t, f.callback("corp", state, "good-code", stateCookie(start)), "https://"+acmeHost)
	assert.Equal(t, http.StatusOK, redeemOn(f.h, acmeHost, code, bindingCookie(onDomain)).Code)
}

// A login start on the BASE_URL host that names a return_host without a
// valid binding of that domain (a crafted link) ends on FRONTEND_URL: a code
// for the domain could never be redeemed.
func TestLoginStart_ReturnHostNeedsADomainBinding(t *testing.T) {
	for name, binding := range map[string]string{"missing": "", "malformed": "not-a-hash", "too short": strings.Repeat("a", 63)} {
		t.Run(name, func(t *testing.T) {
			f := newGlobalOIDCFixture(t)
			t.Setenv("FRONTEND_URL", "https://app.example.org")
			pages := customDomainPages(t, f.db)
			u := f.member(t)
			grantPageRole(t, f.db, u.ID, pages[acmeHost].ID)
			q := url.Values{"return_host": {acmeHost}}
			if binding != "" {
				q.Set(loginBindingParam, binding)
			}
			start := serveProvider(f.h.ProviderLoginHandler, http.MethodGet, "/api/auth/provider/corp/login?"+q.Encode(), "corp", nil, 0)
			state := f.rememberAuthorizeURL(t, start.Header().Get("Location"))
			code := boundCodeOf(t, f.callback("corp", state, "good-code", stateCookie(start)), "https://app.example.org")
			assert.Equal(t, http.StatusOK, redeemOn(f.h, "app.example.org", code, bindingCookie(start)).Code)
		})
	}
}

// member creates the account the mock IdP signs in (e-mail of the claims).
func (f *globalOIDCFixture) member(t *testing.T) models.User {
	t.Helper()
	u := models.User{Email: "neu@example.org", RoleID: f.viewer.ID}
	require.NoError(t, f.db.Create(&u).Error)
	return u
}
