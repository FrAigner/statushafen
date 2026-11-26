package handlers

import (
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"gorm.io/gorm"
)

// White-box tests (package handlers instead of handlers_test) for the
// maintenance window scheduler: runMaintenanceTick() and its
// sub-steps are unexported, an external _test package would have no
// access.

// requireIncidentStatus reloads the incident and aborts the (sub)test
// if the status is not as expected - reduces the otherwise per-test-case
// repeated reload/if/Errorf boilerplate.
func requireIncidentStatus(t *testing.T, db *gorm.DB, incidentID uint, want string) {
	t.Helper()
	var reloaded models.Incident
	db.First(&reloaded, incidentID)
	if reloaded.Status != want {
		t.Errorf("Expected status %q, got %q", want, reloaded.Status)
	}
}

// requireComponentStatus reloads the component and aborts the (sub)test
// if the status is not as expected.
func requireComponentStatus(t *testing.T, db *gorm.DB, componentID uint, want string) {
	t.Helper()
	var reloaded models.Component
	db.First(&reloaded, componentID)
	if reloaded.Status != want {
		t.Errorf("Expected component status %q, got %q", want, reloaded.Status)
	}
}

// requireAuditLogCount counts AuditLog entries for the given action and
// TargetID and aborts the (sub)test if the count does not match.
func requireAuditLogCount(t *testing.T, db *gorm.DB, action string, targetID uint, want int64) {
	t.Helper()
	var count int64
	db.Model(&models.AuditLog{}).Where("action = ? AND target_id = ?", action, strconv.FormatUint(uint64(targetID), 10)).Count(&count)
	if count != want {
		t.Errorf("Expected %d audit log entries for %s, got %d", want, action, count)
	}
}

func TestMaintenanceScheduler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)

	page := models.StatusPage{Name: "Scheduler Page", Slug: "scheduler-page"}
	db.Create(&page)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	db.Create(&comp)

	t.Run("startDueMaintenanceWindows schaltet fällige geplante Fenster auf in_progress und Komponente auf under_maintenance", func(t *testing.T) {
		past := time.Now().Add(-5 * time.Minute)
		future := time.Now().Add(1 * time.Hour)
		inc := models.Incident{
			StatusPageID: page.ID, Title: "Wartung A", Message: "m", Status: "scheduled",
			IsMaintenance: true, ScheduledStartAt: &past, ScheduledEndAt: &future,
		}
		db.Create(&inc)
		db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID})

		h.runMaintenanceTick()

		requireIncidentStatus(t, db, inc.ID, "in_progress")
		requireComponentStatus(t, db, comp.ID, "under_maintenance")

		var updateCount int64
		db.Model(&models.IncidentUpdate{}).Where("incident_id = ?", inc.ID).Count(&updateCount)
		if updateCount != 1 {
			t.Errorf("Expected 1 timeline update, got %d", updateCount)
		}

		requireAuditLogCount(t, db, "maintenance.window_started", inc.ID, 1)
	})

	t.Run("completeDueMaintenanceWindows schaltet fällige laufende Fenster auf resolved und Komponente zurück auf operational", func(t *testing.T) {
		start := time.Now().Add(-2 * time.Hour)
		end := time.Now().Add(-1 * time.Minute)
		comp2 := models.Component{StatusPageID: page.ID, Name: "DB", Status: "under_maintenance"}
		db.Create(&comp2)
		inc := models.Incident{
			StatusPageID: page.ID, Title: "Wartung B", Message: "m", Status: "in_progress",
			IsMaintenance: true, ScheduledStartAt: &start, ScheduledEndAt: &end,
		}
		db.Create(&inc)
		db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp2.ID})

		h.runMaintenanceTick()

		requireIncidentStatus(t, db, inc.ID, "resolved")
		requireComponentStatus(t, db, comp2.ID, "operational")
		requireAuditLogCount(t, db, "maintenance.window_completed", inc.ID, 1)
	})

	t.Run("sendDueMaintenanceReminders setzt ReminderSentAt und verschickt nur einmal", func(t *testing.T) {
		start := time.Now().Add(30 * time.Minute) // Reminder window (60 min before) has been reached
		end := time.Now().Add(2 * time.Hour)
		inc := models.Incident{
			StatusPageID: page.ID, Title: "Wartung C", Message: "m", Status: "scheduled",
			IsMaintenance: true, ScheduledStartAt: &start, ScheduledEndAt: &end,
			ReminderLeadMinutes: 60,
		}
		db.Create(&inc)
		db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID})

		h.runMaintenanceTick()

		var reloaded models.Incident
		db.First(&reloaded, inc.ID)
		if reloaded.ReminderSentAt == nil {
			t.Fatal("Expected ReminderSentAt to be set")
		}
		firstSentAt := *reloaded.ReminderSentAt

		// A second tick must not send/overwrite the reminder again.
		h.runMaintenanceTick()
		db.First(&reloaded, inc.ID)
		if !reloaded.ReminderSentAt.Equal(firstSentAt) {
			t.Errorf("Expected ReminderSentAt to stay unchanged on second tick, got %v vs %v", reloaded.ReminderSentAt, firstSentAt)
		}
	})

	t.Run("nicht fällige Wartungsfenster bleiben unverändert", func(t *testing.T) {
		future := time.Now().Add(2 * time.Hour)
		futureEnd := time.Now().Add(4 * time.Hour)
		inc := models.Incident{
			StatusPageID: page.ID, Title: "Wartung D", Message: "m", Status: "scheduled",
			IsMaintenance: true, ScheduledStartAt: &future, ScheduledEndAt: &futureEnd,
		}
		db.Create(&inc)
		db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID})

		h.runMaintenanceTick()

		requireIncidentStatus(t, db, inc.ID, "scheduled")
	})
}

