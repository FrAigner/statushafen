package handlers

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/statushistory"
)

// Repeated literals (go:S1192).
const (
	preloadIncidentComponentsComponent = "IncidentComponents.Component"
	whereIDEquals                      = "id = ?"
)

// StartMaintenanceScheduler: simple background loop that
// advances scheduled maintenance windows automatically. Runs on a 1-minute tick,
// analogous to the existing RateLimiter cleanup loop
// (middleware/security.go NewRateLimiter) - deliberately no external
// cron/scheduler package, since it is sufficient for this one periodic task.
func (h *Handler) StartMaintenanceScheduler() {
	go func() {
		for {
			h.runMaintenanceTick()
			time.Sleep(maintenanceSchedulerInterval())
		}
	}()
}

func (h *Handler) runMaintenanceTick() {
	now := time.Now()

	h.sendDueMaintenanceReminders(now)
	h.startDueMaintenanceWindows(now)
	h.completeDueMaintenanceWindows(now)
	h.pruneStatusHistory(now)
	h.pruneExpiredPageInvitations(now)
	h.pruneExpiredPageOrders(now)
}

// pruneExpiredPageOrders: clean up unconfirmed, expired orders.
func (h *Handler) pruneExpiredPageOrders(now time.Time) {
	marked, deleted, err := pruneExpiredOrders(h.DB, now)
	if err != nil {
		slog.Error("Bestellungen: Bereinigung fehlgeschlagen", "error", err)
		return
	}
	if marked+deleted > 0 {
		slog.Info("Abgelaufene Bestellungen bereinigt", "expired", marked, "deleted", deleted)
	}
}

// pruneExpiredPageInvitations: remove expired invitations of all
// pages; one single DELETE statement per tick.
func (h *Handler) pruneExpiredPageInvitations(now time.Time) {
	n, err := pruneExpiredInvitations(h.DB, now)
	if err != nil {
		slog.Error("Einladungen: Bereinigung fehlgeschlagen", "error", err)
		return
	}
	if n > 0 {
		slog.Info("Abgelaufene Einladungen bereinigt", "deleted", n)
	}
}

// statusPruneInterval: the cleanup of the status history runs at most
// once a day.
const statusPruneInterval = 24 * time.Hour

func (h *Handler) pruneStatusHistory(now time.Time) {
	if now.Sub(h.lastStatusPrune) < statusPruneInterval {
		return
	}
	h.lastStatusPrune = now
	n, err := statushistory.Prune(h.DB, now)
	if err != nil {
		slog.Error("Statushistorie: Bereinigung fehlgeschlagen", "error", err)
		return
	}
	if n > 0 {
		slog.Info("Statushistorie bereinigt", "deleted", n)
	}
}

// sendDueMaintenanceReminders sends the (optional, configured by the admin per
// maintenance window) reminder notification when the
// remaining time until ScheduledStartAt is less than ReminderLeadMinutes.
// ReminderSentAt prevents repeated sending on every tick.
func (h *Handler) sendDueMaintenanceReminders(now time.Time) {
	var due []models.Incident
	h.DB.Preload(preloadIncidentComponentsComponent).
		Where("is_maintenance = ? AND status = ? AND reminder_sent_at IS NULL AND reminder_lead_minutes > 0", true, "scheduled").
		Find(&due)

	for _, inc := range due {
		if inc.ScheduledStartAt == nil {
			continue
		}
		reminderAt := inc.ScheduledStartAt.Add(-time.Duration(inc.ReminderLeadMinutes) * time.Minute)
		if now.Before(reminderAt) {
			continue
		}

		comps := componentsOf(inc)
		h.DispatchNotifications(inc, comps, "MAINTENANCE_REMINDER")

		sentAt := now
		h.DB.Model(&models.Incident{}).Where(whereIDEquals, inc.ID).Update("reminder_sent_at", &sentAt)
		slog.Info("Maintenance reminder sent", "incident_id", inc.ID, "title", inc.Title)
	}
}

