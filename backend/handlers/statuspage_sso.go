package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"

	"github.com/gin-gonic/gin"
)

// GetSSO — GET /api/admin/pages/:page_id/sso — reads the SSO config per page
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
		// No config exists yet — return the default (SSO disabled)
		// same response shape as with a config, so that the
		// admin form finds all fields.
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
		// Upsert onto the page's existing row – including a
		// soft-deleted one (Unscoped). Previously every PUT created a new row
		// (Save without ID = INSERT, the Where is ignored); the unique index
		// on status_page_id therefore made every second PUT and every PUT after
		// DELETE fail with 500. Reusing the deleted row instead of making the
		// index partial keeps exactly one row per page.
		if err := tx.Unscoped().Where("status_page_id = ?", pageID).Limit(1).Find(&ssoConfig).Error; err != nil {
			return err
		}

		// GET never returns the secret. An empty secret therefore keeps
		// the stored one, so that the admin form does not demand it anew on every
		// save. The secret of a config removed via DELETE
		// no longer applies – otherwise it would silently live on when reactivating.
		secret := req.OIDCClientSecret
		if secret == "" && !ssoConfig.DeletedAt.Valid {
			secret = ssoConfig.OIDCClientSecret
		}
		// Validation: if OIDC is enabled, discovery URL + credentials must be set
		if req.OIDCEnabled && (req.OIDCDiscoveryURL == "" || req.OIDCClientID == "" || secret == "") {
			return errBadRequest
		}

		// On INSERT, GORM skips zero values for columns with a
		// default tag and stores the DB default (test_login_enabled=false
		// thus became true). New rows are therefore created with only the page ID
		// and all fields written via UPDATE – which accepts zero values. Not a
		// pointer type, so that model, schema and the other readers stay unchanged;
		// the transaction prevents a half-written row.
		if ssoConfig.ID == 0 {
			ssoConfig = models.StatusPageSSOConfig{StatusPageID: uint(pageID)}
			if err := tx.Create(&ssoConfig).Error; err != nil {
				return err
			}
		}
		// Remember the trust anchor before overwriting.
		before := ssoConfig
		ssoConfig.DeletedAt = gorm.DeletedAt{} // reactivates a config removed via DELETE
		ssoConfig.OIDCEnabled = req.OIDCEnabled
		ssoConfig.OIDCDiscoveryURL = req.OIDCDiscoveryURL
		ssoConfig.OIDCClientID = req.OIDCClientID
		ssoConfig.OIDCClientSecret = secret
		// Empty scopes/attributes deliberately get the model defaults – the same on
		// creation as on update (previously only on INSERT).
		ssoConfig.OIDCScopes = orDefault(req.OIDCScopes, models.SSODefaultScopes)
		ssoConfig.OIDCAttributeEmail = orDefault(req.OIDCAttributeEmail, models.SSODefaultAttributeEmail)
		ssoConfig.OIDCAttributeName = orDefault(req.OIDCAttributeName, models.SSODefaultAttributeName)
		ssoConfig.OIDCAttributeRole = req.OIDCAttributeRole
		ssoConfig.OIDCACSUrl = oidcRedirectURL(uint(pageID)) // informational only, see ssoPublicResponse
		ssoConfig.TestLoginEnabled = req.TestLoginEnabled

		// Save with ID = UPDATE of all columns (incl. deleted_at = NULL, hence
		// Unscoped – otherwise the UPDATE would not hit the deleted row).
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

// DeleteSSO — DELETE /api/admin/pages/:page_id/sso — deletes the SSO config
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

// orDefault returns def if v is empty.
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