// seedTriggerTestFixtures creates a StatusPage with a component and
// an incident linked to it - shared fixture for the two
// trigger-based error-path tests below, which otherwise differ only
// in the table name of the blocking trigger.
func seedTriggerTestFixtures(db *gorm.DB, slug string) (models.Component, models.Incident) {
	page := models.StatusPage{Name: slug, Slug: slug}
	db.Create(&page)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	db.Create(&comp)
	past := time.Now().Add(-5 * time.Minute)
	future := time.Now().Add(1 * time.Hour)
	inc := models.Incident{
		StatusPageID: page.ID, Title: "Blockierte Wartung", Message: "m", Status: "scheduled",
		IsMaintenance: true, ScheduledStartAt: &past, ScheduledEndAt: &future,
	}
	db.Create(&inc)
	db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID})
	return comp, inc
}

// withBlockedTableWrites installs a SQLite trigger that aborts every UPDATE
// on the given table with an error, runs fn and
// removes the trigger afterwards - simulates a DB error isolated
// to exactly one table, without (like a blanket read-only
// DB file) also blocking downstream writes that
// actually must NOT happen.
func withBlockedTableWrites(t *testing.T, db *gorm.DB, table string, fn func()) {
	t.Helper()
	triggerName := "block_" + table + "_update"
	if err := db.Exec("CREATE TRIGGER " + triggerName + " BEFORE UPDATE ON " + table + " BEGIN SELECT RAISE(ABORT, 'blocked for test'); END;").Error; err != nil {
		t.Fatalf("failed to install test trigger on %s: %v", table, err)
	}
	defer db.Exec("DROP TRIGGER " + triggerName)
	fn()
}

// TestMaintenanceScheduler_SaveErrorAbortsTransition:
// startDueMaintenanceWindows ignored h.DB.Save's error, so a failed status
// transition still went on to update the component status and record a
// timeline entry as if it had succeeded.
func TestMaintenanceScheduler_SaveErrorAbortsTransition(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	comp, inc := seedTriggerTestFixtures(db, "trigger-scheduler-page")

	withBlockedTableWrites(t, db, "incidents", func() {
		h.startDueMaintenanceWindows(time.Now())
	})

	requireIncidentStatus(t, db, inc.ID, "scheduled")
	requireComponentStatus(t, db, comp.ID, "operational")

	var updateCount int64
	db.Model(&models.IncidentUpdate{}).Where("incident_id = ?", inc.ID).Count(&updateCount)
	if updateCount != 0 {
		t.Errorf("expected no timeline update to be recorded when the transition aborts, got %d", updateCount)
	}
}

