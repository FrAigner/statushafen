package handlers

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/crewjam/saml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Wechselt der Vertrauensanker des Seiten-SSO
// (SAML: Entity-ID oder Signaturzertifikat des IdP bzw. die Quelle der
// Metadaten; OIDC: der Issuer), gelten die bestehenden Verknüpfungen der Seite
// für dieses Protokoll nicht mehr. Sie werden gelöscht und das Löschen im
// Audit-Log vermerkt; die Konten verknüpfen neu.

type identityResetFixture struct {
	*samlConfigFixture
	other models.StatusPage
}

func newIdentityResetFixture(t *testing.T) *identityResetFixture {
	t.Helper()
	f := &identityResetFixture{samlConfigFixture: newSAMLConfigFixture(t)}
	f.other = models.StatusPage{Name: "Andere", Slug: "andere-reset"}
	require.NoError(t, f.db.Create(&f.other).Error)
	return f
}

// seed legt je eine SAML- und OIDC-Verknüpfung auf der Seite und eine
// SAML-Verknüpfung auf einer anderen Seite an.
func (f *identityResetFixture) seed(t *testing.T) {
	t.Helper()
	for _, ident := range []models.UserSSOIdentity{
		{UserID: 1, StatusPageID: f.page.ID, Protocol: models.SSOProtocolSAML, Issuer: "i", Subject: "s1"},
		{UserID: 2, StatusPageID: f.page.ID, Protocol: models.SSOProtocolOIDC, Issuer: "i", Subject: "s2"},
		{UserID: 3, StatusPageID: f.other.ID, Protocol: models.SSOProtocolSAML, Issuer: "i", Subject: "s3"},
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

// otherIdP: gleiche Entity-ID, anderes Signaturzertifikat bzw. andere
// Entity-ID.
func (f *identityResetFixture) idpVariant(t *testing.T, newCert bool, entityID string) string {
	t.Helper()
	ti := newSAMLTestIdP(t, func() *saml.EntityDescriptor { return nil })
	if newCert {
		_, other := samlTestKeys()
		ti.key, ti.cert = other, samlTestCert(t, other, "neuer-idp")
		ti.idp.Key, ti.idp.Certificate = ti.key, ti.cert
	}
	if entityID != "" {
		u, _ := url.Parse(entityID)
		ti.idp.MetadataURL = *u
	}
	return ti.metadataXML()
}

func TestUpdateSSO_SAMLIdentityReset(t *testing.T) {
	for name, tc := range map[string]struct {
		next  func(f *identityResetFixture, t *testing.T) map[string]interface{}
		reset bool
	}{
		"gleiche Metadaten": {func(f *identityResetFixture, _ *testing.T) map[string]interface{} {
			return map[string]interface{}{"saml_idp_metadata_xml": f.idp.metadataXML()}
		}, false},
		"neues Signaturzertifikat": {func(f *identityResetFixture, t *testing.T) map[string]interface{} {
			return map[string]interface{}{"saml_idp_metadata_xml": f.idpVariant(t, true, "")}
		}, true},
		"neue Entity-ID": {func(f *identityResetFixture, t *testing.T) map[string]interface{} {
			return map[string]interface{}{"saml_idp_metadata_xml": f.idpVariant(t, false, "https://idp2.example.test/metadata")}
		}, true},
		"Wechsel auf URL": {func(*identityResetFixture, *testing.T) map[string]interface{} {
			return map[string]interface{}{"saml_idp_metadata_url": "https://idp.example.test/metadata"}
		}, true},
		"Metadaten entfernt": {func(*identityResetFixture, *testing.T) map[string]interface{} {
			return map[string]interface{}{}
		}, true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newIdentityResetFixture(t)
			code, _ := f.put(t, map[string]interface{}{"saml_idp_metadata_xml": f.idp.metadataXML()})
			require.Equal(t, http.StatusOK, code)
			f.seed(t)

			code, _ = f.put(t, tc.next(f, t))

			require.Equal(t, http.StatusOK, code)
			audits := f.resetAudits(t, "sso.saml_identities_reset")
			if !tc.reset {
				assert.Equal(t, []uint{1, 2, 3}, f.remaining(t))
				assert.Empty(t, audits)
				return
			}
			assert.Equal(t, []uint{2, 3}, f.remaining(t))
			require.Len(t, audits, 1)
			assert.Equal(t, "count=1", audits[0].Details)
			assert.Equal(t, f.page.Name, audits[0].TargetName)
		})
	}
}

func TestUpdateSSO_SAMLURLChangeResets(t *testing.T) {
	f := newIdentityResetFixture(t)
	code, _ := f.put(t, map[string]interface{}{"saml_idp_metadata_url": "https://idp.example.test/a"})
	require.Equal(t, http.StatusOK, code)
	f.seed(t)

	code, _ = f.put(t, map[string]interface{}{"saml_idp_metadata_url": "https://idp.example.test/a"})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []uint{1, 2, 3}, f.remaining(t))

	code, _ = f.put(t, map[string]interface{}{"saml_idp_metadata_url": "https://idp.example.test/b"})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []uint{2, 3}, f.remaining(t))
}

