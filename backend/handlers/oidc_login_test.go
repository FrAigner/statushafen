package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// OIDC-Login je Statusseite gegen einen echten (Mock-)Anbieter.

type oidcFixture struct {
	h    *Handler
	db   *gorm.DB
	idp  *testIdP
	page models.StatusPage
	user models.User
}

func newOIDCFixture(t *testing.T) *oidcFixture {
	t.Helper()
	return newOIDCFixtureWith(t, newTestIdP(t))
}

func newOIDCFixtureWith(t *testing.T, idp *testIdP) *oidcFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := testutils.SetupTestDB()
	role := models.Role{Name: "Viewer", Permissions: "incidents:read"}
	require.NoError(t, db.Create(&role).Error)
	page := models.StatusPage{Name: "OIDC-Seite", Slug: "oidc-seite"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&models.StatusPageSSOConfig{
		StatusPageID:      page.ID,
		OIDCEnabled:       true,
		OIDCDiscoveryURL:  idp.srv.URL + "/.well-known/openid-configuration",
		OIDCClientID:      testIdPClientID,
		OIDCClientSecret:  "geheim",
		OIDCScopes:        "openid email profile",
		OIDCAttributeName: "name",
	}).Error)
	user := models.User{Email: "erika@example.org", RoleID: role.ID}
	require.NoError(t, db.Create(&user).Error)
	return &oidcFixture{h: NewHandler(db), db: db, idp: idp, page: page, user: user}
}

func (f *oidcFixture) pageQuery() string {
	return "page=" + strconv.FormatUint(uint64(f.page.ID), 10)
}

func (f *oidcFixture) linkIdentity(t *testing.T, userID uint) {
	t.Helper()
	require.NoError(t, f.db.Create(&models.UserSSOIdentity{
		UserID: userID, StatusPageID: f.page.ID, Issuer: f.idp.srv.URL, Subject: "idp-user-1",
	}).Error)
}

func serveOIDC(handler gin.HandlerFunc, method, target string, cookie *http.Cookie, userID uint) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	if cookie != nil {
		c.Request.AddCookie(cookie)
	}
	if userID != 0 {
		c.Set("userID", userID)
	}
	handler(c)
	return w
}

func stateCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, ck := range w.Result().Cookies() {
		if ck.Name == oidcStateCookie {
			return ck
		}
	}
	return nil
}

// startLogin ruft den Login-Start auf, merkt dem Mock-IdP Nonce und
// PKCE-Challenge aus der Authorize-URL und liefert State und Cookie.
func (f *oidcFixture) startLogin(t *testing.T) (string, *http.Cookie) {
	t.Helper()
	w := serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?"+f.pageQuery(), nil, 0)
	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	return f.rememberAuthorizeURL(t, w.Header().Get("Location")), stateCookie(w)
}

func (f *oidcFixture) rememberAuthorizeURL(t *testing.T, location string) string {
	t.Helper()
	u, err := url.Parse(location)
	require.NoError(t, err)
	q := u.Query()
	f.idp.mu.Lock()
	f.idp.nonce = q.Get("nonce")
	f.idp.challenge = q.Get("code_challenge")
	f.idp.mu.Unlock()
	return q.Get("state")
}

func (f *oidcFixture) callback(state, code string, cookie *http.Cookie) *httptest.ResponseRecorder {
	q := url.Values{}
	q.Set("page", strconv.FormatUint(uint64(f.page.ID), 10))
	if state != "" {
		q.Set("state", state)
	}
	if code != "" {
		q.Set("code", code)
	}
	return serveOIDC(f.h.OIDCCallbackHandler, http.MethodGet, "/api/auth/oidc/callback?"+q.Encode(), cookie, 0)
}

func errorOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body["error"]
}

// assertOIDCError: Fehler im Browser-Ablauf enden nicht mehr als
// JSON, sondern als Redirect auf eine feste Frontend-Seite mit Fehlercode.
func assertOIDCError(t *testing.T, w *httptest.ResponseRecorder, path, code string) {
	t.Helper()
	assertSSOError(t, w, http.StatusTemporaryRedirect, path, code)
}

