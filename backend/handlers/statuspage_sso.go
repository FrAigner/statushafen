package handlers

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"

	"github.com/gin-gonic/gin"
)

// GetSSO — GET /api/admin/pages/:page_id/sso — liest SSO-Config pro Seite
func (h *Handler) GetSSO(c *gin.Context) {
	pageID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page_id", "code": "invalid_id"})
		return
	}

	var page models.StatusPage
	if err := h.DB.First(&page, pageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden", "code": "page_not_found"})
		return
	}

	var ssoConfig models.StatusPageSSOConfig
	if err := h.DB.Where("status_page_id = ?", pageID).First(&ssoConfig).Error; err != nil {
		// Keine Config noch vorhanden — Default zurückgeben (SSO deaktiviert)
		// dieselbe Antwortform wie mit Config, damit das
		// Admin-Formular alle Felder vorfindet.
		def := models.StatusPageSSOConfig{
			StatusPageID:       uint(pageID),
			OIDCScopes:         models.SSODefaultScopes,
			OIDCAttributeEmail: models.SSODefaultAttributeEmail,
			OIDCAttributeName:  models.SSODefaultAttributeName,
			SAMLAttributeEmail: models.SSODefaultAttributeEmail,
			SAMLAttributeName:  models.SSODefaultAttributeName,
			TestLoginEnabled:   true,
		}
		c.JSON(http.StatusOK, ssoPublicResponse(&def))
		return
	}

	c.JSON(http.StatusOK, ssoPublicResponse(&ssoConfig))
}

// ssoPublicResponse ersetzt die gespeicherte oidc_acs_url durch die zur
// Laufzeit gültige Redirect-URL: Die Spalte wird beim Speichern
// aus BASE_URL befüllt und veraltet, sobald sich BASE_URL ändert, der Login
// nutzt aber immer oidcRedirectURL. Angezeigt wird, was der Anbieter
// tatsächlich als Redirect-URI erhält.
//
// Ebenso zur Laufzeit die SAML-Werte, die der Admin beim IdP
// einträgt: Entity-ID, Metadaten-URL (identisch, siehe samlEntityID) und
// ACS-URL.
func ssoPublicResponse(cfg *models.StatusPageSSOConfig) map[string]interface{} {
	resp := cfg.ToPublicResponse()
	resp["oidc_acs_url"] = oidcRedirectURL(cfg.StatusPageID)
	resp["saml_entity_id"] = samlEntityID(cfg.StatusPageID)
	resp["saml_metadata_url"] = samlEntityID(cfg.StatusPageID)
	resp["saml_acs_url"] = samlACSURL(cfg.StatusPageID)
	return resp
}

// Größengrenzen des PUT. Metadaten eines einzelnen
// IdP liegen bei wenigen KB, Entra ID bei rund 10 KB.
const (
	ssoMaxRequestBytes      = 256 << 10
	samlMaxMetadataXMLBytes = 128 << 10
)

// samlRequest sind die SAML-Felder des PUT.
type samlRequest struct {
	SAMLEnabled        bool   `json:"saml_enabled"`
	SAMLIdPMetadataURL string `json:"saml_idp_metadata_url"`
	SAMLIdPMetadataXML string `json:"saml_idp_metadata_xml"`
	SAMLAttributeEmail string `json:"saml_attribute_email"`
	SAMLAttributeName  string `json:"saml_attribute_name"`
	// SAMLAllowUnspecifiedNameID: siehe models.StatusPageSSOConfig.
	SAMLAllowUnspecifiedNameID bool `json:"saml_allow_unspecified_nameid"`
}

