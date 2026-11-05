package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Frontend-Einstieg für das Seiten-SSO (Login-Knopf, Verknüpfen im
// Profil, Testanmeldung aus der SSO-Seite) und Auswertung der Claim-Felder.

func serveWithParams(handler gin.HandlerFunc, method, target string, params gin.Params, userID uint) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, nil)
	c.Params = params
	if userID != 0 {
		c.Set("userID", userID)
	}
	handler(c)
	return w
}

func (f *oidcFixture) updateSSO(t *testing.T, column string, value interface{}) {
	t.Helper()
	require.NoError(t, f.db.Model(&models.StatusPageSSOConfig{}).Where("status_page_id = ?", f.page.ID).
		Update(column, value).Error)
}

// --- Öffentliche Abfrage: ist OIDC für die Seite aktiv? ---

func TestPublicOIDC_ReportsOnlyWhetherActive(t *testing.T) {
	f := newOIDCFixture(t)

	w := serveWithParams(f.h.GetPublicOIDCHandler, http.MethodGet, "/", gin.Params{{Key: "slug", Value: "oidc-seite"}}, 0)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	// Genau diese Felder: keine Discovery-URL, keine Client-ID (
	// dazu nur, ob SAML aktiv ist).
	assert.Equal(t, map[string]any{"oidc_enabled": true, "saml_enabled": false, "page_id": float64(f.page.ID)}, decodeJSON(t, w))
}

func TestPublicOIDC_InactiveOrMissingConfig(t *testing.T) {
	f := newOIDCFixture(t)
	f.updateSSO(t, "o_id_c_enabled", false)
	other := models.StatusPage{Name: "Ohne SSO", Slug: "ohne-sso"}
	require.NoError(t, f.db.Create(&other).Error)

	for _, slug := range []string{"oidc-seite", "ohne-sso"} {
		w := serveWithParams(f.h.GetPublicOIDCHandler, http.MethodGet, "/", gin.Params{{Key: "slug", Value: slug}}, 0)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, false, decodeJSON(t, w)["oidc_enabled"], slug)
	}
}

