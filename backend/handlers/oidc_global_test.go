package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Sign-in through a global OIDC provider (OIDC_PROVIDERS) against the mock
// IdP. The provider ID used throughout is "corp".

type globalOIDCFixture struct {
	h      *Handler
	db     *gorm.DB
	idp    *testIdP
	viewer models.Role
	admin  models.Role
}

func newGlobalOIDCFixture(t *testing.T) *globalOIDCFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := testutils.SetupTestDB()
	viewer := models.Role{Name: "Viewer", Permissions: "incidents:read"}
	require.NoError(t, db.Create(&viewer).Error)
	admin := models.Role{Name: models.GlobalAdminRoleName, Permissions: "*"}
	require.NoError(t, db.Create(&admin).Error)
	idp := newTestIdP(t)
	t.Setenv("BASE_URL", "https://status.example.org")
	t.Setenv("OIDC_PROVIDERS", "corp")
	t.Setenv("OIDC_CORP_ISSUER", idp.srv.URL)
	t.Setenv("OIDC_CORP_CLIENT_ID", testIdPClientID)
	t.Setenv("OIDC_CORP_CLIENT_SECRET", "corp-secret")
	t.Setenv("OIDC_CORP_NAME", "Corp SSO")
	t.Setenv("SSO_ALLOWED_DOMAINS", "")
	t.Setenv("SSO_BOOTSTRAP_ADMIN_EMAILS", "")
	idp.claims["email"] = "neu@example.org"
	idp.claims["email_verified"] = true
	return &globalOIDCFixture{h: NewHandler(db), db: db, idp: idp, viewer: viewer, admin: admin}
}

// serveProvider calls a provider handler with :id set, optionally with the
// state cookie and a signed-in user.
func serveProvider(handler gin.HandlerFunc, method, target, id string, cookie *http.Cookie, userID uint) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	c.Params = gin.Params{{Key: "id", Value: id}}
	if cookie != nil {
		c.Request.AddCookie(cookie)
	}
	if userID != 0 {
		c.Set("userID", userID)
	}
	handler(c)
	return w
}

func (f *globalOIDCFixture) rememberAuthorizeURL(t *testing.T, location string) string {
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

func (f *globalOIDCFixture) startLogin(t *testing.T, id string) (string, *http.Cookie) {
	t.Helper()
	w := serveProvider(f.h.ProviderLoginHandler, http.MethodGet, "/api/auth/provider/"+id+"/login", id, nil, 0)
	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	return f.rememberAuthorizeURL(t, w.Header().Get("Location")), stateCookie(w)
}

func (f *globalOIDCFixture) callback(id, state, code string, cookie *http.Cookie) *httptest.ResponseRecorder {
	q := url.Values{}
	if state != "" {
		q.Set("state", state)
	}
	if code != "" {
		q.Set("code", code)
	}
	return serveProvider(f.h.ProviderCallbackHandler, http.MethodGet, "/api/auth/provider/"+id+"/callback?"+q.Encode(), id, cookie, 0)
}

func (f *globalOIDCFixture) login(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	state, ck := f.startLogin(t, "corp")
	return f.callback("corp", state, "good-code", ck)
}

func (f *globalOIDCFixture) users(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, f.db.Model(&models.User{}).Count(&n).Error)
	return n
}

