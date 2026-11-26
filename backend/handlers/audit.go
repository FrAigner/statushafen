package handlers

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/logging"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// Audit log (review finding A4)
//
// Routes (in main.go, inside the authGroup):
//   auditAdmin := authGroup.Group("/admin/audit")
//   auditAdmin.Use(middleware.RequireGlobalAdmin())
//   auditAdmin.GET("", h.GetAuditLogsHandler)
// ─────────────────────────────────────────────────────────────────────────────

// RecordAudit writes an audit entry. Call it from mutating handlers,
// e.g. at the end of UpdateIncidentHandler:
//
//	h.RecordAudit(c, "incident.status_changed", "incident", "Incident",
//	    strconv.FormatUint(uint64(incident.ID), 10), incident.Title, "")
//
// Errors are deliberately only logged, not returned to the caller -
// an audit failure must not make the actual action fail.
func (h *Handler) RecordAudit(c *gin.Context, action, category, targetType, targetID, targetName, details string) {
	entry := newAuditEntry(c, action, category, targetType, targetID, targetName, details)
	if err := h.DB.Create(&entry).Error; err != nil {
		// Only log - do not block the action.
		c.Error(err) //nolint:errcheck
	}
	logAuditLine(c, entry)
}

// newAuditEntry builds the audit entry from the request (Review F3:
// separate from RecordAudit so that a handler can write it in its
// transaction).
func newAuditEntry(c *gin.Context, action, category, targetType, targetID, targetName, details string) models.AuditLog {
	entry := models.AuditLog{
		Action:     action,
		Category:   category,
		TargetType: targetType,
		TargetID:   targetID,
		TargetName: targetName,
		Details:    details,
		IP:         c.ClientIP(),
		ActorEmail: getEmail(c),
	}
	if role, ok := c.Get("role"); ok {
		if s, ok := role.(string); ok {
			entry.ActorRole = s
		}
	}
	if uid, ok := c.Get("userID"); ok {
		if id, ok := uid.(uint); ok {
			entry.ActorID = &id
		}
	}
	return entry
}

// logAuditLine writes the access log line for the audit entry.
func logAuditLine(c *gin.Context, entry models.AuditLog) {
	action, category, targetType, targetID := entry.Action, entry.Category, entry.TargetType, entry.TargetID
	// AuditLog deliberately (without a schema change) has no
	// request ID field - the log line via the request logger carries it and
	// links the entry to the access log. TargetName/Details are left
	// out (they often contain email addresses).
	attrs := []any{"action", action, "category", category, "target_type", targetType, "target_id", targetID}
	if entry.ActorID != nil {
		attrs = append(attrs, "actor_id", *entry.ActorID)
	}
	logging.FromContext(c.Request.Context()).Info("audit", attrs...)
}

// RecordSystemAudit writes an audit entry for operations that do not run
// within an HTTP request (no *gin.Context available), e.g. the
// asynchronous email dispatch in DispatchNotifications (also called from the
// maintenance scheduler) or future
// background jobs. ActorID/ActorEmail stay empty or "system" – see
// models.AuditLog ("ActorID *uint ... nil = System/Automation").
//
// Makes it visible when e.g. notification dispatch fails, instead of
// the error only ending up in the server log (slog.Error).
func (h *Handler) RecordSystemAudit(action, category, targetType, targetID, targetName, details string) {
	entry := models.AuditLog{
		Action:     action,
		Category:   category,
		TargetType: targetType,
		TargetID:   targetID,
		TargetName: targetName,
		Details:    details,
		ActorEmail: "system",
	}
	if err := h.DB.Create(&entry).Error; err != nil {
		// Only log - an audit error must not block the caller.
		slog.Error("Failed to write system audit log entry", "action", action, "error", err)
	}
}

// GetAuditLogsHandler returns paginated, filterable audit entries.
// Query-Parameter: ?page=1&limit=25&category=incident (auth|incident|config|user|subscriber|notice|maintenance|system)&q=INC-2041
func (h *Handler) GetAuditLogsHandler(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "25"))
	if limit < 1 || limit > 200 {
		limit = 25
	}
	offset := (page - 1) * limit

	q := h.DB.Model(&models.AuditLog{})
	if cat := c.Query("category"); cat != "" {
		// An unknown value used to silently return an empty list;
		// now a clear error (typos in the caller stand out).
		if !models.IsAuditCategory(cat) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown audit category", "code": "invalid_category"})
			return
		}
		q = q.Where("category = ?", cat)
	}
	if search := c.Query("q"); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		q = q.Where(
			"LOWER(actor_email) LIKE ? OR LOWER(target_name) LIKE ? OR LOWER(target_id) LIKE ? OR LOWER(ip) LIKE ?",
			like, like, like, like)
	}

	var total int64
	q.Count(&total)

	var items []models.AuditLog
	if err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch audit log", "code": "server_error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}
