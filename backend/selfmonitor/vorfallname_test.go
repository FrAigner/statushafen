package selfmonitor

import (
	"errors"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// vorfall liefert den einzigen Vorfall der Fixture samt Updates.
func vorfall(t *testing.T, f *fixture) (models.Incident, []models.IncidentUpdate) {
	t.Helper()
	var inc models.Incident
	require.NoError(t, f.db.Unscoped().First(&inc).Error)
	var ups []models.IncidentUpdate
	require.NoError(t, f.db.Where("incident_id = ?", inc.ID).Order("id").Find(&ups).Error)
	return inc, ups
}

// uebersetzungen legt je eine bestätigte Übersetzung für den Vorfall und das
// Eröffnungs-Update an (beide nennen den Prüfungsnamen).
func uebersetzungen(t *testing.T, f *fixture) {
	t.Helper()
	inc, ups := vorfall(t, f)
	require.NoError(t, f.db.Create(&models.IncidentTranslation{IncidentID: inc.ID, LanguageCode: "en",
		Title: "Outage: API", Message: "Self-monitoring reports an outage of API.", Status: "confirmed"}).Error)
	require.NoError(t, f.db.Create(&models.IncidentUpdateTranslation{UpdateID: ups[0].ID, LanguageCode: "en",
		Message: "Self-monitoring reports an outage of API.", Status: "confirmed"}).Error)
}

// Wird die Komponente während einer Störung archiviert/versteckt,
// bleibt der Vorfall öffentlich in der Historie. Titel, Meldung, Updates und
// Übersetzungen dürfen danach weder den Prüfungsnamen noch das Fehlerdetail nennen.
func TestMonitor_AusblendenNeutralisiertVorfalltexte(t *testing.T) {
	for name, hide := range map[string]func(*testing.T, *fixture){"archiviert": archiviere, "unsichtbar": verstecke} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			offeneStoerung(t, f)
			uebersetzungen(t, f)
			hide(t, f)
			f.run(1)
			inc, ups := vorfall(t, f)
			assert.Equal(t, "resolved", inc.Status)
			assert.Equal(t, neutralTitle, inc.Title)
			assert.Equal(t, neutralMessage, inc.Message)
			for _, u := range ups {
				assert.NotContains(t, u.Message, "API")
				assert.NotContains(t, u.Message, "nicht erreichbar")
			}
			assert.Equal(t, neutralMessage, ups[0].Message)
			assert.Equal(t, hiddenReason, ups[len(ups)-1].Message)
			assert.Zero(t, f.count(t, &models.IncidentTranslation{}, "1 = 1"), "Übersetzungen nennen den Namen")
			assert.Zero(t, f.count(t, &models.IncidentUpdateTranslation{}, "1 = 1"))
			assert.Zero(t, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
		})
	}
}

// Nur die unveränderten Texte des Monitors werden ersetzt. Ein vom
// Admin geänderter Titel und eigene Updates bleiben, ebenso die Übersetzungen,
// wenn am Vorfall selbst nichts ersetzt wurde.
func TestMonitor_AusblendenLaesstAdminTexteStehen(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	uebersetzungen(t, f)
	inc, _ := vorfall(t, f)
	require.NoError(t, f.db.Model(&inc).Updates(map[string]any{"title": "Schnittstelle gestört", "message": "Wir prüfen die API."}).Error)
	require.NoError(t, f.db.Create(&models.IncidentUpdate{IncidentID: inc.ID, Status: "identified",
		Message: "Die automatische Selbstüberwachung meldet eine Störung bei API (vom Admin zitiert).", AuthorEmail: "admin@example.com"}).Error)
	archiviere(t, f)
	f.run(1)
	inc, ups := vorfall(t, f)
	assert.Equal(t, "Schnittstelle gestört", inc.Title)
	assert.Equal(t, "Wir prüfen die API.", inc.Message)
	assert.Equal(t, neutralMessage, ups[0].Message, "Eröffnungs-Update des Monitors")
	assert.Contains(t, ups[1].Message, "vom Admin zitiert", "fremde Updates bleiben")
	assert.EqualValues(t, 1, f.count(t, &models.IncidentTranslation{}, "1 = 1"))
	assert.Zero(t, f.count(t, &models.IncidentUpdateTranslation{}, "1 = 1"))
}