func TestProviderLogin_RedirectsToIdPWithStateNonceAndPKCE(t *testing.T) {
	f := newGlobalOIDCFixture(t)

	w := serveProvider(f.h.ProviderLoginHandler, http.MethodGet, "/api/auth/provider/corp/login", "corp", nil, 0)

	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, f.idp.srv.URL+"/authorize", loc.Scheme+"://"+loc.Host+loc.Path)
	q := loc.Query()
	assert.Equal(t, testIdPClientID, q.Get("client_id"))
	assert.Equal(t, "https://status.example.org/api/auth/provider/corp/callback", q.Get("redirect_uri"))
	assert.Equal(t, "openid email profile", q.Get("scope"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.NotEmpty(t, q.Get("code_challenge"))
	assert.NotEmpty(t, q.Get("nonce"))

	var st models.OIDCLoginState
	require.NoError(t, f.db.Where("state = ?", q.Get("state")).First(&st).Error)
	assert.Equal(t, "corp", st.Provider)
	assert.Zero(t, st.StatusPageID)
	assert.Zero(t, st.LinkUserID)
	ck := stateCookie(w)
	require.NotNil(t, ck)
	assert.Equal(t, st.State, ck.Value)
	assert.Equal(t, "/api/auth/provider", ck.Path)
	assert.True(t, ck.HttpOnly)
	assert.True(t, ck.Secure)
}

func TestProviderLogin_UnknownProvider(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	for _, id := range []string{"github", "", "../corp", "CORP"} {
		w := serveProvider(f.h.ProviderLoginHandler, http.MethodGet, "/x", id, nil, 0)
		assertOIDCError(t, w, "/login", "provider_unknown")
		w = f.callback(id, "s", "good-code", nil)
		assertOIDCError(t, w, "/login", "provider_unknown")
	}
}

func TestProviderLogin_DiscoveryFailureIsReported(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	f.idp.srv.Close()
	resetOIDCProviderCache()
	w := serveProvider(f.h.ProviderLoginHandler, http.MethodGet, "/x", "corp", nil, 0)
	assertOIDCError(t, w, "/login", "provider_unreachable")
}

func TestProviderCallback_FirstLoginCreatesAccount(t *testing.T) {
	f := newGlobalOIDCFixture(t)

	claims := jwtFromRedirect(t, f.login(t))

	var user models.User
	require.NoError(t, f.db.Preload("Role").First(&user, claims.UserID).Error)
	assert.Equal(t, "neu@example.org", user.Email)
	assert.Empty(t, user.PasswordHash)
	assert.False(t, user.IsGlobalAdmin)
	assert.Equal(t, "Viewer", user.Role.Name)
	assert.Zero(t, claims.SSOPageID, "a global provider grants a normal session")
	assert.Equal(t, "Erika Muster", claims.Name)
	var ident models.UserSSOIdentity
	require.NoError(t, f.db.First(&ident).Error)
	assert.Equal(t, models.UserSSOIdentity{
		ID: ident.ID, UserID: user.ID, StatusPageID: 0, Protocol: models.SSOProtocolOIDC,
		Issuer: f.idp.srv.URL, Subject: "idp-user-1", Email: "neu@example.org", CreatedAt: ident.CreatedAt,
	}, ident)
	var audit models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "auth.sso_login").First(&audit).Error)
	assert.Equal(t, "provider=corp", audit.Details)

	// The next login finds the account through (issuer, subject), even when
	// the e-mail at the IdP changed in between.
	f.idp.claims["email"] = "umbenannt@example.org"
	again := jwtFromRedirect(t, f.login(t))
	assert.Equal(t, user.ID, again.UserID)
	assert.Equal(t, int64(1), f.users(t))
}

func TestProviderCallback_EmailRules(t *testing.T) {
	cases := map[string]struct {
		verified any // nil = claim missing
		existing bool
		wantCode string // "" = success
	}{
		"verified, existing account is linked":        {true, true, ""},
		"verified as string, existing account linked": {"true", true, ""},
		"missing claim, existing account not linked":  {nil, true, "email_unverified"},
		"unverified, existing account not linked":     {false, true, "email_unverified"},
		"unverified, no account":                      {false, false, "email_unverified"},
		"missing claim, no account: new account":      {nil, false, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newGlobalOIDCFixture(t)
			var existing models.User
			if tc.existing {
				existing = models.User{Email: "neu@example.org", PasswordHash: testutils.MustHash("altes-passwort"), RoleID: f.viewer.ID}
				require.NoError(t, f.db.Create(&existing).Error)
			}
			if tc.verified == nil {
				delete(f.idp.claims, "email_verified")
			} else {
				f.idp.claims["email_verified"] = tc.verified
			}
			before := f.users(t)

			w := f.login(t)

			if tc.wantCode != "" {
				assertOIDCError(t, w, "/login", tc.wantCode)
				assert.Equal(t, before, f.users(t), "no account created")
				var n int64
				f.db.Model(&models.UserSSOIdentity{}).Count(&n)
				assert.Zero(t, n, "no link created")
				return
			}
			claims := jwtFromRedirect(t, w)
			if tc.existing {
				assert.Equal(t, existing.ID, claims.UserID)
				assert.Equal(t, before, f.users(t))
			} else {
				assert.Equal(t, before+1, f.users(t))
			}
		})
	}
}

