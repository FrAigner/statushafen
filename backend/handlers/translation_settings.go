package handlers

import (
	"net/http"
	"os"
	"strings"

	"github.com/fraigner/statushafen/externalstatus"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm/clause"

	"github.com/fraigner/statushafen/models"
)

// Systemeinstellungen für die automatische Übersetzung, nur
// Global Admin (system_settings). Der Schlüssel wird nie ausgegeben: die
// Antwort nennt nur api_key_set.

// translationSettingsView ist die Antwort von GET/PUT /settings/translation.
func (h *Handler) translationSettingsView() gin.H {
	st := h.storedTranslation()
	eff := h.effectiveTranslationConfig()
	_, _, err := h.translationProvider()
	return gin.H{
		"provider":           st.Provider,
		"url":                st.URL,
		"api_key_set":        st.APIKey != "",
		"env_url_set":        strings.TrimSpace(os.Getenv(envLibreTranslateURL)) != "",
		"env_api_key_set":    os.Getenv(envLibreTranslateKey) != "",
		"effective_provider": eff.Provider,
		"configured":         err == nil,
	}
}

// GetTranslationSettingsHandler liefert die Einstellungen ohne Schlüssel.
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

// UpdateTranslationSettingsHandler speichert Dienst, Adresse und Schlüssel.
// Jedes Feld ist optional (Pointer): fehlt api_key, bleibt der Schlüssel
// unverändert, ein leerer String löscht ihn. Das Audit-Log nennt nie den Wert.
func (h *Handler) UpdateTranslationSettingsHandler(c *gin.Context) {
	var req struct {
		Provider *string `json:"provider"`
		URL      *string `json:"url"`
		APIKey   *string `json:"api_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	if req.Provider == nil && req.URL == nil && req.APIKey == nil {
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

// TestTranslationSettingsHandler übersetzt einen festen Beispieltext mit der
// gespeicherten Konfiguration (Verbindungstest, POST /settings/translation/test).
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
