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

// Wiederholtes Literal (go:S1192).
const errComponentNotFound = "Component not found"

// ─────────────────────────────────────────────────────────────────────────────
// ComponentTranslation: Stage 3 der Mehrsprachigkeit - Component-
// Namen und Beschreibungen in mehreren Sprachen mit Draft/Confirmed-Workflow.
//
// Draft-Translations sind nur für Admins sichtbar; Confirmed-Translations
// werden über die öffentliche API auch für Subscriber sichtbar.
// Source wird zum Tracking gesetzt (manual vs. deepl).
//
// Routen (main.go):
//   öffentlich: r.GET("/api/components/:component_id/translations", h.GetComponentTranslationsHandler)
//   admin:      translationAdmin (RequirePermission "components:write")
// ─────────────────────────────────────────────────────────────────────────────

func parseComponentID(c *gin.Context) (uint, bool) {
	idStr := c.Param("component_id")
	if idStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "component_id parameter missing", "code": "parameter_missing"})
		return 0, false
	}
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid component_id parameter", "code": "invalid_id"})
		return 0, false
	}
	return uint(id), true
}

// GetComponentTranslationsHandler returns all confirmed translations for a
// component to be displayed on the public status page. Draft translations
// are excluded from the public API.
func (h *Handler) GetComponentTranslationsHandler(c *gin.Context) {
	componentID, ok := parseComponentID(c)
	if !ok {
		return
	}

	// Verify component exists.
	var component models.Component
	if err := h.DB.First(&component, componentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errComponentNotFound, "code": "component_not_found"})
		return
	}

	// siehe incident_translation.go - status_page_id-Join fehlte.
	var page models.StatusPage
	if err := h.DB.First(&page, component.StatusPageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errComponentNotFound, "code": "component_not_found"})
		return
	}
	if !h.requirePageVisible(c, page, errComponentNotFound, "component_not_found") {
		return
	}

	// Review: archivierte und unsichtbare Komponenten (samt
	// Gruppen-Nachfahren) sind für Unberechtigte nicht abrufbar (fortlaufende IDs).
	if !h.callerMaySeeHiddenComponents(c, page.ID) {
		var siblings []models.Component
		if err := h.DB.Select("id", "parent_id", "visible", "archived_at").Where("status_page_id = ?", page.ID).Find(&siblings).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
			return
		}
		if hiddenComponentIDs(siblings)[component.ID] {
			c.JSON(http.StatusNotFound, gin.H{"error": errComponentNotFound, "code": "component_not_found"})
			return
		}
	}

	var translations []models.ComponentTranslation
	if err := h.DB.Where("component_id = ? AND status = ?", componentID, "confirmed").
		Find(&translations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}

	// Return empty array instead of null if no translations.
	if translations == nil {
		translations = []models.ComponentTranslation{}
	}

	c.JSON(http.StatusOK, translations)
}

// CreateComponentTranslationHandler creates a new component translation in draft
// status. The language_code must not already exist for this component.
func (h *Handler) CreateComponentTranslationHandler(c *gin.Context) {
	componentID, ok := parseComponentID(c)
	if !ok {
		return
	}

	// Verify component exists.
	var component models.Component
	if err := h.DB.First(&component, componentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errComponentNotFound, "code": "component_not_found"})
		return
	}

	var req struct {
		LanguageCode string `json:"language_code" binding:"required"`
		Title        string `json:"title" binding:"required"`
		Description  string `json:"description" binding:"required"`
		Source       string `json:"source" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "code": "invalid_request"})
		return
	}

	// Check if translation already exists for this language.
	var existing models.ComponentTranslation
	result := h.DB.Where("component_id = ? AND language_code = ?", componentID, req.LanguageCode).
		First(&existing)
	if result.Error == nil {
		// Translation already exists.
		c.JSON(http.StatusConflict, gin.H{"error": "Translation for this language already exists", "code": "translation_exists"})
		return
	}

	trans := models.ComponentTranslation{
		ComponentID:  uint(componentID),
		LanguageCode: req.LanguageCode,
		Title:        req.Title,
		Description:  req.Description,
		Status:       "draft",
		Source:       req.Source,
	}

	if err := h.DB.Create(&trans).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create translation", "code": "server_error"})
		return
	}

	c.JSON(http.StatusCreated, trans)
}

// ConfirmComponentTranslationHandler transitions a translation from draft to
// confirmed status.
func (h *Handler) ConfirmComponentTranslationHandler(c *gin.Context) {
	translationID, ok := parseTranslationID(c)
	if !ok {
		return
	}

	var trans models.ComponentTranslation
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

// DeleteComponentTranslationHandler deletes a translation.
func (h *Handler) DeleteComponentTranslationHandler(c *gin.Context) {
	translationID, ok := parseTranslationID(c)
	if !ok {
		return
	}

	var trans models.ComponentTranslation
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

// SaveComponentTranslationHandler speichert die Übersetzung einer Komponente
// bzw. Gruppe für genau eine Sprache: anlegen oder ändern, Stand
// draft oder confirmed. Gegenstück zu SaveIncidentTranslationHandler.
//
// Anders als beim Anlegen (CreateComponentTranslationHandler) ist die
// Beschreibung optional: Sie ist auch an der Komponente selbst optional und
// Gruppen haben gar keine. Die Spalte ist not null, daher genügt der leere String.
func (h *Handler) SaveComponentTranslationHandler(c *gin.Context) {
	componentID, ok := parseComponentID(c)
	if !ok {
		return
	}
	var component models.Component
	if err := h.DB.First(&component, componentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errComponentNotFound, "code": "component_not_found"})
		return
	}

	var req struct {
		Title       string `json:"title" binding:"required"`
		Description string `json:"description"`
		Status      string `json:"status" binding:"required,oneof=draft confirmed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "code": "invalid_request"})
		return
	}

	code := strings.ToLower(strings.TrimSpace(c.Param("language_code")))
	if !h.requireAdditionalLanguage(c, component.StatusPageID, code) {
		return
	}

	var trans models.ComponentTranslation
	err := h.DB.Where("component_id = ? AND language_code = ?", componentID, code).First(&trans).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}
	trans.ComponentID = componentID
	trans.LanguageCode = code
	trans.Title = req.Title
	trans.Description = req.Description
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
