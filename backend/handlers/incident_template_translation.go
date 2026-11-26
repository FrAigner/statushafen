package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ─────────────────────────────────────────────────────────────────────────────
// Template translations: title and message of a template in
// the other languages of the page. The routes hang under
// /pages/:page_id/incident-templates/:id/translations; the page is thus in the
// path, and the template is checked against it (foreign template → 404). Permissions like
// the template itself: incident_templates:read and :update respectively.
// ─────────────────────────────────────────────────────────────────────────────

const errTemplateNotFound = "Incident template not found"

// templateOfPage loads the template from :id/:page_id or answers the request with the error.
func (h *Handler) templateOfPage(c *gin.Context) (models.IncidentTemplate, bool) {
	var tmpl models.IncidentTemplate
	tmplID, ok := parseTemplateIDParam(c)
	if !ok {
		return tmpl, false
	}
	pageID, ok := parsePageIDParam(c)
	if !ok {
		return tmpl, false
	}
	if err := h.DB.Where("id = ? AND status_page_id = ?", tmplID, pageID).First(&tmpl).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errTemplateNotFound, "code": "template_not_found"})
		return tmpl, false
	}
	return tmpl, true
}

// GetIncidentTemplateTranslationsHandler returns all translations of a template (drafts included).
func (h *Handler) GetIncidentTemplateTranslationsHandler(c *gin.Context) {
	tmpl, ok := h.templateOfPage(c)
	if !ok {
		return
	}
	translations := []models.IncidentTemplateTranslation{}
	if err := h.DB.Where("incident_template_id = ? AND language_code IN ?", tmpl.ID, h.deliveredTranslationCodes()).Order("language_code").Find(&translations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, translations)
}

// SaveIncidentTemplateTranslationHandler saves the translation of a template
// for exactly one other language of the page (create or update).
func (h *Handler) SaveIncidentTemplateTranslationHandler(c *gin.Context) {
	tmpl, ok := h.templateOfPage(c)
	if !ok {
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
	if !h.requireAdditionalLanguage(c, tmpl.StatusPageID, code) {
		return
	}

	var trans models.IncidentTemplateTranslation
	err := h.DB.Where("incident_template_id = ? AND language_code = ?", tmpl.ID, code).First(&trans).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}
	trans.IncidentTemplateID = tmpl.ID
	trans.LanguageCode = code
	trans.Title = req.Title
	trans.Message = req.Message
	trans.Status = req.Status
	if err := h.DB.Save(&trans).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save translation", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, trans)
}
