package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Admin-Nachrichtencenter (Glocke). Dieser Dienst schreibt die
// Nachrichten (NotifyAdmins), die Handler darunter liefern sie an die Admins
// aus. Kein Audit-Eintrag: Nachrichten sind flüchtige Betriebsmeldungen und
// werden nach adminNotificationRetention gelöscht.

const (
	// adminNotificationWindow: Innerhalb dieser Zeit fasst NotifyAdmins gleiche
	// Meldungen (gleicher DedupKey) zusammen.
	adminNotificationWindow = 15 * time.Minute

	// Nachrichtenarten (Kind). Die Texte stehen im Frontend unter
	// adminNotifications.<kind>.title/body.
	AdminNotifExternalSourceOutage    = "external_source_outage"
	AdminNotifExternalSourceRecovered = "external_source_recovered"
	AdminNotifWebhookRateLimited      = "webhook_rate_limited"
	AdminNotifWebhookRejected         = "webhook_rejected"

	adminNotifDefaultLimit = 20
	adminNotifMaxLimit     = 100
	permSubscribersWrite   = "subscribers:update"
	adminSubscribersPath   = "/admin/subscribers"
	// adminNotifUnreadClause: für den angemeldeten Nutzer noch nicht gelesen.
	adminNotifUnreadClause = "NOT EXISTS (SELECT 1 FROM notification_reads r WHERE r.notification_id = admin_notifications.id AND r.user_id = ?)"
)

// adminNotifForbiddenParams: Parameter, die nie in einer Nachricht landen
// dürfen (Webhook-URL/-Schlüssel/-Token, Adressen von Abonnenten).
var adminNotifForbiddenParams = []string{"url", "key", "token", "secret", "password", "email", "mail", "address", "webhook"}

