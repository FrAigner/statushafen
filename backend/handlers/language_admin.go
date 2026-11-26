package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Stage 1 of 5 of the multilingual feature: admin CRUD for
// the global language catalog (Language) as well as reading/writing the
// languages activated for a status page + default (StatusPageLanguage). Replaces
// the previous language assumption implicitly fixed to en/de
// with a real, openly extensible catalog.

// GetLanguagesHandler returns the complete global language catalog. Deliberately
// readable for every logged-in user (see comment in main.go) - even a
// page admin without global admin rights must be able to see the available languages
// in order to activate them for their status page.
func (h *Handler) GetLanguagesHandler(c *gin.Context) {
	var languages []models.Language
	// Only content languages; legacy catalog rows stay stored but are not offered.
	if err := h.DB.Where("code IN ?", database.ContentLanguageCodes).Order("code asc").Find(&languages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch languages", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, languages)
}

// languageUpsertReq is used for create AND update - code is deliberately not
// bound on update ("required" would otherwise reject every update request
// without a code field sent along, although UpdateLanguageHandler ignores the
// code anyway (not changeable, see handler comment)). The
// mandatory check for code instead happens explicitly in
// CreateLanguageHandler.
type languageUpsertReq struct {
	Code     string `json:"code"`
	Name     string `json:"name" binding:"required"`
	IsActive *bool  `json:"is_active"`
}

// CreateLanguageHandler creates a new entry in the global language catalog
// - global-admin-only, analogous to the tag catalog (see handlers/tag.go).
func (h *Handler) CreateLanguageHandler(c *gin.Context) {
	var req languageUpsertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	code := strings.ToLower(strings.TrimSpace(req.Code))
	name := strings.TrimSpace(req.Name)
	if name == "" {
		abortInvalidInput(c, codeNameRequired, "name darf nicht leer sein")
		return
	}
	// Check format (previously only "not empty", frontend only maxlength).
	if !languageCodeRegex.MatchString(code) {
		abortInvalidInput(c, codeLanguageCodeInvalid, "code muss ein Sprachcode wie de oder en-us sein")
		return
	}
	// Content exists only in German and English.
	if !database.IsContentLanguage(code) {
		abortInvalidInput(c, codeLanguageNotSupported, "only de and en are supported as content languages")
		return
	}

	var count int64
	if err := h.DB.Model(&models.Language{}).Where("code = ?", code).Count(&count).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check for duplicate language", "code": "server_error"})
		return
	}
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "Eine Sprache mit diesem Code existiert bereits", "code": "language_exists"})
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	// Deleted languages remain as a soft-deleted row (tombstone for
	// the seed, FK from StatusPageLanguage, full unique index on code). If
	// the same code is created again, this row comes back to life with the new name.
	var tomb models.Language
	tombErr := h.DB.Unscoped().Where("code = ? AND deleted_at IS NOT NULL", code).First(&tomb).Error
	if tombErr == nil {
		tomb.Name, tomb.IsActive = name, isActive
		tomb.DeletedAt = gorm.DeletedAt{}
		if err := h.DB.Unscoped().Save(&tomb).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create language", "code": "server_error"})
			return
		}
		h.RecordAudit(c, "language.created", "config", "Language", strconv.FormatUint(uint64(tomb.ID), 10), tomb.Code, "")
		c.JSON(http.StatusCreated, tomb)
		return
	}
	if !errors.Is(tombErr, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check for deleted language", "code": "server_error"})
		return
	}
	lang := models.Language{Code: code, Name: name, IsActive: isActive}
	// GORM replaces the zero-value false with default:true; a language created as inactive
	// is therefore explicitly deactivated in the same step (one
	// transaction, so that no active row is left behind on errors).
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&lang).Error; err != nil {
			return err
		}
		if isActive {
			return nil
		}
		return tx.Model(&lang).Update("is_active", false).Error
	})
	if err != nil {
		if isUniqueViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "Eine Sprache mit diesem Code existiert bereits", "code": "language_exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create language", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "language.created", "config", "Language", strconv.FormatUint(uint64(lang.ID), 10), lang.Code, "")
	c.JSON(http.StatusCreated, lang)
}