// Hat der Admin nur die Meldung geändert, wird allein der Titel ersetzt.
func TestMonitor_AusblendenErsetztNurUnveraendertenTitel(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	inc, _ := vorfall(t, f)
	require.NoError(t, f.db.Model(&inc).Update("message", "Eigene Meldung").Error)
	verstecke(t, f)
	f.run(1)
	inc, _ = vorfall(t, f)
	assert.Equal(t, neutralTitle, inc.Title)
	assert.Equal(t, "Eigene Meldung", inc.Message)
}

// Auch ein vom Admin schon geschlossener Vorfall bleibt öffentlich
// und wird beim Freigeben neutralisiert.
func TestMonitor_AusblendenNeutralisiertGeschlossenenVorfall(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	require.NoError(t, f.db.Model(&models.Incident{}).Where("1 = 1").Update("status", "resolved").Error)
	archiviere(t, f)
	f.run(1)
	inc, _ := vorfall(t, f)
	assert.Equal(t, neutralTitle, inc.Title)
	assert.Zero(t, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
}

// Gelöschter Vorfall oder unbrauchbare Vorfall-Marke: nichts zu
// neutralisieren, die Freigabe gelingt trotzdem.
func TestMonitor_AusblendenOhneVorfallGibtFrei(t *testing.T) {
	for name, prep := range map[string]func(*testing.T, *fixture){
		"gelöscht": func(t *testing.T, f *fixture) {
			require.NoError(t, f.db.Where("1 = 1").Delete(&models.Incident{}).Error)
		},
		"Marke kaputt": func(t *testing.T, f *fixture) {
			require.NoError(t, f.db.Model(&models.SystemSetting{}).Where("key = ?", incidentKeyPrefix+testSlug).Update("value", "kaputt").Error)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			offeneStoerung(t, f)
			prep(t, f)
			archiviere(t, f)
			f.run(1)
			assert.Equal(t, "operational", f.status(t))
			assert.Zero(t, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
		})
	}
}

// Scheitert ein Schritt des Neutralisierens, wird die ganze Freigabe
// zurückgerollt (Marken bleiben, Vorfall offen) und im nächsten Takt wiederholt.
func TestMonitor_AusblendenNeutralisierenFehlerWirdWiederholt(t *testing.T) {
	trigger := func(sql string) func(*testing.T, *fixture) func() {
		return func(t *testing.T, f *fixture) func() {
			require.NoError(t, f.db.Exec("CREATE TRIGGER sperre812 "+sql+" BEGIN SELECT RAISE(ABORT, 'kaputt'); END").Error)
			return func() { require.NoError(t, f.db.Exec("DROP TRIGGER sperre812").Error) }
		}
	}
	lesefehler := func(table string) func(*testing.T, *fixture) func() {
		return func(t *testing.T, f *fixture) func() {
			require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("test:lesefehler812", func(d *gorm.DB) {
				if d.Statement.Table == table {
					_ = d.AddError(errors.New("lesefehler"))
				}
			}))
			return func() { require.NoError(t, f.db.Callback().Query().Remove("test:lesefehler812")) }
		}
	}
	for name, fault := range map[string]func(*testing.T, *fixture) func(){
		"Vorfall lesen":         lesefehler("incidents"),
		"Updates lesen":         lesefehler("incident_updates"),
		"Meldung schreiben":     trigger("BEFORE UPDATE OF message ON incidents"),
		"Titel schreiben":       trigger("BEFORE UPDATE OF title ON incidents"),
		"Übersetzung löschen":   trigger("BEFORE UPDATE OF deleted_at ON incident_translations"),
		"Update schreiben":      trigger("BEFORE UPDATE OF message ON incident_updates"),
		"Update-Übers. löschen": trigger("BEFORE UPDATE OF deleted_at ON incident_update_translations"),
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			offeneStoerung(t, f)
			uebersetzungen(t, f)
			archiviere(t, f)
			undo := fault(t, f)
			f.run(1)
			undo()
			inc, _ := vorfall(t, f)
			assert.Equal(t, "Störung: API", inc.Title, "zurückgerollt")
			assert.NotEqual(t, "resolved", inc.Status)
			assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
			f.run(1)
			inc, _ = vorfall(t, f)
			assert.Equal(t, neutralTitle, inc.Title)
			assert.Equal(t, "resolved", inc.Status)
			assert.Zero(t, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))
		})
	}
}

