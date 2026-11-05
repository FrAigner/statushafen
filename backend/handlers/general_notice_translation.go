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
const errNoticeNotFound = "Notice not found"

// ─────────────────────────────────────────────────────────────────────────────
// GeneralNoticeTranslation: Stage 4 der Mehrsprachigkeit - Notice-
// Titel und Meldungen in mehreren Sprachen mit Draft/Confirmed-Workflow.
//
// Draft-Translations sind nur für Admins sichtbar; Confirmed-Translations
// werden über die öffentliche API auch für Subscriber sichtbar.
// Source wird zum Tracking gesetzt (manual vs. deepl).
//
// Routen (main.go):
//   öffentlich: r.GET("/api/notices/:notice_id/translations", h.GetGeneralNoticeTranslationsHandler)
//   admin:      translationAdmin (RequirePermission "notices:write")
// ─────────────────────────────────────────────────────────────────────────────

func parseNoticeID(c *gin.Context) (uint, bool) {
	idStr := c.Param("notice_id")
	if idStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "notice_id parameter missing", "code": "parameter_missing"})
		return 0, false
	}
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid notice_id parameter", "code": "invalid_id"})
		return 0, false
	}
	return uint(id), true
}

// GetGeneralNoticeTranslationsHandler returns all confirmed translations for a
// notice to be displayed on the public status page. Draft translations
// are excluded from the public API.
func (h *Handler) GetGeneralNoticeTranslationsHandler(c *gin.Context) {
	noticeID, ok := parseNoticeID(c)
	if !ok {
		return
	}

	// Verify notice exists.
	var notice models.GeneralNotice
	if err := h.DB.First(&notice, noticeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errNoticeNotFound, "code": "notice_not_found"})
		return
	}

	// siehe incident_translation.go - status_page_id-Join fehlte.
	var page models.StatusPage
	if err := h.DB.First(&page, notice.StatusPageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errNoticeNotFound, "code": "notice_not_found"})
		return
	}
	if !h.requirePageVisible(c, page, errNoticeNotFound, "notice_not_found") {
		return
	}

	var translations []models.GeneralNoticeTranslation
	if err := h.DB.Where("general_notice_id = ? AND status = ?", noticeID, "confirmed").
		Find(&translations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}

	// Return empty array instead of null if no translations.
	if translations == nil {
		translations = []models.GeneralNoticeTranslation{}
	}

	c.JSON(http.StatusOK, translations)
}

// CreateGeneralNoticeTranslationHandler creates a new notice translation in draft
// status. The language_code must not already exist for this notice.
func (h *Handler) CreateGeneralNoticeTranslationHandler(c *gin.Context) {
	noticeID, ok := parseNoticeID(c)
	if !ok {
		return
	}

	// Verify notice exists.
	var notice models.GeneralNotice
	if err := h.DB.First(&notice, noticeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errNoticeNotFound, "code": "notice_not_found"})
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
	var existing models.GeneralNoticeTranslation
	result := h.DB.Where("general_notice_id = ? AND language_code = ?", noticeID, req.LanguageCode).
		First(&existing)
	if result.Error == nil {
		// Translation already exists.
		c.JSON(http.StatusConflict, gin.H{"error": "Translation for this language already exists", "code": "translation_exists"})
		return
	}

	trans := models.GeneralNoticeTranslation{
		GeneralNoticeID: uint(noticeID),
		LanguageCode:    req.LanguageCode,
		Title:           req.Title,
		Message:         req.Message,
		Status:          "draft",
		Source:          req.Source,
	}

	if err := h.DB.Create(&trans).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create translation", "code": "server_error"})
		return
	}

	c.JSON(http.StatusCreated, trans)
}

// ConfirmGeneralNoticeTranslationHandler transitions a translation from draft to
// confirmed status.
func (h *Handler) ConfirmGeneralNoticeTranslationHandler(c *gin.Context) {
	translationID, ok := parseTranslationID(c)
	if !ok {
		return
	}

	var trans models.GeneralNoticeTranslation
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

// DeleteGeneralNoticeTranslationHandler deletes a translation.
func (h *Handler) DeleteGeneralNoticeTranslationHandler(c *gin.Context) {
	translationID, ok := parseTranslationID(c)
	if !ok {
		return
	}

	var trans models.GeneralNoticeTranslation
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

// SaveGeneralNoticeTranslationHandler speichert die Übersetzung einer Ankündigung
// für genau eine Sprache: anlegen oder ändern, Stand draft oder
// confirmed. Gegenstück zu SaveIncidentTranslationHandler.
func (h *Handler) SaveGeneralNoticeTranslationHandler(c *gin.Context) {
	noticeID, ok := parseNoticeID(c)
	if !ok {
		return
	}
	var notice models.GeneralNotice
	if err := h.DB.First(&notice, noticeID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errNoticeNotFound, "code": "notice_not_found"})
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
	if !h.requireAdditionalLanguage(c, notice.StatusPageID, code) {
		return
	}

	var trans models.GeneralNoticeTranslation
	err := h.DB.Where("general_notice_id = ? AND language_code = ?", noticeID, code).First(&trans).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}
	trans.GeneralNoticeID = noticeID
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
