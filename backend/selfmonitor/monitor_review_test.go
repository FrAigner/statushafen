package selfmonitor

import (
	"errors"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// offeneStoerung bringt die Fixture in den Zustand "Störung und Vorfall offen".
func offeneStoerung(t *testing.T, f *fixture) {
	t.Helper()
	f.fail()
	f.run(3)
	f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
	f.run(1)
	require.EqualValues(t, 1, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
}

// A: Scheitert das Schließen des Vorfalls, darf die Vorfall-Marke
// nicht verloren gehen; spätere Takte schließen ihn.
func TestMonitor_VorfallSchliessenScheitertWirdWiederholt(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	require.NoError(t, f.db.Exec("CREATE TRIGGER sperre_inc BEFORE UPDATE ON incidents BEGIN SELECT RAISE(ABORT, 'kaputt'); END").Error)
	f.res = &Result{OK: true}
	f.run(1)
	require.NoError(t, f.db.Exec("DROP TRIGGER sperre_inc").Error)
	f.run(2)
	assert.EqualValues(t, 0, f.count(t, &models.Incident{}, "status <> ?", "resolved"), "Vorfall muss am Ende geschlossen sein")
	assert.Equal(t, "operational", f.status(t))
}

// B: Scheitert das Löschen der Störungs-Marke, bleibt der Status
// auf Störung (Erholung ist atomar) und der nächste Fehlschlag legt keinen
// widersprüchlichen Zustand an.
func TestMonitor_ErholungIstAtomar(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	f.sperre(t, "DELETE", "selfmonitor.failing.%")
	f.res = &Result{OK: true}
	f.run(1)
	assert.Equal(t, "major_outage", f.status(t), "Status und Marke bleiben gemeinsam stehen")
	assert.EqualValues(t, 1, f.count(t, &models.SystemSetting{}, "key = ?", failingKeyPrefix+testSlug))
}

// D: Hat der Admin den Status zwischenzeitlich gesetzt (Wartung),
// überschreibt weder fail noch recover ihn.
func TestMonitor_VeralteterStatusWirdNichtUeberschrieben(t *testing.T) {
	f := newFixture(t)
	check := f.m.Checks[0]
	require.NoError(t, f.db.Model(&models.Component{}).Where("id = ?", f.comp.ID).Update("status", "under_maintenance").Error)

	stale := f.comp // Status "operational" im alten Lesestand
	f.m.fail(f.page.ID, check, stale, "x")
	assert.Equal(t, "under_maintenance", f.status(t))
	assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"), "keine Marke ohne Statusänderung")

	require.NoError(t, setValue(f.db, failingKeyPrefix+testSlug, f.now.UTC().Format(time.RFC3339)))
	stale.Status = check.FailedStatus
	f.m.recover(check, stale, recoveredReason)
	assert.Equal(t, "under_maintenance", f.status(t))
}

func TestMonitor_AbschaltTextIstNeutral(t *testing.T) {
	assert.Equal(t, "Automatische Verfolgung beendet.", offReason)
}

// Scheitert der Status-Reset, bleibt alles unverändert (Rollback).
func TestMonitor_StatusResetScheitertRollback(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	require.NoError(t, f.db.Exec("CREATE TRIGGER sperre_comp BEFORE UPDATE ON components BEGIN SELECT RAISE(ABORT, 'kaputt'); END").Error)
	f.res = &Result{OK: true}
	f.run(1)
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
	assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
}

// Ein Lesefehler beim Vorfall-Marker bricht die Erholung ab, statt
// den Marker zu übergehen und die Störungs-Marke zu löschen.
func TestMonitor_VorfallMarkerLesefehlerBrichtErholungAb(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	n := 0
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("test:lesefehler", func(d *gorm.DB) {
		if d.Statement.Table == "system_settings" {
			n++
			if n == 3 { // 1. Besitzprüfung, 2. gesperrte Störungs-Marke, 3. Vorfall-Marke
				_ = d.AddError(errors.New("lesefehler"))
			}
		}
	}))
	f.res = &Result{OK: true}
	comp := f.comp
	comp.Status = f.m.Checks[0].FailedStatus // aktueller Lesestand: der Status-Reset-Zweig läuft wirklich
	f.m.recover(f.m.Checks[0], comp, recoveredReason)
	assert.Equal(t, 3, n)
	assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
	assert.Equal(t, "major_outage", f.status(t))
}

// Logging-Regel: Der Lesefehler des Schalters enthält ein
// Geheimnis; es darf nicht im Log stehen.
func TestMonitor_SchalterLesefehlerLogOhneGeheimnis(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	f.m.Enabled = func() (bool, error) {
		return false, errors.New("kaputt https://db.example/x?token=GEHEIM123")
	}
	f.run(1)
	assert.Contains(t, buf.String(), "Schalter nicht lesbar, Takt entfällt")
	assert.NotContains(t, buf.String(), "GEHEIM123")
}

// Weicht der Status ab (Admin), gibt es weder Marke noch Audit.
func TestMonitor_VeralteterStatusSchreibtKeinFailedAudit(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.db.Model(&models.Component{}).Where("id = ?", f.comp.ID).Update("status", "under_maintenance").Error)
	f.m.fail(f.page.ID, f.m.Checks[0], f.comp, "x")
	assert.EqualValues(t, 0, f.count(t, &models.AuditLog{}, "action = ?", "selfmonitor.component_failed"))
}
