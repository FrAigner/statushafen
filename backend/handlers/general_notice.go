package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// GeneralNotice: seitenweite Ankündigungen, die nicht an einen
// konkreten Incident/Wartungsfenster gebunden sind (z.B. Richtlinien-
// änderungen, organisatorische Hinweise). CRUD ist page-scoped und folgt dem
// gleichen Muster wie die Incident-Handler (RequirePermission auf :page_id),
// der Lesezugriff für die öffentliche Statusseite ist ungeschützt und liefert
// nur aktive, nicht abgelaufene Notices.
//
// Routen (main.go):
//   öffentlich: r.GET("/api/pages/:page_id/notices", h.GetActiveNoticesHandler)
//   admin:      noticeAdmin (RequirePermission "notices:write")
// ─────────────────────────────────────────────────────────────────────────────

var validNoticeLevels = map[string]bool{"info": true, "warning": true, "critical": true}

// parseNoticePageIDParam extrahiert und validiert :page_id, schreibt bei
// Fehler direkt die Fehlerantwort und liefert ok=false zurück.
func parseNoticePageIDParam(c *gin.Context) (uint64, bool) {
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

// parseNoticeIDParam extrahiert und validiert :id, schreibt bei Fehler
// direkt die Fehlerantwort und liefert ok=false zurück.
func parseNoticeIDParam(c *gin.Context) (uint64, bool) {
	noticeID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid notice ID", "code": "invalid_id"})
		return 0, false
	}
	return noticeID, true
}

// validateNoticeFields normalisiert Level (Default "info") und prüft
// Level-Gültigkeit sowie StartAt/EndAt-Reihenfolge; schreibt bei Fehler
// direkt die Fehlerantwort und liefert ok=false zurück.
func validateNoticeFields(c *gin.Context, level string, startAt, endAt *time.Time) (string, bool) {
	if level == "" {
		level = "info"
	}
	if !validNoticeLevels[level] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Level muss 'info', 'warning' oder 'critical' sein", "code": "notice_level_invalid"})
		return "", false
	}
	if startAt != nil && endAt != nil && !endAt.After(*startAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Endzeitpunkt muss nach dem Startzeitpunkt liegen", "code": "notice_end_before_start"})
		return "", false
	}
	return level, true
}

// GetActiveNoticesHandler liefert die auf der öffentlichen Statusseite
// anzuzeigenden Notices: IsActive muss true sein, und falls StartAt/EndAt
// gesetzt sind, muss "jetzt" innerhalb des Zeitfensters liegen. Neueste
// zuerst (analog zur Incident-Liste).
func (h *Handler) GetActiveNoticesHandler(c *gin.Context) {
	tenantID, ok := parseNoticePageIDParam(c)
	if !ok {
		return
	}

	var page models.StatusPage
	if err := h.DB.First(&page, tenantID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Status page not found", "code": "page_not_found"})
		return
	}
	if !h.requirePageVisible(c, page, "Status page not found", "page_not_found") {
		return
	}

	now := time.Now()
	notices := []models.GeneralNotice{}
	if err := h.DB.Where("status_page_id = ? AND is_active = ?", tenantID, true).
		Where("start_at IS NULL OR start_at <= ?", now).
		Where("end_at IS NULL OR end_at >= ?", now).
		Order("created_at desc").
		Find(&notices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch notices", "code": "server_error"})
		return
	}

	c.JSON(http.StatusOK, notices)
}

// GetNoticesHandler (Admin): liefert ALLE Notices einer Seite (auch
// inaktive/abgelaufene), damit die Verwaltungsseite sie bearbeiten kann.
func (h *Handler) GetNoticesHandler(c *gin.Context) {
	tenantID, ok := parseNoticePageIDParam(c)
	if !ok {
		return
	}

	notices := []models.GeneralNotice{}
	if err := h.DB.Where("status_page_id = ?", tenantID).Order("created_at desc").Find(&notices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch notices", "code": "server_error"})
		return
	}

	c.JSON(http.StatusOK, notices)
}