// validate prüft die SAML-Felder vor dem Speichern und liefert
// die Fehlermeldung für 400, sonst "". Genau eine Quelle für die Metadaten,
// der URL nur mit https (der Abruf lehnt alles andere ohnehin ab, so sieht der
// Admin den Fehler gleich beim Speichern) und eingefügtes XML muss sich als
// IdP-Metadaten lesen lassen. Ob der URL erreichbar ist, prüft erst der Login
// (wie die OIDC-Discovery).
func (r *samlRequest) validate() string {
	r.SAMLIdPMetadataURL = strings.TrimSpace(r.SAMLIdPMetadataURL)
	r.SAMLIdPMetadataXML = strings.TrimSpace(r.SAMLIdPMetadataXML)
	if r.SAMLIdPMetadataURL != "" && r.SAMLIdPMetadataXML != "" {
		return "Entweder Metadaten-URL oder Metadaten-XML angeben, nicht beides"
	}
	if r.SAMLEnabled && r.SAMLIdPMetadataURL == "" && r.SAMLIdPMetadataXML == "" {
		return "SAML enabled erfordert IdP-Metadaten (URL oder XML)"
	}
	if r.SAMLIdPMetadataURL != "" && !strings.HasPrefix(r.SAMLIdPMetadataURL, "https://") {
		return "Die Metadaten-URL muss mit https:// beginnen"
	}
	if len(r.SAMLIdPMetadataXML) > samlMaxMetadataXMLBytes {
		return "IdP-Metadaten zu groß (höchstens 128 KB)"
	}
	if r.SAMLIdPMetadataXML != "" {
		if _, err := parseIdPMetadata([]byte(r.SAMLIdPMetadataXML)); err != nil {
			return "IdP-Metadaten ungültig: " + err.Error()
		}
	}
	return ""
}

// applySAML übernimmt die SAML-Felder. Das SP-Schlüsselpaar
// entsteht beim ersten Speichern und bleibt danach stehen, weil der IdP das
// Zertifikat kennt; nach einem DELETE gilt es wie das OIDC-Secret als
// verworfen und wird neu erzeugt.
func applySAML(cfg *models.StatusPageSSOConfig, r samlRequest) {
	cfg.SAMLEnabled = r.SAMLEnabled
	cfg.SAMLIdPMetadataURL = r.SAMLIdPMetadataURL
	cfg.SAMLIdPMetadataXML = r.SAMLIdPMetadataXML
	cfg.SAMLAttributeEmail = orDefault(r.SAMLAttributeEmail, models.SSODefaultAttributeEmail)
	cfg.SAMLAttributeName = orDefault(r.SAMLAttributeName, models.SSODefaultAttributeName)
	cfg.SAMLAllowUnspecifiedNameID = r.SAMLAllowUnspecifiedNameID
	if cfg.SAMLSPKey == "" || cfg.DeletedAt.Valid {
		cfg.SAMLSPKey, cfg.SAMLSPCertificate = generateSAMLKeyPair(cfg.StatusPageID)
	}
}

