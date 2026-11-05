package selfmonitor

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// erholt lässt die Störung regulär enden (Erholung, Texte bleiben original):
// der Altfall, den die frühere Korrektur nicht erfasst, weil die Komponente erst später
// ausgeblendet wird.
func erholt(t *testing.T, f *fixture) {
	t.Helper()
	offeneStoerung(t, f)
	uebersetzungen(t, f)
	f.res = &Result{OK: true}
	f.run(1)
	inc, _ := vorfall(t, f)
	require.Equal(t, "resolved", inc.Status)
	require.Equal(t, "Störung: API", inc.Title)
}

func verstecktOderWeg() map[string]func(*testing.T, *fixture) {
	return map[string]func(*testing.T, *fixture){
		"archiviert": archiviere,
		"unsichtbar": verstecke,
		"gelöscht":   func(t *testing.T, f *fixture) { require.NoError(t, f.db.Delete(&f.comp).Error) },
		"hart gelöscht": func(t *testing.T, f *fixture) {
			require.NoError(t, f.db.Unscoped().Delete(&f.comp).Error)
		},
		"verschoben": func(t *testing.T, f *fixture) { verschiebe(t, f) },
	}
}

func neutral(t *testing.T, f *fixture) {
	t.Helper()
	inc, ups := vorfall(t, f)
	assert.Equal(t, neutralTitle, inc.Title)
	assert.Equal(t, neutralMessage, inc.Message)
	assert.Equal(t, neutralMessage, ups[0].Message)
	for _, u := range ups {
		assert.NotContains(t, u.Message, "API")
		assert.NotContains(t, u.Message, "nicht erreichbar")
	}
	assert.Zero(t, f.count(t, &models.IncidentTranslation{}, "1 = 1"))
	assert.Zero(t, f.count(t, &models.IncidentUpdateTranslation{}, "1 = 1"))
}

// 1: Wird die Komponente während der Störung gelöscht oder
// verschoben, schließt releaseOrphan den Vorfall; er bleibt öffentlich und darf
// weder Prüfungsnamen noch Fehlerdetail nennen.
func TestMonitor_WaiseNeutralisiertVorfalltexte(t *testing.T) {
	for name, weg := range verstecktOderWeg() {
		if name == "archiviert" || name == "unsichtbar" {
			continue // kein Waisen-Pfad, wird an anderer Stelle geprüft
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			offeneStoerung(t, f)
			uebersetzungen(t, f)
			weg(t, f)
			f.run(1)
			inc, _ := vorfall(t, f)
			assert.Equal(t, "resolved", inc.Status)
			neutral(t, f)
			assert.Zero(t, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
		})
	}
}

// 1: Auch beim Abschalten der Überwachung (releaseAll -> Waise).
func TestMonitor_WaiseBeimAbschaltenNeutralisiert(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	f.m.Enabled = func() (bool, error) { return false, nil }
	f.run(1)
	inc, _ := vorfall(t, f)
	assert.Equal(t, neutralTitle, inc.Title)
}

// 1: Texte des Admins bleiben auch im Waisen-Pfad stehen.
func TestMonitor_WaiseLaesstAdminTexteStehen(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	inc, _ := vorfall(t, f)
	require.NoError(t, f.db.Model(&inc).Updates(map[string]any{"title": "Schnittstelle gestört", "message": "Wir prüfen die API."}).Error)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	f.run(1)
	inc, _ = vorfall(t, f)
	assert.Equal(t, "Schnittstelle gestört", inc.Title)
	assert.Equal(t, "Wir prüfen die API.", inc.Message)
}

// 1: Scheitert das Neutralisieren, wird die Freigabe zurückgerollt
// (Marken bleiben, Vorfall offen) und beim nächsten Takt wiederholt.
func TestMonitor_WaiseNeutralisierenFehlerWirdWiederholt(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	require.NoError(t, f.db.Delete(&f.comp).Error)
	require.NoError(t, f.db.Exec("CREATE TRIGGER sperre829 BEFORE UPDATE OF title ON incidents BEGIN SELECT RAISE(ABORT, 'kaputt'); END").Error)
	f.run(1)
	require.NoError(t, f.db.Exec("DROP TRIGGER sperre829").Error)
	inc, _ := vorfall(t, f)
	assert.NotEqual(t, "resolved", inc.Status, "zurückgerollt")
	assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
	f.run(1)
	inc, _ = vorfall(t, f)
	assert.Equal(t, "resolved", inc.Status)
	assert.Equal(t, neutralTitle, inc.Title)
}

// 2: Altfälle (regulär erholt, Komponente später ausgeblendet,
// gelöscht oder verschoben) werden beim Backfill neutralisiert.
func TestNeutralizeHiddenIncidents_Altfaelle(t *testing.T) {
	for name, weg := range verstecktOderWeg() {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			erholt(t, f)
			weg(t, f)
			require.NoError(t, NeutralizeHiddenIncidents(f.db))
			neutral(t, f)
			// Idempotent: zweiter Lauf ändert nichts mehr.
			require.NoError(t, NeutralizeHiddenIncidents(f.db))
			neutral(t, f)
		})
	}
}