// assertSSOError prüft den Fehler-Redirect mit dem erwarteten
// Status (307 bei GET, 303 nach dem POST an den SAML-ACS).
func assertSSOError(t *testing.T, w *httptest.ResponseRecorder, status int, path, code string) {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, getFrontendURL()+path, loc.Scheme+"://"+loc.Host+loc.Path)
	param := "oidc_error"
	if path == "/admin/sso" {
		param = "oidc_test"
	}
	assert.Equal(t, url.Values{param: {code}}, loc.Query())
}

// jwtFromRedirect löst den Einmal-Code aus dem Frontend-Redirect ein und
// liefert die Claims des ausgestellten Login-JWT.
func jwtFromRedirect(t *testing.T, w *httptest.ResponseRecorder) *middleware.Claims {
	t.Helper()
	return jwtFromRedirectStatus(t, w, http.StatusTemporaryRedirect)
}

func jwtFromRedirectStatus(t *testing.T, w *httptest.ResponseRecorder, status int) *middleware.Claims {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, getFrontendURL()+"/auth/sso-callback", loc.Scheme+"://"+loc.Host+loc.Path)
	token, ok := consumeSSOCode(loc.Query().Get("code"))
	require.True(t, ok)
	claims := &middleware.Claims{}
	_, err = jwt.ParseWithClaims(token, claims, func(*jwt.Token) (interface{}, error) {
		return []byte("test-jwt-secret-not-for-production"), nil
	})
	require.NoError(t, err)
	return claims
}

