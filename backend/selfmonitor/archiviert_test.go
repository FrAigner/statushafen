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

func archiviere(t *testing.T, f *fixture) {
	t.Helper()
	now := time.Now()
	require.NoError(t, f.db.Model(&models.Component{}).Where("id = ?", f.comp.ID).Update("archived_at", now).Error)
}

func verstecke(t *testing.T, f *fixture) {
	t.Helper()
	require.NoError(t, f.db.Model(&models.Component{}).Where("id = ?", f.comp.ID).Update("visible", false).Error)
}

// Eine archivierte oder unsichtbare Funktions-Komponente wird nicht
// ausgewertet: kein Statuswechsel, kein Marker, kein öffentlicher Vorfall mit Namen.
func TestMonitor_ArchivierteOderUnsichtbareKomponenteWirdNichtAusgewertet(t *testing.T) {
	for name, hide := range map[string]func(*testing.T, *fixture){"archiviert": archiviere, "unsichtbar": verstecke} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			hide(t, f)
			f.fail()
			f.run(3)
			f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
			f.run(2)
			assert.Equal(t, "operational", f.status(t))
			assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
			assert.EqualValues(t, 0, f.count(t, &models.Incident{}, "1 = 1"))
			assert.EqualValues(t, 0, f.count(t, &models.AuditLog{}, "1 = 1"))
		})
	}
}

// Wird die Komponente während einer Störung archiviert/versteckt,
// gibt der Monitor sie frei; der Abschlusstext nennt den Namen nicht.
func TestMonitor_ArchivierenWaehrendStoerungGibtFrei(t *testing.T) {
	for name, hide := range map[string]func(*testing.T, *fixture){"archiviert": archiviere, "unsichtbar": verstecke} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			offeneStoerung(t, f)
			hide(t, f)
			f.run(1)
			assert.Equal(t, "operational", f.status(t))
			assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
			assert.EqualValues(t, 0, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
			assert.EqualValues(t, 1, f.count(t, &models.IncidentUpdate{}, "status = ? AND message = ?", "resolved", hiddenReason))
			assert.EqualValues(t, 0, f.count(t, &models.IncidentUpdate{}, "status = ? AND message LIKE ?", "resolved", "%API%"))
			assert.EqualValues(t, 1, f.count(t, &models.AuditLog{}, "action = ? AND details LIKE ?", "selfmonitor.component_recovered", "%archiviert oder ausgeblendet%"))
			f.run(5)
			assert.EqualValues(t, 1, f.count(t, &models.AuditLog{}, "action = ?", "selfmonitor.component_recovered"), "kein erneutes Aufräumen")
		})
	}
}

// Wiederherstellen: die Überwachung läuft normal weiter.
func TestMonitor_WiederherstellenUeberwachtWeiter(t *testing.T) {
	f := newFixture(t)
	archiviere(t, f)
	f.fail()
	f.run(3)
	require.Equal(t, "operational", f.status(t))
	require.NoError(t, f.db.Model(&models.Component{}).Where("id = ?", f.comp.ID).Update("archived_at", nil).Error)
	f.run(3)
	assert.Equal(t, "major_outage", f.status(t))
}

// Der Zähler läuft nicht im Verborgenen weiter: nach dem
// Wiederherstellen braucht es wieder die volle Entprellung.
func TestMonitor_ArchivierungSetztZaehlerZurueck(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(2)
	archiviere(t, f)
	f.run(1)
	require.NoError(t, f.db.Model(&models.Component{}).Where("id = ?", f.comp.ID).Update("archived_at", nil).Error)
	f.run(1)
	assert.Equal(t, "operational", f.status(t))
}

// gruppeUeber hängt die überwachte Komponente unter eine neue Gruppe (wie die
// Funktions-Komponenten der Systemseite unter gruppe-<Key>) und liefert deren ID.
func gruppeUeber(t *testing.T, f *fixture) uint {
	t.Helper()
	g := models.Component{StatusPageID: f.page.ID, Name: "Plattform", Slug: "gruppe-plattform", Status: "group"}
	require.NoError(t, f.db.Create(&g).Error)
	require.NoError(t, f.db.Model(&models.Component{}).Where("id = ?", f.comp.ID).Update("parent_id", g.ID).Error)
	return g.ID
}

func gruppeAendern(t *testing.T, f *fixture, id uint, col string, val any) {
	t.Helper()
	require.NoError(t, f.db.Model(&models.Component{}).Where("id = ?", id).Update(col, val).Error)
}

// Review: Öffentlich gilt auch ein Nachfahre einer unsichtbaren oder
// archivierten Gruppe als ausgeblendet (models.HiddenComponentIDs).
func TestMonitor_KindEinerVersteckenGruppeWirdNichtAusgewertet(t *testing.T) {
	for name, hide := range map[string]func(*testing.T, *fixture, uint){
		"unsichtbar": func(t *testing.T, f *fixture, g uint) { gruppeAendern(t, f, g, "visible", false) },
		"archiviert": func(t *testing.T, f *fixture, g uint) { gruppeAendern(t, f, g, "archived_at", time.Now()) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			g := gruppeUeber(t, f)
			hide(t, f, g)
			f.fail()
			f.run(3)
			f.now = f.now.Add(DefaultIncidentAfter + time.Minute)
			f.run(2)
			assert.Equal(t, "operational", f.status(t))
			assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
			assert.EqualValues(t, 0, f.count(t, &models.Incident{}, "1 = 1"))
		})
	}
}

// Review: Gruppe während der Störung ausgeblendet: Freigabe; beim
// Wiedereinblenden läuft die Überwachung normal weiter.
func TestMonitor_GruppeWaehrendStoerungAusgeblendetGibtFreiUndBlendetWiederEin(t *testing.T) {
	f := newFixture(t)
	g := gruppeUeber(t, f)
	offeneStoerung(t, f)
	gruppeAendern(t, f, g, "visible", false)
	f.run(1)
	assert.Equal(t, "operational", f.status(t))
	assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
	assert.EqualValues(t, 0, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
	assert.EqualValues(t, 1, f.count(t, &models.IncidentUpdate{}, "status = ? AND message = ?", "resolved", hiddenReason))
	gruppeAendern(t, f, g, "visible", true)
	f.run(3)
	assert.Equal(t, "major_outage", f.status(t))
}

// Review: Auch ein Ergebnis "nicht prüfbar" (Skip) gibt eine
// laufende Störung frei, sobald die Komponente ausgeblendet ist.
func TestMonitor_VerstecktMitSkipGibtFrei(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	verstecke(t, f)
	f.res = &Result{Skip: true}
	f.run(1)
	assert.Equal(t, "operational", f.status(t))
	assert.EqualValues(t, 0, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
	assert.EqualValues(t, 0, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
}

// Review: Lesefehler beim Laden der Komponentenliste: Takt entfällt,
// nichts wird verändert, das Geheimnis steht nicht im Log.
func TestMonitor_KomponentenlisteLesefehlerUeberspringtPruefung(t *testing.T) {
	buf := logPuffer(t)
	f := newFixture(t)
	n := 0
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("test:komp_fehler", func(d *gorm.DB) {
		if d.Statement.Table == "components" {
			n++
			if n >= 2 {
				_ = d.AddError(errors.New("lesefehler https://db.example/x?token=GEHEIM123"))
			}
		}
	}))
	f.fail()
	f.run(4)
	require.NoError(t, f.db.Callback().Query().Remove("test:komp_fehler"))
	assert.Equal(t, "operational", f.status(t))
	assert.Contains(t, buf.String(), "Komponentenliste")
	assert.NotContains(t, buf.String(), "GEHEIM123")
}