// 2: Ohne Zuordnung (Komponente samt Verknüpfung entfernt) gilt der
// Vorfall als verwaist und wird neutralisiert (konservativ).
func TestNeutralizeHiddenIncidents_OhneZuordnung(t *testing.T) {
	f := newFixture(t)
	erholt(t, f)
	require.NoError(t, f.db.Unscoped().Where("1 = 1").Delete(&models.IncidentComponent{}).Error)
	require.NoError(t, NeutralizeHiddenIncidents(f.db))
	neutral(t, f)
}

// 2: Sichtbare Komponente, offener Vorfall, Admin-Texte und fremde
// Seiten bleiben unangetastet.
func TestNeutralizeHiddenIncidents_LaesstAnderesStehen(t *testing.T) {
	t.Run("sichtbare Komponente", func(t *testing.T) {
		f := newFixture(t)
		erholt(t, f)
		require.NoError(t, NeutralizeHiddenIncidents(f.db))
		inc, _ := vorfall(t, f)
		assert.Equal(t, "Störung: API", inc.Title)
	})
	t.Run("offener Vorfall", func(t *testing.T) {
		f := newFixture(t)
		offeneStoerung(t, f)
		archiviere(t, f)
		require.NoError(t, NeutralizeHiddenIncidents(f.db))
		inc, _ := vorfall(t, f)
		assert.Equal(t, "Störung: API", inc.Title, "gehört dem Monitor, der gibt ihn im nächsten Takt frei")
	})
	t.Run("Admin-Texte", func(t *testing.T) {
		f := newFixture(t)
		erholt(t, f)
		inc, _ := vorfall(t, f)
		require.NoError(t, f.db.Model(&inc).Updates(map[string]any{"title": "Schnittstelle gestört", "message": "Wir prüfen die API."}).Error)
		verstecke(t, f)
		require.NoError(t, NeutralizeHiddenIncidents(f.db))
		inc, ups := vorfall(t, f)
		assert.Equal(t, "Schnittstelle gestört", inc.Title)
		assert.Equal(t, "Wir prüfen die API.", inc.Message)
		assert.Equal(t, neutralMessage, ups[0].Message, "Eröffnungs-Update des Monitors")
	})
	t.Run("andere Seite", func(t *testing.T) {
		f := newFixture(t)
		other := models.StatusPage{Name: "Kunde", Slug: "kunde"}
		require.NoError(t, f.db.Create(&other).Error)
		fremd := models.Incident{StatusPageID: other.ID, Title: "Störung: API", Message: "x", Status: "resolved"}
		require.NoError(t, f.db.Create(&fremd).Error)
		require.NoError(t, f.db.Create(&models.IncidentUpdate{IncidentID: fremd.ID, Status: "investigating", AuthorEmail: systemActor,
			Message: incidentMessagePrefix(Check{Name: "API"}) + "x)."}).Error)
		require.NoError(t, NeutralizeHiddenIncidents(f.db))
		var nachher models.Incident
		require.NoError(t, f.db.First(&nachher, fremd.ID).Error)
		assert.Equal(t, "Störung: API", nachher.Title, "Mandantentrennung: nur die Systemseite")
	})
	t.Run("Update eines Admins", func(t *testing.T) {
		f := newFixture(t)
		erholt(t, f)
		inc, _ := vorfall(t, f)
		require.NoError(t, f.db.Create(&models.IncidentUpdate{IncidentID: inc.ID, Status: "identified", AuthorEmail: "admin@example.com",
			Message: incidentMessagePrefix(Check{Name: "API"}) + "zitiert)."}).Error)
		verstecke(t, f)
		require.NoError(t, NeutralizeHiddenIncidents(f.db))
		var u models.IncidentUpdate
		require.NoError(t, f.db.Where("author_email = ?", "admin@example.com").First(&u).Error)
		assert.Contains(t, u.Message, "zitiert")
	})
	t.Run("unlesbarer Name", func(t *testing.T) {
		f := newFixture(t)
		erholt(t, f)
		require.NoError(t, f.db.Model(&models.IncidentUpdate{}).Where("author_email = ?", systemActor).
			Where("message LIKE ?", "Die automatische%").Update("message", incidentMessageHead+"API ohne Klammer").Error)
		verstecke(t, f)
		require.NoError(t, NeutralizeHiddenIncidents(f.db))
		inc, _ := vorfall(t, f)
		assert.Equal(t, "Störung: API", inc.Title)
	})
}

// 2: Ohne Systemseite ist nichts zu tun.
func TestNeutralizeHiddenIncidents_SystemseiteFehlt(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.db.Unscoped().Delete(&f.page).Error)
	require.NoError(t, NeutralizeHiddenIncidents(f.db))
}