// UpdateSSO — PUT /api/admin/pages/:page_id/sso — speichert SSO-Config
func (h *Handler) UpdateSSO(c *gin.Context) {
	pageID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page_id", "code": "invalid_id"})
		return
	}

	var page models.StatusPage
	if err := h.DB.First(&page, pageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden", "code": "page_not_found"})
		return
	}

	var req struct {
		OIDCEnabled        bool   `json:"oidc_enabled"`
		OIDCDiscoveryURL   string `json:"oidc_discovery_url"`
		OIDCClientID       string `json:"oidc_client_id"`
		OIDCClientSecret   string `json:"oidc_client_secret"`
		OIDCScopes         string `json:"oidc_scopes"`
		OIDCAttributeEmail string `json:"oidc_attribute_email"`
		OIDCAttributeName  string `json:"oidc_attribute_name"`
		OIDCAttributeRole  string `json:"oidc_attribute_role"`
		TestLoginEnabled   bool   `json:"test_login_enabled"`
		samlRequest
	}

	// Die Anfrage trägt höchstens die Metadaten eines
	// IdP (samlMaxMetadataXMLBytes) und ein paar kurze Felder.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, ssoMaxRequestBytes)
	if err := c.ShouldBindJSON(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Anfrage zu groß", "code": "request_too_large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request", "code": "invalid_request"})
		return
	}
	if msg := req.validate(); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": "invalid_request"})
		return
	}

	var ssoConfig models.StatusPageSSOConfig
	var reset identityResets
	const oidcIncompleteMessage = "OIDC enabled erfordert Discovery URL, Client ID und Secret"
	errBadRequest := errors.New(oidcIncompleteMessage)
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		// Upsert auf die bestehende Zeile der Seite – auch eine
		// soft-gelöschte (Unscoped). Früher legte jeder PUT eine neue Zeile an
		// (Save ohne ID = INSERT, das Where wird dabei ignoriert); der Unique-Index
		// auf status_page_id ließ deshalb jeden zweiten PUT und jeden PUT nach
		// DELETE mit 500 scheitern. Die gelöschte Zeile wiederzuverwenden statt den
		// Index partiell zu machen hält genau eine Zeile pro Seite.
		if err := tx.Unscoped().Where("status_page_id = ?", pageID).Limit(1).Find(&ssoConfig).Error; err != nil {
			return err
		}

		// GET liefert das Secret nie aus. Ein leeres Secret behält
		// deshalb das gespeicherte, damit das Admin-Formular es nicht bei jedem
		// Speichern neu verlangt. Das Secret einer per DELETE entfernten Config
		// gilt nicht mehr – sonst würde es beim Reaktivieren still weiterleben.
		secret := req.OIDCClientSecret
		if secret == "" && !ssoConfig.DeletedAt.Valid {
			secret = ssoConfig.OIDCClientSecret
		}
		// Validierung: wenn OIDC enabled, dann muss Discovery URL + Credentials gesetzt sein
		if req.OIDCEnabled && (req.OIDCDiscoveryURL == "" || req.OIDCClientID == "" || secret == "") {
			return errBadRequest
		}

		// GORM überspringt bei INSERT Nullwerte für Spalten mit
		// default-Tag und speichert den DB-Default (test_login_enabled=false
		// wurde so zu true). Neue Zeilen deshalb nur mit der Seiten-ID anlegen
		// und alle Felder per UPDATE schreiben – das übernimmt Nullwerte. Kein
		// Pointer-Typ, damit Model, Schema und die übrigen Leser unverändert
		// bleiben; die Transaktion verhindert eine halbe Zeile.
		if ssoConfig.ID == 0 {
			ssoConfig = models.StatusPageSSOConfig{StatusPageID: uint(pageID)}
			if err := tx.Create(&ssoConfig).Error; err != nil {
				return err
			}
		}
		// Vertrauensanker vor dem Überschreiben merken.
		before := ssoConfig
		applySAML(&ssoConfig, req.samlRequest) // vor dem Reaktivieren, s. applySAML
		ssoConfig.DeletedAt = gorm.DeletedAt{} // reaktiviert eine per DELETE entfernte Config
		ssoConfig.OIDCEnabled = req.OIDCEnabled
		ssoConfig.OIDCDiscoveryURL = req.OIDCDiscoveryURL
		ssoConfig.OIDCClientID = req.OIDCClientID
		ssoConfig.OIDCClientSecret = secret
		// Leere Scopes/Attribute bekommen bewusst die Model-Defaults – beim
		// Anlegen wie beim Aktualisieren gleich (früher nur beim INSERT).
		ssoConfig.OIDCScopes = orDefault(req.OIDCScopes, models.SSODefaultScopes)
		ssoConfig.OIDCAttributeEmail = orDefault(req.OIDCAttributeEmail, models.SSODefaultAttributeEmail)
		ssoConfig.OIDCAttributeName = orDefault(req.OIDCAttributeName, models.SSODefaultAttributeName)
		ssoConfig.OIDCAttributeRole = req.OIDCAttributeRole
		ssoConfig.OIDCACSUrl = oidcRedirectURL(uint(pageID)) // nur informativ, s. ssoPublicResponse
		ssoConfig.TestLoginEnabled = req.TestLoginEnabled

		// Save mit ID = UPDATE aller Spalten (inkl. deleted_at = NULL, deshalb
		// Unscoped – sonst träfe das UPDATE die gelöschte Zeile nicht).
		if err := tx.Unscoped().Save(&ssoConfig).Error; err != nil {
			return err
		}
		var err error
		reset, err = resetChangedSSOIdentities(tx, before, ssoConfig)
		return err
	})
	if errors.Is(err, errBadRequest) {
		c.JSON(http.StatusBadRequest, gin.H{"error": oidcIncompleteMessage, "code": "oidc_incomplete"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Speichern der SSO-Config", "code": "server_error"})
		return
	}

	h.RecordAudit(c, "statuspage.sso_updated", "config", "StatusPage", strconv.FormatUint(pageID, 10), page.Name, "OIDC enabled: "+strconv.FormatBool(req.OIDCEnabled)+", SAML enabled: "+strconv.FormatBool(req.SAMLEnabled))
	for _, r := range []struct {
		action string
		count  int64
	}{{"sso.oidc_identities_reset", reset.oidc}, {"sso.saml_identities_reset", reset.saml}} {
		if r.count > 0 {
			h.RecordAudit(c, r.action, "config", "StatusPage", strconv.FormatUint(pageID, 10), page.Name, "count="+strconv.FormatInt(r.count, 10))
		}
	}
	c.JSON(http.StatusOK, ssoPublicResponse(&ssoConfig))
}

// DeleteSSO — DELETE /api/admin/pages/:page_id/sso — löscht SSO-Config
func (h *Handler) DeleteSSO(c *gin.Context) {
	pageID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page_id", "code": "invalid_id"})
		return
	}

	var page models.StatusPage
	if err := h.DB.First(&page, pageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Statusseite nicht gefunden", "code": "page_not_found"})
		return
	}

	if err := h.DB.Where("status_page_id = ?", pageID).Delete(&models.StatusPageSSOConfig{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Löschen", "code": "server_error"})
		return
	}

	h.RecordAudit(c, "statuspage.sso_deleted", "config", "StatusPage", strconv.FormatUint(pageID, 10), page.Name, "")
	c.JSON(http.StatusOK, gin.H{"message": "SSO-Config gelöscht"})
}

// identityResets zählt die gelöschten Verknüpfungen je Protokoll.
type identityResets struct{ oidc, saml int64 }

// resetChangedSSOIdentities): Wechselt der
// Vertrauensanker eines Protokolls, gehören die bestehenden Verknüpfungen der
// Seite zu einem anderen IdP bzw. Schlüssel und werden gelöscht. SAML: Quelle
// der Metadaten oder (bei eingefügtem XML) Entity-ID und Signaturzertifikate
// des IdP; OIDC: der Issuer. Eine erste Konfiguration (kein alter Anker)
// löscht nichts.
func resetChangedSSOIdentities(tx *gorm.DB, before, after models.StatusPageSSOConfig) (identityResets, error) {
	var r identityResets
	var err error
	oldIssuer := oidcIssuer(before.OIDCDiscoveryURL)
	if oldIssuer != "" && oldIssuer != oidcIssuer(after.OIDCDiscoveryURL) {
		if r.oidc, err = deleteSSOIdentities(tx, after.StatusPageID, models.SSOProtocolOIDC); err != nil {
			return r, err
		}
	}
	oldAnchor := samlTrustAnchor(before.SAMLIdPMetadataURL, before.SAMLIdPMetadataXML)
	if oldAnchor != "" && oldAnchor != samlTrustAnchor(after.SAMLIdPMetadataURL, after.SAMLIdPMetadataXML) {
		r.saml, err = deleteSSOIdentities(tx, after.StatusPageID, models.SSOProtocolSAML)
	}
	return r, err
}

func deleteSSOIdentities(tx *gorm.DB, pageID uint, protocol string) (int64, error) {
	res := tx.Where("status_page_id = ? AND protocol = ?", pageID, protocol).Delete(&models.UserSSOIdentity{})
	return res.RowsAffected, res.Error
}

// samlTrustAnchor beschreibt, wem die SAML-Verknüpfungen einer Seite
// vertrauen. Beim URL zählt der URL selbst (sein Inhalt wechselt bei
// Schlüsselwechseln laufend, das ist gewollt). Bei eingefügtem XML zählen
// Entity-ID und die Signaturzertifikate, nicht etwa validUntil oder
// Formatierung. Unlesbares XML (nur nach einem Eingriff in die DB) zählt
// über seinen Text.
func samlTrustAnchor(metadataURL, metadataXML string) string {
	if metadataURL != "" {
		return "url:" + metadataURL
	}
	if metadataXML == "" {
		return ""
	}
	md, err := parseIdPMetadata([]byte(metadataXML))
	if err != nil {
		return "xml:" + metadataXML
	}
	certs := []string{}
	for _, d := range md.IDPSSODescriptors {
		for _, kd := range d.KeyDescriptors {
			for _, c := range kd.KeyInfo.X509Data.X509Certificates {
				if kd.Use != "encryption" {
					certs = append(certs, strings.Join(strings.Fields(c.Data), ""))
				}
			}
		}
	}
	sort.Strings(certs)
	return "saml:" + md.EntityID + "|" + strings.Join(certs, ",")
}

// orDefault liefert def, wenn v leer ist.
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
