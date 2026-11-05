package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/crewjam/saml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// SAML-Felder der SSO-Konfiguration je Seite.

type samlConfigFixture struct {
	db   *gorm.DB
	r    http.Handler
	page models.StatusPage
	path string
	idp  *samlTestIdP
}

func newSAMLConfigFixture(t *testing.T) *samlConfigFixture {
	t.Helper()
	t.Setenv("BASE_URL", "https://status.example.org")
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "SAML-Konfig", Slug: "saml-konfig"}
	require.NoError(t, db.Create(&page).Error)
	return &samlConfigFixture{
		db: db, r: setupSSORouter(NewHandler(db)), page: page,
		path: "/api/admin/pages/" + strconv.Itoa(int(page.ID)) + "/sso",
		idp:  newSAMLTestIdP(t, func() *saml.EntityDescriptor { return nil }),
	}
}

func (f *samlConfigFixture) put(t *testing.T, body map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	w := doSSORequest(f.r, "PUT", f.path, body)
	resp := map[string]interface{}{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	assert.NotContains(t, w.Body.String(), "PRIVATE KEY")
	return w.Code, resp
}

func (f *samlConfigFixture) saved(t *testing.T) models.StatusPageSSOConfig {
	t.Helper()
	var cfg models.StatusPageSSOConfig
	require.NoError(t, f.db.Where("status_page_id = ?", f.page.ID).First(&cfg).Error)
	return cfg
}

func TestUpdateSSO_SAMLSavesConfigAndCreatesKeyPair(t *testing.T) {
	f := newSAMLConfigFixture(t)
	metadata := f.idp.metadataXML()
	body := map[string]interface{}{
		"saml_enabled":          true,
		"saml_idp_metadata_xml": "  " + metadata + "\n",
		"saml_attribute_email":  "mail",
		"saml_attribute_name":   "displayName",
		// Security-Review: unspecified nur mit ausdrücklicher Freigabe.
		"saml_allow_unspecified_nameid": true,
	}

	code, resp := f.put(t, body)

	require.Equal(t, http.StatusOK, code, resp)
	cfg := f.saved(t)
	assert.True(t, cfg.SAMLEnabled)
	assert.Equal(t, metadata, cfg.SAMLIdPMetadataXML)
	assert.Equal(t, "mail", cfg.SAMLAttributeEmail)
	assert.Equal(t, "displayName", cfg.SAMLAttributeName)
	assert.True(t, cfg.SAMLAllowUnspecifiedNameID)
	assert.Equal(t, true, resp["saml_allow_unspecified_nameid"])
	_, _, err := parseSAMLKeyPair(cfg.SAMLSPKey, cfg.SAMLSPCertificate)
	require.NoError(t, err)

	id := strconv.Itoa(int(f.page.ID))
	assert.Equal(t, true, resp["saml_enabled"])
	assert.Equal(t, "https://status.example.org/api/auth/saml/"+id+"/metadata", resp["saml_entity_id"])
	assert.Equal(t, "https://status.example.org/api/auth/saml/"+id+"/metadata", resp["saml_metadata_url"])
	assert.Equal(t, "https://status.example.org/api/auth/saml/"+id+"/acs", resp["saml_acs_url"])
	assert.Equal(t, cfg.SAMLSPCertificate, resp["saml_sp_certificate"])
	assert.NotContains(t, resp, "saml_sp_key")

	// Ein weiteres Speichern behält das Schlüsselpaar (der IdP kennt das
	// Zertifikat schon).
	code, _ = f.put(t, body)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, cfg.SAMLSPKey, f.saved(t).SAMLSPKey)

	var audit models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "statuspage.sso_updated").First(&audit).Error)
	assert.Equal(t, "OIDC enabled: false, SAML enabled: true", audit.Details)
}

func TestUpdateSSO_SAMLDefaultsAndMetadataURL(t *testing.T) {
	f := newSAMLConfigFixture(t)
	code, resp := f.put(t, map[string]interface{}{"saml_idp_metadata_url": " https://idp.example.org/metadata "})
	require.Equal(t, http.StatusOK, code, resp)
	cfg := f.saved(t)
	assert.Equal(t, "https://idp.example.org/metadata", cfg.SAMLIdPMetadataURL)
	assert.Equal(t, "email", cfg.SAMLAttributeEmail)
	assert.Equal(t, "name", cfg.SAMLAttributeName)
	assert.False(t, cfg.SAMLEnabled)
	// Schlüssel entstehen schon beim ersten Speichern, damit der Admin die
	// SP-Metadaten vor dem Einschalten beim IdP hinterlegen kann.
	assert.NotEmpty(t, cfg.SAMLSPCertificate)
}