func TestPublicOIDC_UnknownPage(t *testing.T) {
	f := newOIDCFixture(t)
	w := serveWithParams(f.h.GetPublicOIDCHandler, http.MethodGet, "/", gin.Params{{Key: "slug", Value: "gibt-es-nicht"}}, 0)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- Claims aus der Konfiguration ---

func TestOIDCCallback_UsesConfiguredNameClaim(t *testing.T) {
	f := newOIDCFixture(t)
	f.linkIdentity(t, f.user.ID)
	f.updateSSO(t, "o_id_c_attribute_name", "preferred_username")
	f.idp.claims["preferred_username"] = "erika.m"
	state, ck := f.startLogin(t)

	assert.Equal(t, "erika.m", jwtFromRedirect(t, f.callback(state, "good-code", ck)).Name)
}

func TestOIDCLink_StoresConfiguredEmailClaim(t *testing.T) {
	f := newOIDCFixture(t)
	f.updateSSO(t, "o_id_c_attribute_email", "mail")
	f.idp.claims["email"] = "nicht-dieser@example.org"
	f.idp.claims["mail"] = "erika@idp.example.org"
	_, state, ck := f.startLink(t, f.user.ID)

	jwtFromRedirect(t, f.callback(state, "good-code", ck))

	var ident models.UserSSOIdentity
	require.NoError(t, f.db.First(&ident).Error)
	assert.Equal(t, "erika@idp.example.org", ident.Email)
	// Die E-Mail des Kontos selbst bleibt unberührt (keine Zuordnung über die E-Mail).
	var user models.User
	require.NoError(t, f.db.First(&user, f.user.ID).Error)
	assert.Equal(t, "erika@example.org", user.Email)
}

func TestOIDCLogin_RefreshesStoredEmailClaim(t *testing.T) {
	f := newOIDCFixture(t)
	f.linkIdentity(t, f.user.ID)
	f.idp.claims["email"] = "neu@idp.example.org"
	state, ck := f.startLogin(t)

	jwtFromRedirect(t, f.callback(state, "good-code", ck))

	var ident models.UserSSOIdentity
	require.NoError(t, f.db.First(&ident).Error)
	assert.Equal(t, "neu@idp.example.org", ident.Email)
}

func TestClaimString(t *testing.T) {
	claims := map[string]interface{}{"email": "a@example.org", "zahl": 3}
	assert.Equal(t, "a@example.org", claimString(claims, "email"))
	assert.Empty(t, claimString(claims, "zahl"))
	assert.Empty(t, claimString(claims, "fehlt"))
	assert.Empty(t, claimString(claims, ""))
}

// --- Fehler im Callback als Redirect ---

func TestOIDCCallback_ProviderErrorRedirectsWithCode(t *testing.T) {
	f := newOIDCFixture(t)
	state, ck := f.startLogin(t)
	q := url.Values{"page": {strconv.FormatUint(uint64(f.page.ID), 10)}, "state": {state},
		"error": {"access_denied"}, "error_description": {"<script>"}}

	w := serveOIDC(f.h.OIDCCallbackHandler, http.MethodGet, "/api/auth/oidc/callback?"+q.Encode(), ck, 0)

	// Nur der feste Code landet in der URL, nichts aus der Antwort des Anbieters.
	assertOIDCError(t, w, "/login", "provider_denied")
}

func TestOIDCCallback_LinkErrorsGoBackToProfile(t *testing.T) {
	f := newOIDCFixture(t)
	_, state, ck := f.startLink(t, f.user.ID)
	f.idp.omitIDToken = true

	assertOIDCError(t, f.callback(state, "good-code", ck), "/admin/profile", "no_id_token")
}

func TestOIDCErrorURL_TargetsByFlow(t *testing.T) {
	t.Setenv("FRONTEND_URL", "https://status.example.org")
	assert.Equal(t, "https://status.example.org/login?oidc_error=not_linked",
		oidcErrorURL(models.OIDCLoginState{}, errOIDCNotLinked))
	assert.Equal(t, "https://status.example.org/admin/profile?oidc_error=not_linked",
		oidcErrorURL(models.OIDCLoginState{LinkUserID: 1}, errOIDCNotLinked))
	assert.Equal(t, "https://status.example.org/admin/sso?oidc_test=not_linked",
		oidcErrorURL(models.OIDCLoginState{TestLogin: true}, errOIDCNotLinked))
}

// --- Testanmeldung (test_login_enabled) ---

func (f *oidcFixture) startTest(t *testing.T, pageParam string) (*httptest.ResponseRecorder, string, *http.Cookie) {
	t.Helper()
	w := serveWithParams(f.h.StartSSOTestHandler, http.MethodPost, "/", gin.Params{{Key: "page_id", Value: pageParam}}, 0)
	if w.Code != http.StatusOK {
		return w, "", nil
	}
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return w, f.rememberAuthorizeURL(t, body["url"]), stateCookie(w)
}

func (f *oidcFixture) pageParam() string {
	return strconv.FormatUint(uint64(f.page.ID), 10)
}

func TestSSOTest_WorksBeforeGoingLiveAndCreatesNoSession(t *testing.T) {
	f := newOIDCFixture(t)
	f.updateSSO(t, "o_id_c_enabled", false)
	f.updateSSO(t, "test_login_enabled", true)
	f.idp.claims["email"] = "erika@idp.example.org"
	w, state, ck := f.startTest(t, f.pageParam())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var st models.OIDCLoginState
	require.NoError(t, f.db.Where("state = ?", state).First(&st).Error)
	assert.True(t, st.TestLogin)
	assert.Zero(t, st.LinkUserID)

	w = f.callback(state, "good-code", ck)

	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	assert.Equal(t, getFrontendURL()+"/admin/sso?oidc_test=ok", w.Header().Get("Location"))
	var n int64
	f.db.Model(&models.UserSSOIdentity{}).Count(&n)
	assert.Zero(t, n)
	f.db.Model(&models.AuditLog{}).Where("action LIKE ?", "auth.sso_%").Count(&n)
	assert.Zero(t, n)
}

func TestSSOTest_ReportsMissingClaims(t *testing.T) {
	f := newOIDCFixture(t)
	f.updateSSO(t, "test_login_enabled", true)
	f.idp.claims["name"] = ""
	_, state, ck := f.startTest(t, f.pageParam())

	w := f.callback(state, "good-code", ck)

	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, url.Values{"oidc_test": {"ok"}, "missing": {"email,name"}}, loc.Query())
}