func TestOIDCLogin_RedirectsToProviderWithStateNonceAndPKCE(t *testing.T) {
	f := newOIDCFixture(t)
	t.Setenv("BASE_URL", "https://status.example.org")

	before := time.Now()
	w := serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?"+f.pageQuery(), nil, 0)

	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, f.idp.srv.URL+"/authorize", loc.Scheme+"://"+loc.Host+loc.Path)
	q := loc.Query()
	assert.Equal(t, testIdPClientID, q.Get("client_id"))
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, "https://status.example.org/api/auth/oidc/callback?"+f.pageQuery(), q.Get("redirect_uri"))
	assert.Equal(t, "openid email profile", q.Get("scope"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.NotEmpty(t, q.Get("code_challenge"))
	assert.NotEmpty(t, q.Get("nonce"))

	var st models.OIDCLoginState
	require.NoError(t, f.db.Where("state = ?", q.Get("state")).First(&st).Error)
	assert.Equal(t, q.Get("nonce"), st.Nonce)
	assert.NotEqual(t, st.State, st.Nonce)
	assert.Equal(t, f.page.ID, st.StatusPageID)
	assert.Zero(t, st.LinkUserID)
	assert.WithinDuration(t, before.Add(10*time.Minute), st.ExpiresAt, 5*time.Second)

	ck := stateCookie(w)
	require.NotNil(t, ck)
	assert.Equal(t, st.State, ck.Value)
	assert.Equal(t, "/api/auth/oidc", ck.Path)
	assert.Equal(t, 600, ck.MaxAge)
	assert.True(t, ck.HttpOnly)
	assert.True(t, ck.Secure)
}

func TestOIDCLogin_AddsOpenIDScopeWhenMissing(t *testing.T) {
	f := newOIDCFixture(t)
	require.NoError(t, f.db.Model(&models.StatusPageSSOConfig{}).Where("status_page_id = ?", f.page.ID).
		Update("o_id_c_scopes", "email").Error)

	w := serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?"+f.pageQuery(), nil, 0)

	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	loc, _ := url.Parse(w.Header().Get("Location"))
	assert.Equal(t, "openid email", loc.Query().Get("scope"))
}

func TestOIDCLogin_RemovesExpiredStates(t *testing.T) {
	f := newOIDCFixture(t)
	old := models.OIDCLoginState{State: "alt", Nonce: "n", CodeVerifier: "v", StatusPageID: f.page.ID, ExpiresAt: time.Now().Add(-time.Second)}
	fresh := models.OIDCLoginState{State: "frisch", Nonce: "n", CodeVerifier: "v", StatusPageID: f.page.ID, ExpiresAt: time.Now().Add(time.Minute)}
	require.NoError(t, f.db.Create(&old).Error)
	require.NoError(t, f.db.Create(&fresh).Error)

	f.startLogin(t)

	var states []string
	f.db.Model(&models.OIDCLoginState{}).Pluck("state", &states)
	assert.NotContains(t, states, "alt")
	assert.Contains(t, states, "frisch")
	assert.Len(t, states, 2)
}

func TestOIDCLogin_Errors(t *testing.T) {
	f := newOIDCFixture(t)

	t.Run("ungültige Seite", func(t *testing.T) {
		w := serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?page=abc", nil, 0)
		assertOIDCError(t, w, "/login", "bad_page")
	})
	t.Run("Seite ohne aktives OIDC", func(t *testing.T) {
		w := serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?page=999", nil, 0)
		assertOIDCError(t, w, "/login", "not_configured")
	})
	t.Run("Anbieter nicht erreichbar", func(t *testing.T) {
		require.NoError(t, f.db.Model(&models.StatusPageSSOConfig{}).Where("status_page_id = ?", f.page.ID).
			Update("o_id_c_discovery_url", f.idp.srv.URL+"/gibt-es-nicht").Error)
		t.Cleanup(func() {
			f.db.Model(&models.StatusPageSSOConfig{}).Where("status_page_id = ?", f.page.ID).
				Update("o_id_c_discovery_url", f.idp.srv.URL)
		})
		w := serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?"+f.pageQuery(), nil, 0)
		assertOIDCError(t, w, "/login", "provider_unreachable")
		assert.Nil(t, stateCookie(w))
	})
	t.Run("State nicht speicherbar", func(t *testing.T) {
		require.NoError(t, f.db.Migrator().DropTable(&models.OIDCLoginState{}))
		t.Cleanup(func() { _ = f.db.AutoMigrate(&models.OIDCLoginState{}) })
		w := serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?"+f.pageQuery(), nil, 0)
		assertOIDCError(t, w, "/login", "internal")
		assert.Nil(t, stateCookie(w))
	})
}

func TestOIDCIssuer(t *testing.T) {
	assert.Equal(t, "https://idp.example.org/realms/x", oidcIssuer(" https://idp.example.org/realms/x/.well-known/openid-configuration "))
	assert.Equal(t, "https://idp.example.org/", oidcIssuer("https://idp.example.org/"))
}

func TestOIDCProvider_RejectsPlainHTTP(t *testing.T) {
	newTestIdP(t)
	_, err := oidcProviderFor("http://idp.example.org")
	assert.ErrorIs(t, err, errOIDCInsecureIssuer)
}

// Der echte Client darf keine internen Adressen erreichen (SSRF-Schutz wie
// bei Webhooks): Der Discovery-URL pflegt der Seiten-Admin.
func TestDefaultOIDCHTTPClient_HasTimeout(t *testing.T) {
	assert.Equal(t, 10*time.Second, defaultOIDCHTTPClient().Timeout)
}

func TestOIDCProvider_DefaultClientBlocksInternalAddresses(t *testing.T) {
	idp := newTestIdP(t)
	oidcHTTPClient = defaultOIDCHTTPClient()

	_, err := oidcProviderFor(idp.srv.URL)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "interne Adresse")
	assert.Zero(t, idp.hits())
}

func TestOIDCProvider_CachesDiscoveryUntilTTL(t *testing.T) {
	idp := newTestIdP(t)
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	prevNow := oidcNow
	oidcNow = func() time.Time { return now }
	t.Cleanup(func() { oidcNow = prevNow })

	p1, err := oidcProviderFor(idp.srv.URL)
	require.NoError(t, err)
	now = now.Add(time.Hour - time.Nanosecond)
	p2, err := oidcProviderFor(idp.srv.URL)
	require.NoError(t, err)
	assert.Same(t, p1, p2)
	assert.Equal(t, 1, idp.hits())

	now = now.Add(time.Nanosecond)
	p3, err := oidcProviderFor(idp.srv.URL)
	require.NoError(t, err)
	assert.NotSame(t, p1, p3)
	assert.Equal(t, 2, idp.hits())
}