// 2: Fail closed: Jeder Fehler gibt einen Fehler zurück und
// schreibt nichts (eine Transaktion); der nächste Start wiederholt den Backfill.
func TestNeutralizeHiddenIncidents_FehlerRolltZurueck(t *testing.T) {
	trigger := func(sql string) func(*testing.T, *fixture) func() {
		return func(t *testing.T, f *fixture) func() {
			require.NoError(t, f.db.Exec("CREATE TRIGGER sperre829b "+sql+" BEGIN SELECT RAISE(ABORT, 'kaputt'); END").Error)
			return func() { require.NoError(t, f.db.Exec("DROP TRIGGER sperre829b").Error) }
		}
	}
	lesefehler := func(table string) func(*testing.T, *fixture) func() {
		return func(t *testing.T, f *fixture) func() {
			require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("test:lesefehler829", func(d *gorm.DB) {
				if d.Statement.Table == table {
					_ = d.AddError(errors.New("lesefehler"))
				}
			}))
			return func() { require.NoError(t, f.db.Callback().Query().Remove("test:lesefehler829")) }
		}
	}
	for name, fault := range map[string]func(*testing.T, *fixture) func(){
		"Systemseite lesen":   lesefehler("status_pages"),
		"Komponenten lesen":   lesefehler("components"),
		"Eröffnungen lesen":   lesefehler("incident_updates"),
		"Zuordnung lesen":     lesefehler("incident_components"),
		"Titel schreiben":     trigger("BEFORE UPDATE OF title ON incidents"),
		"Update schreiben":    trigger("BEFORE UPDATE OF message ON incident_updates"),
		"Übersetzung löschen": trigger("BEFORE UPDATE OF deleted_at ON incident_translations"),
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			erholt(t, f)
			verstecke(t, f)
			undo := fault(t, f)
			require.Error(t, NeutralizeHiddenIncidents(f.db))
			undo()
			inc, _ := vorfall(t, f)
			assert.Equal(t, "Störung: API", inc.Title, "nichts geschrieben")
			require.NoError(t, NeutralizeHiddenIncidents(f.db))
			neutral(t, f)
		})
	}
}

// 2: Die Komponente ist per ID nicht lesbar (Lesefehler, nicht
// "nicht gefunden"): Fehler statt Raten.
func TestNeutralizeHiddenIncidents_KomponenteNichtLesbar(t *testing.T) {
	f := newFixture(t)
	erholt(t, f)
	verstecke(t, f)
	n := 0
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("test:lesefehler829c", func(d *gorm.DB) {
		if d.Statement.Table == "components" {
			n++
			if n == 2 { // 1. Liste der Seite, 2. Einzelabfrage
				_ = d.AddError(errors.New("lesefehler"))
			}
		}
	}))
	require.Error(t, NeutralizeHiddenIncidents(f.db))
	require.NoError(t, f.db.Callback().Query().Remove("test:lesefehler829c"))
	inc, _ := vorfall(t, f)
	assert.Equal(t, "Störung: API", inc.Title)
}

// Review Wird die Komponente erst NACH regulärer Erholung ausgeblendet,
// gelöscht oder verschoben, gehört der Vorfall dem Monitor nicht mehr. Der Takt
// neutralisiert ihn deshalb regelmäßig (alle neutralizeEvery Takte), nicht nur
// beim Start, und unabhängig vom Schalter (die Texte sind öffentlich).
func TestMonitor_TaktNeutralisiertNachtraeglichAusgeblendeteVorfaelle(t *testing.T) {
	for name, weg := range verstecktOderWeg() {
		for _, an := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/an=%v", name, an), func(t *testing.T) {
				f := newFixture(t)
				erholt(t, f) // Takte bis hier: neutralizeEvery ist nicht erreicht
				weg(t, f)
				f.m.Enabled = func() (bool, error) { return an, nil }
				f.res = &Result{OK: true}
				for f.m.tick < neutralizeEvery-1 {
					f.run(1)
				}
				inc, _ := vorfall(t, f)
				assert.Equal(t, "Störung: API", inc.Title, "noch nicht fällig")
				f.run(1)
				neutral(t, f)
			})
		}
	}
}

// Review Scheitert die Bereinigung im Takt, wird geloggt, nichts
// geschrieben und der Takt läuft weiter; der nächste fällige Takt wiederholt sie.
func TestMonitor_TaktBereinigungFehlerIstFailClosed(t *testing.T) {
	f := newFixture(t)
	erholt(t, f)
	verstecke(t, f)
	buf := &bytes.Buffer{}
	alt := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(alt) })
	require.NoError(t, f.db.Exec("CREATE TRIGGER sperre829t BEFORE UPDATE OF title ON incidents BEGIN SELECT RAISE(ABORT, 'kaputt'); END").Error)
	for f.m.tick < neutralizeEvery {
		f.run(1)
	}
	assert.Contains(t, buf.String(), "Vorfalltexte nicht bereinigt")
	inc, _ := vorfall(t, f)
	assert.Equal(t, "Störung: API", inc.Title)
	require.NoError(t, f.db.Exec("DROP TRIGGER sperre829t").Error)
	for f.m.tick < 2*neutralizeEvery {
		f.run(1)
	}
	neutral(t, f)
}