func (h *Handler) CreateNoticeHandler(c *gin.Context) {
	tenantID, ok := parseNoticePageIDParam(c)
	if !ok {
		return
	}

	type CreateReq struct {
		Title             string     `json:"Title" binding:"required"`
		Message           string     `json:"Message" binding:"required"`
		Level             string     `json:"Level"`
		IsActive          *bool      `json:"IsActive"`
		Collapsed         bool       `json:"Collapsed"`
		StartAt           *time.Time `json:"StartAt"`
		EndAt             *time.Time `json:"EndAt"`
		NotifySubscribers bool       `json:"NotifySubscribers"`
	}
	var req CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	if !requireTitleLength(c, req.Title) {
		return
	}
	if !requireMessageLength(c, req.Message) {
		return
	}

	level, ok := validateNoticeFields(c, req.Level, req.StartAt, req.EndAt)
	if !ok {
		return
	}
	req.Level = level

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	notice := models.GeneralNotice{
		StatusPageID:      uint(tenantID),
		Title:             req.Title,
		Message:           req.Message,
		Level:             req.Level,
		IsActive:          isActive,
		Collapsed:         req.Collapsed,
		StartAt:           req.StartAt,
		EndAt:             req.EndAt,
		NotifySubscribers: req.NotifySubscribers,
	}

	if err := h.DB.Create(&notice).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create notice", "code": "server_error"})
		return
	}

	h.RecordAudit(c, "notice.created", "notice", "GeneralNotice", strconv.FormatUint(uint64(notice.ID), 10), notice.Title, "")
	// Mail-Versand nur bei aktiver Notice mit NotifySubscribers=true.
	// Der Dispatcher entscheidet nochmal selbst per NotifySubscribers-Feld,
	// aber ein zusätzlicher IsActive-Guard hier verhindert, dass eine
	// deaktivierte Notice bei Anlage Mails auslöst (die man sehen würde,
	// bevor sie sichtbar ist).
	if notice.IsActive && notice.NotifySubscribers {
		h.DispatchNoticeNotifications(notice, "created")
	}
	c.JSON(http.StatusCreated, notice)
}

func (h *Handler) UpdateNoticeHandler(c *gin.Context) {
	tenantID, ok := parseNoticePageIDParam(c)
	if !ok {
		return
	}
	noticeID, ok := parseNoticeIDParam(c)
	if !ok {
		return
	}

	var notice models.GeneralNotice
	if err := h.DB.Where("id = ? AND status_page_id = ?", noticeID, tenantID).First(&notice).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Notice not found", "code": "notice_not_found"})
		return
	}

	type UpdateReq struct {
		Title             string     `json:"Title" binding:"required"`
		Message           string     `json:"Message" binding:"required"`
		Level             string     `json:"Level"`
		IsActive          *bool      `json:"IsActive"`
		Collapsed         bool       `json:"Collapsed"`
		StartAt           *time.Time `json:"StartAt"`
		EndAt             *time.Time `json:"EndAt"`
		NotifySubscribers *bool      `json:"NotifySubscribers"`
	}
	var req UpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	if !requireTitleLength(c, req.Title) {
		return
	}
	if !requireMessageLength(c, req.Message) {
		return
	}

	level, ok := validateNoticeFields(c, req.Level, req.StartAt, req.EndAt)
	if !ok {
		return
	}

	// alten Aktivzustand merken, um zu erkennen, ob ein Update
	// die Notice erstmals aktiviert (dann "created"-Kind) oder deaktiviert
	// (dann "ended"-Kind). Ein reguläres Update einer bereits aktiven
	// Notice wird zu "updated".
	wasActive := notice.IsActive

	notice.Title = req.Title
	notice.Message = req.Message
	notice.Level = level
	notice.Collapsed = req.Collapsed
	notice.StartAt = req.StartAt
	notice.EndAt = req.EndAt
	if req.IsActive != nil {
		notice.IsActive = *req.IsActive
	}
	if req.NotifySubscribers != nil {
		notice.NotifySubscribers = *req.NotifySubscribers
	}

	if err := h.DB.Save(&notice).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update notice", "code": "server_error"})
		return
	}

	h.RecordAudit(c, "notice.updated", "notice", "GeneralNotice", strconv.FormatUint(uint64(notice.ID), 10), notice.Title, "")
	if notice.NotifySubscribers {
		switch {
		case !wasActive && notice.IsActive:
			h.DispatchNoticeNotifications(notice, "created")
		case wasActive && !notice.IsActive:
			h.DispatchNoticeNotifications(notice, "ended")
		case wasActive && notice.IsActive:
			h.DispatchNoticeNotifications(notice, "updated")
		}
	}
	c.JSON(http.StatusOK, notice)
}

func (h *Handler) DeleteNoticeHandler(c *gin.Context) {
	tenantID, ok := parseNoticePageIDParam(c)
	if !ok {
		return
	}
	noticeID, ok := parseNoticeIDParam(c)
	if !ok {
		return
	}

	var notice models.GeneralNotice
	if err := h.DB.Where("id = ? AND status_page_id = ?", noticeID, tenantID).First(&notice).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Notice not found", "code": "notice_not_found"})
		return
	}

	if err := h.DB.Delete(&notice).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete notice", "code": "server_error"})
		return
	}

	h.RecordAudit(c, "notice.deleted", "notice", "GeneralNotice", strconv.FormatUint(uint64(notice.ID), 10), notice.Title, "")
	c.JSON(http.StatusOK, gin.H{"message": "Notice deleted successfully"})
}