func TestOIDCCallback_LogsInLinkedUser(t *testing.T) {
	f := newOIDCFixture(t)
	f.linkIdentity(t, f.user.ID)
	state, ck := f.startLogin(t)

	w := f.callback(state, "good-code", ck)

	claims := jwtFromRedirect(t, w)
	assert.Equal(t, f.user.ID, claims.UserID)
	assert.Equal(t, "erika@example.org", claims.Email)
	assert.Equal(t, "Erika Muster", claims.Name)
	assert.Equal(t, "oidc", claims.SSOProvider)
	assert.Equal(t, "incidents:read", claims.Permissions)
	assert.Equal(t, f.page.ID, claims.SSOPageID)

	var audit models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "auth.sso_login").First(&audit).Error)
	assert.Equal(t, "erika@example.org", audit.ActorEmail)
	assert.Equal(t, "provider=oidc page="+strconv.FormatUint(uint64(f.page.ID), 10), audit.Details)

	// Cookie wird in jedem Fall gelöscht, der State ist verbraucht.
	gone := stateCookie(w)
	require.NotNil(t, gone)
	assert.Equal(t, -1, gone.MaxAge)
	var count int64
	f.db.Model(&models.OIDCLoginState{}).Count(&count)
	assert.Zero(t, count)
}

func TestOIDCCallback_StateIsSingleUse(t *testing.T) {
	f := newOIDCFixture(t)
	f.linkIdentity(t, f.user.ID)
	state, ck := f.startLogin(t)
	jwtFromRedirect(t, f.callback(state, "good-code", ck))

	w := f.callback(state, "good-code", ck)

	assertOIDCError(t, w, "/login", "state_invalid")
}

func TestOIDCCallback_RejectsUnlinkedIdentity(t *testing.T) {
	f := newOIDCFixture(t)
	// Gleiche E-Mail wie das Konto: Eine Zuordnung über die E-Mail findet
	// trotzdem nicht statt.
	f.idp.claims["email"] = "erika@example.org"
	f.idp.claims["email_verified"] = true
	state, ck := f.startLogin(t)

	w := f.callback(state, "good-code", ck)

	assertOIDCError(t, w, "/login", "not_linked")
}

func TestOIDCCallback_IdentityOfOtherPageDoesNotCount(t *testing.T) {
	f := newOIDCFixture(t)
	other := models.StatusPage{Name: "Andere", Slug: "andere"}
	require.NoError(t, f.db.Create(&other).Error)
	require.NoError(t, f.db.Create(&models.UserSSOIdentity{
		UserID: f.user.ID, StatusPageID: other.ID, Issuer: f.idp.srv.URL, Subject: "idp-user-1",
	}).Error)
	state, ck := f.startLogin(t)

	w := f.callback(state, "good-code", ck)

	assertOIDCError(t, w, "/login", "not_linked")
}

func TestOIDCCallback_RejectsDeletedOrGlobalAdminUser(t *testing.T) {
	t.Run("gelöschtes Konto", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.linkIdentity(t, f.user.ID)
		require.NoError(t, f.db.Delete(&f.user).Error)
		state, ck := f.startLogin(t)

		w := f.callback(state, "good-code", ck)

		assertOIDCError(t, w, "/login", "not_linked")
	})
	t.Run("später zum Global Admin gewordenes Konto", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.linkIdentity(t, f.user.ID)
		require.NoError(t, f.db.Model(&f.user).Update("is_global_admin", true).Error)
		state, ck := f.startLogin(t)

		w := f.callback(state, "good-code", ck)

		assertOIDCError(t, w, "/login", "global_admin")
	})
}

