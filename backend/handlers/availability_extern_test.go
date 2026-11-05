package handlers_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gespiegelte Meldungen externer Quellen färben den 90-Tage-
// Verlauf der gekoppelten Komponente (vorher: nur eigene Vorfälle).

type extAvail struct {
	*availEnv
	page models.StatusPage
	comp models.Component
	src  models.ExternalSource
	ext  models.ExternalComponent
}

func newExtAvail(t *testing.T) *extAvail {
	t.Helper()
	e := newAvailEnv(t)
	p := e.page("ext", false)
	c := e.comp(p.ID, "API", "operational", nil)
	src := models.ExternalSource{StatusPageID: p.ID, Name: "Example Cloud", BaseURL: "https://status.example.com", Active: true}
	require.NoError(t, e.db.Create(&src).Error)
	ec := models.ExternalComponent{ExternalSourceID: src.ID, ExternalID: "vm", Name: "VM", Status: "operational", LastSeenAt: e.now}
	require.NoError(t, e.db.Create(&ec).Error)
	require.NoError(t, e.db.Create(&models.ExternalComponentLink{StatusPageID: p.ID, ComponentID: c.ID, ExternalComponentID: ec.ID}).Error)
	return &extAvail{availEnv: e, page: p, comp: c, src: src, ext: ec}
}

func (x *extAvail) add(kind, impact string, start, end *time.Time, closed bool) {
	x.t.Helper()
	inc := models.ExternalIncident{
		StatusPageID: x.page.ID, ExternalSourceID: x.src.ID, ExternalID: kind + impact, Kind: kind, Title: "t",
		Status: "investigating", Impact: impact, Closed: closed, LastSeenAt: x.now, ExternalUpdatedAt: end,
	}
	if kind == models.ExternalIncidentKindMaintenance {
		inc.ScheduledFor, inc.ScheduledUntil = start, end
	} else {
		inc.StartedAt, inc.ResolvedAt = start, end
		if closed {
			inc.Status = "resolved"
		}
	}
	require.NoError(x.t, x.db.Create(&inc).Error)
	require.NoError(x.t, x.db.Create(&models.ExternalIncidentComponent{ExternalIncidentID: inc.ID, ComponentExternalID: "vm", Name: "VM"}).Error)
}

func (x *extAvail) days() []availDay {
	x.t.Helper()
	x.now = x.now.Add(2 * time.Minute) // Cache (60 s) umgehen
	_, resp := x.get("/api/public/pages/ext/availability?include=components")
	require.Len(x.t, resp.Components, 1)
	return resp.Components[0].Days
}

func TestAvailability_ExterneMeldungenFaerbenVerlauf(t *testing.T) {
	x := newExtAvail(t)
	x.add(models.ExternalIncidentKindIncident, "major", utc(20, 6), utc(20, 12), true)
	x.add(models.ExternalIncidentKindMaintenance, "maintenance", utc(22, 2), utc(22, 4), false)
	x.add(models.ExternalIncidentKindIncident, "none", utc(24, 6), utc(24, 12), true)

	days := x.days()
	d := day(t, days, "2026-10-20")
	assert.Equal(t, "partial_outage", d.Status, "impact major -> partial_outage")
	assert.EqualValues(t, 6*3600*6/10, d.DowntimeSeconds, "6 h x Gewicht 0,6")
	m := day(t, days, "2026-10-22")
	assert.Equal(t, "under_maintenance", m.Status)
	assert.EqualValues(t, 0, m.DowntimeSeconds, "Wartung ist kein Ausfall")
	assert.Equal(t, "operational", day(t, days, "2026-10-24").Status, "Auswirkung none färbt nichts")

	_, resp := x.get("/api/public/pages/ext/availability")
	assert.Equal(t, "partial_outage", day(t, resp.Days, "2026-10-20").Status, "auch die Gesamtseite")
}

// das `impact` der externen Meldung bestimmt Status und Gewicht
// (Vorschlag). Rot gegen den alten Stand: dort zählte jede Meldung als
// degraded_performance mit voller Dauer.
func TestAvailability_ExterneMeldungSchweregradAusImpact(t *testing.T) {
	cases := []struct {
		impact     string
		wantStatus string
		wantDown   int64 // 6 h gewichtet
		wantPct    float64
	}{
		{"minor", "degraded_performance", 6480, 100 - 100*0.3*6/24},
		{"major", "partial_outage", 12960, 100 - 100*0.6*6/24},
		{"critical", "major_outage", 21600, 75},
		{"none", "operational", 0, 100},
		{"unbekannt", "degraded_performance", 6480, 92.5}, // unbekannter Wert: vorsichtig wie bisher
	}
	for _, tc := range cases {
		t.Run("impact "+tc.impact, func(t *testing.T) {
			x := newExtAvail(t)
			x.add(models.ExternalIncidentKindIncident, tc.impact, utc(20, 6), utc(20, 12), true)
			d := day(t, x.days(), "2026-10-20")
			assert.Equal(t, tc.wantStatus, d.Status)
			assert.Equal(t, tc.wantDown, d.DowntimeSeconds)
			require.NotNil(t, d.Availability)
			assert.InDelta(t, tc.wantPct, *d.Availability, 1e-9)
			if tc.wantDown == 0 {
				assert.Empty(t, d.Impairments)
			} else {
				require.Len(t, d.Impairments, 1)
				assert.Equal(t, tc.wantStatus, d.Impairments[0].Status)
				assert.EqualValues(t, 6*3600, d.Impairments[0].Seconds, "Dauer je Schweregrad ist ungewichtet")
			}
		})
	}
}