// Review A: Die Meldung gilt nur als unverändert, wenn sie exakt der
// Originalmeldung des Monitors entspricht. Ein Anhang des Admins bleibt stehen.
func TestMonitor_AusblendenLaesstErgaenzteMeldungStehen(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	inc, _ := vorfall(t, f)
	ergaenzt := inc.Message + " Nachtrag vom Admin: Ursache gefunden."
	require.NoError(t, f.db.Model(&inc).Update("message", ergaenzt).Error)
	archiviere(t, f)
	f.run(1)
	inc, ups := vorfall(t, f)
	assert.Equal(t, ergaenzt, inc.Message)
	assert.Equal(t, neutralTitle, inc.Title)
	assert.Equal(t, neutralMessage, ups[0].Message, "das Eröffnungs-Update des Monitors wird trotzdem ersetzt")
}

// Review B: Ändert der Admin Titel und Meldung, nachdem der Monitor den
// Vorfall gelesen hat, darf das UPDATE den neuen Text nicht überschreiben. Der
// Edit wird unmittelbar vor dem Ersetzen des Titels in derselben Verbindung
// eingespielt.
func TestMonitor_AusblendenUeberschreibtZwischenzeitlichenAdminEditNicht(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	archiviere(t, f)
	done := false
	require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register("test:adminedit812", func(d *gorm.DB) {
		if done || d.Statement.Table != "incidents" {
			return
		}
		if _, ok := d.Statement.Dest.(map[string]any)["title"]; !ok {
			return
		}
		done = true
		_, err := d.Statement.ConnPool.ExecContext(d.Statement.Context,
			"UPDATE incidents SET title = 'Admin-Titel', message = 'Admin-Meldung'")
		require.NoError(t, err)
	}))
	f.run(1)
	require.NoError(t, f.db.Callback().Update().Remove("test:adminedit812"))
	require.True(t, done, "Admin-Edit eingespielt")
	inc, _ := vorfall(t, f)
	assert.Equal(t, "Admin-Titel", inc.Title)
	assert.Equal(t, "Admin-Meldung", inc.Message)
}

// Review C: Die Übersetzung eines nicht ersetzten Updates bleibt stehen.
func TestMonitor_AusblendenLaesstUebersetzungFremderUpdatesStehen(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	inc, _ := vorfall(t, f)
	admin := models.IncidentUpdate{IncidentID: inc.ID, Status: "identified", Message: "Wir prüfen das.", AuthorEmail: "admin@example.com"}
	require.NoError(t, f.db.Create(&admin).Error)
	require.NoError(t, f.db.Create(&models.IncidentUpdateTranslation{UpdateID: admin.ID, LanguageCode: "en",
		Message: "We are looking into it.", Status: "confirmed"}).Error)
	verstecke(t, f)
	f.run(1)
	assert.EqualValues(t, 1, f.count(t, &models.IncidentUpdateTranslation{}, "update_id = ?", admin.ID))
}

// Andere Abschlussgründe (Erholung) lassen die Texte unverändert.
func TestMonitor_ErholungBehaeltVorfalltexte(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	f.res = &Result{OK: true}
	f.run(1)
	inc, _ := vorfall(t, f)
	assert.Equal(t, "Störung: API", inc.Title)
	assert.Contains(t, inc.Message, "nicht erreichbar")
}
