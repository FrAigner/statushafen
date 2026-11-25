package handlers

import (
	"net/http"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Content languages: only German and English (database.ContentLanguageCodes).
// Status page assignments and translations in other languages may still exist in
// the database (former catalog with 30 languages); every read and write path for
// content goes through these helpers so that such rows are neither offered nor
// delivered.

// codeLanguageNotSupported: content languages are only de and en.
const codeLanguageNotSupported = "language_not_supported"

const (
	// settingTranslationEnabled switches automatic translation and the second
	// content language on. A missing row means off (translationEnabledDefault): only
	// an explicit "true" switches it on, so upgraded installations start with it off.
	settingTranslationEnabled = "translation.enabled"
	translationEnabledDefault = false
)

// translationEnabled reports whether additional content languages and automatic
// translation are switched on.
func (h *Handler) translationEnabled() bool {
	return h.getSystemSettingBool(settingTranslationEnabled, translationEnabledDefault)
}

// GetPublicTranslationEnabledHandler reports without authentication whether the
// switch is on (GET /api/public/translation/enabled). Admin views of page admins
// need it, and GET /settings/translation is reserved for global admins. Modelled on
// GetPublicOrdersEnabledHandler.
func (h *Handler) GetPublicTranslationEnabledHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"enabled": h.translationEnabled()})
}

// pageContentLanguages returns the languages of a status page that are content
// languages, ordered by code, with the catalog entry preloaded. With the switch
// off only the page's default language is returned; the other rows stay stored.
func (h *Handler) pageContentLanguages(pageID uint) ([]models.StatusPageLanguage, error) {
	rows := []models.StatusPageLanguage{}
	q := h.DB.Preload("Language").Where("status_page_id = ? AND language_code IN ?", pageID, database.ContentLanguageCodes)
	if !h.translationEnabled() {
		q = q.Where("is_default = ?", true)
	}
	err := q.Order("language_code asc").Find(&rows).Error
	return rows, err
}

// deliveredTranslationCodes returns the language codes whose stored translations
// may be read or delivered (admin lists and public endpoints). With the switch off
// there are none: content is shown in the default language only.
func (h *Handler) deliveredTranslationCodes() []string {
	if !h.translationEnabled() {
		return nil
	}
	return database.ContentLanguageCodes
}

// abortTranslationDisabled answers 409 translation_disabled: the request needs a
// second content language or automatic translation, which are switched off.
func abortTranslationDisabled(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "Automatic translation and additional content languages are switched off", "code": "translation_disabled"})
}

// RequireTranslationEnabled guards every route that writes translations or asks
// for an automatic translation: with the switch off they answer 409
// translation_disabled and nothing is stored. Registered after the permission and
// tag guards, so callers without access still get 401/403 first.
func (h *Handler) RequireTranslationEnabled() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.translationEnabled() {
			abortTranslationDisabled(c)
			return
		}
		c.Next()
	}
}

// requireTemplateLanguageEnabled lets notification templates in a language other
// than the page default be written only while the switch is on (else 409).
func (h *Handler) requireTemplateLanguageEnabled(c *gin.Context, pageID uint, language string) bool {
	if h.translationEnabled() || language == h.pageDefaultLanguageCode(pageID) {
		return true
	}
	abortTranslationDisabled(c)
	return false
}

// setPageDefaultLanguage makes code the default language of the page without
// touching the other assignments (switch off: they stay stored and reappear once
// the switch is on). The old default is cleared first because of the "exactly one
// default" unique index; a missing row for code is created.
func setPageDefaultLanguage(tx *gorm.DB, pageID uint, code string) error {
	if err := tx.Model(&models.StatusPageLanguage{}).Where("status_page_id = ? AND is_default = ?", pageID, true).Update("is_default", false).Error; err != nil {
		return err
	}
	var row models.StatusPageLanguage
	if err := tx.Where("status_page_id = ? AND language_code = ?", pageID, code).Limit(1).Find(&row).Error; err != nil {
		return err
	}
	if row.ID == 0 {
		return tx.Create(&models.StatusPageLanguage{StatusPageID: pageID, LanguageCode: code, IsDefault: true}).Error
	}
	return tx.Model(&row).Update("is_default", true).Error
}

// fixedStaffMailLang returns the language (de/en) of mails to internal users when
// the switch is off: the page default, as for subscribers. With the switch on it is
// empty and each user gets the mail in their UI language (staffMailLang).
func (h *Handler) fixedStaffMailLang(pageID uint) string {
	if h.translationEnabled() {
		return ""
	}
	return outageMailLang(h.pageDefaultLanguageCode(pageID))
}
