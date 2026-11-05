package selfmonitor

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/fraigner/statushafen/logging"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const testSlug = "feature-plattform-api"

type fixture struct {
	db   *gorm.DB
	comp models.Component
	page models.StatusPage
	now  time.Time
	res  *Result
	m    *Monitor
}

// newFixture legt Systemseite und Komponente an; res steuert das Prüfergebnis.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "System", Slug: "system", IsSystem: true}
	require.NoError(t, db.Create(&page).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Slug: testSlug, Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	f := &fixture{db: db, comp: comp, page: page, now: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), res: &Result{OK: true}}
	check := Check{Slug: testSlug, Name: "API", FailedStatus: "major_outage",
		Run: func(context.Context) Result { return *f.res }}
	f.m = New(db, []Check{check}, nil)
	f.m.Now = func() time.Time { return f.now }
	return f
}

func (f *fixture) status(t *testing.T) string {
	t.Helper()
	var c models.Component
	require.NoError(t, f.db.First(&c, f.comp.ID).Error)
	return c.Status
}

func (f *fixture) run(n int) {
	for i := 0; i < n; i++ {
		f.m.RunOnce(context.Background())
	}
}

func (f *fixture) count(t *testing.T, model any, query string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, f.db.Model(model).Where(query, args...).Count(&n).Error)
	return n
}

func (f *fixture) fail() { f.res = &Result{Detail: "nicht erreichbar"} }

func TestMonitor_EntprellungErstNachSchwelle(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(2)
	assert.Equal(t, "operational", f.status(t), "zwei Fehlschläge reichen nicht")
	f.run(1)
	assert.Equal(t, "major_outage", f.status(t))
	assert.EqualValues(t, 1, f.count(t, &models.AuditLog{}, "action = ? AND actor_email = ?", "selfmonitor.component_failed", "system"))
	f.run(2)
	assert.EqualValues(t, 1, f.count(t, &models.AuditLog{}, "action = ?", "selfmonitor.component_failed"), "nur einmal auditiert")
}

func TestMonitor_ZwischenErfolgSetztZaehlerZurueck(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(2)
	f.res = &Result{OK: true}
	f.run(1)
	f.fail()
	f.run(2)
	assert.Equal(t, "operational", f.status(t))
}

func TestMonitor_UeberschreibtKeinenManuellenStatus(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.db.Model(&f.comp).Update("status", "under_maintenance").Error)
	f.fail()
	f.run(5)
	assert.Equal(t, "under_maintenance", f.status(t))
	f.res = &Result{OK: true}
	f.run(1)
	assert.Equal(t, "under_maintenance", f.status(t), "Erholung fasst fremden Status nicht an")
}

func TestMonitor_VorfallErstBeiAnhaltenderStoerungUndNurEinmal(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	assert.Zero(t, f.count(t, &models.Incident{}, "1 = 1"), "noch kein Vorfall")
	f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
	f.run(2)
	var inc models.Incident
	require.NoError(t, f.db.First(&inc).Error)
	assert.Equal(t, "Störung: API", inc.Title)
	assert.Equal(t, "major_outage", inc.Severity)
	assert.Equal(t, "investigating", inc.Status)
	assert.Equal(t, f.page.ID, inc.StatusPageID)
	assert.EqualValues(t, 1, f.count(t, &models.IncidentComponent{}, "incident_id = ? AND component_id = ?", inc.ID, f.comp.ID))
	assert.EqualValues(t, 1, f.count(t, &models.IncidentUpdate{}, "incident_id = ? AND author_email = ?", inc.ID, "system"))
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "1 = 1"), "kein zweiter Vorfall")
}

