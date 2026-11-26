package handlers

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	uiOverrideScopeGlobal = "global"
	uiOverrideScopePage   = "page"
)

// scopeUiOverrideQuery applies the WHERE clause for the global (statusPageID nil)
// or page scope to an existing query - shared logic for
// upsertUiOverride/deleteUiOverride/listUiOverrides instead of triplicated.
func scopeUiOverrideQuery(query *gorm.DB, statusPageID *uint) *gorm.DB {
	if statusPageID == nil {
		return query.Where("status_page_id IS NULL")
	}
	return query.Where("status_page_id = ?", *statusPageID)
}

func uiOverrideScopeLabel(statusPageID *uint) string {
	if statusPageID != nil {
		return uiOverrideScopePage
	}
	return uiOverrideScopeGlobal
}

// UiTranslationOverride: global (Global Admin) and
// per-page (Page Admin, own page only) editable overrides for
// app UI texts (nav, buttons, form labels - the text statically compiled from
// frontend/src/i18n/locales/{de,en}.json). Unlike the
// content translation models (IncidentTranslation etc.), there is no
// draft/confirm workflow here - an override is either set or
// not, no multilingual editorial process.
//
// No server-side check against the known key set: the frontend
// already knows all valid keys from its own bundle (de.json/en.json)
// and builds the editor list from them - the backend is a
// generic (scope, locale, key) -> value store.

// uiOverrideKeyPattern allows only segments from [a-zA-Z0-9_-], separated by
// single dots. The segments __proto__, constructor and
// prototype are additionally forbidden because the frontend builds the key into a
// nested object and Object.prototype would otherwise be polluted.
var uiOverrideKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+(\.[a-zA-Z0-9_-]+)*$`)

func isValidUiOverrideKey(key string) bool {
	if !uiOverrideKeyPattern.MatchString(key) {
		return false
	}
	for _, part := range strings.Split(key, ".") {
		switch part {
		case "__proto__", "constructor", "prototype":
			return false
		}
	}
	return true
}

func overridesToMap(overrides []models.UiTranslationOverride) map[string]string {
	result := make(map[string]string, len(overrides))
	for _, o := range overrides {
		result[o.Key] = o.Value
	}
	return result
}

// upsertUiOverride creates or updates an override - shared
// logic for the global (statusPageID nil) and the page scope.
func (h *Handler) upsertUiOverride(c *gin.Context, statusPageID *uint) {
	var req struct {
		LanguageCode string `json:"language_code"`
		Key          string `json:"key"`
		Value        string `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	languageCode := strings.TrimSpace(req.LanguageCode)
	key := strings.TrimSpace(req.Key)
	value := req.Value
	if languageCode == "" || key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "language_code und key dürfen nicht leer sein", "code": "override_fields_required"})
		return
	}
	if !isValidUiOverrideKey(key) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültiger key", "code": "override_key_invalid"})
		return
	}
	// An empty value used to be stored as override "" and
	// deleted the text (in the global scope for all visitors). Deliberately 400
	// instead of implicit deletion: PUT means "set this text", an empty field
	// is almost always a mistake, and a silent delete would discard an
	// existing override just as unnoticed. Resetting to the
	// default text works only explicitly via DELETE for exactly this entry.
	if strings.TrimSpace(value) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "value darf nicht leer sein; zum Zurücksetzen DELETE verwenden", "code": "override_value_empty"})
		return
	}

	query := scopeUiOverrideQuery(h.DB.Where("language_code = ? AND key = ?", languageCode, key), statusPageID)

	var existing models.UiTranslationOverride
	err := query.First(&existing).Error
	if err == nil {
		existing.Value = value
		if err := h.DB.Save(&existing).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update override", "code": "server_error"})
			return
		}
	} else {
		created := models.UiTranslationOverride{
			StatusPageID: statusPageID,
			LanguageCode: languageCode,
			Key:          key,
			Value:        value,
		}
		if err := h.DB.Create(&created).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create override", "code": "server_error"})
			return
		}
	}

	h.RecordAudit(c, "ui_override.upserted", "config", "UiTranslationOverride", key, languageCode, uiOverrideScopeLabel(statusPageID))
	c.JSON(http.StatusOK, gin.H{"key": key, "language_code": languageCode, "value": value})
}

// deleteUiOverride deletes an override again (revert to the
// bundle default) - idempotent, a delete without an existing override is
// not an error, since "no override" is already the desired end state.
func (h *Handler) deleteUiOverride(c *gin.Context, statusPageID *uint) {
	languageCode := strings.TrimSpace(c.Query("language_code"))
	key := strings.TrimSpace(c.Query("key"))
	if languageCode == "" || key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "language_code und key sind erforderlich", "code": "override_fields_required"})
		return
	}

	query := scopeUiOverrideQuery(h.DB.Where("language_code = ? AND key = ?", languageCode, key), statusPageID)
	if err := query.Delete(&models.UiTranslationOverride{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete override", "code": "server_error"})
		return
	}

	h.RecordAudit(c, "ui_override.deleted", "config", "UiTranslationOverride", key, languageCode, uiOverrideScopeLabel(statusPageID))
	c.JSON(http.StatusOK, gin.H{"key": key, "language_code": languageCode})
}

func (h *Handler) listUiOverrides(c *gin.Context, statusPageID *uint) {
	languageCode := strings.TrimSpace(c.Query("language_code"))
	if languageCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "language_code ist erforderlich", "code": "override_language_required"})
		return
	}

	query := scopeUiOverrideQuery(h.DB.Where("language_code = ?", languageCode), statusPageID)
	var overrides []models.UiTranslationOverride
	if err := query.Find(&overrides).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch overrides", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, overridesToMap(overrides))
}

// GetPublicUiOverridesHandler returns the global overrides without auth -
// anonymous visitors too (home page, public status pages) should see the
// globally customized texts. Deliberately contains no page overrides,
// those apply only within the admin area of the respective page (see
// frontend/src/layouts/AdminLayout.vue).
func (h *Handler) GetPublicUiOverridesHandler(c *gin.Context) {
	h.listUiOverrides(c, nil)
}

// GetGlobalUiOverridesHandler is the admin variant of
// GetPublicUiOverridesHandler (Global Admin, identical data - for the
// editor view).
func (h *Handler) GetGlobalUiOverridesHandler(c *gin.Context) {
	h.listUiOverrides(c, nil)
}

func (h *Handler) PutGlobalUiOverrideHandler(c *gin.Context) {
	h.upsertUiOverride(c, nil)
}

func (h *Handler) DeleteGlobalUiOverrideHandler(c *gin.Context) {
	h.deleteUiOverride(c, nil)
}

func (h *Handler) GetPageUiOverridesHandler(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}
	id := uint(pageID)
	h.listUiOverrides(c, &id)
}

func (h *Handler) PutPageUiOverrideHandler(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}
	id := uint(pageID)
	h.upsertUiOverride(c, &id)
}

func (h *Handler) DeletePageUiOverrideHandler(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}
	id := uint(pageID)
	h.deleteUiOverride(c, &id)
}
