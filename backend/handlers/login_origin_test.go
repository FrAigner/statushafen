package handlers

import (
	"encoding/json"
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

// grantPageRole gives the account a role on the page.
func grantPageRole(t *testing.T, db *gorm.DB, userID, pageID uint) {
	t.Helper()
	role := models.Role{Name: "Leser " + strconv.FormatUint(uint64(pageID), 10), Permissions: "incidents:read"}
	require.NoError(t, db.Create(&role).Error)
	require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: userID, StatusPageID: pageID, RoleID: role.ID}).Error)
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
		"cyrillic homograph":    "st\u0430tus.acme.example",
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
		"zero-width joiner":     "status.acme\u200d.example",
		"full-width dot":        "status\uff0eacme.example",
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
	for _, host := range []string{"status.pending.example", "status.example.org", "evil.example", acmeHost + ".evil.example"} {
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

// The Host header is normalised before the lookup (case, port, trailing dot);
// the move always names the domain as stored.
func TestLoginStartOnCustomDomain_HostIsNormalised(t *testing.T) {
	for _, host := range []string{"STATUS.ACME.EXAMPLE", acmeHost + ":443", acmeHost + ":8443", acmeHost + ".", "Status.Acme.Example.:443"} {
		t.Run(host, func(t *testing.T) {
			f := newGlobalOIDCFixture(t)
			customDomainPages(t, f.db)
			w := serveOnHost(f.h.ProviderLoginHandler, host, "/api/auth/provider/corp/login", "corp")
			loc, err := url.Parse(w.Header().Get("Location"))
			require.NoError(t, err)
			assert.Equal(t, "status.example.org", loc.Host)
			assert.Equal(t, acmeHost, loc.Query().Get("return_host"))
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
	member := models.User{Email: "neu@example.org", RoleID: f.viewer.ID}
	require.NoError(t, f.db.Create(&member).Error)
	grantPageRole(t, f.db, member.ID, pages[acmeHost].ID)

	returnTo, claims := callbackClaims(t, f.providerLoginFrom(t, acmeHost), "https://"+acmeHost)

	assert.Equal(t, "/", returnTo)
	assert.Equal(t, pages[acmeHost].ID, claims.SSOPageID)
	assert.Equal(t, acmeHost, claims.CustomDomain)
	assert.False(t, claims.IsGlobalAdmin, "a bootstrap admin gets no global rights on a tenant's domain")
	var user models.User
	require.NoError(t, f.db.Where("email = ?", "neu@example.org").First(&user).Error)
	assert.True(t, user.IsGlobalAdmin, "the account itself stays global admin")
	var audit models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "auth.sso_login").First(&audit).Error)
	assert.Equal(t, "provider=corp page="+strconv.FormatUint(uint64(pages[acmeHost].ID), 10), audit.Details)
}

// Abuse case: a tenant verifies a domain for their own page and sends a victim
// a login link with that domain as return_host. With a running IdP session
// the code would reach the tenant without any click. A session goes to a
// custom domain only for an account with a role on its page; anyone else
// signs in on the main domain (full session) and lands on the page there.
// The auto-join e-mail domains do not count: the tenant sets them.
func TestProviderLoginFromOtherTenantsDomain_WithoutARoleEndsOnTheMainDomain(t *testing.T) {
	for name, bootstrap := range map[string]string{"new account": "", "global admin": "neu@example.org"} {
		t.Run(name, func(t *testing.T) {
			f := newGlobalOIDCFixture(t)
			t.Setenv("FRONTEND_URL", "https://app.example.org")
			t.Setenv("SSO_BOOTSTRAP_ADMIN_EMAILS", bootstrap)
			pages := customDomainPages(t, f.db)
			require.NoError(t, f.db.Model(&models.StatusPage{}).Where("id = ?", pages[acmeHost].ID).
				Updates(map[string]any{"auto_join_email_domains": "example.org"}).Error)

			returnTo, claims := callbackClaims(t, f.providerLoginFrom(t, acmeHost), "https://app.example.org")

			assert.Equal(t, "/status/"+pages[acmeHost].Slug, returnTo, "back to the same page, on the main domain")
			assert.Zero(t, claims.SSOPageID)
			assert.Empty(t, claims.CustomDomain)
			assert.Equal(t, bootstrap != "", claims.IsGlobalAdmin)
		})
	}
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
	grantPageRole(t, f.db, f.user.ID, f.page.ID)
	require.NoError(t, f.db.Model(&f.page).Updates(map[string]any{"custom_domain": acmeHost, "custom_domain_status": "verified"}).Error)

	returnTo, claims := callbackClaims(t, f.pageLoginFrom(t, acmeHost), "https://"+acmeHost)

	assert.Equal(t, "/", returnTo)
	assert.Equal(t, f.page.ID, claims.SSOPageID)
	assert.Equal(t, acmeHost, claims.CustomDomain)
}

func TestPageSSOLoginFromItsCustomDomain_WithoutARoleEndsOnTheMainDomain(t *testing.T) {
	f := newOIDCFixture(t)
	t.Setenv("FRONTEND_URL", "https://app.example.org")
	f.linkIdentity(t, f.user.ID)
	require.NoError(t, f.db.Model(&f.page).Updates(map[string]any{"custom_domain": acmeHost, "custom_domain_status": "verified"}).Error)

	returnTo, claims := callbackClaims(t, f.pageLoginFrom(t, acmeHost), "https://app.example.org")

	assert.Equal(t, "/status/oidc-seite", returnTo)
	assert.Equal(t, f.page.ID, claims.SSOPageID, "a page SSO session stays limited to its page")
	assert.Empty(t, claims.CustomDomain)
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

// exchangeOn redeems a one-time code as the frontend on host does.
func exchangeOn(h *Handler, code, host, origin string) (int, map[string]string) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/sso-exchange", strings.NewReader(`{"code":"`+code+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Host = host
	if origin != "" {
		c.Request.Header.Set("Origin", origin)
	}
	h.SSOExchangeHandler(c)
	body := map[string]string{}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func codeOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	return loc.Query().Get("code")
}

// A code is bound to the origin it was sent to: a code for a custom domain is
// not redeemed on the main domain and the other way round. (This is no
// boundary against a server-side attacker, who sets any header; that is what
// the page limit and the role check are for.)
func TestSSOCode_IsBoundToItsOrigin(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	pages := customDomainPages(t, f.db)
	member := models.User{Email: "neu@example.org", RoleID: f.viewer.ID}
	require.NoError(t, f.db.Create(&member).Error)
	grantPageRole(t, f.db, member.ID, pages[acmeHost].ID)

	code := codeOf(t, f.providerLoginFrom(t, acmeHost))
	status, body := exchangeOn(f.h, code, "status.example.org", "https://status.example.org")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "login_code_invalid", body["code"])
	status, _ = exchangeOn(f.h, code, acmeHost, "https://"+acmeHost)
	assert.Equal(t, http.StatusUnauthorized, status, "a refused code is gone")

	status, _ = exchangeOn(f.h, codeOf(t, f.providerLoginFrom(t, acmeHost)), acmeHost, "https://"+acmeHost)
	assert.Equal(t, http.StatusOK, status)
	status, _ = exchangeOn(f.h, codeOf(t, f.providerLoginFrom(t, acmeHost)), "backend:8080", "https://"+acmeHost)
	assert.Equal(t, http.StatusOK, status, "the browser's Origin counts when the proxy rewrites Host")

	status, _ = exchangeOn(f.h, codeOf(t, f.login(t)), acmeHost, "https://"+acmeHost)
	assert.Equal(t, http.StatusUnauthorized, status, "a main domain code is not redeemed on a custom domain")
	status, _ = exchangeOn(f.h, codeOf(t, f.login(t)), "status.example.org", "")
	assert.Equal(t, http.StatusOK, status)
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

// loginOnHost signs in with a password through the router, as the browser on
// host does (same-origin POST).
func loginOnHost(t *testing.T, e passwordEnv, host, email string) (*middleware.Claims, map[string]any) {
	t.Helper()
	return loginWithOrigin(t, e, host, "", email)
}

// loginWithOrigin is loginOnHost with an Origin header (sent by browsers on
// every POST, also same-origin).
func loginWithOrigin(t *testing.T, e passwordEnv, host, origin, email string) (*middleware.Claims, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/login",
		strings.NewReader(`{"email":"`+email+`","password":"`+passwordEnvSecret+`"}`))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	req.Host = host
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := map[string]any{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	claims := &middleware.Claims{}
	_, err := jwt.ParseWithClaims(body["token"].(string), claims, func(*jwt.Token) (interface{}, error) {
		return []byte("test-jwt-secret-not-for-production"), nil
	})
	require.NoError(t, err)
	return claims, body
}

// The password login on a custom domain is already same-origin; its session
// is limited to the page of that domain like an OIDC session handed there.
func TestPasswordLoginOnCustomDomain_GetsAPageSession(t *testing.T) {
	t.Setenv("PASSWORD_LOGIN_ENABLED", "true")
	e := newPasswordEnv(t)
	pages := customDomainPages(t, e.db)

	claims, body := loginOnHost(t, e, acmeHost, e.admin.Email)
	assert.Equal(t, pages[acmeHost].ID, claims.SSOPageID)
	assert.Equal(t, acmeHost, claims.CustomDomain)
	assert.False(t, claims.IsGlobalAdmin)
	assert.Equal(t, false, body["is_global_admin"], "the frontend must not show global admin features")

	claims, _ = loginOnHost(t, e, acmeHost, e.user.Email)
	assert.Equal(t, pages[acmeHost].ID, claims.SSOPageID)
}

func TestPasswordLoginOnOtherHosts_KeepsTheFullSession(t *testing.T) {
	t.Setenv("PASSWORD_LOGIN_ENABLED", "true")
	e := newPasswordEnv(t)
	customDomainPages(t, e.db)
	for _, host := range []string{"status.example.org", "status.pending.example", acmeHost + ".evil.example", "evil.example"} {
		claims, body := loginOnHost(t, e, host, e.admin.Email)
		assert.Zero(t, claims.SSOPageID, host)
		assert.True(t, claims.IsGlobalAdmin, host)
		assert.Equal(t, true, body["is_global_admin"], host)
	}
	claims, _ := loginWithOrigin(t, e, "status.example.org", "https://status.example.org", e.admin.Email)
	assert.True(t, claims.IsGlobalAdmin)
	claims, _ = loginWithOrigin(t, e, "status.example.org", "null", e.admin.Email)
	assert.True(t, claims.IsGlobalAdmin, "an opaque origin is no custom domain")
}

// Abuse case: a full session (even global admin) must not end up in the
// storage of a tenant's origin because Host differs in case, port or trailing
// dot, or because the proxy rewrote it. Host and the browser's Origin are
// both normalised and either one on a custom domain limits the session.
func TestPasswordLoginOnCustomDomain_HostVariantsAndRewrittenHost(t *testing.T) {
	t.Setenv("PASSWORD_LOGIN_ENABLED", "true")
	e := newPasswordEnv(t)
	pages := customDomainPages(t, e.db)
	for _, tc := range []struct{ host, origin string }{
		{"STATUS.ACME.EXAMPLE", ""},
		{acmeHost + ":443", ""},
		{acmeHost + ":8443", ""},
		{acmeHost + ".", ""},
		{"backend:8080", "https://" + acmeHost},
		{"backend:8080", "https://STATUS.ACME.EXAMPLE:443"},
		{"status.example.org", "https://" + acmeHost},
		{acmeHost, "https://status.example.org"},
	} {
		claims, body := loginWithOrigin(t, e, tc.host, tc.origin, e.admin.Email)
		assert.Equal(t, pages[acmeHost].ID, claims.SSOPageID, "%+v", tc)
		assert.False(t, claims.IsGlobalAdmin, "%+v", tc)
		assert.Equal(t, false, body["is_global_admin"], "%+v", tc)
	}
}
