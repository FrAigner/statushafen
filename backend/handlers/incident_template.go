package handlers

import (
	"net/http"
	"strconv"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// IncidentTemplate-CRUD: wiederverwendbare Vorlagen für
// Vorfälle/Wartungsfenster. Seiten-scoped analog zu Components/Incidents -
// dieselbe RequirePermission("incidents:write")-Berechtigung wird
// verwendet, da Vorlagen nur beim Anlegen von Incidents relevant sind und
// keine eigene Permission die Rollenmatrix unnötig verkomplizieren soll.

// parsePageIDParam extrahiert und validiert :page_id, schreibt bei Fehler
// direkt die Fehlerantwort und liefert ok=false zurück.
func parsePageIDParam(c *gin.Context) (uint64, bool) {
	tenantIDStr := c.Param("page_id")
	if tenantIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "page_id parameter missing", "code": "parameter_missing"})
		return 0, false
	}
	tenantID, err := strconv.ParseUint(tenantIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page_id parameter", "code": "invalid_id"})
		return 0, false
	}
	return tenantID, true
}

// parseTemplateIDParam extrahiert und validiert :id, schreibt bei Fehler
// direkt die Fehlerantwort und liefert ok=false zurück.
func parseTemplateIDParam(c *gin.Context) (uint64, bool) {
	tmplID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid template ID", "code": "invalid_id"})
		return 0, false
	}
	return tmplID, true
}

func (h *Handler) GetIncidentTemplatesHandler(c *gin.Context) {
	tenantID, ok := parsePageIDParam(c)
	if !ok {
		return
	}

	templates := []models.IncidentTemplate{}
	if err := h.DB.Where("status_page_id = ?", tenantID).Order("name asc").Find(&templates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch incident templates", "code": "server_error"})
		return
	}

	c.JSON(http.StatusOK, templates)
}

func (h *Handler) CreateIncidentTemplateHandler(c *gin.Context) {
	tenantID, ok := parsePageIDParam(c)
	if !ok {
		return
	}

	type CreateReq struct {
		Name          string `json:"Name" binding:"required"`
		Kind          string `json:"Kind"`
		Title         string `json:"Title" binding:"required"`
		Message       string `json:"Message" binding:"required"`
		DefaultStatus string `json:"DefaultStatus"`
		ComponentIDs  string `json:"ComponentIDs"`
	}
	var req CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	if !requireTitleLength(c, req.Name) || !requireTitleLength(c, req.Title) {
		return
	}
	if !requireMessageLength(c, req.Message) {
		return
	}
	if req.Kind != "incident" && req.Kind != "maintenance" {
		req.Kind = "incident"
	}

	tmpl := models.IncidentTemplate{
		StatusPageID:  uint(tenantID),
		Name:          req.Name,
		Kind:          req.Kind,
		Title:         req.Title,
		Message:       req.Message,
		DefaultStatus: req.DefaultStatus,
		ComponentIDs:  req.ComponentIDs,
	}
	if err := h.DB.Create(&tmpl).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create incident template", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "incident_template.created", "config", "IncidentTemplate", strconv.FormatUint(uint64(tmpl.ID), 10), tmpl.Name, "")
	c.JSON(http.StatusCreated, tmpl)
}

func (h *Handler) UpdateIncidentTemplateHandler(c *gin.Context) {
	tmplID, ok := parseTemplateIDParam(c)
	if !ok {
		return
	}
	tenantID, ok := parsePageIDParam(c)
	if !ok {
		return
	}

	var tmpl models.IncidentTemplate
	if err := h.DB.Where("id = ? AND status_page_id = ?", tmplID, tenantID).First(&tmpl).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errTemplateNotFound, "code": "template_not_found"})
		return
	}

	type UpdateReq struct {
		Name          string `json:"Name" binding:"required"`
		Kind          string `json:"Kind"`
		Title         string `json:"Title" binding:"required"`
		Message       string `json:"Message" binding:"required"`
		DefaultStatus string `json:"DefaultStatus"`
		ComponentIDs  string `json:"ComponentIDs"`
	}
	var req UpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	if !requireTitleLength(c, req.Name) || !requireTitleLength(c, req.Title) {
		return
	}
	if !requireMessageLength(c, req.Message) {
		return
	}
	if req.Kind != "incident" && req.Kind != "maintenance" {
		req.Kind = "incident"
	}

	tmpl.Name = req.Name
	tmpl.Kind = req.Kind
	tmpl.Title = req.Title
	tmpl.Message = req.Message
	tmpl.DefaultStatus = req.DefaultStatus
	tmpl.ComponentIDs = req.ComponentIDs

	if err := h.DB.Save(&tmpl).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update incident template", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "incident_template.updated", "config", "IncidentTemplate", strconv.FormatUint(uint64(tmpl.ID), 10), tmpl.Name, "")
	c.JSON(http.StatusOK, tmpl)
}

func (h *Handler) DeleteIncidentTemplateHandler(c *gin.Context) {
	tmplID, ok := parseTemplateIDParam(c)
	if !ok {
		return
	}
	tenantID, ok := parsePageIDParam(c)
	if !ok {
		return
	}

	var tmpl models.IncidentTemplate
	if err := h.DB.Where("id = ? AND status_page_id = ?", tmplID, tenantID).First(&tmpl).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errTemplateNotFound, "code": "template_not_found"})
		return
	}

	// Bewusst KEIN Aufräumen von Incidents, die aus dieser Vorlage entstanden
	// sind: es gibt keine FK-Beziehung zurück zur Vorlage - die
	// Feldwerte wurden beim Anlegen nur kopiert, das Löschen der Vorlage hat
	// also keine Auswirkung auf bereits erstellte Incidents/Wartungsfenster.
	// Soft-Delete löst das FK-Cascade nicht aus, die Übersetzungen der Vorlage mitlöschen.
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("incident_template_id = ?", tmpl.ID).Delete(&models.IncidentTemplateTranslation{}).Error; err != nil {
			return err
		}
		return tx.Delete(&tmpl).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete incident template", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "incident_template.deleted", "config", "IncidentTemplate", strconv.FormatUint(uint64(tmpl.ID), 10), tmpl.Name, "")
	c.JSON(http.StatusOK, gin.H{"message": "Incident template deleted successfully"})
}