// UpdateLanguageHandler changes Name/IsActive of a catalog entry. The code
// itself is deliberately not changeable - it is the domain key through
// which StatusPageLanguage (and future localized content) is referenced.
func (h *Handler) UpdateLanguageHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid language ID", "code": "invalid_id"})
		return
	}
	var lang models.Language
	if err := h.DB.First(&lang, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Language not found", "code": "language_not_found"})
		return
	}

	var req languageUpsertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		abortInvalidInput(c, codeNameRequired, "name darf nicht leer sein")
		return
	}
	// A language other than de/en may be renamed but never switched on again.
	if req.IsActive != nil && *req.IsActive && !database.IsContentLanguage(lang.Code) {
		abortInvalidInput(c, codeLanguageNotSupported, "only de and en are supported as content languages")
		return
	}
	lang.Name = name
	if req.IsActive != nil {
		lang.IsActive = *req.IsActive
	}
	if err := h.DB.Save(&lang).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update language", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "language.updated", "config", "Language", strconv.FormatUint(id, 10), lang.Code, "")
	c.JSON(http.StatusOK, lang)
}

// languageBlocked ends the delete transaction with a business-rule rejection (409).
type languageBlocked struct {
	code  string
	error string
	count int
	pages []string
}

func (b *languageBlocked) Error() string { return b.code }

// DeleteLanguageHandler deletes a language from the global catalog
// (global-admin-only). Rules, all checked in ONE transaction:
//  1. Default language of at least one status page: 409 language_is_default with the
//     page names (pages) and the count (count). A page without a default language
//     must never arise; the admin switches the page over first.
//  2. "In use" means: there are still active mail templates (NotificationTemplate.
//     Language) with exactly this code; they would otherwise never be sent again or could
//     no longer be edited: 409 language_in_use with count. Not counted are the
//     UI language of users (User.PreferredLanguage knows only de/en, not the catalog)
//     and stored translations of incidents/components/announcements.
//  3. Otherwise: StatusPageLanguage assignments (all non-default, see 1) and the
//     language are soft-deleted. The row remains as a tombstone: the seed
//     (seedLanguageCatalog) checks Unscoped and does not recreate a
//     deliberately deleted language; CreateLanguageHandler revives it for the same code.
//
// Already stored translations remain in the database but are no longer
// delivered: the public path rejects languages the page has not activated
// (400), and only queries the page's code and default language.
func (h *Handler) DeleteLanguageHandler(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid language ID", "code": "invalid_id"})
		return
	}
	var lang models.Language
	if err := h.DB.First(&lang, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Language not found", "code": "language_not_found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load language", "code": "server_error"})
		}
		return
	}

	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var pageNames []string
		if err := tx.Model(&models.StatusPage{}).
			Joins("JOIN status_page_languages spl ON spl.status_page_id = status_pages.id AND spl.deleted_at IS NULL").
			Where("spl.language_code = ? AND spl.is_default = ?", lang.Code, true).
			Order("status_pages.name").Pluck("status_pages.name", &pageNames).Error; err != nil {
			return err
		}
		if len(pageNames) > 0 {
			return &languageBlocked{code: "language_is_default", error: "Sprache ist Standardsprache einer Statusseite", count: len(pageNames), pages: pageNames}
		}
		var templates int64
		if err := tx.Model(&models.NotificationTemplate{}).Where("language = ?", lang.Code).Count(&templates).Error; err != nil {
			return err
		}
		if templates > 0 {
			return &languageBlocked{code: "language_in_use", error: "Zur Sprache gibt es noch Mail-Vorlagen", count: int(templates)}
		}
		if err := tx.Where("language_code = ?", lang.Code).Delete(&models.StatusPageLanguage{}).Error; err != nil {
			return err
		}
		return tx.Delete(&lang).Error
	})
	var blocked *languageBlocked
	if errors.As(err, &blocked) {
		body := gin.H{"error": blocked.error, "code": blocked.code, "count": blocked.count}
		if blocked.pages != nil {
			body["pages"] = blocked.pages
		}
		c.JSON(http.StatusConflict, body)
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete language", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "language.deleted", "config", "Language", strconv.FormatUint(id, 10), lang.Code, "")
	c.Status(http.StatusNoContent)
}