func TestAvailability_ExterneMeldungOffenLaeuftBisJetztUndEntkoppeltZaehltNicht(t *testing.T) {
	x := newExtAvail(t)
	x.add(models.ExternalIncidentKindIncident, "minor", utc(29, 6), nil, false)
	d := day(t, x.days(), "2026-10-29")
	assert.Equal(t, "degraded_performance", d.Status)
	assert.EqualValues(t, (6*3600+120)*3/10, d.DowntimeSeconds, "offen: bis jetzt (6 h 2 min x 0,3)")

	require.NoError(t, x.db.Where("1 = 1").Delete(&models.ExternalComponentLink{}).Error)
	assert.Equal(t, "operational", day(t, x.days(), "2026-10-29").Status, "ohne Kopplung betrifft die Meldung keine eigene Komponente")
}

// addRaw legt eine Meldung mit genau den übergebenen Feldern an (Randfälle der
// Zeitabbildung: fehlender Beginn, Behebung ohne ResolvedAt, Zeiten in der Zukunft).
func (x *extAvail) addRaw(inc models.ExternalIncident) {
	x.t.Helper()
	inc.StatusPageID, inc.ExternalSourceID = x.page.ID, x.src.ID
	inc.Kind, inc.Title, inc.LastSeenAt = models.ExternalIncidentKindIncident, "t", x.now
	if inc.Impact == "" {
		inc.Impact = "major"
	}
	require.NoError(x.t, x.db.Create(&inc).Error)
	require.NoError(x.t, x.db.Create(&models.ExternalIncidentComponent{ExternalIncidentID: inc.ID, ComponentExternalID: "vm", Name: "VM"}).Error)
}

func TestAvailability_ExterneMeldungRandfaelleDerZeitabbildung(t *testing.T) {
	x := newExtAvail(t)
	// Ohne Beginn gilt die letzte Änderung als Beginn, behoben ohne ResolvedAt endet mit ihr (Länge 0 färbt den Tag).
	x.addRaw(models.ExternalIncident{ExternalID: "a", Closed: true, ExternalUpdatedAt: utc(20, 8)})
	// Ohne jede Zeitangabe nicht auswertbar.
	x.addRaw(models.ExternalIncident{ExternalID: "b"})
	// Ende in der Zukunft wird auf jetzt gekappt, Ende vor Beginn auf den Beginn.
	x.addRaw(models.ExternalIncident{ExternalID: "c", StartedAt: utc(25, 6), ResolvedAt: utc(30, 6)})
	x.addRaw(models.ExternalIncident{ExternalID: "d", StartedAt: utc(22, 6), ResolvedAt: utc(22, 2)})
	days := x.days()
	assert.Equal(t, "partial_outage", day(t, days, "2026-10-20").Status)
	assert.EqualValues(t, 0, day(t, days, "2026-10-20").DowntimeSeconds)
	assert.Equal(t, "partial_outage", day(t, days, "2026-10-25").Status)
	assert.Equal(t, "partial_outage", day(t, days, "2026-10-22").Status)
	assert.EqualValues(t, 0, day(t, days, "2026-10-22").DowntimeSeconds)
}

func TestAvailability_ExterneMeldungVorSeitenanlageVerschiebtDatenbeginn(t *testing.T) {
	x := newExtAvail(t)
	require.NoError(t, x.db.Model(&x.page).Update("created_at", *utc(25, 0)).Error)
	x.add(models.ExternalIncidentKindIncident, "major", utc(22, 6), utc(22, 12), true)
	days := x.days()
	assert.Equal(t, "partial_outage", day(t, days, "2026-10-22").Status, "Daten beginnen mit der Meldung")
	assert.Equal(t, "no_data", day(t, days, "2026-10-21").Status)
}

func TestAvailability_ExterneMeldungenFehlerGibt500(t *testing.T) {
	x := newExtAvail(t)
	require.NoError(t, x.db.Exec("DROP TABLE external_incidents").Error)
	w, _ := x.get("/api/public/pages/ext/availability")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAvailability_ExterneMeldungOhneZeitenOderAusserhalbDesFensters(t *testing.T) {
	x := newExtAvail(t)
	x.add(models.ExternalIncidentKindMaintenance, "maintenance", nil, nil, false)
	old := time.Date(2026, 5, 1, 6, 0, 0, 0, time.UTC)
	oldEnd := old.Add(time.Hour)
	x.add(models.ExternalIncidentKindIncident, "major", &old, &oldEnd, true)
	for _, d := range x.days() {
		assert.NotEqual(t, "degraded_performance", d.Status)
		assert.NotEqual(t, "under_maintenance", d.Status)
	}
}