func TestProviderCallback_EmailMissing(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	delete(f.idp.claims, "email")
	assertOIDCError(t, f.login(t), "/login", "email_missing")
	assert.Zero(t, f.users(t))
}

func TestProviderCallback_AllowedDomains(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	t.Setenv("SSO_ALLOWED_DOMAINS", "firma.example")
	assertOIDCError(t, f.login(t), "/login", "domain_not_allowed")
	assert.Zero(t, f.users(t))

	f.idp.claims["email"] = "erika@FIRMA.example"
	jwtFromRedirect(t, f.login(t))
	assert.Equal(t, int64(1), f.users(t))
}

// The allow-list only restricts new accounts: an existing account with a
// verified e-mail is linked whatever its domain.
func TestProviderCallback_AllowedDomainsDoNotBlockExistingAccounts(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	t.Setenv("SSO_ALLOWED_DOMAINS", "firma.example")
	existing := models.User{Email: "neu@example.org", RoleID: f.viewer.ID}
	require.NoError(t, f.db.Create(&existing).Error)
	assert.Equal(t, existing.ID, jwtFromRedirect(t, f.login(t)).UserID)
}

func TestProviderCallback_BootstrapAdmin(t *testing.T) {
	t.Run("verified bootstrap e-mail becomes global admin", func(t *testing.T) {
		f := newGlobalOIDCFixture(t)
		t.Setenv("SSO_BOOTSTRAP_ADMIN_EMAILS", "chef@example.org")
		f.idp.claims["email"] = "Chef@example.org"
		claims := jwtFromRedirect(t, f.login(t))
		assert.True(t, claims.IsGlobalAdmin)
		var user models.User
		require.NoError(t, f.db.First(&user, claims.UserID).Error)
		assert.True(t, user.IsGlobalAdmin)
		assert.Equal(t, f.admin.ID, user.RoleID)
	})
	t.Run("existing account is raised on its next login", func(t *testing.T) {
		f := newGlobalOIDCFixture(t)
		first := jwtFromRedirect(t, f.login(t))
		assert.False(t, first.IsGlobalAdmin)
		t.Setenv("SSO_BOOTSTRAP_ADMIN_EMAILS", "neu@example.org")
		assert.True(t, jwtFromRedirect(t, f.login(t)).IsGlobalAdmin)
	})
	t.Run("without a verified e-mail nobody becomes admin", func(t *testing.T) {
		f := newGlobalOIDCFixture(t)
		t.Setenv("SSO_BOOTSTRAP_ADMIN_EMAILS", "neu@example.org")
		delete(f.idp.claims, "email_verified")
		claims := jwtFromRedirect(t, f.login(t))
		assert.False(t, claims.IsGlobalAdmin)
	})
	t.Run("the account e-mail counts, not a changed claim", func(t *testing.T) {
		f := newGlobalOIDCFixture(t)
		jwtFromRedirect(t, f.login(t))
		t.Setenv("SSO_BOOTSTRAP_ADMIN_EMAILS", "chef@example.org")
		f.idp.claims["email"] = "chef@example.org"
		assert.False(t, jwtFromRedirect(t, f.login(t)).IsGlobalAdmin)
	})
}