func TestOIDCCallback_RequestErrors(t *testing.T) {
	f := newOIDCFixture(t)
	f.linkIdentity(t, f.user.ID)

	t.Run("ohne Code (Abbruch beim Anbieter)", func(t *testing.T) {
		state, ck := f.startLogin(t)
		w := f.callback(state, "", ck)
		assertOIDCError(t, w, "/login", "provider_denied")
		// Der State ist trotzdem verbraucht.
		var count int64
		f.db.Model(&models.OIDCLoginState{}).Where("state = ?", state).Count(&count)
		assert.Zero(t, count)
	})
	t.Run("ohne State", func(t *testing.T) {
		_, ck := f.startLogin(t)
		w := f.callback("", "good-code", ck)
		assertOIDCError(t, w, "/login", "state_invalid")
	})
	t.Run("State passt nicht zum Cookie (Login-CSRF)", func(t *testing.T) {
		state, _ := f.startLogin(t)
		other, _ := f.startLogin(t)
		w := f.callback(state, "good-code", &http.Cookie{Name: oidcStateCookie, Value: other})
		assertOIDCError(t, w, "/login", "state_invalid")
		// Der State bleibt dabei unverbraucht.
		var count int64
		f.db.Model(&models.OIDCLoginState{}).Where("state = ?", state).Count(&count)
		assert.EqualValues(t, 1, count)
	})
	t.Run("ohne Cookie", func(t *testing.T) {
		state, _ := f.startLogin(t)
		w := f.callback(state, "good-code", nil)
		assertOIDCError(t, w, "/login", "state_invalid")
	})
	t.Run("unbekannter State", func(t *testing.T) {
		w := f.callback("erfunden", "good-code", &http.Cookie{Name: oidcStateCookie, Value: "erfunden"})
		assertOIDCError(t, w, "/login", "state_invalid")
	})
	t.Run("abgelaufener State", func(t *testing.T) {
		state, ck := f.startLogin(t)
		require.NoError(t, f.db.Model(&models.OIDCLoginState{}).Where("state = ?", state).
			Update("expires_at", time.Now().Add(-time.Second)).Error)
		w := f.callback(state, "good-code", ck)
		assertOIDCError(t, w, "/login", "state_invalid")
		var count int64
		f.db.Model(&models.OIDCLoginState{}).Where("state = ?", state).Count(&count)
		assert.Zero(t, count)
	})
	t.Run("SSO inzwischen abgeschaltet", func(t *testing.T) {
		state, ck := f.startLogin(t)
		require.NoError(t, f.db.Model(&models.StatusPageSSOConfig{}).Where("status_page_id = ?", f.page.ID).
			Update("o_id_c_enabled", false).Error)
		t.Cleanup(func() {
			f.db.Model(&models.StatusPageSSOConfig{}).Where("status_page_id = ?", f.page.ID).Update("o_id_c_enabled", true)
		})
		w := f.callback(state, "good-code", ck)
		assertOIDCError(t, w, "/login", "not_configured")
	})
}

func TestOIDCCallback_ProviderErrors(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(f *oidcFixture)
		code    string
		errCode string
	}{
		{"Code abgelehnt", func(f *oidcFixture) {}, "bad-code", "token_exchange"},
		{"Token-Endpunkt mit Fehler", func(f *oidcFixture) { f.idp.tokenStatus = http.StatusInternalServerError }, "good-code", "token_exchange"},
		{"PKCE-Verifier passt nicht", func(f *oidcFixture) { f.idp.challenge = "falsch" }, "good-code", "token_exchange"},
		{"kein ID-Token", func(f *oidcFixture) { f.idp.omitIDToken = true }, "good-code", "no_id_token"},
		{"falsche Signatur", func(f *oidcFixture) { _, other := testIdPKeys(t); f.idp.signWith = other }, "good-code", "id_token_invalid"},
		{"falsche Audience", func(f *oidcFixture) { f.idp.claims["aud"] = "anderer-client" }, "good-code", "id_token_invalid"},
		{"falscher Issuer", func(f *oidcFixture) { f.idp.claims["iss"] = "https://fremd.example.org" }, "good-code", "id_token_invalid"},
		{"abgelaufen", func(f *oidcFixture) { f.idp.claims["exp"] = time.Now().Add(-time.Hour).Unix() }, "good-code", "id_token_invalid"},
		{"falsche Nonce", func(f *oidcFixture) { f.idp.claims["nonce"] = "fremd" }, "good-code", "id_token_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newOIDCFixture(t)
			f.linkIdentity(t, f.user.ID)
			state, ck := f.startLogin(t)
			tc.setup(f)

			w := f.callback(state, tc.code, ck)

			assertOIDCError(t, w, "/login", tc.errCode)
		})
	}
}

