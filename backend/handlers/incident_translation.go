package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ─────────────────────────────────────────────────────────────────────────────
// IncidentTranslation: Stage 2 der Mehrsprachigkeit - Incident-
// Titel und Meldungen in mehreren Sprachen mit Draft/Confirmed-Workflow.
//
// Draft-Translations sind nur für Admins sichtbar; Confirmed-Translations
// werden über die öffentliche API auch für Subscriber sichtbar.
// Source wird zum Tracking gesetzt (manual vs. deepl).
//
// Routen (main.go):
//   öffentlich: r.GET("/api/incidents/:incident_id/translations", h.GetIncidentTranslationsHandler)
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

	// Translation-Endpunkt lief über eine globale incident_id
	// ohne status_page_id-Join - wer die ID kannte/erriet, bekam Translations
	// auch für Incidents auf privaten Statusseiten ohne Rolle darauf.
	var page models.StatusPage
	if err := h.DB.First(&page, incident.StatusPageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errIncidentNotFound, "code": "incident_not_found"})
		return
	}
	if !h.requirePageVisible(c, page, errIncidentNotFound, "incident_not_found") {
		return
	}

	var translations []models.IncidentTranslation
	if err := h.DB.Where("incident_id = ? AND status = ?", incidentID, "confirmed").
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

// SaveIncidentTranslationHandler speichert die Übersetzung eines Vorfalls für
// genau eine Sprache: legt sie an oder ändert die bestehende, und
// setzt den Stand (draft = in Arbeit, confirmed = fertig und öffentlich).
// Das Bearbeiten-Formular schickt je Sprachreiter einen solchen Aufruf. Die
// Standardsprache der Seite ist der Vorfall selbst und hat keine Übersetzung;
// andere als die aktiven Sprachen der Seite werden abgewiesen.
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

// requireAdditionalLanguage prüft für das Speichern je Sprache, dass
// code eine aktive, nicht als Standard geführte Sprache der Seite ist; sonst
// antwortet sie mit 400 language_not_available und liefert false.
func (h *Handler) requireAdditionalLanguage(c *gin.Context, pageID uint, code string) bool {
	var pageLang models.StatusPageLanguage
	if err := h.DB.Where("status_page_id = ? AND language_code = ?", pageID, code).First(&pageLang).Error; err != nil || pageLang.IsDefault {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Language is not an additional language of this page", "code": "language_not_available"})
		return false
	}
	return true
}
