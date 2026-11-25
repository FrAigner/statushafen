package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A login started on a verified custom domain returns to that domain. The
// host is attacker input (query parameter return_host, or the Host header of
// the login start): only the exact hostname of a verified or active custom
// domain from the database counts, everything else falls back to FRONTEND_URL.

const (
	acmeHost  = "status.acme.example"
	otherHost = "status.other.example"
)

// customDomainPages creates one page per custom domain state; the returned
// map is keyed by domain.
func customDomainPages(t *testing.T, db *gorm.DB) map[string]models.StatusPage {
	t.Helper()
	pages := map[string]models.StatusPage{}
	for i, d := range []struct{ domain, status string }{
		{acmeHost, "verified"},
		{otherHost, "active"},
		{"status.pending.example", "pending"},
		{"status.failed.example", "check_failed"},
		{"status.deleted.example", "verified"},
	} {
		p := models.StatusPage{Name: "Domain " + d.domain, Slug: "domain-" + strconv.Itoa(i), CustomDomain: d.domain, CustomDomainStatus: d.status}
		require.NoError(t, db.Create(&p).Error)
		pages[d.domain] = p
	}
	require.NoError(t, db.Delete(&models.StatusPage{}, pages["status.deleted.example"].ID).Error)
	return pages
}

func TestAcceptReturnHost_OnlyExactVerifiedDomains(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	t.Setenv("FRONTEND_URL", "https://app.example.org")
	pages := customDomainPages(t, f.db)

	// Global logins (no page): every verified or active domain.
	assert.Equal(t, acmeHost, f.h.acceptReturnHost(acmeHost, 0).host)
	assert.Equal(t, pages[acmeHost].ID, f.h.acceptReturnHost(acmeHost, 0).pageID)
	assert.Equal(t, otherHost, f.h.acceptReturnHost(otherHost, 0).host)
	assert.Equal(t, pages[otherHost].ID, f.h.acceptReturnHost(otherHost, 0).pageID)

	// Page SSO: only the domain of that same page (no other tenant's domain).
	assert.Equal(t, acmeHost, f.h.acceptReturnHost(acmeHost, pages[acmeHost].ID).host)
	assert.Equal(t, loginOrigin{}, f.h.acceptReturnHost(otherHost, pages[acmeHost].ID), "other tenant's domain")
	assert.Equal(t, loginOrigin{}, f.h.acceptReturnHost(acmeHost, pages[otherHost].ID), "other tenant's domain")

	rejected := map[string]string{
		"empty":                 "",
		"pending":               "status.pending.example",
		"check failed":          "status.failed.example",
		"deleted page":          "status.deleted.example",
		"unknown":               "status.unknown.example",
		"main domain":           "app.example.org",
		"suffix look-alike":     acmeHost + ".evil.example",
		"prefix look-alike":     "evil-" + acmeHost,
		"glued prefix":          "x" + acmeHost,
		"parent domain":         "acme.example",
		"subdomain":             "a." + acmeHost,
		"trailing dot":          acmeHost + ".",
		"leading dot":           "." + acmeHost,
		"upper case":            "STATUS.ACME.EXAMPLE",
		"leading space":         " " + acmeHost,
		"trailing space":        acmeHost + " ",
		"cyrillic homograph":    "stаtus.acme.example",
		"punycode homograph":    "xn--sttus-2we.acme.example",
		"https scheme":          "https://" + acmeHost,
		"http scheme":           "http://" + acmeHost,
		"protocol-relative":     "//" + acmeHost,
		"default port":          acmeHost + ":443",
		"other port":            acmeHost + ":8443",
		"empty port":            acmeHost + ":",
		"path":                  acmeHost + "/x",
		"trailing slash":        acmeHost + "/",
		"userinfo before":       "user@" + acmeHost,
		"userinfo trick":        acmeHost + "@evil.example",
		"fragment trick":        "evil.example#" + acmeHost,
		"query trick":           "evil.example?" + acmeHost,
		"backslash":             acmeHost + "\\evil.example",
		"encoded slash":         acmeHost + "%2Fevil.example",
		"CRLF":                  acmeHost + "\r\nX-Injected: 1",
		"NUL":                   acmeHost + "\x00",
		"SQL wildcard":          "status.%.example",
		"SQL single char":       "status.acm_.example",
		"SQL injection":         "' OR '1'='1",
		"IPv4":                  "127.0.0.1",
		"IPv6":                  "[::1]",
		"comma list":            acmeHost + "," + otherHost,
		"two hosts with space":  acmeHost + " " + otherHost,
		"wildcard":              "*.acme.example",
		"javascript scheme":     "javascript:alert(1)",
		"overlong label":        strings.Repeat("a", 64) + ".example",
		"invalid UTF-8":         "status.acme.example\xff",
		"zero-width joiner":     "status.acme‍.example",
		"full-width dot":        "status．acme.example",
		"verified, other case":  "Status.Acme.Example",
		"tab inside":            "status.acme\t.example",
		"encoded dot":           "status%2Eacme.example",
		"double encoded":        "status.acme.example%252F",
		"scheme without slash":  "https:" + acmeHost,
		"data scheme":           "data:text/html,x",
		"just a dot":            ".",
		"numeric TLD":           "status.acme.123",
		"main domain with port": "app.example.org:443",
	}
	for name, raw := range rejected {
		assert.Equal(t, loginOrigin{}, f.h.acceptReturnHost(raw, 0), "%s: %q must be rejected", name, raw)
	}
}

