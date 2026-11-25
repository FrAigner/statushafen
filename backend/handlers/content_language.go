package handlers

import (
	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"
)

// Content languages: only German and English (database.ContentLanguageCodes).
// Status page assignments and translations in other languages may still exist in
// the database (former catalog with 30 languages); every read and write path for
// content goes through these helpers so that such rows are neither offered nor
// delivered.

// codeLanguageNotSupported: content languages are only de and en.
const codeLanguageNotSupported = "language_not_supported"

// pageContentLanguages returns the languages of a status page that are content
// languages, ordered by code, with the catalog entry preloaded.
func (h *Handler) pageContentLanguages(pageID uint) ([]models.StatusPageLanguage, error) {
	rows := []models.StatusPageLanguage{}
	err := h.DB.Preload("Language").
		Where("status_page_id = ? AND language_code IN ?", pageID, database.ContentLanguageCodes).
		Order("language_code asc").Find(&rows).Error
	return rows, err
}

// deliveredTranslationCodes returns the language codes whose stored translations
// may be read or delivered (admin lists and public endpoints).
func (h *Handler) deliveredTranslationCodes() []string {
	return database.ContentLanguageCodes
}