func TestMonitor_ErholungSchliesstVorfallUndGibtStatusFrei(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	f.now = f.now.Add(20 * time.Minute)
	f.run(1)
	f.res = &Result{OK: true}
	f.now = f.now.Add(time.Minute)
	f.run(1)
	assert.Equal(t, "operational", f.status(t))
	var inc models.Incident
	require.NoError(t, f.db.First(&inc).Error)
	assert.Equal(t, "resolved", inc.Status)
	require.NotNil(t, inc.ResolvedAt)
	assert.EqualValues(t, 1, f.count(t, &models.IncidentUpdate{}, "incident_id = ? AND status = ?", inc.ID, "resolved"))
	assert.Zero(t, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"), "Merker entfernt")
	assert.EqualValues(t, 1, f.count(t, &models.AuditLog{}, "action = ?", "selfmonitor.component_recovered"))
	// Neue Störung danach: wieder entprellt, neuer Vorfall möglich
	f.fail()
	f.run(3)
	assert.Equal(t, "major_outage", f.status(t))
}

func TestMonitor_ErholungLaesstGeaendertenAdminStatusUndGeschlossenenVorfall(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	f.now = f.now.Add(20 * time.Minute)
	f.run(1)
	require.NoError(t, f.db.Model(&f.comp).Update("status", "degraded_performance").Error)
	require.NoError(t, f.db.Model(&models.Incident{}).Where("1 = 1").Update("status", "resolved").Error)
	f.res = &Result{OK: true}
	f.run(1)
	assert.Equal(t, "degraded_performance", f.status(t))
	assert.Zero(t, f.count(t, &models.IncidentUpdate{}, "status = ?", "resolved"), "bereits geschlossener Vorfall bekommt kein zweites Ende")
}

func TestMonitor_ErholungOhneBesitzIstNoop(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.db.Model(&f.comp).Update("status", "major_outage").Error)
	f.run(2) // Admin hat selbst auf Störung gesetzt, Prüfung ok
	assert.Equal(t, "major_outage", f.status(t))
}

func TestMonitor_NeustartMitStoerungErholtSauber(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	f.now = f.now.Add(20 * time.Minute)
	f.run(1)
	// frischer Monitor (Neustart): Zähler leer, Besitz steht in der DB
	neu := New(f.db, f.m.Checks, nil)
	neu.Now = f.m.Now
	f.res = &Result{OK: true}
	neu.RunOnce(context.Background())
	assert.Equal(t, "operational", f.status(t))
	assert.Equal(t, "resolved", func() string { var i models.Incident; f.db.First(&i); return i.Status }())
}

func TestMonitor_NeustartInnerhalbDerFristLegtVorfallMitOriginalbeginnAn(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	neu := New(f.db, f.m.Checks, nil)
	f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
	neu.Now = f.m.Now
	neu.fails[testSlug] = 3
	neu.RunOnce(context.Background())
	var inc models.Incident
	require.NoError(t, f.db.First(&inc).Error)
	require.NotNil(t, inc.StartedAt)
	assert.True(t, inc.StartedAt.Before(f.now.Add(-DefaultIncidentAfter)))
}

func TestMonitor_AbgeschaltetTutNichtsUndVergisstZaehler(t *testing.T) {
	f := newFixture(t)
	on := false
	f.m.Enabled = func() (bool, error) { return on, nil }
	f.fail()
	f.run(5)
	assert.Equal(t, "operational", f.status(t))
	on = true
	f.run(2)
	assert.Equal(t, "operational", f.status(t), "Zähler begann nach dem Einschalten neu")
	f.run(1)
	assert.Equal(t, "major_outage", f.status(t))
}

func TestMonitor_NichtPruefbarLaesstZustandUnveraendert(t *testing.T) {
	f := newFixture(t)
	f.res = &Result{Skip: true}
	f.run(5)
	assert.Equal(t, "operational", f.status(t))
	f.fail()
	f.run(3)
	f.res = &Result{Skip: true}
	f.run(2)
	assert.Equal(t, "major_outage", f.status(t), "Skip bedeutet weder gesund noch Erholung")
}

func TestMonitor_Every(t *testing.T) {
	f := newFixture(t)
	calls := 0
	f.m.Checks = []Check{{Slug: testSlug, Name: "API", FailedStatus: "major_outage", Every: 3,
		Run: func(context.Context) Result { calls++; return Result{OK: true} }}}
	f.run(7)
	assert.Equal(t, 3, calls, "Takte 1, 4 und 7")
}

