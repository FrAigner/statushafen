package handlers_test

import (
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
)

// Wartungsfenster-Actions müssen genauso wie Incidents auf
// Custom-Templates ziehen. Deckt die Erweiterung von resolveIncidentKind
// ab (MAINTENANCE_STARTED -> maintenance_started) und stellt gleichzeitig
// sicher, dass ein fehlendes Custom-Template die bisherige Default-Mail
// unverändert lässt (Verifikationskriterium).

func TestDispatchNotifications_MaintenanceCustomTemplate_IsUsed(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "maint-custom-page", "maint-custom-admin@example.com")

	seedTemplate(t, db, page.ID, handlers.NotificationKindMaintenanceStarted, handlers.NotificationAudienceStaff, "en",
		"MAINT SUBJECT :: {{.IncidentTitle}}",
		`<p>MAINT BODY :: {{.Status}} :: {{.Message}}</p>`)

	inc := models.Incident{
		StatusPageID:  page.ID,
		Title:         "DB failover window",
		Message:       "Starting DB failover.",
		Status:        "in_progress",
		IsMaintenance: true,
	}
	if err := db.Create(&inc).Error; err != nil {
		t.Fatalf("create maintenance incident: %v", err)
	}

	h.DispatchNotifications(inc, []models.Component{comp}, "MAINTENANCE_STARTED")

	msg := waitForEmail(t, srv)
	if !strings.Contains(msg, "MAINT SUBJECT :: DB failover window") {
		t.Fatalf("expected maintenance custom subject in message, got:\n%s", msg)
	}
	if !strings.Contains(msg, "MAINT BODY :: in_progress :: Starting DB failover.") {
		t.Fatalf("expected maintenance custom body with placeholders substituted, got:\n%s", msg)
	}
}
