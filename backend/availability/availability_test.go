package availability

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	now   = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

func at(day, hour, min int) time.Time { // Oktober 2026
	return time.Date(2026, 10, day, hour, min, 0, 0, time.UTC)
}

func inc(s, e time.Time) Interval { return Interval{Start: s, End: e, Status: "degraded_performance"} }

func maint(s, e time.Time) Interval { return Interval{Start: s, End: e, Status: "under_maintenance"} }

func ptr(f float64) *float64 { return &f }

func find(t *testing.T, days []Day, date string) Day {
	t.Helper()
	for _, d := range days {
		if d.Date == date {
			return d
		}
	}
	t.Fatalf("Tag %s fehlt", date)
	return Day{}
}

// Weniger als 1 s nach 00:00 UTC war ObservedSeconds (ganze
// Sekunden) 0 und die Verfügbarkeit NaN (0/0). Kein NaN, kein Inf; die
// Beobachtung zählt in Bruchteilen von Sekunden.
func TestCompute_KnappNachMitternachtKeinNaN(t *testing.T) {
	n := time.Date(2026, 10, 29, 0, 0, 0, 400*int(time.Millisecond), time.UTC)
	days := Compute(nil, start, n, 3)
	today := find(t, days, "2026-10-29")
	require.NotNil(t, today.Availability)
	assert.False(t, math.IsNaN(*today.Availability), "NaN")
	assert.False(t, math.IsInf(*today.Availability, 0), "Inf")
	near(t, 100, today.Availability)
	assert.InDelta(t, 0.4, today.ObservedSeconds, 1e-9)

	// Mit Ausfall in diesen 400 ms: Verfügbarkeit weiter endlich.
	iv := []Interval{{Start: n.Add(-200 * time.Millisecond), End: n, Status: "major_outage"}}
	d := find(t, Compute(iv, start, n, 3), "2026-10-29")
	require.NotNil(t, d.Availability)
	assert.False(t, math.IsNaN(*d.Availability))
	near(t, 50, d.Availability)

	// Genau Mitternacht: nichts beobachtet, NoData statt 0/0.
	z := find(t, Compute(nil, start, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), 3), "2026-10-29")
	assert.Equal(t, NoData, z.Status)
	assert.Nil(t, z.Availability)
}

func near(t *testing.T, want float64, got *float64) {
	t.Helper()
	require.NotNil(t, got)
	assert.True(t, math.Abs(want-*got) < 1e-9, "erwartet %v, bekam %v", want, *got)
}

func TestCompute_Fenster(t *testing.T) {
	days := Compute(nil, start, now, 90)
	require.Len(t, days, 90)
	assert.Equal(t, "2026-08-01", days[0].Date)
	assert.Equal(t, "2026-10-29", days[89].Date, "inkl. heute")
}

func sev(status string, s, e time.Time) Interval { return Interval{Start: s, End: e, Status: status} }

// Verfügbarkeit = 1 - gewichtete Ausfallzeit / beobachtete Zeit.
// Die Gewichte sind festgeschrieben; eine Änderung muss bewusst erfolgen.
func TestWeight_Festgeschrieben(t *testing.T) {
	assert.Equal(t, 0.3, WeightDegradedPerformance)
	assert.Equal(t, 0.6, WeightPartialOutage)
	assert.Equal(t, 1.0, WeightMajorOutage)
	assert.Equal(t, 0.3, Weight("degraded_performance"))
	assert.Equal(t, 0.6, Weight("partial_outage"))
	assert.Equal(t, 1.0, Weight("major_outage"))
	for _, st := range []string{"operational", "under_maintenance", "unknown", "group", "", "irgendwas"} {
		assert.Zero(t, Weight(st), st)
	}
}

