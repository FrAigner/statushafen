package selfmonitor

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// lesefehlerAb makes the n-th query on system_settings (and all following) fail;
// the error message carries a secret (logging rule: no secrets in the log).
func lesefehlerAb(t *testing.T, db *gorm.DB, n int) {
	t.Helper()
	zaehler := 0
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:lesefehler_ab", func(d *gorm.DB) {
		if d.Statement.Table == "system_settings" {
			zaehler++
			if zaehler >= n {
				_ = d.AddError(errors.New("lesefehler https://db.example/x?token=GEHEIM123"))
			}
		}
	}))
}

// lesefehlerAus withdraws the read error (the test's count queries count too).
func lesefehlerAus(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Callback().Query().Remove("test:lesefehler_ab"))
}

// 1: a read error on the incident marker must not count as "no
// marker", otherwise a second incident arises and the first stays open.
func TestMonitor_VorfallMarkeLesefehlerLegtKeinenZweitenVorfallAn(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	offeneStoerung(t, f)
	lesefehlerAb(t, f.db, 2) // 1st query: outage time, 2nd: incident marker
	comp := f.comp
	comp.Status = f.m.Checks[0].FailedStatus
	f.m.fail(f.page.ID, f.m.Checks[0], comp, "x")
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "1 = 1"), "kein zweiter Vorfall")
	assert.Contains(t, buf.String(), "Marke nicht lesbar")
	assert.NotContains(t, buf.String(), "GEHEIM123")
}

// 1: reading the outage time can also fail; then the step is skipped
// (the next tick repeats it).
func TestMonitor_StoerungsMarkeLesefehlerUeberspringtSchritt(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	f.fail()
	f.run(3)
	lesefehlerAb(t, f.db, 1)
	f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
	f.run(2)
	assert.EqualValues(t, 0, f.count(t, &models.Incident{}, "1 = 1"))
	assert.Contains(t, buf.String(), "Marke nicht lesbar")
	assert.NotContains(t, buf.String(), "GEHEIM123")
}

// 1: second replica (or lost read): if the incident
// marker already exists on creation, the transaction is rolled back.
func TestMonitor_VorfallAnlegenRollbackWennMarkeSchonExistiert(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	var vorher models.SystemSetting
	require.NoError(t, f.db.Where("key = ?", incidentKeyPrefix+testSlug).First(&vorher).Error)
	f.m.openIncident(f.page.ID, f.m.Checks[0], f.comp, f.now, "x")
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "1 = 1"), "kein zweiter Vorfall")
	assert.EqualValues(t, 1, f.count(t, &models.IncidentUpdate{}, "1 = 1"), "keine verwaisten Updates")
	var nachher models.SystemSetting
	require.NoError(t, f.db.Where("key = ?", incidentKeyPrefix+testSlug).First(&nachher).Error)
	assert.Equal(t, vorher.Value, nachher.Value, "Marke zeigt weiter auf den ersten Vorfall")
}

// 2: recovery only closes incidents of the system page, even if
// the marker points to a foreign incident.
func TestMonitor_ErholungSchliesstKeinenVorfallEinerAnderenSeite(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	other := models.StatusPage{Name: "Kunde", Slug: "kunde"}
	require.NoError(t, f.db.Create(&other).Error)
	fremd := models.Incident{StatusPageID: other.ID, Title: "Fremd", Status: "investigating", Severity: "major_outage"}
	require.NoError(t, f.db.Create(&fremd).Error)
	require.NoError(t, setValue(f.db, incidentKeyPrefix+testSlug, strconv.FormatUint(uint64(fremd.ID), 10)))
	f.res = &Result{OK: true}
	f.run(1)
	var nachher models.Incident
	require.NoError(t, f.db.First(&nachher, fremd.ID).Error)
	assert.Equal(t, "investigating", nachher.Status, "fremder Vorfall bleibt unberührt")
	assert.Equal(t, "operational", f.status(t))
}

// If an admin deletes the component, the monitor cleans up markers and its
// own open incident.
func TestMonitor_KomponenteGeloeschtRaeumtMarkerUndVorfallAuf(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	f.run(1)
	assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
	assert.EqualValues(t, 0, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
	assert.EqualValues(t, 1, f.count(t, &models.IncidentUpdate{}, "status = ? AND message LIKE ?", "resolved", "%nicht mehr vorhanden%"))
	assert.EqualValues(t, 1, f.count(t, &models.AuditLog{}, "action = ?", "selfmonitor.component_released"))
}

// the same when monitoring is switched off (releaseAll); without an
// incident, deleting the outage marker suffices.
func TestMonitor_AbschaltenMitGeloeschterKomponenteRaeumtAuf(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	f.m.Enabled = func() (bool, error) { return false, nil }
	f.run(1)
	assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
}

// Without an ownership marker, a missing component stays a no-op.
func TestMonitor_GeloeschteKomponenteOhneMarkeIstNoop(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	f.fail()
	f.run(4)
	assert.EqualValues(t, 0, f.count(t, &models.AuditLog{}, "1 = 1"))
}

// If the cleanup fails, everything stays (rollback), the error
// appears without a secret in the log and the next tick tries again.
func TestMonitor_AufraeumenScheitertRollbackUndLog(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	offeneStoerung(t, f)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	f.sperre(t, "DELETE", "selfmonitor.failing.%")
	f.run(1)
	assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "status <> ?", "resolved"), "Vorfall-Schließen zurückgerollt")
	assert.Contains(t, buf.String(), "Aufräumen nicht gespeichert")
	assert.NotContains(t, buf.String(), "GEHEIM123")
	require.NoError(t, f.db.Exec("DROP TRIGGER sperre_DELETE").Error)
	f.run(1)
	assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
}