// applyMaintenanceStatusTransition saves the new incident status and
// sets the status of all affected components; on a
// DB error it aborts with a log entry (false) instead of carrying on
// with an inconsistent state.
func (h *Handler) applyMaintenanceStatusTransition(inc *models.Incident, componentStatus string) bool {
	// targeted update instead of Save(inc). Save wrote the preloaded
	// associations (IncidentComponents.Component) back via upsert: under
	// SQLite that failed with "near DEFAULT" for mixed parent_id, and
	// even on Postgres it overwrote component rows with the (stale)
	// state from the preload. Only the status is changed.
	if err := h.DB.Model(&models.Incident{}).Where(whereIDEquals, inc.ID).Update("status", inc.Status).Error; err != nil {
		slog.Error("Failed maintenance status transition", "incident_id", inc.ID, "new_status", inc.Status, "error", err)
		return false
	}
	for _, comp := range componentsOf(*inc) {
		if _, err := statushistory.Set(h.DB, comp.ID, "", componentStatus, models.StatusSourceMaintenance, time.Now()); err != nil {
			slog.Error("Failed to update component status", "incident_id", inc.ID, "component_id", comp.ID, "status", componentStatus, "error", err)
			return false
		}
	}
	return true
}

// finishMaintenanceTransition advances a single due
// maintenance-window incident: save status + component status (aborts
// on a DB error, see applyMaintenanceStatusTransition), timeline
// entry, send notification, write audit log.
// Parameterized over everything that differs between "start" and "complete"
// - startDueMaintenanceWindows/completeDueMaintenanceWindows
// thereby share this flow instead of duplicating it (only with different string
// literals).
func (h *Handler) finishMaintenanceTransition(inc models.Incident, newStatus, componentStatus, timelineMessage, notificationAction, auditAction, logMessage string) {
	inc.Status = newStatus
	if !h.applyMaintenanceStatusTransition(&inc, componentStatus) {
		return
	}

	comps := componentsOf(inc)
	h.DB.Create(&models.IncidentUpdate{
		IncidentID: inc.ID,
		Status:     newStatus,
		Message:    timelineMessage,
	})

	h.DispatchNotifications(inc, comps, notificationAction)
	h.RecordSystemAudit(auditAction, "maintenance", "Incident", strconv.FormatUint(uint64(inc.ID), 10), inc.Title, "")
	slog.Info(logMessage, "incident_id", inc.ID, "title", inc.Title)
}

// startDueMaintenanceWindows switches "scheduled" -> "in_progress" as soon as
// ScheduledStartAt is reached, sets the affected components to
// "under_maintenance" and creates a timeline entry.
func (h *Handler) startDueMaintenanceWindows(now time.Time) {
	var due []models.Incident
	h.DB.Preload(preloadIncidentComponentsComponent).
		Where("is_maintenance = ? AND status = ? AND scheduled_start_at IS NOT NULL AND scheduled_start_at <= ?", true, "scheduled", now).
		Find(&due)

	for _, inc := range due {
		h.finishMaintenanceTransition(inc, "in_progress", "under_maintenance",
			"Wartungsfenster hat automatisch begonnen.", "MAINTENANCE_STARTED",
			"maintenance.window_started", "Maintenance window started")
	}
}

// completeDueMaintenanceWindows switches "in_progress" -> "resolved" (deliberately the
// same terminal status as for normal incidents, see models.Incident),
// once ScheduledEndAt is reached, and resets the components to
// "operational". Simplification for v1: a component status that may already be disturbed
// independently of the maintenance window is overwritten in the process.
func (h *Handler) completeDueMaintenanceWindows(now time.Time) {
	var due []models.Incident
	h.DB.Preload(preloadIncidentComponentsComponent).
		Where("is_maintenance = ? AND status = ? AND scheduled_end_at IS NOT NULL AND scheduled_end_at <= ?", true, "in_progress", now).
		Find(&due)

	for _, inc := range due {
		h.finishMaintenanceTransition(inc, "resolved", "operational",
			"Wartungsfenster wurde automatisch abgeschlossen.", "MAINTENANCE_COMPLETED",
			"maintenance.window_completed", "Maintenance window completed")
	}
}

func componentsOf(inc models.Incident) []models.Component {
	comps := make([]models.Component, len(inc.IncidentComponents))
	for i, ic := range inc.IncidentComponents {
		comps[i] = ic.Component
	}
	return comps
}