func TestOIDCCallback_ProviderUnreachableAfterCacheExpiry(t *testing.T) {
	f := newOIDCFixture(t)
	f.linkIdentity(t, f.user.ID)
	state, ck := f.startLogin(t)
	resetOIDCProviderCache()
	f.idp.srv.Close()

	w := f.callback(state, "good-code", ck)

	assertOIDCError(t, w, "/login", "provider_unreachable")
}

func (f *oidcFixture) startLink(t *testing.T, userID uint) (*httptest.ResponseRecorder, string, *http.Cookie) {
	t.Helper()
	w := serveOIDC(f.h.OIDCLinkHandler, http.MethodPost, "/api/auth/oidc/link?"+f.pageQuery(), nil, userID)
	if w.Code != http.StatusOK {
		return w, "", nil
	}
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return w, f.rememberAuthorizeURL(t, body["url"]), stateCookie(w)
}

func TestOIDCLink_LinksIdentityAndLogsIn(t *testing.T) {
	f := newOIDCFixture(t)
	w, state, ck := f.startLink(t, f.user.ID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotEmpty(t, state)
	var st models.OIDCLoginState
	require.NoError(t, f.db.Where("state = ?", state).First(&st).Error)
	assert.Equal(t, f.user.ID, st.LinkUserID)

	w = f.callback(state, "good-code", ck)
	// Nach dem Verknüpfen zurück ins Profil (fester Wert, kein Ziel aus der Anfrage).
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "profile", loc.Query().Get("next"))
	claims := jwtFromRedirect(t, w)

	assert.Equal(t, f.user.ID, claims.UserID)
	assert.Equal(t, f.page.ID, claims.SSOPageID) // auch nach Verknüpfung
	var ident models.UserSSOIdentity
	require.NoError(t, f.db.First(&ident).Error)
	assert.Equal(t, f.user.ID, ident.UserID)
	assert.Equal(t, f.page.ID, ident.StatusPageID)
	assert.Equal(t, f.idp.srv.URL, ident.Issuer)
	assert.Equal(t, "idp-user-1", ident.Subject)
	var audit models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "auth.sso_linked").First(&audit).Error)

	// Danach klappt der normale Login ohne Verknüpfung.
	state, ck = f.startLogin(t)
	assert.Equal(t, f.user.ID, jwtFromRedirect(t, f.callback(state, "good-code", ck)).UserID)
}

func TestOIDCLink_SameUserAgainIsIdempotent(t *testing.T) {
	f := newOIDCFixture(t)
	f.linkIdentity(t, f.user.ID)
	_, state, ck := f.startLink(t, f.user.ID)

	jwtFromRedirect(t, f.callback(state, "good-code", ck))

	var count int64
	f.db.Model(&models.UserSSOIdentity{}).Count(&count)
	assert.EqualValues(t, 1, count)
}

func TestOIDCLink_IdentityOfOtherUserIsConflict(t *testing.T) {
	f := newOIDCFixture(t)
	other := models.User{Email: "max@example.org", RoleID: f.user.RoleID}
	require.NoError(t, f.db.Create(&other).Error)
	f.linkIdentity(t, other.ID)
	_, state, ck := f.startLink(t, f.user.ID)

	w := f.callback(state, "good-code", ck)

	assertOIDCError(t, w, "/admin/profile", "already_linked")
	var ident models.UserSSOIdentity
	require.NoError(t, f.db.First(&ident).Error)
	assert.Equal(t, other.ID, ident.UserID)
}

func TestOIDCLink_IdentityNotStorable(t *testing.T) {
	f := newOIDCFixture(t)
	_, state, ck := f.startLink(t, f.user.ID)
	require.NoError(t, f.db.Migrator().DropTable(&models.UserSSOIdentity{}))

	w := f.callback(state, "good-code", ck)

	assertOIDCError(t, w, "/admin/profile", "internal")
}