// serveOnHost calls a login handler as the browser would on host.
func serveOnHost(handler gin.HandlerFunc, host, target, id string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	c.Request.Host = host
	if id != "" {
		c.Params = gin.Params{{Key: "id", Value: id}}
	}
	handler(c)
	return w
}

// The state cookie must reach the callback on the BASE_URL host, so a login
// started on a custom domain (relative URL in the frontend) first moves to
// the BASE_URL host and names the custom domain as return_host.
func TestLoginStartOnCustomDomain_MovesToTheBaseHost(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	customDomainPages(t, f.db)

	w := serveOnHost(f.h.ProviderLoginHandler, acmeHost, "/api/auth/provider/corp/login?return_to=%2F", "corp")

	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "https://status.example.org/api/auth/provider/corp/login", loc.Scheme+"://"+loc.Host+loc.Path)
	assert.Equal(t, url.Values{"return_to": {"/"}, "return_host": {acmeHost}}, loc.Query())
	assert.Nil(t, stateCookie(w), "no state on the custom domain: the cookie would never reach the callback")
	var n int64
	f.db.Model(&models.OIDCLoginState{}).Count(&n)
	assert.Zero(t, n)
}

// The Host header decides on the custom domain; a return_host in the query
// cannot redirect the login to another tenant's domain.
func TestLoginStartOnCustomDomain_HostWinsOverTheQuery(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	customDomainPages(t, f.db)

	w := serveOnHost(f.h.ProviderLoginHandler, acmeHost, "/api/auth/provider/corp/login?return_host="+otherHost, "corp")

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, []string{acmeHost}, loc.Query()["return_host"])
}

// An unverified or unknown host never triggers the move (no redirect from an
// arbitrary host to the BASE_URL either); the login starts as before.
func TestLoginStartOnUnverifiedHost_StartsNormally(t *testing.T) {
	for _, host := range []string{"status.pending.example", "status.example.org", "evil.example", acmeHost + ":8443"} {
		t.Run(host, func(t *testing.T) {
			f := newGlobalOIDCFixture(t)
			customDomainPages(t, f.db)
			w := serveOnHost(f.h.ProviderLoginHandler, host, "/api/auth/provider/corp/login", "corp")
			require.Equal(t, http.StatusTemporaryRedirect, w.Code)
			assert.Contains(t, w.Header().Get("Location"), f.idp.srv.URL+"/authorize")
			assert.NotNil(t, stateCookie(w))
		})
	}
}