func TestSSOTest_ErrorsGoToSSOPage(t *testing.T) {
	f := newOIDCFixture(t)
	f.updateSSO(t, "test_login_enabled", true)

	t.Run("Anbieter lehnt ab", func(t *testing.T) {
		_, state, ck := f.startTest(t, f.pageParam())
		assertOIDCError(t, f.callback(state, "bad-code", ck), "/admin/sso", "token_exchange")
	})
	t.Run("Testanmeldung inzwischen abgeschaltet", func(t *testing.T) {
		_, state, ck := f.startTest(t, f.pageParam())
		f.updateSSO(t, "test_login_enabled", false)
		t.Cleanup(func() { f.updateSSO(t, "test_login_enabled", true) })
		assertOIDCError(t, f.callback(state, "good-code", ck), "/admin/sso", "test_disabled")
	})
	t.Run("Anbieter nicht erreichbar", func(t *testing.T) {
		_, state, ck := f.startTest(t, f.pageParam())
		f.updateSSO(t, "o_id_c_discovery_url", f.idp.srv.URL+"/gibt-es-nicht")
		t.Cleanup(func() { f.updateSSO(t, "o_id_c_discovery_url", f.idp.srv.URL) })
		resetOIDCProviderCache()
		assertOIDCError(t, f.callback(state, "good-code", ck), "/admin/sso", "provider_unreachable")
	})
}