func TestMonitor_FehlendeSeiteOderKomponenteIstHarmlos(t *testing.T) {
	f := newFixture(t)
	f.fail()
	require.NoError(t, f.db.Delete(&f.comp).Error)
	assert.NotPanics(t, func() { f.run(4) })
	require.NoError(t, f.db.Unscoped().Where("1 = 1").Delete(&models.StatusPage{}).Error)
	assert.NotPanics(t, func() { f.run(1) })
}

func TestMonitor_BeschaedigterMerkerGiltAlsGeradeBegonnen(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	require.NoError(t, f.db.Model(&models.SystemSetting{}).Where("key = ?", failingKeyPrefix+testSlug).Update("value", "kaputt").Error)
	f.now = f.now.Add(time.Hour)
	f.run(1)
	assert.Zero(t, f.count(t, &models.Incident{}, "1 = 1"))
}

func TestMonitor_StartStopp(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.m.Interval = 5 * time.Millisecond
	f.m.Threshold = 1
	stop := f.m.Start(context.Background())
	require.Eventually(t, func() bool { return f.status(t) == "major_outage" }, 3*time.Second, 5*time.Millisecond)
	stop()
}

// failOn lässt Schreibzugriffe auf die Tabelle fehlschlagen (Fehlerpfade).
func failOn(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	inject := func(tx *gorm.DB) {
		if tx.Statement.Table == table {
			_ = tx.AddError(assert.AnError)
		}
	}
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_create_"+table, inject))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fail_update_"+table, inject))
}