func TestLoginStartOnCustomDomain_PageSSOAndSlugMoveToo(t *testing.T) {
	f := newOIDCFixture(t)
	t.Setenv("BASE_URL", "https://status.example.org")
	require.NoError(t, f.db.Model(&f.page).Updates(map[string]any{"custom_domain": acmeHost, "custom_domain_status": "active"}).Error)

	for name, start := range map[string]struct {
		handler gin.HandlerFunc
		target  string
	}{
		"oidc/login": {f.h.OIDCLoginHandler, "/api/auth/oidc/login?" + f.pageQuery()},
		"sso/login":  {f.h.SSOLoginBySlugHandler, "/api/auth/sso/login?slug=oidc-seite"},
	} {
		t.Run(name, func(t *testing.T) {
			w := serveOnHost(start.handler, acmeHost, start.target, "")
			require.Equal(t, http.StatusTemporaryRedirect, w.Code)
			loc, err := url.Parse(w.Header().Get("Location"))
			require.NoError(t, err)
			path, _, _ := strings.Cut(start.target, "?")
			assert.Equal(t, "https://status.example.org"+path, loc.Scheme+"://"+loc.Host+loc.Path)
			assert.Equal(t, acmeHost, loc.Query().Get("return_host"))
			assert.Nil(t, stateCookie(w))
		})
	}
}

// providerLoginFrom runs a full global provider login started with the given
// return_host on the BASE_URL host.
func (f *globalOIDCFixture) providerLoginFrom(t *testing.T, returnHost string) *httptest.ResponseRecorder {
	t.Helper()
	w := serveProvider(f.h.ProviderLoginHandler, http.MethodGet,
		"/api/auth/provider/corp/login?return_to=%2F&return_host="+url.QueryEscape(returnHost), "corp", nil, 0)
	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	state := f.rememberAuthorizeURL(t, w.Header().Get("Location"))
	return f.callback("corp", state, "good-code", stateCookie(w))
}

// callbackClaims redeems the code of a callback redirect that must point at
// base and returns the claims of the session.
func callbackClaims(t *testing.T, w *httptest.ResponseRecorder, base string) (string, *middleware.Claims) {
	t.Helper()
	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, base+"/auth/sso-callback", loc.Scheme+"://"+loc.Host+loc.Path)
	entry, ok := consumeSSOCode(loc.Query().Get("code"))
	require.True(t, ok)
	claims := &middleware.Claims{}
	_, err = jwt.ParseWithClaims(entry.token, claims, func(*jwt.Token) (interface{}, error) {
		return []byte("test-jwt-secret-not-for-production"), nil
	})
	require.NoError(t, err)
	return entry.returnTo, claims
}

// A global provider login from a custom domain ends there, with a session
// limited to the page of that domain: whoever controls the domain's DNS
// (the tenant) must never receive a session with rights on other pages or
// global admin rights.
func TestProviderLoginFromCustomDomain_ReturnsThereWithAPageSession(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	t.Setenv("SSO_BOOTSTRAP_ADMIN_EMAILS", "neu@example.org")
	pages := customDomainPages(t, f.db)

	returnTo, claims := callbackClaims(t, f.providerLoginFrom(t, acmeHost), "https://"+acmeHost)

	assert.Equal(t, "/", returnTo)
	assert.Equal(t, pages[acmeHost].ID, claims.SSOPageID)
	assert.False(t, claims.IsGlobalAdmin, "a bootstrap admin gets no global rights on a tenant's domain")
	var user models.User
	require.NoError(t, f.db.Where("email = ?", "neu@example.org").First(&user).Error)
	assert.True(t, user.IsGlobalAdmin, "the account itself stays global admin")
}

