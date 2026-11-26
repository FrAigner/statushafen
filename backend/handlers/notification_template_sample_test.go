package handlers

import (
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// The preview's sample data must be filled in for EVERY template kind;
// previously the branch per kind was untested (overall coverage below
// baseline).
func TestSampleNotificationTemplateData_AlleArten(t *testing.T) {
	cases := map[string]struct{ action, status string }{
		NotificationKindIncidentCreated:             {"created", "investigating"},
		NotificationKindIncidentUpdated:             {"updated", "identified"},
		NotificationKindIncidentResolved:            {"updated", "resolved"},
		NotificationKindMaintenanceReminder:         {"maintenance_reminder", "scheduled"},
		NotificationKindMaintenanceStarted:          {"maintenance_started", "in_progress"},
		NotificationKindMaintenanceCompleted:        {"maintenance_completed", "resolved"},
		NotificationKindNoticeCreated:               {"created", ""},
		NotificationKindNoticeUpdated:               {"updated", ""},
		NotificationKindNoticeEnded:                 {"ended", ""},
		NotificationKindExternalIncidentCreated:     {"created", "investigating"},
		NotificationKindExternalIncidentUpdated:     {"updated", "investigating"},
		NotificationKindExternalIncidentResolved:    {"resolved", "investigating"},
		NotificationKindExternalMaintenanceCreated:  {"created", "investigating"},
		NotificationKindExternalMaintenanceUpdated:  {"updated", "investigating"},
		NotificationKindExternalMaintenanceResolved: {"resolved", "investigating"},
	}
	for kind, want := range cases {
		d := sampleNotificationTemplateData(kind, "en")
		if d.Action != want.action || d.Status != want.status {
			t.Errorf("%s: Action/Status = %q/%q, erwartet %q/%q", kind, d.Action, d.Status, want.action, want.status)
		}
		if d.Title == "" || d.Message == "" {
			t.Errorf("%s: Titel/Nachricht leer", kind)
		}
	}
	if d := sampleNotificationTemplateData(NotificationKindNoticeCreated, "de"); d.Level != "warning" || d.ComponentNames != "" {
		t.Errorf("Ankündigung: Level/Komponenten falsch: %+v", d)
	}
	if d := sampleNotificationTemplateData(NotificationKindExternalIncidentCreated, "en"); d.SourceName == "" {
		t.Error("externe Art ohne Quelle")
	}
}

func TestIsValidNotificationLanguage(t *testing.T) {
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "Lang", Slug: "lang-835"}
	if err := db.Create(&page).Error; err != nil {
		t.Fatal(err)
	}
	// Without language rows: fallback en/de.
	if !isValidNotificationLanguage(db, page.ID, "de") || isValidNotificationLanguage(db, page.ID, "fr") {
		t.Error("Rückfall en/de falsch")
	}
	// With language rows: only configured content languages (de/en); a legacy
	// fr assignment is not valid any more.
	for _, code := range []string{"en", "fr"} {
		if err := db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: code}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if !isValidNotificationLanguage(db, page.ID, "en") || isValidNotificationLanguage(db, page.ID, "de") || isValidNotificationLanguage(db, page.ID, "fr") {
		t.Error("configured languages wrong")
	}
	// DB error (table gone): fallback en/de.
	if err := db.Migrator().DropTable(&models.StatusPageLanguage{}); err != nil {
		t.Fatal(err)
	}
	if !isValidNotificationLanguage(db, page.ID, "en") || isValidNotificationLanguage(db, page.ID, "fr") {
		t.Error("Fehlerrückfall falsch")
	}
}