// sanitizeAdminNotifParams entfernt Werte, die geheim oder personenbezogen
// sein können: Schlüssel mit den Wörtern aus adminNotifForbiddenParams sowie
// Zeichenketten, die wie URL oder E-Mail-Adresse aussehen. Das Ergebnis ist
// ein JSON-Objekt als Text.
func sanitizeAdminNotifParams(params map[string]any) string {
	safe := map[string]any{}
	for k, v := range params {
		lower := strings.ToLower(k)
		forbidden := false
		for _, bad := range adminNotifForbiddenParams {
			if strings.Contains(lower, bad) {
				forbidden = true
				break
			}
		}
		if s, ok := v.(string); ok && (strings.Contains(s, "://") || strings.Contains(s, "@")) {
			forbidden = true
		}
		if !forbidden {
			safe[k] = v
		}
	}
	b, err := json.Marshal(safe)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// sanitizeAdminNotifLink lässt nur relative Admin-Pfade zu (kein Open Redirect).
func sanitizeAdminNotifLink(p string) string {
	if !strings.HasPrefix(p, "/admin/") || strings.Contains(p, "//") || strings.Contains(p, "\\") || strings.Contains(p, ":") {
		return ""
	}
	return p
}

func adminNotifKeyPrefix(pageID *uint) string {
	if pageID == nil {
		return "g:"
	}
	return "p" + strconv.FormatUint(uint64(*pageID), 10) + ":"
}

func (h *Handler) adminNotifNow() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

// NotifyAdmins schreibt eine Nachricht für die Admins der Statusseite pageID
// (nil = systemweit, nur Global Admins) oder fasst sie mit einer offenen
// gleichen zusammen. Gleicher dedupKey (je Seite) innerhalb von 15 Minuten:
// Count wird erhöht, LastSeenAt aktualisiert und der Lesestatus aller Benutzer
// zurückgesetzt, die Nachricht lebt also wieder als ungelesen auf. Danach
// entsteht eine neue Zeile (die alte verliert ihren Schlüssel). Leerer
// dedupKey: nie zusammenfassen.
//
// Ein Fehler wird nur geloggt, der Aufrufer (Versand, Abruf-Takt) wird nie
// blockiert oder abgebrochen. params darf nur unkritische Werte enthalten; die
// Funktion filtert URL-/Schlüssel-/Token-/E-Mail-artige Werte zusätzlich.
func (h *Handler) NotifyAdmins(pageID *uint, kind, severity, titleKey, bodyKey string, params map[string]any, linkPath, dedupKey string) {
	if severity != models.AdminNotificationInfo && severity != models.AdminNotificationWarning && severity != models.AdminNotificationError {
		severity = models.AdminNotificationInfo
	}
	now := h.adminNotifNow()
	n := models.AdminNotification{
		StatusPageID: pageID, Kind: kind, Severity: severity, TitleKey: titleKey, BodyKey: bodyKey,
		Params: sanitizeAdminNotifParams(params), LinkPath: sanitizeAdminNotifLink(linkPath),
		Count: 1, FirstSeenAt: now, LastSeenAt: now,
	}
	if dedupKey != "" {
		n.DedupKey = adminNotifKeyPrefix(pageID) + dedupKey
		n.GroupKey = n.DedupKey
	}
	if err := h.upsertAdminNotification(n, now); err != nil {
		slog.Error("Admin-Nachricht nicht gespeichert", "kind", kind, "error", err)
	}
}

func (h *Handler) upsertAdminNotification(n models.AdminNotification, now time.Time) error {
	if n.DedupKey != "" && h.mergeAdminNotification(n, now) {
		return nil
	}
	err := h.createAdminNotification(n, now)
	// Unique-Verletzung: Eine parallele Anfrage hat die offene Meldung
	// inzwischen angelegt, dann zählt diese hoch statt zu scheitern.
	if err != nil && n.DedupKey != "" && isUniqueViolation(err) && h.mergeAdminNotification(n, now) {
		return nil
	}
	return err
}

func (h *Handler) createAdminNotification(n models.AdminNotification, now time.Time) error {
	if n.DedupKey != "" {
		// Nur abgelaufene Zeilen ablösen: eine frische, parallel angelegte
		// darf nicht ihren Schlüssel verlieren.
		if err := h.DB.Model(&models.AdminNotification{}).
			Where("dedup_key = ? AND last_seen_at < ?", n.DedupKey, now.Add(-adminNotificationWindow)).
			Update("dedup_key", "").Error; err != nil {
			return err
		}
	}
	return h.DB.Create(&n).Error
}

// mergeAdminNotification zählt die offene Meldung mit gleichem Schlüssel hoch
// (nur innerhalb des Zeitfensters) und setzt den Lesestatus zurück. false =
// nichts zusammengefasst (keine offene Meldung, parallel verändert oder
// Datenbankfehler, der geloggt wird); der Aufrufer legt dann neu an.
func (h *Handler) mergeAdminNotification(n models.AdminNotification, now time.Time) bool {
	var found []models.AdminNotification
	if err := h.DB.Where("dedup_key = ? AND last_seen_at >= ?", n.DedupKey, now.Add(-adminNotificationWindow)).Limit(1).Find(&found).Error; err != nil {
		slog.Error("Admin-Nachricht: offene Meldung nicht lesbar", "error", err)
		return false
	}
	if len(found) == 0 {
		return false
	}
	res := h.DB.Model(&models.AdminNotification{}).
		Where("id = ? AND dedup_key = ?", found[0].ID, n.DedupKey).
		Updates(map[string]any{
			"count": gorm.Expr("count + 1"), "last_seen_at": now, "updated_at": now,
			"severity": n.Severity, "params": n.Params, "link_path": n.LinkPath,
			"title_key": n.TitleKey, "body_key": n.BodyKey,
		})
	if res.Error != nil {
		slog.Error("Admin-Nachricht: Zusammenfassen fehlgeschlagen", "error", res.Error)
		return false
	}
	if res.RowsAffected == 0 {
		return false // parallel erledigt oder abgelöst
	}
	if err := h.DB.Where("notification_id = ?", found[0].ID).Delete(&models.NotificationRead{}).Error; err != nil {
		slog.Error("Admin-Nachricht: Lesestatus nicht zurückgesetzt", "error", err)
	}
	return true
}

// ResolveAdminNotifications markiert alle offenen Zeilen mit diesem dedupKey
// (auch die durch das Zeitfenster abgelösten) als erledigt (Entwarnung): Sie
// bleibt in der Liste, zählt aber nicht mehr als ungelesen, und eine spätere gleiche Meldung fasst sich nicht mehr mit ihr
// zusammen. Fehler werden nur geloggt.
func (h *Handler) ResolveAdminNotifications(pageID *uint, dedupKey string) {
	if dedupKey == "" {
		return
	}
	now := h.adminNotifNow()
	err := h.DB.Model(&models.AdminNotification{}).
		Where("group_key = ? AND resolved_at IS NULL", adminNotifKeyPrefix(pageID)+dedupKey).
		Updates(map[string]any{"resolved_at": now, "updated_at": now, "dedup_key": ""}).Error
	if err != nil {
		slog.Error("Admin-Nachricht nicht als erledigt markiert", "error", err)
	}
}

// NotifyWebhookRateLimited meldet, dass ein Webhook-Kanal (z. B. google_chat)
// mit HTTP 429 begrenzt wird. Je Seite und Kanal zusammengefasst. Für den
// zentralen Webhook-Sender (retryAfterSeconds 0 = unbekannt).
func (h *Handler) NotifyWebhookRateLimited(pageID uint, channel string, retryAfterSeconds int) {
	h.NotifyAdmins(&pageID, AdminNotifWebhookRateLimited, models.AdminNotificationWarning,
		"adminNotifications.webhook_rate_limited.title", "adminNotifications.webhook_rate_limited.body",
		map[string]any{"channel": channel, "seconds": retryAfterSeconds}, adminSubscribersPath,
		AdminNotifWebhookRateLimited+":"+channel)
}

// NotifyWebhookRejected meldet, dass ein Webhook dauerhaft abgelehnt wird
// (z. B. 401/403/404/410). Je Seite, Kanal und Abonnent zusammengefasst. Die
// Webhook-URL gehört nie in die Nachricht, nur die Abonnenten-ID.
func (h *Handler) NotifyWebhookRejected(pageID uint, channel string, subscriberID uint) {
	h.NotifyAdmins(&pageID, AdminNotifWebhookRejected, models.AdminNotificationError,
		"adminNotifications.webhook_rejected.title", "adminNotifications.webhook_rejected.body",
		map[string]any{"channel": channel, "subscriber_id": subscriberID}, adminSubscribersPath,
		AdminNotifWebhookRejected+":"+channel+":"+strconv.FormatUint(uint64(subscriberID), 10))
}

// ---------------------------------------------------------------------------
// Auslieferung
// ---------------------------------------------------------------------------

// adminNotifScope bestimmt Umfang und Berechtigung der Anfrage. Mit :page_id
// gelten die Nachrichten der Seite für Global Admins sowie Rollen mit
// external_sources:update oder subscribers:update auf genau dieser Seite; ohne
// :page_id (systemweit) nur für Global Admins. Bei Fehler ist die Antwort
// schon geschrieben.
func (h *Handler) adminNotifScope(c *gin.Context) (*uint, uint, bool) {
	userID := getUserID(c)
	var user models.User
	if userID == 0 || h.DB.First(&user, userID).Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized", "code": "unauthorized"})
		return nil, 0, false
	}
	global := middleware.EffectiveGlobalAdmin(c, user)
	if c.Param("page_id") == "" {
		if !global {
			c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden", "code": "forbidden"})
			return nil, 0, false
		}
		return nil, userID, true
	}
	pageID, ok := parseTenantID(c)
	if !ok {
		return nil, 0, false
	}
	if global {
		var n int64
		if err := h.DB.Model(&models.StatusPage{}).Where("id = ?", pageID).Count(&n).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
			return nil, 0, false
		}
		if n == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Status page not found", "code": "page_not_found"})
			return nil, 0, false
		}
		return &pageID, userID, true
	}
	if !middleware.SSOAllowsPage(c, pageID) || !h.hasNotificationRole(userID, pageID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden", "code": "forbidden"})
		return nil, 0, false
	}
	return &pageID, userID, true
}

