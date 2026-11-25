package handlers

import (
	"errors"
	"net/http"
	"strconv"

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
			TestLoginEnabled:   true,
		}
		c.JSON(http.StatusOK, ssoPublicResponse(&def))
		return
	}

	c.JSON(http.StatusOK, ssoPublicResponse(&ssoConfig))
}

// ssoPublicResponse replaces the stored oidc_acs_url with the redirect URL
// valid at runtime: the column is filled from BASE_URL when saving and goes
// stale once BASE_URL changes, while the login always uses oidcRedirectURL.
// It shows what the provider actually receives as redirect URI.
func ssoPublicResponse(cfg *models.StatusPageSSOConfig) map[string]interface{} {
	resp := cfg.ToPublicResponse()
	resp["oidc_acs_url"] = oidcRedirectURL(cfg.StatusPageID)
	return resp
}

// ssoMaxRequestBytes limits the PUT body (short fields only).
const ssoMaxRequestBytes = 64 << 10

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
	}

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

	var ssoConfig models.StatusPageSSOConfig
	var reset int64
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

	h.RecordAudit(c, "statuspage.sso_updated", "config", "StatusPage", strconv.FormatUint(pageID, 10), page.Name, "OIDC enabled: "+strconv.FormatBool(req.OIDCEnabled))
	if reset > 0 {
		h.RecordAudit(c, "sso.oidc_identities_reset", "config", "StatusPage", strconv.FormatUint(pageID, 10), page.Name, "count="+strconv.FormatInt(reset, 10))
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

// resetChangedSSOIdentities: when the issuer changes, the page's existing
// OIDC links belong to another IdP and are deleted. A first configuration
// (no previous issuer) deletes nothing.
func resetChangedSSOIdentities(tx *gorm.DB, before, after models.StatusPageSSOConfig) (int64, error) {
	oldIssuer := oidcIssuer(before.OIDCDiscoveryURL)
	if oldIssuer == "" || oldIssuer == oidcIssuer(after.OIDCDiscoveryURL) {
		return 0, nil
	}
	res := tx.Where("status_page_id = ? AND protocol = ?", after.StatusPageID, models.SSOProtocolOIDC).Delete(&models.UserSSOIdentity{})
	return res.RowsAffected, res.Error
}

// orDefault liefert def, wenn v leer ist.
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
