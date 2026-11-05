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

// scopeUiOverrideQuery wendet auf eine bestehende Query die WHERE-Klausel für
// den globalen (statusPageID nil) bzw. Seiten-Scope an - gemeinsame Logik für
// upsertUiOverride/deleteUiOverride/listUiOverrides statt dreifach dupliziert.
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

// UiTranslationOverride: globale (Global Admin) und
// pro-Seite (Page Admin, nur eigene Seite) editierbare Overrides für
// App-UI-Texte (Nav, Buttons, Formularlabels - der statisch aus
// frontend/src/i18n/locales/{de,en}.json kompilierte Text). Anders als die
// Content-Translation-Modelle (IncidentTranslation etc.) gibt es hier
// keinen Draft/Confirm-Workflow - ein Override ist entweder gesetzt oder
// nicht, kein mehrsprachiger Redaktionsprozess.
//
// Kein serverseitiger Abgleich gegen die bekannte Key-Menge: das Frontend
// kennt aus dem eigenen Bundle (de.json/en.json) bereits alle gültigen
// Keys und erzeugt daraus die Editor-Liste - das Backend ist ein
// generischer (scope, locale, key) -> value Store.

// uiOverrideKeyPattern erlaubt nur Segmente aus [a-zA-Z0-9_-], getrennt durch
// einzelne Punkte. Die Segmente __proto__, constructor und
// prototype sind zusätzlich verboten, weil das Frontend den Key zu einem
// verschachtelten Objekt aufbaut und sonst Object.prototype verschmutzt würde.
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

// upsertUiOverride legt einen Override an oder aktualisiert ihn - gemeinsame
// Logik für den globalen (statusPageID nil) und den Seiten-Scope.
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
	// Ein leerer Wert wurde bisher als Override "" gespeichert und
	// hat den Text (im globalen Scope für alle Besucher) gelöscht. Bewusst 400
	// statt implizitem Löschen: PUT heißt "diesen Text setzen", ein leeres Feld
	// ist fast immer ein Versehen, und ein stilles Delete würde einen
	// bestehenden Override ebenso unbemerkt verwerfen. Zurücksetzen auf den
	// Standardtext geht nur explizit per DELETE für genau diesen Eintrag.
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

// deleteUiOverride löscht einen Override wieder (Revert auf den
// Bundle-Default) - idempotent, ein Delete ohne vorhandenen Override ist
// kein Fehler, da "kein Override" bereits der gewünschte Endzustand ist.
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

// GetPublicUiOverridesHandler liefert die globalen Overrides ohne Auth -
// auch anonyme Besucher (Startseite, öffentliche Statusseiten) sollen die
// global angepassten Texte sehen. Enthält bewusst keine Seiten-Overrides,
// die gelten nur innerhalb des Admin-Bereichs der jeweiligen Seite (siehe
// frontend/src/layouts/AdminLayout.vue).
func (h *Handler) GetPublicUiOverridesHandler(c *gin.Context) {
	h.listUiOverrides(c, nil)
}

// GetGlobalUiOverridesHandler ist die Admin-Variante von
// GetPublicUiOverridesHandler (Global Admin, identische Daten - für die
// Editor-Ansicht).
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