func TestSSOTest_StartErrors(t *testing.T) {
	f := newOIDCFixture(t)
	f.updateSSO(t, "test_login_enabled", true)

	t.Run("ungültige Seite", func(t *testing.T) {
		w, _, _ := f.startTest(t, "abc")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("ohne Konfiguration", func(t *testing.T) {
		w, _, _ := f.startTest(t, "999")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
	t.Run("Testanmeldung abgeschaltet", func(t *testing.T) {
		f.updateSSO(t, "test_login_enabled", false)
		t.Cleanup(func() { f.updateSSO(t, "test_login_enabled", true) })
		w, _, _ := f.startTest(t, f.pageParam())
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Equal(t, errOIDCTestDisabled.msg, errorOf(t, w))
	})
	t.Run("Konfiguration unvollständig", func(t *testing.T) {
		f.updateSSO(t, "o_id_c_client_secret", "")
		t.Cleanup(func() { f.updateSSO(t, "o_id_c_client_secret", "geheim") })
		w, _, _ := f.startTest(t, f.pageParam())
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, errOIDCTestIncomplete.msg, errorOf(t, w))
	})
	t.Run("Anbieter nicht erreichbar", func(t *testing.T) {
		f.updateSSO(t, "o_id_c_discovery_url", f.idp.srv.URL+"/gibt-es-nicht")
		t.Cleanup(func() { f.updateSSO(t, "o_id_c_discovery_url", f.idp.srv.URL) })
		w, _, _ := f.startTest(t, f.pageParam())
		assert.Equal(t, http.StatusBadGateway, w.Code)
		assert.Nil(t, stateCookie(w))
	})
}

func TestSSOTest_IncompleteConfigCases(t *testing.T) {
	full := models.StatusPageSSOConfig{OIDCDiscoveryURL: "https://idp", OIDCClientID: "c", OIDCClientSecret: "s"}
	assert.True(t, oidcConfigComplete(full))
	for _, mutate := range []func(*models.StatusPageSSOConfig){
		func(c *models.StatusPageSSOConfig) { c.OIDCDiscoveryURL = "" },
		func(c *models.StatusPageSSOConfig) { c.OIDCClientID = "" },
		func(c *models.StatusPageSSOConfig) { c.OIDCClientSecret = "" },
	} {
		c := full
		mutate(&c)
		assert.False(t, oidcConfigComplete(c))
	}
}

// --- Verknüpfungen im Profil ---

func (f *oidcFixture) mySSO(t *testing.T, userID uint) (int, mySSOResponse) {
	t.Helper()
	w := serveWithParams(f.h.GetMySSOHandler, http.MethodGet, "/", nil, userID)
	var body mySSOResponse
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	}
	return w.Code, body
}

func (f *oidcFixture) ssoPage(t *testing.T, name, slug string, enabled bool, mutate func(*models.StatusPage)) models.StatusPage {
	t.Helper()
	page := models.StatusPage{Name: name, Slug: slug}
	if mutate != nil {
		mutate(&page)
	}
	// Create überschreibt Nullwerte mit dem DB-Default (is_public_indexed=true),
	// deshalb die gewünschten Werte vorher merken und danach setzen.
	flags := map[string]interface{}{"is_private": page.IsPrivate, "is_public_indexed": page.IsPublicIndexed}
	require.NoError(t, f.db.Create(&page).Error)
	require.NoError(t, f.db.Model(&page).Updates(flags).Error)
	cfg := models.StatusPageSSOConfig{StatusPageID: page.ID, OIDCDiscoveryURL: "https://idp", OIDCClientID: "c", OIDCClientSecret: "s"}
	require.NoError(t, f.db.Create(&cfg).Error)
	require.NoError(t, f.db.Model(&cfg).Update("o_id_c_enabled", enabled).Error)
	return page
}

func linkablePageIDs(body mySSOResponse) []uint {
	ids := []uint{}
	for _, p := range body.Linkable {
		ids = append(ids, p.StatusPageID)
	}
	return ids
}

func TestMySSO_ListsIdentitiesAndLinkablePublicPages(t *testing.T) {
	f := newOIDCFixture(t)
	public := f.ssoPage(t, "Öffentlich", "oeffentlich", true, func(p *models.StatusPage) { p.IsPublicIndexed = true })
	f.ssoPage(t, "Privat", "privat", true, func(p *models.StatusPage) { p.IsPrivate = true; p.IsPublicIndexed = true })
	f.ssoPage(t, "Nicht gelistet", "ungelistet", true, func(p *models.StatusPage) { p.IsPublicIndexed = false })
	f.ssoPage(t, "Aus", "aus", false, func(p *models.StatusPage) { p.IsPublicIndexed = true })
	require.NoError(t, f.db.Create(&models.UserSSOIdentity{
		UserID: f.user.ID, StatusPageID: f.page.ID, Issuer: "https://idp", Subject: "s1", Email: "erika@idp.example.org",
	}).Error)
	other := models.User{Email: "max@example.org", RoleID: f.user.RoleID}
	require.NoError(t, f.db.Create(&other).Error)
	require.NoError(t, f.db.Create(&models.UserSSOIdentity{UserID: other.ID, StatusPageID: public.ID, Issuer: "https://idp", Subject: "s2"}).Error)

	code, body := f.mySSO(t, f.user.ID)

	require.Equal(t, http.StatusOK, code)
	require.Len(t, body.Identities, 1)
	assert.Equal(t, f.page.ID, body.Identities[0].StatusPageID)
	assert.Equal(t, "OIDC-Seite", body.Identities[0].PageName)
	assert.Equal(t, "erika@idp.example.org", body.Identities[0].Email)
	assert.NotZero(t, body.Identities[0].ID)
	// Die schon verknüpfte Seite erscheint nicht noch einmal als verknüpfbar.
	assert.Equal(t, []uint{public.ID}, linkablePageIDs(body))
	assert.Equal(t, "Öffentlich", body.Linkable[0].PageName)
}

func TestMySSO_RoleOnOnePageAllowsOnlyThatPage(t *testing.T) {
	f := newOIDCFixture(t)
	f.ssoPage(t, "Öffentlich", "oeffentlich", true, func(p *models.StatusPage) { p.IsPublicIndexed = true })
	private := f.ssoPage(t, "Privat", "privat", true, func(p *models.StatusPage) { p.IsPrivate = true })
	require.NoError(t, f.db.Create(&models.UserStatusPageRole{UserID: f.user.ID, StatusPageID: private.ID, RoleID: f.user.RoleID}).Error)

	_, body := f.mySSO(t, f.user.ID)

	assert.Equal(t, []uint{private.ID}, linkablePageIDs(body))
}

func TestMySSO_RoleOnPageWithoutOIDC(t *testing.T) {
	f := newOIDCFixture(t)
	off := f.ssoPage(t, "Aus", "aus", false, nil)
	require.NoError(t, f.db.Create(&models.UserStatusPageRole{UserID: f.user.ID, StatusPageID: off.ID, RoleID: f.user.RoleID}).Error)

	_, body := f.mySSO(t, f.user.ID)

	assert.Empty(t, body.Linkable)
}

func TestMySSO_NothingLinkableForGlobalAdminsOrMultiPageRoles(t *testing.T) {
	f := newOIDCFixture(t)
	t.Run("Rollen auf zwei Seiten", func(t *testing.T) {
		f.addPageRole(t, f.user.ID, "seite-a")
		f.addPageRole(t, f.user.ID, "seite-b")
		_, body := f.mySSO(t, f.user.ID)
		assert.Empty(t, body.Linkable)
		assert.NotNil(t, body.Identities)
	})
	t.Run("Global Admin", func(t *testing.T) {
		admin := models.User{Email: "admin@example.org", IsGlobalAdmin: true}
		require.NoError(t, f.db.Create(&admin).Error)
		code, body := f.mySSO(t, admin.ID)
		require.Equal(t, http.StatusOK, code)
		assert.Empty(t, body.Linkable)
	})
}

func TestMySSO_UnknownUser(t *testing.T) {
	f := newOIDCFixture(t)
	code, _ := f.mySSO(t, 0)
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestDeleteMySSO_RemovesOwnIdentityOnly(t *testing.T) {
	f := newOIDCFixture(t)
	own := models.UserSSOIdentity{UserID: f.user.ID, StatusPageID: f.page.ID, Issuer: "https://idp", Subject: "s1"}
	require.NoError(t, f.db.Create(&own).Error)
	other := models.User{Email: "max@example.org", RoleID: f.user.RoleID}
	require.NoError(t, f.db.Create(&other).Error)
	foreign := models.UserSSOIdentity{UserID: other.ID, StatusPageID: f.page.ID, Issuer: "https://idp", Subject: "s2"}
	require.NoError(t, f.db.Create(&foreign).Error)
	del := func(id string) *httptest.ResponseRecorder {
		return serveWithParams(f.h.DeleteMySSOHandler, http.MethodDelete, "/", gin.Params{{Key: "id", Value: id}}, f.user.ID)
	}

	assert.Equal(t, http.StatusBadRequest, del("abc").Code)
	assert.Equal(t, http.StatusNotFound, del(strconv.FormatUint(uint64(foreign.ID), 10)).Code)
	assert.Equal(t, http.StatusNoContent, del(strconv.FormatUint(uint64(own.ID), 10)).Code)

	var ids []uint
	f.db.Model(&models.UserSSOIdentity{}).Pluck("id", &ids)
	assert.Equal(t, []uint{foreign.ID}, ids)
	var audit models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "auth.sso_unlinked").First(&audit).Error)
	assert.Equal(t, "erika@example.org", audit.TargetName)
	assert.Equal(t, strconv.FormatUint(uint64(f.user.ID), 10), audit.TargetID)
	assert.Equal(t, "provider=oidc page="+f.pageParam(), audit.Details)
}

func TestDeleteMySSO_UnknownUser(t *testing.T) {
	f := newOIDCFixture(t)
	w := serveWithParams(f.h.DeleteMySSOHandler, http.MethodDelete, "/", gin.Params{{Key: "id", Value: "1"}}, 0)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- Private Seiten: Seiten-ID und SSO-Art nur für Berechtigte ---

func TestPublicOIDC_PrivatePageAnswersLikeMissingPageWithoutPermission(t *testing.T) {
	f := newOIDCFixture(t)
	require.NoError(t, f.db.Model(&models.StatusPage{}).Where("id = ?", f.page.ID).Update("is_private", true).Error)
	stranger := models.User{Email: "fremd-sso@example.org"}
	require.NoError(t, f.db.Create(&stranger).Error)
	member := models.User{Email: "mitglied-sso@example.org"}
	require.NoError(t, f.db.Create(&member).Error)
	require.NoError(t, f.db.Create(&models.UserStatusPageRole{UserID: member.ID, StatusPageID: f.page.ID, RoleID: f.user.RoleID}).Error)

	slug := func(s string) gin.Params { return gin.Params{{Key: "slug", Value: s}} }
	missing := serveWithParams(f.h.GetPublicOIDCHandler, http.MethodGet, "/", slug("gibt-es-nicht"), 0)
	require.Equal(t, http.StatusNotFound, missing.Code)

	for name, uid := range map[string]uint{"anonym": 0, "Konto ohne Rolle": stranger.ID} {
		w := serveWithParams(f.h.GetPublicOIDCHandler, http.MethodGet, "/", slug("oidc-seite"), uid)
		assert.Equal(t, http.StatusNotFound, w.Code, name)
		assert.JSONEq(t, missing.Body.String(), w.Body.String(), name+": gleicher Body wie bei einer fehlenden Seite")
	}

	w := serveWithParams(f.h.GetPublicOIDCHandler, http.MethodGet, "/", slug("oidc-seite"), member.ID)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, float64(f.page.ID), decodeJSON(t, w)["page_id"])
}