func TestMonitor_FehlerpfadStatusNichtSchreibbar(t *testing.T) {
	f := newFixture(t)
	failOn(t, f.db, "components")
	f.fail()
	f.run(4)
	assert.Zero(t, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"), "ohne Statuswechsel kein Merker")
}

func TestMonitor_FehlerpfadVorfallNichtAnlegbar(t *testing.T) {
	for _, table := range []string{"incidents", "incident_components", "incident_updates"} {
		t.Run(table, func(t *testing.T) {
			f := newFixture(t)
			failOn(t, f.db, table)
			f.fail()
			f.run(3)
			f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
			f.run(2)
			assert.Zero(t, f.count(t, &models.Incident{}, "1 = 1"), "Transaktion zurückgerollt")
			assert.Zero(t, f.count(t, &models.SystemSetting{}, "key LIKE ?", incidentKeyPrefix+"%"), "kein Vorfall-Merker, nächster Takt versucht es erneut")
		})
	}
}

func TestMonitor_FehlerpfadStatusNichtZuruecksetzbar(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	failOn(t, f.db, "components")
	f.res = &Result{OK: true}
	f.run(1)
	assert.Equal(t, "major_outage", f.status(t))
	assert.EqualValues(t, 1, f.count(t, &models.SystemSetting{}, "key = ?", failingKeyPrefix+testSlug), "Merker bleibt, nächster Takt versucht es erneut")
}

func TestMonitor_FehlerpfadUngueltigeVorfallKennung(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	require.NoError(t, setValue(f.db, incidentKeyPrefix+testSlug, "keine-zahl"))
	f.res = &Result{OK: true}
	assert.NotPanics(t, func() { f.run(1) })
	assert.Equal(t, "operational", f.status(t))
}

func TestMonitor_FehlerpfadAuditNichtSchreibbar(t *testing.T) {
	f := newFixture(t)
	failOn(t, f.db, "audit_logs")
	f.fail()
	f.run(3)
	assert.Equal(t, "major_outage", f.status(t), "Audit-Panne blockiert die Aktion nicht")
}

// logPuffer leitet slog über das Projekt-Setup (mit Bereinigung) in einen Puffer.
func logPuffer(t *testing.T) *bytes.Buffer {
	t.Helper()
	alt := slog.Default()
	buf := &bytes.Buffer{}
	logging.Init(buf, func(string) string { return "" })
	t.Cleanup(func() { slog.SetDefault(alt) })
	return buf
}

// sperre legt einen SQLite-Trigger an, der Schreibzugriffe auf system_settings
// zu key-Mustern abbricht (Fehlermeldung enthält eine URL mit Geheimnis).
func (f *fixture) sperre(t *testing.T, ereignis, keyMuster string) {
	t.Helper()
	ref := "NEW"
	if ereignis == "DELETE" {
		ref = "OLD"
	}
	sql := "CREATE TRIGGER sperre_" + ereignis + " BEFORE " + ereignis + " ON system_settings WHEN " + ref +
		".key LIKE '" + keyMuster + "' BEGIN SELECT RAISE(ABORT, 'kaputt https://db.example/x?token=GEHEIM123'); END"
	require.NoError(t, f.db.Exec(sql).Error)
}

// Fund 14: Scheitert die Besitzmarke, darf die Komponente nicht
// ohne Marke auf Störung stehen bleiben (Status und Marke in einer Transaktion).
func TestMonitor_MarkeScheitertStatusBleibtUnveraendert(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	f.sperre(t, "INSERT", "selfmonitor.failing.%")
	f.fail()
	f.run(4)
	assert.Equal(t, "operational", f.status(t))
	assert.Contains(t, buf.String(), "Status nicht gesetzt")
	assert.NotContains(t, buf.String(), "GEHEIM123", "Geheimnis aus Fehlertext nicht im Log")
}

// Fund 14: Scheitert die Vorfall-Marke, entsteht nicht bei jedem
// Takt ein neuer Vorfall.
func TestMonitor_VorfallMarkeScheitertKeinVorfallProTakt(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	f.fail()
	f.run(3)
	f.sperre(t, "INSERT", "selfmonitor.incident.%")
	f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
	f.run(4)
	assert.EqualValues(t, 0, f.count(t, &models.Incident{}, "1 = 1"))
	assert.Contains(t, buf.String(), "Vorfall nicht angelegt")
	assert.NotContains(t, buf.String(), "GEHEIM123")
}

// Fund 14: Schlägt das Löschen der Marke fehl, wird nicht
// "erholt" gemeldet und der Fehler steht im Log.
func TestMonitor_MarkeLoeschenScheitertKeineErholungsmeldung(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	f.fail()
	f.run(3)
	f.sperre(t, "DELETE", "selfmonitor.failing.%")
	f.res = &Result{OK: true}
	f.run(2)
	assert.EqualValues(t, 0, f.count(t, &models.AuditLog{}, "action = ?", "selfmonitor.component_recovered"))
	assert.Contains(t, buf.String(), "Erholung nicht gespeichert")
	assert.NotContains(t, buf.String(), "GEHEIM123")
}

// Fund 14: Ein beschädigter Zeitwert wird repariert, sonst würde
// IncidentAfter nie erreicht.
func TestMonitor_BeschaedigterZeitwertVerschiebtVorfallNichtEndlos(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	require.NoError(t, f.db.Model(&models.SystemSetting{}).Where("key = ?", failingKeyPrefix+testSlug).
		Update("value", "kaputt").Error)
	f.run(1)
	f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
	f.run(1)
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "1 = 1"))
}

// Fund 8a: Wird die Überwachung ausgeschaltet, während eine
// Komponente auf Störung steht, wird sie freigegeben und der Vorfall geschlossen.
func TestMonitor_AusschaltenGibtStoerungFrei(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
	f.run(1)
	require.EqualValues(t, 1, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
	f.m.Enabled = func() (bool, error) { return false, nil }
	f.run(1)
	assert.Equal(t, "operational", f.status(t))
	assert.EqualValues(t, 0, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
	assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
}

// Fund 8a: Meldet eine Prüfung "abgeschaltet" (Mailversand aus),
// wird ihre Störung ebenso freigegeben; "nicht prüfbar" (Skip) lässt sie stehen.
func TestMonitor_PruefungAbgeschaltetGibtStoerungFrei(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	f.res = &Result{Off: true}
	f.run(1)
	assert.Equal(t, "operational", f.status(t))
	assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
}