func TestProviderCallback_StateAbuse(t *testing.T) {
	t.Run("forged state without cookie", func(t *testing.T) {
		f := newGlobalOIDCFixture(t)
		f.startLogin(t, "corp")
		w := f.callback("corp", "selbst-gewaehlt", "good-code", &http.Cookie{Name: oidcStateCookie, Value: "selbst-gewaehlt"})
		assertOIDCError(t, w, "/login", "state_invalid")
	})
	t.Run("cookie does not match the state", func(t *testing.T) {
		f := newGlobalOIDCFixture(t)
		state, _ := f.startLogin(t, "corp")
		w := f.callback("corp", state, "good-code", &http.Cookie{Name: oidcStateCookie, Value: "anders"})
		assertOIDCError(t, w, "/login", "state_invalid")
	})
	t.Run("state used twice", func(t *testing.T) {
		f := newGlobalOIDCFixture(t)
		state, ck := f.startLogin(t, "corp")
		jwtFromRedirect(t, f.callback("corp", state, "good-code", ck))
		assertOIDCError(t, f.callback("corp", state, "good-code", ck), "/login", "state_invalid")
	})
	t.Run("expired state", func(t *testing.T) {
		f := newGlobalOIDCFixture(t)
		state, ck := f.startLogin(t, "corp")
		require.NoError(t, f.db.Model(&models.OIDCLoginState{}).Where("state = ?", state).
			Update("expires_at", time.Now().Add(-time.Second)).Error)
		assertOIDCError(t, f.callback("corp", state, "good-code", ck), "/login", "state_invalid")
	})
	t.Run("state of another provider", func(t *testing.T) {
		f := newGlobalOIDCFixture(t)
		t.Setenv("OIDC_PROVIDERS", "corp,other")
		setProvider(t, "OTHER", f.idp.srv.URL)
		state, ck := f.startLogin(t, "other")
		assertOIDCError(t, f.callback("corp", state, "good-code", ck), "/login", "state_invalid")
	})
	t.Run("state of a provider login at the page callback", func(t *testing.T) {
		f := newGlobalOIDCFixture(t)
		state, ck := f.startLogin(t, "corp")
		w := serveOIDC(f.h.OIDCCallbackHandler, http.MethodGet, "/api/auth/oidc/callback?state="+state+"&code=good-code", ck, 0)
		assertOIDCError(t, w, "/login", "state_invalid")
	})
	t.Run("provider aborted (no code)", func(t *testing.T) {
		f := newGlobalOIDCFixture(t)
		state, ck := f.startLogin(t, "corp")
		assertOIDCError(t, f.callback("corp", state, "", ck), "/login", "provider_denied")
	})
}

func TestProviderCallback_IDTokenAbuse(t *testing.T) {
	for name, claims := range map[string]map[string]any{
		"wrong nonce":     {"nonce": "fremde-nonce"},
		"other issuer":    {"iss": "https://other-idp.example.org"},
		"other client ID": {"aud": "other-client"},
		"expired token":   {"exp": time.Now().Add(-time.Hour).Unix()},
		"no subject":      {"sub": ""},
	} {
		t.Run(name, func(t *testing.T) {
			f := newGlobalOIDCFixture(t)
			state, ck := f.startLogin(t, "corp")
			for k, v := range claims {
				f.idp.claims[k] = v
			}
			assertOIDCError(t, f.callback("corp", state, "good-code", ck), "/login", "id_token_invalid")
			assert.Zero(t, f.users(t))
		})
	}
	t.Run("token exchange rejected", func(t *testing.T) {
		f := newGlobalOIDCFixture(t)
		state, ck := f.startLogin(t, "corp")
		assertOIDCError(t, f.callback("corp", state, "bad-code", ck), "/login", "token_exchange")
	})
}

func TestProviderCallback_DeletedAccountOfALink(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	first := jwtFromRedirect(t, f.login(t))
	require.NoError(t, f.db.Delete(&models.User{}, first.UserID).Error)
	assertOIDCError(t, f.login(t), "/login", "not_linked")
}

func (f *globalOIDCFixture) startLink(t *testing.T, id string, userID uint, pageScope uint) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/provider/"+id+"/link", nil)
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Set("userID", userID)
	if pageScope != 0 {
		c.Set("ssoPageID", pageScope)
	}
	f.h.ProviderLinkHandler(c)
	return w
}

func TestProviderLink_LinksSignedInAccount(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	f.idp.claims["email"] = "ganz-andere@idp.example"
	delete(f.idp.claims, "email_verified")
	// Also global admins may link a global provider.
	user := models.User{Email: "ga@example.org", RoleID: f.admin.ID, IsGlobalAdmin: true, PasswordHash: testutils.MustHash("pw-pw-pw-pw")}
	require.NoError(t, f.db.Create(&user).Error)

	w := f.startLink(t, "corp", user.ID, 0)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	state := f.rememberAuthorizeURL(t, body["url"])
	ck := stateCookie(w)

	w = f.callback("corp", state, "good-code", ck)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "profile", loc.Query().Get("next"))
	claims := jwtFromRedirect(t, w)
	assert.Equal(t, user.ID, claims.UserID)
	assert.True(t, claims.IsGlobalAdmin)
	var audit models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "auth.sso_linked").First(&audit).Error)

	// Afterwards the plain login finds the account without any e-mail rule.
	assert.Equal(t, user.ID, jwtFromRedirect(t, f.login(t)).UserID)
	assert.Equal(t, int64(1), f.users(t))
}