func TestProviderLoginFromRejectedHost_EndsOnTheMainDomain(t *testing.T) {
	for _, host := range []string{"status.pending.example", "evil.example", acmeHost + ".evil.example", "https://" + acmeHost} {
		t.Run(host, func(t *testing.T) {
			f := newGlobalOIDCFixture(t)
			t.Setenv("FRONTEND_URL", "https://app.example.org")
			customDomainPages(t, f.db)
			returnTo, claims := callbackClaims(t, f.providerLoginFrom(t, host), "https://app.example.org")
			assert.Equal(t, "/", returnTo)
			assert.Zero(t, claims.SSOPageID)
		})
	}
}

// The domain is checked again on the callback: removed (or no longer
// verified) during the login, the login ends on the main domain.
func TestProviderLoginFromCustomDomain_DomainRemovedDuringLogin(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	t.Setenv("FRONTEND_URL", "https://app.example.org")
	pages := customDomainPages(t, f.db)
	w := serveProvider(f.h.ProviderLoginHandler, http.MethodGet,
		"/api/auth/provider/corp/login?return_host="+acmeHost, "corp", nil, 0)
	state := f.rememberAuthorizeURL(t, w.Header().Get("Location"))
	var st models.OIDCLoginState
	require.NoError(t, f.db.Where("state = ?", state).First(&st).Error)
	assert.Equal(t, acmeHost, st.ReturnHost)
	require.NoError(t, f.db.Model(&models.StatusPage{}).Where("id = ?", pages[acmeHost].ID).
		Updates(map[string]any{"custom_domain_status": "pending"}).Error)

	_, claims := callbackClaims(t, f.callback("corp", state, "good-code", stateCookie(w)), "https://app.example.org")
	assert.Zero(t, claims.SSOPageID)
}

// Linking always ends in the profile on the main domain.
func TestProviderLinkIgnoresReturnHost(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	customDomainPages(t, f.db)
	user := models.User{Email: "u@example.org", RoleID: f.viewer.ID}
	require.NoError(t, f.db.Create(&user).Error)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/provider/corp/link?return_host="+acmeHost, nil)
	c.Params = gin.Params{{Key: "id", Value: "corp"}}
	c.Set("userID", user.ID)
	f.h.ProviderLinkHandler(c)
	require.Equal(t, http.StatusOK, rec.Code)
	var st models.OIDCLoginState
	require.NoError(t, f.db.Where("link_user_id = ?", user.ID).First(&st).Error)
	assert.Empty(t, st.ReturnHost)
}

// Errors after the state is known go to the login page of the custom domain.
func TestProviderLoginFromCustomDomain_ErrorGoesBackThere(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	customDomainPages(t, f.db)
	w := serveProvider(f.h.ProviderLoginHandler, http.MethodGet,
		"/api/auth/provider/corp/login?return_host="+acmeHost, "corp", nil, 0)
	state := f.rememberAuthorizeURL(t, w.Header().Get("Location"))

	cb := f.callback("corp", state, "", stateCookie(w)) // provider reported an error

	require.Equal(t, http.StatusTemporaryRedirect, cb.Code)
	assert.Equal(t, "https://"+acmeHost+"/login?oidc_error=provider_denied", cb.Header().Get("Location"))
}

// Errors of the login start (before any state) go back to the custom domain too.
func TestProviderLoginStartError_GoesBackToTheCustomDomain(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	customDomainPages(t, f.db)
	w := serveProvider(f.h.ProviderLoginHandler, http.MethodGet,
		"/api/auth/provider/nope/login?return_host="+acmeHost, "nope", nil, 0)
	assert.Equal(t, "https://"+acmeHost+"/login?oidc_error=provider_unknown", w.Header().Get("Location"))

	w = serveProvider(f.h.ProviderLoginHandler, http.MethodGet,
		"/api/auth/provider/nope/login?return_host=evil.example", "nope", nil, 0)
	assert.Equal(t, getFrontendURL()+"/login?oidc_error=provider_unknown", w.Header().Get("Location"))
}