// TestApplyMaintenanceStatusTransition_ComponentUpdateErrorAborts covers the
// second failure branch of even after the incident itself was
// saved successfully, a failing component status update must abort the
// transition (no timeline entry/notifications) instead of continuing as if
// nothing happened.
func TestApplyMaintenanceStatusTransition_ComponentUpdateErrorAborts(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	comp, inc := seedTriggerTestFixtures(db, "component-trigger-page")

	withBlockedTableWrites(t, db, "components", func() {
		var reloaded models.Incident
		db.Preload(preloadIncidentComponentsComponent).First(&reloaded, inc.ID)
		reloaded.Status = "in_progress"

		if h.applyMaintenanceStatusTransition(&reloaded, "under_maintenance") {
			t.Fatal("expected applyMaintenanceStatusTransition to report failure when the component update fails")
		}
	})

	requireIncidentStatus(t, db, inc.ID, "in_progress")
	requireComponentStatus(t, db, comp.ID, "operational")
}

// The scheduler must not replace the status of a group.
func TestMaintenanceScheduler_KeepsGroupStatus(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "Grp", Slug: "sched-group-530"}
	db.Create(&page)
	group := models.Component{StatusPageID: page.ID, Name: "Gruppe", Status: "group"}
	db.Create(&group)
	child := models.Component{StatusPageID: page.ID, Name: "Kind", Status: "operational"}
	db.Create(&child)
	past := time.Now().Add(-5 * time.Minute)
	future := time.Now().Add(time.Hour)
	inc := models.Incident{StatusPageID: page.ID, Title: "W", Message: "m", Status: "scheduled",
		IsMaintenance: true, ScheduledStartAt: &past, ScheduledEndAt: &future}
	db.Create(&inc)
	db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: group.ID})
	db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: child.ID})

	h.runMaintenanceTick()

	requireComponentStatus(t, db, group.ID, "group")
	requireComponentStatus(t, db, child.ID, "under_maintenance")
}

// Save(inc) in the scheduler failed under SQLite with "near DEFAULT"
// in the association upsert as soon as the linked components partly had parent_id
// NULL and partly did not. Without a workaround (mixed parent nodes) the
// switch must succeed; the component rows remain untouched except for the status.
func TestMaintenanceScheduler_GemischteElternknotenOhneAssoziationsUpsert(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "Mix", Slug: "sched-mixed-607"}
	db.Create(&page)
	root := models.Component{StatusPageID: page.ID, Name: "Wurzel", Status: "operational"}
	db.Create(&root)
	ohneEltern := models.Component{StatusPageID: page.ID, Name: "Ohne", Status: "operational"}
	db.Create(&ohneEltern)
	mitEltern := models.Component{StatusPageID: page.ID, Name: "Mit", Status: "operational", ParentID: &root.ID}
	db.Create(&mitEltern)
	past := time.Now().Add(-5 * time.Minute)
	future := time.Now().Add(time.Hour)
	inc := models.Incident{StatusPageID: page.ID, Title: "W", Message: "m", Status: "scheduled",
		IsMaintenance: true, ScheduledStartAt: &past, ScheduledEndAt: &future}
	db.Create(&inc)
	db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: ohneEltern.ID})
	db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: mitEltern.ID})

	h.runMaintenanceTick()

	requireIncidentStatus(t, db, inc.ID, "in_progress")
	requireComponentStatus(t, db, ohneEltern.ID, "under_maintenance")
	requireComponentStatus(t, db, mitEltern.ID, "under_maintenance")
	requireComponentStatus(t, db, root.ID, "operational")
	var reloaded models.Component
	db.First(&reloaded, mitEltern.ID)
	if reloaded.ParentID == nil || *reloaded.ParentID != root.ID {
		t.Errorf("parent_id der Komponente darf der Scheduler nicht verändern, got %v", reloaded.ParentID)
	}

	// End of the window: likewise without a workaround.
	end := time.Now().Add(-time.Minute)
	db.Model(&models.Incident{}).Where("id = ?", inc.ID).Update("scheduled_end_at", end)
	h.runMaintenanceTick()
	requireIncidentStatus(t, db, inc.ID, "resolved")
	requireComponentStatus(t, db, ohneEltern.ID, "operational")
	requireComponentStatus(t, db, mitEltern.ID, "operational")
}