// hasNotificationRole: Rolle auf der Seite mit external_sources:update oder subscribers:update.
func (h *Handler) hasNotificationRole(userID, pageID uint) bool {
	var roles []models.UserStatusPageRole
	if err := h.DB.Preload("Role").Where("user_id = ? AND status_page_id = ?", userID, pageID).Find(&roles).Error; err != nil {
		return false
	}
	for _, r := range roles {
		if permissionListContains(r.Role.Permissions, permComponentsWrite) || permissionListContains(r.Role.Permissions, permSubscribersWrite) {
			return true
		}
	}
	return false
}

func adminNotifScopeQuery(db *gorm.DB, pageID *uint) *gorm.DB {
	q := db.Model(&models.AdminNotification{})
	if pageID == nil {
		return q.Where("status_page_id IS NULL")
	}
	return q.Where("status_page_id = ?", *pageID)
}

type adminNotificationItem struct {
	models.AdminNotification
	Params json.RawMessage `json:"params"`
	Read   bool            `json:"read"`
}

// GET /api/pages/:page_id/admin-notifications bzw. /api/admin-notifications
// (?limit=20&unread=1): neueste zuerst, mit Lesestatus des angemeldeten Nutzers.
func (h *Handler) ListAdminNotificationsHandler(c *gin.Context) {
	pageID, userID, ok := h.adminNotifScope(c)
	if !ok {
		return
	}
	limit := adminNotifDefaultLimit
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 {
		limit = min(v, adminNotifMaxLimit)
	}
	q := adminNotifScopeQuery(h.DB, pageID)
	if c.Query("unread") == "1" || c.Query("unread") == "true" {
		q = q.Where("resolved_at IS NULL").Where(adminNotifUnreadClause, userID)
	}
	var rows []models.AdminNotification
	if err := q.Order("last_seen_at desc, id desc").Limit(limit).Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
		return
	}
	ids := make([]uint, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	readSet := map[uint]bool{}
	if len(ids) > 0 {
		var read []uint
		if err := h.DB.Model(&models.NotificationRead{}).Where("user_id = ? AND notification_id IN ?", userID, ids).Pluck("notification_id", &read).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
			return
		}
		for _, id := range read {
			readSet[id] = true
		}
	}
	items := make([]adminNotificationItem, 0, len(rows))
	for _, r := range rows {
		p := json.RawMessage(r.Params)
		if !json.Valid(p) {
			p = json.RawMessage("{}")
		}
		items = append(items, adminNotificationItem{AdminNotification: r, Params: p, Read: readSet[r.ID]})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// GET .../admin-notifications/unread-count: leichte Abfrage für das Polling.
// Erledigte Nachrichten zählen nicht.
func (h *Handler) AdminNotificationUnreadCountHandler(c *gin.Context) {
	pageID, userID, ok := h.adminNotifScope(c)
	if !ok {
		return
	}
	var n int64
	if err := adminNotifScopeQuery(h.DB, pageID).Where("resolved_at IS NULL").Where(adminNotifUnreadClause, userID).Count(&n).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"unread_count": n})
}