func TestCompute_Tagesaggregate(t *testing.T) {
	h := func(n float64) int64 { return int64(n * 3600) }
	cases := []struct {
		name       string
		intervals  []Interval
		dataStart  time.Time
		date       string
		wantStatus string
		wantPct    *float64
		wantDown   int64
	}{
		{"kein Vorfall: operational 100 %", nil, start, "2026-10-20", "operational", ptr(100), 0},
		// 6 h Eingeschränkt = 1,8 h gewichtet = 7,5 % von 24 h (vorher 25 %: voller Ausfall).
		{"Vorfall 6 h degraded gewichtet 0,3", []Interval{inc(at(20, 6, 0), at(20, 12, 0))}, start, "2026-10-20", "degraded_performance", ptr(100 - 100*1.8/24), h(1.8)},
		{"6 h Teilausfall gewichtet 0,6", []Interval{sev("partial_outage", at(20, 6, 0), at(20, 12, 0))}, start, "2026-10-20", "partial_outage", ptr(100 - 100*3.6/24), h(3.6)},
		{"6 h Totalausfall gewichtet 1,0", []Interval{sev("major_outage", at(20, 6, 0), at(20, 12, 0))}, start, "2026-10-20", "major_outage", ptr(75), h(6)},
		{"Vorfall endet genau an Tagesgrenze: Folgetag unberührt", []Interval{inc(at(20, 18, 0), at(21, 0, 0))}, start, "2026-10-21", "operational", ptr(100), 0},
		{"Vorfall über Mitternacht: Tag 1", []Interval{sev("major_outage", at(20, 18, 0), at(21, 6, 0))}, start, "2026-10-20", "major_outage", ptr(75), h(6)},
		{"Vorfall über Mitternacht: Tag 2", []Interval{sev("major_outage", at(20, 18, 0), at(21, 6, 0))}, start, "2026-10-21", "major_outage", ptr(75), h(6)},
		{"mehrtägig: Zwischentag voll degraded = 30 % Ausfall, nicht 100 %", []Interval{inc(at(20, 12, 0), at(23, 12, 0))}, start, "2026-10-21", "degraded_performance", ptr(70), h(7.2)},
		{"mehrtägig: Zwischentag voll Totalausfall = 0 %", []Interval{sev("major_outage", at(20, 12, 0), at(23, 12, 0))}, start, "2026-10-21", "major_outage", ptr(0), 86400},
		{"gleicher Schweregrad überlappt: zählt einmal", []Interval{inc(at(20, 0, 0), at(20, 12, 0)), inc(at(20, 6, 0), at(20, 18, 0))}, start, "2026-10-20", "degraded_performance", ptr(100 - 100*0.3*18/24), h(0.3 * 18)},
		{"schwerer verdrängt leichter, nicht addiert", []Interval{inc(at(20, 0, 0), at(20, 12, 0)), sev("major_outage", at(20, 6, 0), at(20, 12, 0))}, start, "2026-10-20", "major_outage", ptr(100 - 100*(0.3*6+6)/24), h(0.3*6 + 6)},
		{"eingeschlossener leichterer Vorfall ändert nichts", []Interval{sev("major_outage", at(20, 0, 0), at(20, 12, 0)), inc(at(20, 2, 0), at(20, 4, 0))}, start, "2026-10-20", "major_outage", ptr(50), h(12)},
		{"laufender Vorfall, heute bis jetzt (12 h beobachtet)", []Interval{sev("major_outage", at(29, 6, 0), now)}, start, "2026-10-29", "major_outage", ptr(50), h(6)},
		{"heute ohne Vorfall: 100 % der bisherigen Zeit", nil, start, "2026-10-29", "operational", ptr(100), 0},
		{"Wartung: Status ja, Ausfall nein", []Interval{maint(at(20, 2, 0), at(20, 4, 0))}, start, "2026-10-20", "under_maintenance", ptr(100), 0},
		{"Wartung und Vorfall: Vorfall gewinnt, Wartung zählt nicht", []Interval{maint(at(20, 2, 0), at(20, 4, 0)), sev("major_outage", at(20, 3, 0), at(20, 5, 0))}, start, "2026-10-20", "major_outage", ptr(100 - 100*2.0/24), h(2)},
		{"Vorfall der Länge 0 färbt den Tag", []Interval{inc(at(20, 8, 0), at(20, 8, 0))}, start, "2026-10-20", "degraded_performance", ptr(100), 0},
		{"vor Datenbeginn: keine Daten", nil, at(20, 0, 0), "2026-10-19", "no_data", nil, 0},
		{"Datenbeginn mitten am Tag: nur ab dort beobachtet", []Interval{sev("major_outage", at(20, 12, 0), at(20, 18, 0))}, at(20, 12, 0), "2026-10-20", "major_outage", ptr(50), h(6)},
		{"Vorfall in der Zukunft ignoriert", []Interval{inc(at(30, 1, 0), at(30, 2, 0))}, start, "2026-10-29", "operational", ptr(100), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := find(t, Compute(tc.intervals, tc.dataStart, now, 90), tc.date)
			assert.Equal(t, tc.wantStatus, d.Status)
			assert.Equal(t, tc.wantDown, d.DowntimeSeconds)
			if tc.wantPct == nil {
				assert.Nil(t, d.Availability)
				assert.Zero(t, d.ObservedSeconds)
				return
			}
			near(t, *tc.wantPct, d.Availability)
		})
	}
}

