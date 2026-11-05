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
// Audit-Log (Review-Befund A4)
//
// Routen (in main.go, innerhalb der authGroup):
//   auditAdmin := authGroup.Group("/admin/audit")
//   auditAdmin.Use(middleware.RequireGlobalAdmin())
//   auditAdmin.GET("", h.GetAuditLogsHandler)
// ─────────────────────────────────────────────────────────────────────────────

// RecordAudit schreibt einen Audit-Eintrag. Aus mutierenden Handlern aufrufen,
// z.B. am Ende von UpdateIncidentHandler:
//
//	h.RecordAudit(c, "incident.status_changed", "incident", "Incident",
//	    strconv.FormatUint(uint64(incident.ID), 10), incident.Title, "")
//
// Fehler werden bewusst nur geloggt, nicht an den Aufrufer zurückgegeben –
// eine Audit-Panne darf die eigentliche Aktion nicht scheitern lassen.
func (h *Handler) RecordAudit(c *gin.Context, action, category, targetType, targetID, targetName, details string) {
	entry := newAuditEntry(c, action, category, targetType, targetID, targetName, details)
	if err := h.DB.Create(&entry).Error; err != nil {
		// Nur loggen – Aktion nicht blockieren.
		c.Error(err) //nolint:errcheck
	}
	logAuditLine(c, entry)
}

// newAuditEntry baut den Audit-Eintrag aus dem Request (Review F3:
// getrennt von RecordAudit, damit ein Handler ihn in seiner Transaktion
// schreiben kann).
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

// logAuditLine schreibt die Access-Log-Zeile zum Audit-Eintrag.
func logAuditLine(c *gin.Context, entry models.AuditLog) {
	action, category, targetType, targetID := entry.Action, entry.Category, entry.TargetType, entry.TargetID
	// AuditLog hat (bewusst ohne Schemaänderung) kein
	// Request-ID-Feld - die Logzeile über den Request-Logger trägt sie und
	// verknüpft den Eintrag mit dem Access-Log. TargetName/Details bleiben
	// draußen (enthalten oft E-Mail-Adressen).
	attrs := []any{"action", action, "category", category, "target_type", targetType, "target_id", targetID}
	if entry.ActorID != nil {
		attrs = append(attrs, "actor_id", *entry.ActorID)
	}
	logging.FromContext(c.Request.Context()).Info("audit", attrs...)
}

// RecordSystemAudit schreibt einen Audit-Eintrag für Vorgänge, die nicht im
// Rahmen eines HTTP-Requests laufen (kein *gin.Context verfügbar), z.B. den
// asynchronen E-Mail-Versand in DispatchNotifications (auch aus dem
// Maintenance-Scheduler heraus aufgerufen) oder künftige
// Hintergrund-Jobs. ActorID/ActorEmail bleiben leer bzw. "system" – siehe
// models.AuditLog ("ActorID *uint ... nil = System/Automation").
//
// Macht sichtbar, wenn z.B. der Benachrichtigungsversand fehlschlägt, statt
// dass der Fehler nur im Server-Log (slog.Error) landet.
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
		// Nur loggen – ein Audit-Fehler darf den Aufrufer nicht blockieren.
		slog.Error("Failed to write system audit log entry", "action", action, "error", err)
	}
}

// GetAuditLogsHandler liefert paginierte, filterbare Audit-Einträge.
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
		// Ein unbekannter Wert lieferte still eine leere Liste;
		// jetzt ein klarer Fehler (Tippfehler im Aufrufer fallen auf).
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
