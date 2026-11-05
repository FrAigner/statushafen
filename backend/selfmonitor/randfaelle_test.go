package selfmonitor

import (
	"errors"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// a: Liefert die Prüfung dauerhaft "nicht prüfbar" (Backup-Datei
// fehlt), obwohl der Admin die Komponente gelöscht hat, räumt der Monitor seine
// Waise trotzdem auf (Marker weg, Vorfall geschlossen).
func TestMonitor_SkipRaeumtWaiseNachGeloeschterKomponenteAuf(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	f.res = &Result{Skip: true}
	f.run(1)
	assert.Zero(t, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"), "Marker aufgeräumt")
	assert.Zero(t, f.count(t, &models.Incident{}, "status <> ?", "resolved"), "Vorfall geschlossen")
	assert.EqualValues(t, 1, f.count(t, &models.AuditLog{}, "action = ?", "selfmonitor.component_released"))
}

// Gegenprobe zu (a): Existiert die Komponente, lässt "nicht prüfbar" alles unverändert.
func TestMonitor_SkipMitVorhandenerKomponenteAendertNichts(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	f.res = &Result{Skip: true}
	f.run(2)
	assert.Equal(t, "major_outage", f.status(t))
	assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
}

// verschiebe legt eine Kundenseite an und ordnet die Komponente ihr zu.
func verschiebe(t *testing.T, f *fixture) models.StatusPage {
	t.Helper()
	other := models.StatusPage{Name: "Kunde", Slug: "kunde"}
	require.NoError(t, f.db.Create(&other).Error)
	require.NoError(t, f.db.Model(&f.comp).Update("status_page_id", other.ID).Error)
	return other
}

func releasedAudit(t *testing.T, f *fixture) models.AuditLog {
	t.Helper()
	var a models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "selfmonitor.component_released").First(&a).Error)
	return a
}

// b: Wird die Komponente einer anderen Seite zugeordnet, beendet der
// Monitor seine Verfolgung. Die Identität kommt exakt aus dem markierten Vorfall
// (IncidentComponent); steht die Komponente noch auf dem Ausfallstatus des
// Monitors, den er selbst gesetzt hat, wird er per bedingtem Update zurückgesetzt.
func TestMonitor_VerschobeneKomponenteSetztEigenenStatusZurueck(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	verschiebe(t, f)
	f.run(1)
	assert.Zero(t, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
	assert.Zero(t, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
	assert.Equal(t, "operational", f.status(t))
	a := releasedAudit(t, f)
	assert.Equal(t, "Komponente auf eine andere Seite verschoben; Vorfall 1 geschlossen; Status zurückgesetzt", a.Details)
	assert.Equal(t, "1", a.TargetID)
}

// Hat der Admin inzwischen einen anderen Status gesetzt, bleibt er.
func TestMonitor_VerschobeneKomponenteMitFremdemStatusBleibtUnangetastet(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	verschiebe(t, f)
	require.NoError(t, f.db.Model(&f.comp).Update("status", "under_maintenance").Error)
	f.run(1)
	assert.Equal(t, "under_maintenance", f.status(t))
	assert.Equal(t, "Komponente auf eine andere Seite verschoben; Vorfall 1 geschlossen; Status unverändert", releasedAudit(t, f).Details)
}

// Ohne Vorfall ist die Identität nicht exakt bekannt: nicht nach dem Slug raten,
// nichts anfassen, neutrales Audit.
func TestMonitor_VerschobenOhneVorfallRaetNichtNachSlug(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	verschiebe(t, f)
	f.run(1)
	assert.Zero(t, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
	assert.Equal(t, "major_outage", f.status(t))
	a := releasedAudit(t, f)
	assert.Equal(t, "Komponente nicht mehr auf der Systemseite", a.Details)
	assert.Equal(t, "0", a.TargetID)
}

// Hart gelöscht, aber auf einer Kundenseite steht zufällig derselbe Slug: das
// Audit nennt die fremde Komponente nicht und ihr Status bleibt.
func TestMonitor_HartGeloeschtMitFremdemGleichenSlugNenntFremdeKomponenteNicht(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	other := models.StatusPage{Name: "Kunde", Slug: "kunde"}
	require.NoError(t, f.db.Create(&other).Error)
	fremd := models.Component{StatusPageID: other.ID, Name: "Fremd", Slug: testSlug, Status: "major_outage"}
	require.NoError(t, f.db.Create(&fremd).Error)
	require.NoError(t, f.db.Unscoped().Delete(&f.comp).Error)
	f.run(1)
	a := releasedAudit(t, f)
	assert.NotEqual(t, "Fremd", a.TargetName)
	assert.NotEqual(t, strconv.Itoa(int(fremd.ID)), a.TargetID)
	assert.Equal(t, "Komponente nicht mehr auf der Systemseite; Vorfall 1 geschlossen", a.Details)
	var fr models.Component
	require.NoError(t, f.db.First(&fr, fremd.ID).Error)
	assert.Equal(t, "major_outage", fr.Status)
}

// Skip plus Lesefehler bei der Komponentensuche: nichts aufräumen (ein Lesefehler
// ist keine gelöschte Komponente).
func TestMonitor_SkipMitLesefehlerBeiKomponentensucheRaeumtNichtAuf(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	f.res = &Result{Skip: true}
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("test:komp_fehler", func(d *gorm.DB) {
		if d.Statement.Table == "components" {
			_ = d.AddError(errors.New("kaputt"))
		}
	}))
	f.run(1)
	require.NoError(t, f.db.Callback().Query().Remove("test:komp_fehler"))
	assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
}