// GetPageLanguagesHandler returns the languages activated for a status page
// languages incl. default marker.
func (h *Handler) GetPageLanguagesHandler(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}
	pageLanguages, err := h.pageContentLanguages(uint(pageID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch page languages", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, pageLanguages)
}

type putPageLanguagesReq struct {
	// Codes: the complete new set of activated language codes
	// - no PATCH/diff, the UI always sends the desired end state,
	// analogous to the pattern in UpdateMySubscriptionHandler.
	Codes   []string `json:"codes" binding:"required"`
	Default string   `json:"default" binding:"required"`
}

// PutPageLanguagesHandler replaces the complete language selection
// of a status page with the given set + default. Deliberately a
// replace instead of granular add/remove: prevents a page
// from temporarily ending up with no default language at all or with a default
// that is not in the activated set - both are
// validated and enforced here as an atomic transaction.
func (h *Handler) PutPageLanguagesHandler(c *gin.Context) {
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return
	}
	var req putPageLanguagesReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Felder", "code": "invalid_request"})
		return
	}
	if len(req.Codes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mindestens eine Sprache muss aktiviert sein", "code": "language_none_enabled"})
		return
	}
	defaultCode := strings.ToLower(strings.TrimSpace(req.Default))
	codes := make([]string, 0, len(req.Codes))
	hasDefault := false
	for _, raw := range req.Codes {
		code := strings.ToLower(strings.TrimSpace(raw))
		if code == "" {
			continue
		}
		// Only content languages, even if another one were still active in the catalog.
		if !database.IsContentLanguage(code) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Mindestens ein Sprachcode ist unbekannt oder deaktiviert", "code": "language_codes_unknown"})
			return
		}
		codes = append(codes, code)
		if code == defaultCode {
			hasDefault = true
		}
	}
	if !hasDefault {
		c.JSON(http.StatusBadRequest, gin.H{"error": "default muss Teil von codes sein", "code": "language_default_not_in_codes"})
		return
	}
	// Switch off: a page has its default language only; choosing a second one is refused.
	enabled := h.translationEnabled()
	if !enabled && len(codes) != 1 {
		abortTranslationDisabled(c)
		return
	}

	var validCount int64
	if err := h.DB.Model(&models.Language{}).Where("code IN ? AND is_active = ?", codes, true).Count(&validCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to validate languages", "code": "server_error"})
		return
	}
	if int(validCount) != len(codes) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Mindestens ein Sprachcode ist unbekannt oder deaktiviert", "code": "language_codes_unknown"})
		return
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if !enabled {
			return setPageDefaultLanguage(tx, uint(pageID), defaultCode)
		}
		if err := tx.Where("status_page_id = ?", pageID).Delete(&models.StatusPageLanguage{}).Error; err != nil {
			return err
		}
		for _, code := range codes {
			entry := models.StatusPageLanguage{StatusPageID: uint(pageID), LanguageCode: code, IsDefault: code == defaultCode}
			if err := tx.Create(&entry).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save page languages", "code": "server_error"})
		return
	}

	pageLanguages, err := h.pageContentLanguages(uint(pageID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch page languages", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "page_languages.updated", "config", "StatusPageLanguage", strconv.FormatUint(uint64(pageID), 10), strings.Join(codes, ","), "")
	c.JSON(http.StatusOK, pageLanguages)
}
