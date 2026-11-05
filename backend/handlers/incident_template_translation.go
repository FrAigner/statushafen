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
// Übersetzungen von Vorlagen: Titel und Nachricht einer Vorlage in
// den weiteren Sprachen der Seite. Die Routen hängen unter
// /pages/:page_id/incident-templates/:id/translations; die Seite steht damit im
// Pfad, die Vorlage wird gegen sie geprüft (fremde Vorlage → 404). Rechte wie
// die Vorlage selbst: incident_templates:read bzw. :update.
// ─────────────────────────────────────────────────────────────────────────────

const errTemplateNotFound = "Incident template not found"

// templateOfPage lädt die Vorlage aus :id/:page_id oder beantwortet die Anfrage mit dem Fehler.
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

// GetIncidentTemplateTranslationsHandler liefert alle Übersetzungen einer Vorlage (Entwürfe inklusive).
func (h *Handler) GetIncidentTemplateTranslationsHandler(c *gin.Context) {
	tmpl, ok := h.templateOfPage(c)
	if !ok {
		return
	}
	translations := []models.IncidentTemplateTranslation{}
	if err := h.DB.Where("incident_template_id = ?", tmpl.ID).Order("language_code").Find(&translations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, translations)
}

// SaveIncidentTemplateTranslationHandler speichert die Übersetzung einer Vorlage
// für genau eine weitere Sprache der Seite (anlegen oder ändern).
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
