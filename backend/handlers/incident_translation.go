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

// ─────────────────────────────────────────────────────────────────────────────
// IncidentTranslation: stage 2 of multilingual support - incident
// titles and messages in multiple languages with a draft/confirmed workflow.
//
// Draft translations are visible to admins only; confirmed translations
// are also visible to subscribers through the public API.
// Source is set for tracking (manual vs. deepl).
//
// Routes (main.go):
//   public:  r.GET("/api/incidents/:incident_id/translations", h.GetIncidentTranslationsHandler)
//   admin:      translationAdmin (RequirePermission "incidents:write")
// ─────────────────────────────────────────────────────────────────────────────

func parseIncidentID(c *gin.Context) (uint, bool) {
	idStr := c.Param("incident_id")
	if idStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "incident_id parameter missing", "code": "parameter_missing"})
		return 0, false
	}
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid incident_id parameter", "code": "invalid_id"})
		return 0, false
	}
	return uint(id), true
}

func parseTranslationID(c *gin.Context) (uint, bool) {
	idStr := c.Param("translation_id")
	if idStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "translation_id parameter missing", "code": "parameter_missing"})
		return 0, false
	}
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid translation_id parameter", "code": "invalid_id"})
		return 0, false
	}
	return uint(id), true
}

// GetIncidentTranslationsHandler returns all confirmed translations for an
// incident to be displayed on the public status page. Draft translations
// are excluded from the public API.
func (h *Handler) GetIncidentTranslationsHandler(c *gin.Context) {
	incidentID, ok := parseIncidentID(c)
	if !ok {
		return
	}

	// Verify incident exists.
	var incident models.Incident
	if err := h.DB.First(&incident, incidentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errIncidentNotFound, "code": "incident_not_found"})
		return
	}

	// The translation endpoint ran on a global incident_id
	// without a status_page_id join - anyone who knew/guessed the ID got translations
	// even for incidents on private status pages without a role on them.
	var page models.StatusPage
	if err := h.DB.First(&page, incident.StatusPageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errIncidentNotFound, "code": "incident_not_found"})
		return
	}
	if !h.requirePageVisible(c, page, errIncidentNotFound, "incident_not_found") {
		return
	}

	var translations []models.IncidentTranslation
	if err := h.DB.Where("incident_id = ? AND status = ? AND language_code IN ?", incidentID, "confirmed", h.deliveredTranslationCodes()).
		Find(&translations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}

	// Return empty array instead of null if no translations.
	if translations == nil {
		translations = []models.IncidentTranslation{}
	}

	c.JSON(http.StatusOK, translations)
}

