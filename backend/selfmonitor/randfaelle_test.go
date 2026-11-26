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

// a: if the check permanently returns "not checkable" (backup file
// missing) although the admin deleted the component, the monitor still cleans up its
// orphan (marker gone, incident closed).
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

// Control sample for (a): if the component exists, "not checkable" leaves everything unchanged.
func TestMonitor_SkipMitVorhandenerKomponenteAendertNichts(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	f.res = &Result{Skip: true}
	f.run(2)
	assert.Equal(t, "major_outage", f.status(t))
	assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
}

// verschiebe creates a customer page and assigns the component to it.
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

// b: if the component is assigned to another page, the
// monitor ends its tracking. The identity comes exactly from the marked incident
// (IncidentComponent); if the component still has the monitor's outage status
// that it set itself, it is reset via a conditional update.
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

// If the admin has meanwhile set a different status, it stays.
func TestMonitor_VerschobeneKomponenteMitFremdemStatusBleibtUnangetastet(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	verschiebe(t, f)
	require.NoError(t, f.db.Model(&f.comp).Update("status", "under_maintenance").Error)
	f.run(1)
	assert.Equal(t, "under_maintenance", f.status(t))
	assert.Equal(t, "Komponente auf eine andere Seite verschoben; Vorfall 1 geschlossen; Status unverändert", releasedAudit(t, f).Details)
}

// Without an incident the identity is not known exactly: do not guess by slug,
// touch nothing, neutral audit.
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

// Hard-deleted, but a customer page happens to have the same slug: the
// audit does not name the foreign component and its status stays.
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

// Skip plus read error in the component lookup: clean up nothing (a read error
// is not a deleted component).
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