func TestOIDCLink_StartErrors(t *testing.T) {
	f := newOIDCFixture(t)

	t.Run("unbekanntes Konto", func(t *testing.T) {
		w, _, _ := f.startLink(t, 0)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
	t.Run("Global Admin", func(t *testing.T) {
		admin := models.User{Email: "admin@example.org", IsGlobalAdmin: true}
		require.NoError(t, f.db.Create(&admin).Error)
		w, _, _ := f.startLink(t, admin.ID)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Equal(t, errOIDCGlobalAdmin.msg, errorOf(t, w))
	})
	t.Run("Seite ohne OIDC", func(t *testing.T) {
		w := serveOIDC(f.h.OIDCLinkHandler, http.MethodPost, "/api/auth/oidc/link?page=999", nil, f.user.ID)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// multiIssuerIdP bedient mehrere Issuer (Pfade) auf einem TLS-Server, damit ein
// oidcHTTPClient alle erreicht. Je Pfad zählt er Discovery-Aufrufe; Pfade in
// delay antworten erst nach der angegebenen Zeit, Pfade in failing mit 500.
type multiIssuerIdP struct {
	srv     *httptest.Server
	mu      sync.Mutex
	hits    map[string]int
	delay   map[string]time.Duration
	failing map[string]bool
}

func newMultiIssuerIdP(t *testing.T) *multiIssuerIdP {
	t.Helper()
	idp := &multiIssuerIdP{hits: map[string]int{}, delay: map[string]time.Duration{}, failing: map[string]bool{}}
	idp.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, "/.well-known/openid-configuration")
		idp.mu.Lock()
		idp.hits[path]++
		d, fail := idp.delay[path], idp.failing[path]
		idp.mu.Unlock()
		time.Sleep(d)
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                                idp.srv.URL + path,
			"authorization_endpoint":                idp.srv.URL + path + "/authorize",
			"token_endpoint":                        idp.srv.URL + path + "/token",
			"jwks_uri":                              idp.srv.URL + path + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	t.Cleanup(idp.srv.Close)
	prevClient := oidcHTTPClient
	oidcHTTPClient = idp.srv.Client()
	resetOIDCProviderCache()
	t.Cleanup(func() {
		oidcHTTPClient = prevClient
		resetOIDCProviderCache()
	})
	return idp
}

func (idp *multiIssuerIdP) hitsFor(path string) int {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	return idp.hits[path]
}

// Review-Befund 1: Ein langsamer Anbieter darf Logins bei anderen,
// schon gecachten Anbietern nicht blockieren.
func TestOIDCProvider_SlowDiscoveryDoesNotBlockOtherIssuers(t *testing.T) {
	idp := newMultiIssuerIdP(t)
	idp.delay["/langsam"] = 2 * time.Second
	_, err := oidcProviderFor(idp.srv.URL + "/gesund")
	require.NoError(t, err)

	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		_, _ = oidcProviderFor(idp.srv.URL + "/langsam")
		close(done)
	}()
	<-started
	time.Sleep(200 * time.Millisecond) // langsame Discovery läuft sicher

	begin := time.Now()
	_, err = oidcProviderFor(idp.srv.URL + "/gesund")
	require.NoError(t, err)
	assert.Less(t, time.Since(begin), 200*time.Millisecond)

	// Auch eine frische Discovery eines weiteren Anbieters wartet nicht.
	begin = time.Now()
	_, err = oidcProviderFor(idp.srv.URL + "/anderer")
	require.NoError(t, err)
	assert.Less(t, time.Since(begin), time.Second)
	<-done
}

func TestOIDCProvider_ConcurrentCallsShareOneDiscovery(t *testing.T) {
	idp := newMultiIssuerIdP(t)
	idp.delay["/eins"] = 300 * time.Millisecond

	var wg sync.WaitGroup
	providers := make([]*oidc.Provider, 5)
	for i := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := oidcProviderFor(idp.srv.URL + "/eins")
			assert.NoError(t, err)
			providers[i] = p
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, idp.hitsFor("/eins"))
	for _, p := range providers {
		assert.Same(t, providers[0], p)
	}
}

func TestOIDCProvider_CachesFailureBriefly(t *testing.T) {
	idp := newMultiIssuerIdP(t)
	idp.failing["/tot"] = true
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	prevNow := oidcNow
	oidcNow = func() time.Time { return now }
	t.Cleanup(func() { oidcNow = prevNow })

	_, err1 := oidcProviderFor(idp.srv.URL + "/tot")
	require.Error(t, err1)
	now = now.Add(oidcFailureTTL - time.Nanosecond)
	_, err2 := oidcProviderFor(idp.srv.URL + "/tot")
	require.Error(t, err2)
	assert.Equal(t, err1.Error(), err2.Error())
	assert.Equal(t, 1, idp.hitsFor("/tot"))

	// Nach Ablauf wird neu versucht, und ein wieder erreichbarer Anbieter geht.
	now = now.Add(time.Nanosecond)
	idp.mu.Lock()
	idp.failing["/tot"] = false
	idp.mu.Unlock()
	_, err := oidcProviderFor(idp.srv.URL + "/tot")
	require.NoError(t, err)
	assert.Equal(t, 2, idp.hitsFor("/tot"))
}

// Review-Befund 2: Den IdP einer Seite kontrolliert deren Admin. Ein Konto mit
// Rolle auf einer anderen Seite darf sich deshalb weder über dieses SSO
// verknüpfen noch anmelden, sonst erbte der Admin die Rechte der fremden Seite.
func (f *oidcFixture) addPageRole(t *testing.T, userID uint, pageSlug string) models.StatusPage {
	t.Helper()
	page := models.StatusPage{Name: "Fremde Seite " + pageSlug, Slug: pageSlug}
	require.NoError(t, f.db.Create(&page).Error)
	require.NoError(t, f.db.Create(&models.UserStatusPageRole{
		UserID: userID, StatusPageID: page.ID, RoleID: f.user.RoleID,
	}).Error)
	return page
}

func TestOIDCCallback_RejectsAccountWithRoleOnOtherPage(t *testing.T) {
	f := newOIDCFixture(t)
	f.linkIdentity(t, f.user.ID)
	f.addPageRole(t, f.user.ID, "seite-b")
	state, ck := f.startLogin(t)

	w := f.callback(state, "good-code", ck)

	assertOIDCError(t, w, "/login", "other_page_role")
	var n int64
	require.NoError(t, f.db.Model(&models.AuditLog{}).Where("action = ?", "auth.sso_login").Count(&n).Error)
	assert.Zero(t, n)
}

func TestOIDCCallback_AllowsAccountWithRoleOnlyOnThisPage(t *testing.T) {
	f := newOIDCFixture(t)
	f.linkIdentity(t, f.user.ID)
	require.NoError(t, f.db.Create(&models.UserStatusPageRole{
		UserID: f.user.ID, StatusPageID: f.page.ID, RoleID: f.user.RoleID,
	}).Error)
	state, ck := f.startLogin(t)

	assert.Equal(t, f.user.ID, jwtFromRedirect(t, f.callback(state, "good-code", ck)).UserID)
}

func TestOIDCLink_RejectsAccountWithRoleOnOtherPage(t *testing.T) {
	f := newOIDCFixture(t)
	f.addPageRole(t, f.user.ID, "seite-b")

	w, _, _ := f.startLink(t, f.user.ID)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, errOIDCOtherPageRole.msg, errorOf(t, w))
	var n int64
	require.NoError(t, f.db.Model(&models.OIDCLoginState{}).Count(&n).Error)
	assert.Zero(t, n)
}

func TestOIDCLink_AllowsAccountWithRoleOnlyOnThisPage(t *testing.T) {
	f := newOIDCFixture(t)
	require.NoError(t, f.db.Create(&models.UserStatusPageRole{
		UserID: f.user.ID, StatusPageID: f.page.ID, RoleID: f.user.RoleID,
	}).Error)

	w, _, _ := f.startLink(t, f.user.ID)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// Der zweite Cache-Check in discoverOIDCProvider fängt einen Aufruf ab, der
// erst nach einem fertigen Vorgänger in singleflight landet: keine Discovery.
func TestDiscoverOIDCProvider_UsesFreshCacheEntry(t *testing.T) {
	issuer := "https://cache-treffer.example.org"
	want := oidcProviderEntry{err: errors.New("aus dem Cache"), expires: oidcNow().Add(time.Minute)}
	oidcProviderMu.Lock()
	oidcProviders[issuer] = want
	oidcProviderMu.Unlock()
	t.Cleanup(func() {
		oidcProviderMu.Lock()
		delete(oidcProviders, issuer)
		oidcProviderMu.Unlock()
	})

	got := discoverOIDCProvider(issuer)

	assert.Equal(t, want.err, got.err)
}