// CreateIncidentTranslationHandler creates a new incident translation in draft
// status. The language_code must not already exist for this incident.
func (h *Handler) CreateIncidentTranslationHandler(c *gin.Context) {
	incidentID, ok := parseIncidentID(c)
	if !ok {
		return
	}

	// Verify incident exists.
	var incident models.Incident
	if err := h.DB.First(&incident, incidentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errIncidentNotFound, "code": "incident_not_found"})
		return
	}

	var req struct {
		LanguageCode string `json:"language_code" binding:"required"`
		Title        string `json:"title" binding:"required"`
		Message      string `json:"message" binding:"required"`
		Source       string `json:"source" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "code": "invalid_request"})
		return
	}
	if !requireTitleLength(c, req.Title) {
		return
	}
	if !requireMessageLength(c, req.Message) {
		return
	}

	// Only an additional language the page offers, as for the save per language.
	req.LanguageCode = strings.ToLower(strings.TrimSpace(req.LanguageCode))
	if !h.requireAdditionalLanguage(c, incident.StatusPageID, req.LanguageCode) {
		return
	}

	// Check if translation already exists for this language.
	var existing models.IncidentTranslation
	result := h.DB.Where("incident_id = ? AND language_code = ?", incidentID, req.LanguageCode).
		First(&existing)
	if result.Error == nil {
		// Translation already exists.
		c.JSON(http.StatusConflict, gin.H{"error": "Translation for this language already exists", "code": "translation_exists"})
		return
	}

	trans := models.IncidentTranslation{
		IncidentID:   uint(incidentID),
		LanguageCode: req.LanguageCode,
		Title:        req.Title,
		Message:      req.Message,
		Status:       "draft",
		Source:       req.Source,
	}

	if err := h.DB.Create(&trans).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create translation", "code": "server_error"})
		return
	}

	c.JSON(http.StatusCreated, trans)
}

// ConfirmIncidentTranslationHandler transitions a translation from draft to
// confirmed status.
func (h *Handler) ConfirmIncidentTranslationHandler(c *gin.Context) {
	translationID, ok := parseTranslationID(c)
	if !ok {
		return
	}

	var trans models.IncidentTranslation
	if err := h.DB.First(&trans, translationID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Translation not found", "code": "translation_not_found"})
		return
	}

	trans.Status = "confirmed"
	if err := h.DB.Save(&trans).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to confirm translation", "code": "server_error"})
		return
	}

	c.JSON(http.StatusOK, trans)
}

// DeleteIncidentTranslationHandler deletes a translation.
func (h *Handler) DeleteIncidentTranslationHandler(c *gin.Context) {
	translationID, ok := parseTranslationID(c)
	if !ok {
		return
	}

	var trans models.IncidentTranslation
	if err := h.DB.First(&trans, translationID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Translation not found", "code": "translation_not_found"})
		return
	}

	if err := h.DB.Delete(&trans).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete translation", "code": "server_error"})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

// SaveIncidentTranslationHandler saves the translation of an incident for
// exactly one language: it creates it or changes the existing one, and
// sets the state (draft = in progress, confirmed = finished and public).
// The edit form sends one such call per language tab. The
// default language of the page is the incident itself and has no translation;
// languages other than the page's active ones are rejected.
func (h *Handler) SaveIncidentTranslationHandler(c *gin.Context) {
	incidentID, ok := parseIncidentID(c)
	if !ok {
		return
	}
	var incident models.Incident
	if err := h.DB.First(&incident, incidentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errIncidentNotFound, "code": "incident_not_found"})
		return
	}

	var req struct {
		Title   string `json:"title" binding:"required"`
		Message string `json:"message" binding:"required"`
		Status  string `json:"status" binding:"required,oneof=draft confirmed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "code": "invalid_request"})
		return
	}
	if !requireTitleLength(c, req.Title) {
		return
	}
	if !requireMessageLength(c, req.Message) {
		return
	}

	code := strings.ToLower(strings.TrimSpace(c.Param("language_code")))
	if !h.requireAdditionalLanguage(c, incident.StatusPageID, code) {
		return
	}

	var trans models.IncidentTranslation
	err := h.DB.Where("incident_id = ? AND language_code = ?", incidentID, code).First(&trans).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}
	trans.IncidentID = incidentID
	trans.LanguageCode = code
	trans.Title = req.Title
	trans.Message = req.Message
	trans.Status = req.Status
	if trans.Source == "" {
		trans.Source = "manual"
	}
	if err := h.DB.Save(&trans).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save translation", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, trans)
}

// requireAdditionalLanguage checks, for saving per language, that
// code is an active language of the page that is not its default; otherwise it
// responds with 400 language_not_available and returns false.
func (h *Handler) requireAdditionalLanguage(c *gin.Context, pageID uint, code string) bool {
	var pageLang models.StatusPageLanguage
	if err := h.DB.Where("status_page_id = ? AND language_code = ? AND language_code IN ?", pageID, code, database.ContentLanguageCodes).First(&pageLang).Error; err != nil || pageLang.IsDefault {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Language is not an additional language of this page", "code": "language_not_available"})
		return false
	}
	return true
}
