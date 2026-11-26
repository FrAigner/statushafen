package selfmonitor

import (
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Switch-off without a system page or without a component is harmless.
func TestMonitor_AusschaltenOhneSeiteOderKomponenteIstHarmlos(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	f.m.Enabled = func() (bool, error) { return false, nil }
	require.NoError(t, f.db.Delete(&f.comp).Error)
	assert.NotPanics(t, func() { f.run(1) })
	require.NoError(t, f.db.Delete(&f.page).Error)
	assert.NotPanics(t, func() { f.run(1) })
}

// If deleting the incident marker fails, the outage marker also
// stays; no recovery message, error in the log (without secret).
func TestMonitor_VorfallMarkeLoeschenScheitert(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	f.fail()
	f.run(3)
	f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
	f.run(1)
	f.sperre(t, "DELETE", "selfmonitor.incident.%")
	f.res = &Result{OK: true}
	f.run(1)
	assert.EqualValues(t, 1, f.count(t, &models.SystemSetting{}, "key = ?", failingKeyPrefix+testSlug))
	assert.EqualValues(t, 0, f.count(t, &models.AuditLog{}, "action = ?", "selfmonitor.component_recovered"))
	assert.Contains(t, buf.String(), "Erholung nicht gespeichert")
	assert.NotContains(t, buf.String(), "GEHEIM123")
}

// If the closing update fails, the error is logged.
func TestMonitor_AbschlussUpdateScheitertWirdGeloggt(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	f.fail()
	f.run(3)
	f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
	f.run(1)
	require.NoError(t, f.db.Exec("CREATE TRIGGER sperre_upd BEFORE INSERT ON incident_updates WHEN NEW.status = 'resolved' BEGIN SELECT RAISE(ABORT, 'kaputt https://db.example/x?token=GEHEIM123'); END").Error)
	f.res = &Result{OK: true}
	f.run(1)
	assert.Contains(t, buf.String(), "Erholung nicht gespeichert")
	assert.NotContains(t, buf.String(), "GEHEIM123")
}

// If the corrupted time value cannot be repaired, the
// error appears in the log.
func TestMonitor_ZeitwertReparaturScheitertWirdGeloggt(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	f.fail()
	f.run(3)
	require.NoError(t, f.db.Model(&models.SystemSetting{}).Where("key = ?", failingKeyPrefix+testSlug).
		Update("value", "kaputt").Error)
	require.NoError(t, f.db.Exec("CREATE TRIGGER sperre_upd2 BEFORE UPDATE ON system_settings BEGIN SELECT RAISE(ABORT, 'kaputt https://db.example/x?token=GEHEIM123'); END").Error)
	f.run(1)
	assert.Contains(t, buf.String(), "Zeitwert nicht repariert")
	assert.NotContains(t, buf.String(), "GEHEIM123")
}