func TestProviderLink_IdentityOfAnotherAccount(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	owner := jwtFromRedirect(t, f.login(t))
	other := models.User{Email: "zweit@example.org", RoleID: f.viewer.ID}
	require.NoError(t, f.db.Create(&other).Error)

	w := f.startLink(t, "corp", other.ID, 0)
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	state := f.rememberAuthorizeURL(t, body["url"])

	w = f.callback("corp", state, "good-code", stateCookie(w))
	assertOIDCError(t, w, "/admin/profile", "already_linked")
	var ident models.UserSSOIdentity
	require.NoError(t, f.db.First(&ident).Error)
	assert.Equal(t, owner.UserID, ident.UserID)
}

func TestProviderLink_StartErrors(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	user := models.User{Email: "u@example.org", RoleID: f.viewer.ID}
	require.NoError(t, f.db.Create(&user).Error)

	w := f.startLink(t, "github", user.ID, 0)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"not_found"`)

	// A session from a page SSO must not attach a global identity: that
	// would turn a page-scoped session into a full one on the next login.
	w = f.startLink(t, "corp", user.ID, 42)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"forbidden"`)

	w = f.startLink(t, "corp", 9999, 0)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	f.idp.srv.Close()
	resetOIDCProviderCache()
	w = f.startLink(t, "corp", user.ID, 0)
	assert.Equal(t, http.StatusBadGateway, w.Code)
}

// A page SSO callback never resolves global identities and vice versa: the
// page key always carries the page ID.
func TestProviderIdentity_DoesNotOpenPageSSO(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	claims := jwtFromRedirect(t, f.login(t))
	_, e := f.h.resolveSSOUser(0, models.UserSSOIdentity{StatusPageID: 0, Protocol: models.SSOProtocolOIDC, Issuer: f.idp.srv.URL, Subject: "idp-user-1"}, "")
	assert.Equal(t, errOIDCIDTokenInvalid, e)
	assert.NotZero(t, claims.UserID)
}

func TestProviderSession_IsNotPageScoped(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	claims := jwtFromRedirect(t, f.login(t))
	assert.Equal(t, uint(0), claims.SSOPageID)
	assert.Equal(t, "oidc", claims.SSOProvider)
}

func (f *globalOIDCFixture) mySSO(t *testing.T, userID uint, pageScope uint) mySSOResponse {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/me/sso", nil)
	c.Set("userID", userID)
	if pageScope != 0 {
		c.Set("ssoPageID", pageScope)
	}
	f.h.GetMySSOHandler(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body mySSOResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

// The profile lists global identities (status_page_id 0, page_name = the
// provider's name, provider = its ID) and offers the not yet linked global
// providers, also to global admins, never to a page SSO session.
func TestMySSO_GlobalProviders(t *testing.T) {
	f := newGlobalOIDCFixture(t)
	t.Setenv("OIDC_PROVIDERS", "corp,other")
	setProvider(t, "OTHER", "https://other.example.org")
	t.Setenv("OIDC_OTHER_NAME", "Other")
	claims := jwtFromRedirect(t, f.login(t))
	require.NoError(t, f.db.Model(&models.User{}).Where("id = ?", claims.UserID).Update("is_global_admin", true).Error)

	body := f.mySSO(t, claims.UserID, 0)

	require.Len(t, body.Identities, 1)
	assert.Equal(t, uint(0), body.Identities[0].StatusPageID)
	assert.Equal(t, "corp", body.Identities[0].Provider)
	assert.Equal(t, "Corp SSO", body.Identities[0].PageName)
	assert.Equal(t, models.SSOProtocolOIDC, body.Identities[0].Protocol)
	assert.Equal(t, []mySSOLinkable{{StatusPageID: 0, PageName: "Other", Protocol: models.SSOProtocolOIDC, Provider: "other"}}, body.Linkable)

	assert.Empty(t, f.mySSO(t, claims.UserID, 7).Linkable, "no global provider for a page SSO session")

	// A provider that is no longer configured keeps its identity visible
	// (and removable) without a provider ID.
	t.Setenv("OIDC_PROVIDERS", "other")
	body = f.mySSO(t, claims.UserID, 0)
	require.Len(t, body.Identities, 1)
	assert.Equal(t, "", body.Identities[0].Provider)
}
