package selfmonitor

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// lesefehlerAb lässt die n-te Abfrage auf system_settings (und alle folgenden)
// scheitern; die Fehlermeldung trägt ein Geheimnis (Logging-Regel: keine Geheimnisse im Log).
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

// lesefehlerAus nimmt den Lesefehler zurück (die Zähl-Abfragen des Tests zählen mit).
func lesefehlerAus(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Callback().Query().Remove("test:lesefehler_ab"))
}

// 1: Ein Lesefehler auf der Vorfall-Marke darf nicht als "keine
// Marke" gelten, sonst entsteht ein zweiter Vorfall und der erste bleibt offen.
func TestMonitor_VorfallMarkeLesefehlerLegtKeinenZweitenVorfallAn(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	offeneStoerung(t, f)
	lesefehlerAb(t, f.db, 2) // 1. Abfrage: Störungs-Zeit, 2.: Vorfall-Marke
	comp := f.comp
	comp.Status = f.m.Checks[0].FailedStatus
	f.m.fail(f.page.ID, f.m.Checks[0], comp, "x")
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "1 = 1"), "kein zweiter Vorfall")
	assert.Contains(t, buf.String(), "Marke nicht lesbar")
	assert.NotContains(t, buf.String(), "GEHEIM123")
}

// 1: Auch das Lesen der Störungs-Zeit kann scheitern; dann entfällt
// der Schritt (nächster Takt wiederholt ihn).
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

// 1: Zweite Replika (oder verlorene Lesung): Existiert die
// Vorfall-Marke beim Anlegen schon, wird die Transaktion zurückgerollt.
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

// 2: Die Erholung schließt nur Vorfälle der Systemseite, auch wenn
// die Marke auf einen fremden Vorfall zeigt.
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

// Löscht ein Admin die Komponente, räumt der Monitor Marker und den
// eigenen offenen Vorfall auf.
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

// dasselbe beim Abschalten der Überwachung (releaseAll); ohne
// Vorfall genügt das Löschen der Störungs-Marke.
func TestMonitor_AbschaltenMitGeloeschterKomponenteRaeumtAuf(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	f.m.Enabled = func() (bool, error) { return false, nil }
	f.run(1)
	assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
}

// Ohne Besitz-Marke bleibt eine fehlende Komponente ein Noop.
func TestMonitor_GeloeschteKomponenteOhneMarkeIstNoop(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	f.fail()
	f.run(4)
	assert.EqualValues(t, 0, f.count(t, &models.AuditLog{}, "1 = 1"))
}

// Scheitert das Aufräumen, bleibt alles stehen (Rollback), der Fehler
// steht ohne Geheimnis im Log und der nächste Takt versucht es erneut.
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

// Lesefehler beim Aufräumen bricht ab (kein Löschen auf Verdacht).
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

// Auch ein Lesefehler erst auf der Vorfall-Marke, ein Fehler beim
// Schließen und beim Löschen der Vorfall-Marke brechen das Aufräumen ab.
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

// Lesefehler der Besitz-Marke in releaseAll (Abschalten) und recover
// geben nichts frei.
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

// Die Abschalt-Freigabe ist im Audit von einer echten Erholung
// unterscheidbar; die Fehlermeldung nennt die Vorfall-ID.
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