// POST .../admin-notifications/:id/read: eigene Lesemarke, idempotent. Eine
// Nachricht außerhalb des Umfangs (andere Seite, systemweit) ist 404.
func (h *Handler) MarkAdminNotificationReadHandler(c *gin.Context) {
	pageID, userID, ok := h.adminNotifScope(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id", "code": "invalid_id"})
		return
	}
	var n int64
	if err := adminNotifScopeQuery(h.DB, pageID).Where("id = ?", uint(id)).Count(&n).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
		return
	}
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Notification not found", "code": "notification_not_found"})
		return
	}
	read := models.NotificationRead{NotificationID: uint(id), UserID: userID, ReadAt: h.adminNotifNow()}
	if err := h.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&read).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
		return
	}
	c.Status(http.StatusNoContent)
}

// POST .../admin-notifications/read-all: alle Nachrichten des Umfangs als
// gelesen markieren (nur für den angemeldeten Nutzer).
func (h *Handler) MarkAllAdminNotificationsReadHandler(c *gin.Context) {
	pageID, userID, ok := h.adminNotifScope(c)
	if !ok {
		return
	}
	scope := "status_page_id IS NULL"
	args := []any{userID, h.adminNotifNow()}
	if pageID != nil {
		scope = "status_page_id = ?"
		args = append(args, *pageID)
	}
	args = append(args, userID)
	sql := "INSERT INTO notification_reads (notification_id, user_id, read_at) " +
		"SELECT id, ?, ? FROM admin_notifications WHERE " + scope + " AND " + adminNotifUnreadClause +
		" ON CONFLICT DO NOTHING"
	if err := h.DB.Exec(sql, args...).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
		return
	}
	c.Status(http.StatusNoContent)
}