// pageLoginFrom runs a full page SSO login with the given return_host.
func (f *oidcFixture) pageLoginFrom(t *testing.T, returnHost string) *httptest.ResponseRecorder {
	t.Helper()
	w := serveOIDC(f.h.OIDCLoginHandler, http.MethodGet,
		"/api/auth/oidc/login?"+f.pageQuery()+"&return_to=%2F&return_host="+url.QueryEscape(returnHost), nil, 0)
	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	state := f.rememberAuthorizeURL(t, w.Header().Get("Location"))
	return f.callback(state, "good-code", stateCookie(w))
}

func TestPageSSOLoginFromItsCustomDomain_ReturnsThere(t *testing.T) {
	f := newOIDCFixture(t)
	f.linkIdentity(t, f.user.ID)
	require.NoError(t, f.db.Model(&f.page).Updates(map[string]any{"custom_domain": acmeHost, "custom_domain_status": "verified"}).Error)

	returnTo, claims := callbackClaims(t, f.pageLoginFrom(t, acmeHost), "https://"+acmeHost)

	assert.Equal(t, "/", returnTo)
	assert.Equal(t, f.page.ID, claims.SSOPageID)
}

// The page SSO of one tenant can never hand its session to the domain of
// another tenant, also when the domain moves during the login.
func TestPageSSOLoginWithOtherTenantsDomain_EndsOnTheMainDomain(t *testing.T) {
	f := newOIDCFixture(t)
	t.Setenv("FRONTEND_URL", "https://app.example.org")
	f.linkIdentity(t, f.user.ID)
	other := models.StatusPage{Name: "Andere", Slug: "andere", CustomDomain: otherHost, CustomDomainStatus: "active"}
	require.NoError(t, f.db.Create(&other).Error)

	_, claims := callbackClaims(t, f.pageLoginFrom(t, otherHost), "https://app.example.org")
	assert.Equal(t, f.page.ID, claims.SSOPageID)

	// Accepted at the start (own domain), moved to the other page meanwhile.
	require.NoError(t, f.db.Model(&f.page).Updates(map[string]any{"custom_domain": acmeHost, "custom_domain_status": "verified"}).Error)
	w := serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?"+f.pageQuery()+"&return_host="+acmeHost, nil, 0)
	state := f.rememberAuthorizeURL(t, w.Header().Get("Location"))
	require.NoError(t, f.db.Model(&f.page).Updates(map[string]any{"custom_domain": "", "custom_domain_status": ""}).Error)
	require.NoError(t, f.db.Model(&other).Updates(map[string]any{"custom_domain": acmeHost}).Error)
	_, claims = callbackClaims(t, f.callback(state, "good-code", stateCookie(w)), "https://app.example.org")
	assert.Equal(t, f.page.ID, claims.SSOPageID)
}

// Even if the BASE_URL host were registered as a verified custom domain, the
// login start must not redirect to itself in a loop; a broken BASE_URL never
// moves the login either.
func TestLoginStartOnBaseHost_NeverMovesInALoop(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	require.NoError(t, f.db.Create(&models.StatusPage{Name: "Base", Slug: "base", CustomDomain: "status.example.org", CustomDomainStatus: "active"}).Error)
	w := serveOnHost(f.h.ProviderLoginHandler, "status.example.org", "/api/auth/provider/corp/login", "corp")
	assert.Contains(t, w.Header().Get("Location"), f.idp.srv.URL+"/authorize")

	customDomainPages(t, f.db)
	t.Setenv("BASE_URL", "http://[::1")
	w = serveOnHost(f.h.ProviderLoginHandler, acmeHost, "/api/auth/provider/corp/login", "corp")
	assert.NotContains(t, w.Header().Get("Location"), "return_host")
}
