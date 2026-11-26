package handlers

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/externalstatus"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm/clause"

	"github.com/fraigner/statushafen/models"
)

// System settings for automatic translation, global
// admin only (system_settings). The key is never returned: the
// response only reports api_key_set.

// translationSettingsView is the response of GET/PUT /settings/translation.
func (h *Handler) translationSettingsView() gin.H {
	st := h.storedTranslation()
	eff := h.effectiveTranslationConfig()
	_, _, err := h.translationProvider()
	return gin.H{
		"enabled":            h.translationEnabled(),
		"provider":           st.Provider,
		"url":                st.URL,
		"api_key_set":        st.APIKey != "",
		"env_url_set":        strings.TrimSpace(os.Getenv(envLibreTranslateURL)) != "",
		"env_api_key_set":    os.Getenv(envLibreTranslateKey) != "",
		"effective_provider": eff.Provider,
		"configured":         err == nil,
	}
}

// GetTranslationSettingsHandler returns the settings without the key.
func (h *Handler) GetTranslationSettingsHandler(c *gin.Context) {
	c.JSON(http.StatusOK, h.translationSettingsView())
}

func (h *Handler) setSystemSettingString(key, value string) error {
	if value == "" {
		return h.DB.Unscoped().Where("key = ?", key).Delete(&models.SystemSetting{}).Error
	}
	return h.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at", "deleted_at"}),
	}).Create(&models.SystemSetting{Key: key, Value: value}).Error
}

// UpdateTranslationSettingsHandler stores service, address and key.
// Every field is optional (pointer): if api_key is missing, the key stays
// unchanged, an empty string deletes it. The audit log never names the value.
func (h *Handler) UpdateTranslationSettingsHandler(c *gin.Context) {
	var req struct {
		Enabled  *bool   `json:"enabled"`
		Provider *string `json:"provider"`
		URL      *string `json:"url"`
		APIKey   *string `json:"api_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	if req.Enabled == nil && req.Provider == nil && req.URL == nil && req.APIKey == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "At least one field is required", "code": "settings_field_required"})
		return
	}
	if req.Provider != nil {
		switch p := strings.TrimSpace(*req.Provider); p {
		case "", providerLibreTranslate, providerDeepL:
			*req.Provider = p
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown translation provider", "code": "translation_provider_invalid"})
			return
		}
	}
	if req.URL != nil {
		u := strings.TrimSpace(*req.URL)
		if u != "" {
			base, err := externalstatus.ValidateBaseURL(u)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid translation URL", "code": "translation_url_invalid"})
				return
			}
			u = base
		}
		*req.URL = u
	}
	// The on/off switch is stored like the other bool settings (canonical "true"/"false").
	if req.Enabled != nil {
		if err := h.setSystemSettingBool(settingTranslationEnabled, *req.Enabled); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update settings", "code": "server_error"})
			return
		}
		h.RecordAudit(c, "system_setting.updated", "config", "SystemSetting", settingTranslationEnabled, "enabled", strconv.FormatBool(*req.Enabled))
	}
	changes := []struct {
		key, field string
		value      *string
		secret     bool
	}{
		{settingTranslationProvider, "provider", req.Provider, false},
		{settingTranslationURL, "url", req.URL, false},
		{settingTranslationAPIKey, "api_key", req.APIKey, true},
	}
	for _, ch := range changes {
		if ch.value == nil {
			continue
		}
		if err := h.setSystemSettingString(ch.key, *ch.value); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update settings", "code": "server_error"})
			return
		}
		detail := *ch.value
		if ch.secret {
			detail = "gesetzt"
			if *ch.value == "" {
				detail = "entfernt"
			}
		}
		h.RecordAudit(c, "system_setting.updated", "config", "SystemSetting", ch.key, ch.field, detail)
	}
	c.JSON(http.StatusOK, h.translationSettingsView())
}

// TestTranslationSettingsHandler translates a fixed sample text with the
// saved configuration (connection test, POST /settings/translation/test).
func (h *Handler) TestTranslationSettingsHandler(c *gin.Context) {
	provider, name, err := h.translationProvider()
	if err != nil {
		translationErrorResponse(c, err)
		return
	}
	out, err := provider.Translate("Hello", "en", "de")
	if err != nil {
		translationErrorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "provider": name, "sample": out})
}
