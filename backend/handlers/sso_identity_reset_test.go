package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// When the issuer of a page SSO changes, the page's existing OIDC links no
// longer apply. They are deleted and the deletion is audited; the accounts
// link again. Old SAML rows and links of other pages stay untouched.

type identityResetFixture struct {
	db    *gorm.DB
	r     http.Handler
	page  models.StatusPage
	other models.StatusPage
	path  string
}

func newIdentityResetFixture(t *testing.T) *identityResetFixture {
	t.Helper()
	t.Setenv("BASE_URL", "https://status.example.org")
	db := testutils.SetupTestDB()
	f := &identityResetFixture{db: db, r: setupSSORouter(NewHandler(db))}
	f.page = models.StatusPage{Name: "SSO-Konfig", Slug: "sso-konfig"}
	require.NoError(t, db.Create(&f.page).Error)
	f.other = models.StatusPage{Name: "Andere", Slug: "andere-reset"}
	require.NoError(t, db.Create(&f.other).Error)
	f.path = "/api/admin/pages/" + strconv.Itoa(int(f.page.ID)) + "/sso"
	return f
}

func (f *identityResetFixture) put(t *testing.T, body map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	w := doSSORequest(f.r, "PUT", f.path, body)
	resp := map[string]interface{}{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return w.Code, resp
}

// seed creates an old SAML link and an OIDC link on the page and an OIDC link
// on another page.
func (f *identityResetFixture) seed(t *testing.T) {
	t.Helper()
	for _, ident := range []models.UserSSOIdentity{
		{UserID: 1, StatusPageID: f.page.ID, Protocol: "saml", Issuer: "i", Subject: "s1"},
		{UserID: 2, StatusPageID: f.page.ID, Protocol: models.SSOProtocolOIDC, Issuer: "i", Subject: "s2"},
		{UserID: 3, StatusPageID: f.other.ID, Protocol: models.SSOProtocolOIDC, Issuer: "i", Subject: "s3"},
	} {
		require.NoError(t, f.db.Create(&ident).Error)
	}
}

func (f *identityResetFixture) remaining(t *testing.T) []uint {
	t.Helper()
	var users []uint
	require.NoError(t, f.db.Model(&models.UserSSOIdentity{}).Order("user_id").Pluck("user_id", &users).Error)
	return users
}

func (f *identityResetFixture) resetAudits(t *testing.T, action string) []models.AuditLog {
	t.Helper()
	var logs []models.AuditLog
	require.NoError(t, f.db.Where("action = ?", action).Find(&logs).Error)
	return logs
}

func oidcConfigBody(discovery string) map[string]interface{} {
	return map[string]interface{}{"oidc_discovery_url": discovery, "oidc_client_id": "c", "oidc_client_secret": "s"}
}

func TestUpdateSSO_FirstOIDCConfigResetsNothing(t *testing.T) {
	f := newIdentityResetFixture(t)
	f.seed(t)
	code, _ := f.put(t, oidcConfigBody("https://idp.example.org/realms/a"))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []uint{1, 2, 3}, f.remaining(t))
}

func TestUpdateSSO_OIDCIssuerChangeResets(t *testing.T) {
	f := newIdentityResetFixture(t)
	code, _ := f.put(t, oidcConfigBody("https://idp.example.org/realms/a"))
	require.Equal(t, http.StatusOK, code)
	f.seed(t)

	// Same issuer, only written as the full discovery URL: no change.
	code, _ = f.put(t, oidcConfigBody("https://idp.example.org/realms/a/.well-known/openid-configuration"))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []uint{1, 2, 3}, f.remaining(t))
	assert.Empty(t, f.resetAudits(t, "sso.oidc_identities_reset"))

	code, _ = f.put(t, oidcConfigBody("https://idp.example.org/realms/b"))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []uint{1, 3}, f.remaining(t), "only the page's OIDC link is removed")
	audits := f.resetAudits(t, "sso.oidc_identities_reset")
	require.Len(t, audits, 1)
	assert.Equal(t, "count=1", audits[0].Details)
	assert.Equal(t, f.page.Name, audits[0].TargetName)
}

func TestUpdateSSO_OIDCIdentityResetDBError(t *testing.T) {
	f := newIdentityResetFixture(t)
	code, _ := f.put(t, oidcConfigBody("https://idp.example.org/a"))
	require.Equal(t, http.StatusOK, code)
	require.NoError(t, f.db.Migrator().DropTable(&models.UserSSOIdentity{}))
	t.Cleanup(func() { _ = f.db.AutoMigrate(&models.UserSSOIdentity{}) })

	code, _ = f.put(t, oidcConfigBody("https://idp.example.org/b"))

	assert.Equal(t, http.StatusInternalServerError, code)
	var cfg models.StatusPageSSOConfig
	require.NoError(t, f.db.Where("status_page_id = ?", f.page.ID).First(&cfg).Error)
	assert.Equal(t, "https://idp.example.org/a", cfg.OIDCDiscoveryURL, "rolled back")
}

// SAML fields are gone from the admin API: PUT ignores them (no error either),
// GET and PUT no longer return saml_* keys.
func TestSSOConfig_NoSAMLFieldsAnyMore(t *testing.T) {
	f := newIdentityResetFixture(t)
	body := oidcConfigBody("https://idp.example.org/a")
	body["saml_enabled"] = true
	body["saml_idp_metadata_url"] = "https://idp.example.org/metadata"
	code, resp := f.put(t, body)
	require.Equal(t, http.StatusOK, code, resp)
	w := doSSORequest(f.r, "GET", f.path, nil)
	require.Equal(t, http.StatusOK, w.Code)
	got := map[string]interface{}{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	for _, m := range []map[string]interface{}{resp, got} {
		for k := range m {
			assert.NotContains(t, k, "saml", "key %q", k)
		}
	}
}