// Dauer je Schweregrad (Tooltip): ungewichtet, schlimmster zuerst, Überlappung
// zählt nur beim schwereren Status, Wartung fehlt.
func TestCompute_DauerJeSchweregrad(t *testing.T) {
	d := find(t, Compute([]Interval{
		inc(at(20, 0, 0), at(20, 10, 0)),
		sev("partial_outage", at(20, 4, 0), at(20, 8, 0)),
		sev("major_outage", at(20, 6, 0), at(20, 7, 0)),
		maint(at(20, 20, 0), at(20, 22, 0)),
	}, start, now, 90), "2026-10-20")
	assert.Equal(t, []Impairment{
		{Status: "major_outage", Seconds: 3600},
		{Status: "partial_outage", Seconds: 3 * 3600},
		{Status: "degraded_performance", Seconds: 6 * 3600},
	}, d.Impairments)
	assert.Equal(t, "major_outage", d.Status)
	assert.Empty(t, find(t, Compute(nil, start, now, 90), "2026-10-20").Impairments)
}

func TestMerge_DurchschnittDerKomponenten(t *testing.T) {
	a := Compute([]Interval{sev("major_outage", at(28, 0, 0), at(28, 12, 0))}, at(28, 0, 0), now, 2)
	b := Compute(nil, at(28, 0, 0), now, 2)
	page := Compute([]Interval{sev("major_outage", at(28, 0, 0), at(28, 12, 0))}, at(28, 0, 0), now, 2)
	got := Merge(page, [][]Day{a, b})
	near(t, 75, got[0].Availability) // (50 + 100) / 2
	assert.EqualValues(t, 6*3600, got[0].DowntimeSeconds)
	assert.Equal(t, "major_outage", got[0].Status, "Status bleibt der schlimmste des Tages")
	assert.Equal(t, []Impairment{{Status: "major_outage", Seconds: 12 * 3600}}, got[0].Impairments)
	near(t, 100, got[1].Availability)
	// Summary über den Durchschnitt: 6 h gewichtete Ausfallzeit von 36 h beobachtet.
	near(t, 100*30.0/36, Summary(got))
	// Komponenten ohne Daten gehen nicht ein; ohne jede Komponente bleibt die Seite.
	noData := Compute(nil, now.Add(time.Hour), now, 2)
	assert.NotNil(t, Merge(page, [][]Day{noData})[0].Availability, "kein Komponentenwert: Seitenwert bleibt")
	assert.Equal(t, page, Merge(page, nil))
}

func TestCompute_KeineDatenStattHundertProzent(t *testing.T) {
	days := Compute(nil, now.Add(time.Hour), now, 3) // Beobachtung beginnt erst nach jetzt
	for _, d := range days {
		assert.Equal(t, NoData, d.Status)
		assert.Nil(t, d.Availability)
	}
	assert.Nil(t, Summary(days))
}

func TestSummary_GewichtetNachDauer(t *testing.T) {
	// Gestern 24 h beobachtet, 12 h Ausfall; heute 12 h ohne Ausfall:
	// 36 h beobachtet, 12 h down = 66,67 %, nicht der Mittelwert 75 %.
	days := Compute([]Interval{sev("major_outage", at(28, 0, 0), at(28, 12, 0))}, at(28, 0, 0), now, 2)
	near(t, 100*24.0/36, Summary(days))
}

func TestSweep_UeberlappungenUndGrenzen(t *testing.T) {
	by, w := sweep(nil)
	assert.Empty(t, by)
	assert.Zero(t, w)
	// ineinander liegend, aneinander grenzend, getrennt: 12 h degraded
	by, w = sweep([]Interval{
		inc(at(1, 0, 0), at(1, 10, 0)),
		inc(at(1, 2, 0), at(1, 3, 0)),
		inc(at(1, 10, 0), at(1, 11, 0)),
		inc(at(1, 20, 0), at(1, 21, 0)),
	})
	assert.Equal(t, 12*time.Hour, by["degraded_performance"])
	assert.InDelta(t, 0.3*12*3600, w, 1e-6)
}
