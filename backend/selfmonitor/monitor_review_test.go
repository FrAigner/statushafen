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

// offeneStoerung brings the fixture into the state "outage and incident open".
func offeneStoerung(t *testing.T, f *fixture) {
	t.Helper()
	f.fail()
	f.run(3)
	f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
	f.run(1)
	require.EqualValues(t, 1, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
}

// A: if closing the incident fails, the incident marker must
// not be lost; later ticks close it.
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

// B: if deleting the outage marker fails, the status stays
// on outage (recovery is atomic) and the next failure does not create a
// contradictory state.
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

// D: if the admin set the status in the meantime (maintenance),
// neither fail nor recover overwrites it.
func TestMonitor_VeralteterStatusWirdNichtUeberschrieben(t *testing.T) {
	f := newFixture(t)
	check := f.m.Checks[0]
	require.NoError(t, f.db.Model(&models.Component{}).Where("id = ?", f.comp.ID).Update("status", "under_maintenance").Error)

	stale := f.comp // status "operational" in the old read state
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

// If the status reset fails, everything stays unchanged (rollback).
func TestMonitor_StatusResetScheitertRollback(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	require.NoError(t, f.db.Exec("CREATE TRIGGER sperre_comp BEFORE UPDATE ON components BEGIN SELECT RAISE(ABORT, 'kaputt'); END").Error)
	f.res = &Result{OK: true}
	f.run(1)
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
	assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
}

// A read error on the incident marker aborts the recovery, instead of
// skipping the marker and deleting the outage marker.
func TestMonitor_VorfallMarkerLesefehlerBrichtErholungAb(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	n := 0
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("test:lesefehler", func(d *gorm.DB) {
		if d.Statement.Table == "system_settings" {
			n++
			if n == 3 { // 1. ownership check, 2. locked outage marker, 3. incident marker
				_ = d.AddError(errors.New("lesefehler"))
			}
		}
	}))
	f.res = &Result{OK: true}
	comp := f.comp
	comp.Status = f.m.Checks[0].FailedStatus // current read state: the status-reset branch really runs
	f.m.recover(f.m.Checks[0], comp, recoveredReason)
	assert.Equal(t, 3, n)
	assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
	assert.Equal(t, "major_outage", f.status(t))
}

// Logging rule: the switch's read error contains a
// secret; it must not appear in the log.
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

// If the status differs (admin), there is neither marker nor audit.
func TestMonitor_VeralteterStatusSchreibtKeinFailedAudit(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.db.Model(&models.Component{}).Where("id = ?", f.comp.ID).Update("status", "under_maintenance").Error)
	f.m.fail(f.page.ID, f.m.Checks[0], f.comp, "x")
	assert.EqualValues(t, 0, f.count(t, &models.AuditLog{}, "action = ?", "selfmonitor.component_failed"))
}
