package handlers_test

import (
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Gesamtseite = Durchschnitt der sichtbaren Komponenten; Schweregrade
// werden gewichtet, Überlappungen zählen nicht doppelt, Versteckte zählen nicht.
func TestAvailability_GewichtungMehrereKomponentenUndUeberlappung(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("gew", false)
	a := e.comp(p.ID, "A", "operational", nil)
	b := e.comp(p.ID, "B", "operational", nil)
	hidden := e.comp(p.ID, "Versteckt", "operational", nil)
	require.NoError(t, e.db.Model(&hidden).Update("visible", false).Error)

	// A: zwei überlappende eigene Vorfälle 00-12 und 06-18 (je degraded) = 18 h x 0,3.
	e.incident(p.ID, "resolved", *utc(20, 0), utc(20, 12), a.ID)
	e.incident(p.ID, "resolved", *utc(20, 6), utc(20, 18), a.ID)
	// Nur die versteckte Komponente ist betroffen: darf nichts verändern.
	e.incident(p.ID, "resolved", *utc(21, 0), utc(21, 12), hidden.ID)

	_, resp := e.get("/api/public/pages/gew/availability?include=components")
	var ca, cb availDay
	for _, c := range resp.Components {
		switch c.ID {
		case a.ID:
			ca = day(t, c.Days, "2026-10-20")
		case b.ID:
			cb = day(t, c.Days, "2026-10-20")
		default:
			t.Fatalf("versteckte Komponente %d darf nicht erscheinen", c.ID)
		}
	}
	assert.InDelta(t, 100-100*0.3*18/24, *ca.Availability, 1e-9)
	assert.Equal(t, int64(18*3600*3/10), ca.DowntimeSeconds)
	assert.InDelta(t, 100, *cb.Availability, 1e-9)

	page := day(t, resp.Days, "2026-10-20")
	assert.InDelta(t, (100-100*0.3*18/24+100)/2, *page.Availability, 1e-9, "Durchschnitt der sichtbaren Komponenten")
	assert.Equal(t, "degraded_performance", page.Status)
	assert.Equal(t, []availImpairment{{Status: "degraded_performance", Seconds: 18 * 3600}}, page.Impairments)
	assert.Equal(t, "operational", day(t, resp.Days, "2026-10-21").Status, "Vorfall an versteckter Komponente zählt nicht")
}

// Ein längerer, schwererer externer Zeitraum verdrängt den eigenen Vorfall;
// Mitternachtsgrenze und mehrtägige Dauer werden je Tag gewichtet.
func TestAvailability_ExternSchwererVerdraengtEigenenVorfall(t *testing.T) {
	x := newExtAvail(t)
	x.incident(x.page.ID, "resolved", *utc(20, 0), utc(20, 12), x.comp.ID)
	x.add(models.ExternalIncidentKindIncident, "critical", utc(20, 6), utc(21, 6), true)
	days := x.days()
	d20 := day(t, days, "2026-10-20")
	// 00-06 degraded (6 h x 0,3) + 06-24 critical (18 h) = 1,8 h + 18 h
	assert.EqualValues(t, int64((1.8+18)*3600), d20.DowntimeSeconds)
	assert.Equal(t, "major_outage", d20.Status)
	assert.Equal(t, []availImpairment{{Status: "major_outage", Seconds: 18 * 3600}, {Status: "degraded_performance", Seconds: 6 * 3600}}, d20.Impairments)
	d21 := day(t, days, "2026-10-21")
	assert.EqualValues(t, 6*3600, d21.DowntimeSeconds)
	assert.Equal(t, "major_outage", d21.Status)
}