// A read error during cleanup aborts (no deleting on suspicion).
func TestMonitor_AufraeumenLesefehlerBrichtAb(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	offeneStoerung(t, f)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	lesefehlerAb(t, f.db, 1)
	f.run(1)
	assert.Contains(t, buf.String(), "Aufräumen nicht gespeichert")
	assert.NotContains(t, buf.String(), "GEHEIM123")
	require.NoError(t, f.db.Callback().Query().Remove("test:lesefehler_ab"))
	assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
}

// Also a read error only on the incident marker, an error when
// closing and when deleting the incident marker abort the cleanup.
func TestMonitor_AufraeumenFehlerpfade(t *testing.T) {
	faelle := map[string]func(f *fixture, t *testing.T){
		"Vorfall-Marke nicht lesbar": func(f *fixture, t *testing.T) { lesefehlerAb(t, f.db, 2) },
		"Vorfall nicht schließbar": func(f *fixture, t *testing.T) {
			require.NoError(t, f.db.Exec("CREATE TRIGGER sperre_inc2 BEFORE UPDATE ON incidents BEGIN SELECT RAISE(ABORT, 'kaputt'); END").Error)
		},
		"Vorfall-Marke nicht löschbar": func(f *fixture, t *testing.T) { f.sperre(t, "DELETE", "selfmonitor.incident.%") },
	}
	for name, setup := range faelle {
		t.Run(name, func(t *testing.T) {
			buf := logPuffer(t)
			f := newFixture(t)
			offeneStoerung(t, f)
			require.NoError(t, f.db.Delete(&f.comp).Error)
			setup(f, t)
			f.run(1)
			if name == "Vorfall-Marke nicht lesbar" {
				lesefehlerAus(t, f.db)
			}
			assert.Contains(t, buf.String(), "Aufräumen nicht gespeichert")
			assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
			assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
		})
	}
}

// Read errors of the ownership marker in releaseAll (switch-off) and recover
// release nothing.
func TestMonitor_BesitzMarkeLesefehlerGibtNichtsFrei(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	f.fail()
	f.run(3)
	lesefehlerAb(t, f.db, 1)
	f.m.Enabled = func() (bool, error) { return false, nil }
	f.run(1)
	assert.Contains(t, buf.String(), "Marke nicht lesbar")
	assert.NotContains(t, buf.String(), "GEHEIM123")
	buf.Reset()
	comp := f.comp
	comp.Status = f.m.Checks[0].FailedStatus
	f.m.recover(f.m.Checks[0], comp, recoveredReason)
	assert.Contains(t, buf.String(), "Marke nicht lesbar")
	require.NoError(t, f.db.Callback().Query().Remove("test:lesefehler_ab"))
	assert.Equal(t, "major_outage", f.status(t))
	assert.EqualValues(t, 1, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
}

// The switch-off release is distinguishable in the audit from a real recovery;
// the error message names the incident ID.
func TestMonitor_AbschaltAuditUnterscheidbar(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	f.res = &Result{Off: true}
	f.run(1)
	var a models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "selfmonitor.component_recovered").First(&a).Error)
	assert.Contains(t, a.Details, "abgeschaltet")
}

func TestMonitor_ErholungFehlerlogNenntVorfallID(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	offeneStoerung(t, f)
	f.sperre(t, "DELETE", "selfmonitor.failing.%")
	f.res = &Result{OK: true}
	f.run(1)
	assert.Contains(t, buf.String(), "incident=1 ")
}

// The identifier comes from a marker in the database. A number outside the
// uint range (on 32-bit systems from 2^32) is discarded instead of being
// truncated on conversion and hitting a different incident.
// On 64-bit systems the behavior stayed the same; the test describes it.
func TestResolveIncident_KennungAusserhalbDesBereichsSchliesstNichts(t *testing.T) {
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "Sys", Slug: "sys-kennung"}
	require.NoError(t, db.Create(&page).Error)
	inc := models.Incident{StatusPageID: page.ID, Title: "offen", Status: "investigating"}
	require.NoError(t, db.Create(&inc).Error)
	now := time.Now()

	for _, raw := range []string{"18446744073709551616", "99999999999999999999", "-1", "abc", ""} {
		closed, err := resolveIncident(db, page.ID, now, raw, "x")
		require.NoError(t, err, raw)
		assert.False(t, closed, raw)
	}
	var after models.Incident
	require.NoError(t, db.First(&after, inc.ID).Error)
	assert.Equal(t, "investigating", after.Status, "der offene Vorfall bleibt unberührt")

	// Control sample: the correct identifier closes.
	closed, err := resolveIncident(db, page.ID, now, strconv.FormatUint(uint64(inc.ID), 10), "behoben")
	require.NoError(t, err)
	assert.True(t, closed)
}