func TestUpdateSSO_FirstSAMLConfigResetsNothing(t *testing.T) {
	f := newIdentityResetFixture(t)
	f.seed(t)
	code, _ := f.put(t, map[string]interface{}{"saml_idp_metadata_xml": f.idp.metadataXML()})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []uint{1, 2, 3}, f.remaining(t))
}

func TestUpdateSSO_OIDCIssuerChangeResets(t *testing.T) {
	f := newIdentityResetFixture(t)
	oidc := func(discovery string) map[string]interface{} {
		return map[string]interface{}{"oidc_discovery_url": discovery, "oidc_client_id": "c", "oidc_client_secret": "s"}
	}
	code, _ := f.put(t, oidc("https://idp.example.org/realms/a"))
	require.Equal(t, http.StatusOK, code)
	f.seed(t)

	// Derselbe Issuer, nur als voller Discovery-URL: keine Änderung.
	code, _ = f.put(t, oidc("https://idp.example.org/realms/a/.well-known/openid-configuration"))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []uint{1, 2, 3}, f.remaining(t))
	assert.Empty(t, f.resetAudits(t, "sso.oidc_identities_reset"))

	code, _ = f.put(t, oidc("https://idp.example.org/realms/b"))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []uint{1, 3}, f.remaining(t))
	audits := f.resetAudits(t, "sso.oidc_identities_reset")
	require.Len(t, audits, 1)
	assert.Equal(t, "count=1", audits[0].Details)
	assert.Empty(t, f.resetAudits(t, "sso.saml_identities_reset"))
}

func TestUpdateSSO_IdentityResetDBError(t *testing.T) {
	f := newIdentityResetFixture(t)
	code, _ := f.put(t, map[string]interface{}{"saml_idp_metadata_url": "https://idp.example.test/a"})
	require.Equal(t, http.StatusOK, code)
	require.NoError(t, f.db.Migrator().DropTable(&models.UserSSOIdentity{}))
	t.Cleanup(func() { _ = f.db.AutoMigrate(&models.UserSSOIdentity{}) })

	code, _ = f.put(t, map[string]interface{}{"saml_idp_metadata_url": "https://idp.example.test/b"})

	assert.Equal(t, http.StatusInternalServerError, code)
	var cfg models.StatusPageSSOConfig
	require.NoError(t, f.db.Where("status_page_id = ?", f.page.ID).First(&cfg).Error)
	assert.Equal(t, "https://idp.example.test/a", cfg.SAMLIdPMetadataURL, "Rollback")
}

func TestUpdateSSO_OIDCIdentityResetDBError(t *testing.T) {
	f := newIdentityResetFixture(t)
	oidc := func(discovery string) map[string]interface{} {
		return map[string]interface{}{"oidc_discovery_url": discovery, "oidc_client_id": "c", "oidc_client_secret": "s"}
	}
	code, _ := f.put(t, oidc("https://idp.example.org/a"))
	require.Equal(t, http.StatusOK, code)
	require.NoError(t, f.db.Migrator().DropTable(&models.UserSSOIdentity{}))
	t.Cleanup(func() { _ = f.db.AutoMigrate(&models.UserSSOIdentity{}) })

	code, _ = f.put(t, oidc("https://idp.example.org/b"))

	assert.Equal(t, http.StatusInternalServerError, code)
}

func TestSAMLTrustAnchor(t *testing.T) {
	assert.Equal(t, "", samlTrustAnchor("", ""))
	assert.Equal(t, "url:https://x", samlTrustAnchor("https://x", ""))
	// Unlesbares XML (nur durch Eingriff in die DB möglich) zählt als eigener Anker.
	assert.Equal(t, "xml:<kaputt", samlTrustAnchor("", "<kaputt"))
}