func TestUpdateSSO_SAMLValidation(t *testing.T) {
	f := newSAMLConfigFixture(t)
	for name, tc := range map[string]struct {
		body map[string]interface{}
		msg  string
	}{
		"aktiv ohne Metadaten": {map[string]interface{}{"saml_enabled": true}, "SAML enabled erfordert IdP-Metadaten (URL oder XML)"},
		"URL und XML":          {map[string]interface{}{"saml_idp_metadata_url": "https://idp/x", "saml_idp_metadata_xml": f.idp.metadataXML()}, "Entweder Metadaten-URL oder Metadaten-XML angeben, nicht beides"},
		"URL ohne https":       {map[string]interface{}{"saml_idp_metadata_url": "http://idp/x"}, "Die Metadaten-URL muss mit https:// beginnen"},
		"XML kaputt":           {map[string]interface{}{"saml_idp_metadata_xml": "<EntityDescriptor"}, "IdP-Metadaten ungültig"},
		"XML ohne IdP-Rolle":   {map[string]interface{}{"saml_idp_metadata_xml": `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="x"></EntityDescriptor>`}, "IdP-Metadaten ungültig"},
	} {
		t.Run(name, func(t *testing.T) {
			code, resp := f.put(t, tc.body)
			assert.Equal(t, http.StatusBadRequest, code)
			assert.Contains(t, resp["error"], tc.msg)
		})
	}
	var n int64
	f.db.Model(&models.StatusPageSSOConfig{}).Count(&n)
	assert.Zero(t, n)
}

func TestUpdateSSO_SAMLKeyPairRenewedAfterDelete(t *testing.T) {
	f := newSAMLConfigFixture(t)
	code, _ := f.put(t, map[string]interface{}{})
	require.Equal(t, http.StatusOK, code)
	first := f.saved(t).SAMLSPKey
	require.Equal(t, http.StatusOK, doSSORequest(f.r, "DELETE", f.path, nil).Code)

	code, _ = f.put(t, map[string]interface{}{})
	require.Equal(t, http.StatusOK, code)
	assert.NotEqual(t, first, f.saved(t).SAMLSPKey)
}

func TestGetSSO_SAMLDefaults(t *testing.T) {
	f := newSAMLConfigFixture(t)
	w := doSSORequest(f.r, "GET", f.path, nil)
	resp := map[string]interface{}{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	id := strconv.Itoa(int(f.page.ID))
	assert.Equal(t, false, resp["saml_enabled"])
	assert.Equal(t, "", resp["saml_idp_metadata_url"])
	assert.Equal(t, "", resp["saml_idp_metadata_xml"])
	assert.Equal(t, "email", resp["saml_attribute_email"])
	assert.Equal(t, "name", resp["saml_attribute_name"])
	assert.Equal(t, "", resp["saml_sp_certificate"])
	assert.Equal(t, false, resp["saml_allow_unspecified_nameid"])
	assert.Equal(t, "https://status.example.org/api/auth/saml/"+id+"/acs", resp["saml_acs_url"])
}

// Größe von Anfrage und eingefügtem XML begrenzt.
func TestUpdateSSO_SAMLSizeLimits(t *testing.T) {
	f := newSAMLConfigFixture(t)
	w := doSSORequest(f.r, "PUT", f.path, map[string]interface{}{"saml_idp_metadata_xml": strings.Repeat("x", 300<<10)})
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)

	code, resp := f.put(t, map[string]interface{}{"saml_idp_metadata_xml": "<a>" + strings.Repeat("x", samlMaxMetadataXMLBytes) + "</a>"})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "IdP-Metadaten zu groß (höchstens 128 KB)", resp["error"])

	var n int64
	f.db.Model(&models.StatusPageSSOConfig{}).Count(&n)
	assert.Zero(t, n)
}
