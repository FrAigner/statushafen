package handlers

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/statushistory"
)

// Wiederholte Literale (go:S1192).
const (
	preloadIncidentComponentsComponent = "IncidentComponents.Component"
	whereIDEquals                      = "id = ?"
)

// StartMaintenanceScheduler: einfache Hintergrund-Schleife, die
// geplante Wartungsfenster automatisch weiterschaltet. Läuft im 1-Minuten-Takt,
// analog zum bestehenden RateLimiter-Cleanup-Loop
// (middleware/security.go NewRateLimiter) - bewusst kein externes
// Cron-/Scheduler-Package, da für diesen einen periodischen Task ausreichend.
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

// pruneExpiredPageOrders: unbestätigte, abgelaufene Bestellungen aufräumen.
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

// pruneExpiredPageInvitations: abgelaufene Einladungen aller
// Seiten entfernen; eine einzelne DELETE-Anweisung je Tick.
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

// statusPruneInterval: Die Bereinigung der Statushistorie läuft höchstens
// einmal am Tag.
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

// sendDueMaintenanceReminders verschickt die (optionale, vom Admin pro
// Wartungsfenster konfigurierte) Erinnerungs-Benachrichtigung, wenn die
// verbleibende Zeit bis ScheduledStartAt kleiner als ReminderLeadMinutes ist.
// ReminderSentAt verhindert Mehrfachversand bei jedem Tick.
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

// applyMaintenanceStatusTransition speichert den neuen Incident-Status und
// setzt den Status aller betroffenen Komponenten, bricht bei einem
// DB-Fehler geloggt ab (false) statt mit einem inkonsistenten Zustand
// weiterzumachen.
func (h *Handler) applyMaintenanceStatusTransition(inc *models.Incident, componentStatus string) bool {
	// gezieltes Update statt Save(inc). Save schrieb die vorgeladenen
	// Assoziationen (IncidentComponents.Component) per Upsert zurück: unter
	// SQLite scheiterte das mit "near DEFAULT" bei gemischten parent_id, und
	// auch auf Postgres überschrieb es Komponentenzeilen mit dem (veralteten)
	// Stand aus dem Preload. Geändert wird nur der Status.
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

// finishMaintenanceTransition schaltet einen einzelnen fälligen
// Wartungsfenster-Incident um: Status + Komponentenstatus speichern (bricht
// bei einem DB-Fehler ab, siehe applyMaintenanceStatusTransition), Timeline-
// Eintrag anlegen, Benachrichtigung verschicken, Audit-Log schreiben.
// Parametrisiert über alles, was sich zwischen "Start" und "Complete"
// unterscheidet - startDueMaintenanceWindows/completeDueMaintenanceWindows
// teilen sich dadurch diesen Ablauf, statt ihn (nur mit anderen String-
// Literalen) zu duplizieren.
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

// startDueMaintenanceWindows schaltet "scheduled" -> "in_progress", sobald
// ScheduledStartAt erreicht ist, setzt die betroffenen Komponenten auf
// "under_maintenance" und legt einen Timeline-Eintrag an.
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

// completeDueMaintenanceWindows schaltet "in_progress" -> "resolved" (bewusst
// derselbe Terminal-Status wie bei normalen Incidents, siehe models.Incident),
// sobald ScheduledEndAt erreicht ist, und setzt die Komponenten zurück auf
// "operational". Vereinfachung für v1: ein evtl. unabhängig vom Wartungsfenster
// bereits gestörter Komponenten-Status wird dabei überschrieben.
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
