package availability_test

import (
	"testing"
	"time"

	"github.com/fraigner/statushafen/availability"

	"github.com/stretchr/testify/assert"
)

// Statusverlauf einer Komponente -> Intervalle.
func TestFromChanges(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	h := func(n int) time.Time { return now.Add(time.Duration(-n) * time.Hour) }

	t.Run("Status gilt bis zum nächsten Eintrag, der letzte bis jetzt", func(t *testing.T) {
		got := availability.FromChanges([]availability.StatusChange{
			{At: h(10), Status: "major_outage"},
			{At: h(8), Status: "operational"},
			{At: h(3), Status: "degraded_performance"},
		}, now)
		assert.Equal(t, []availability.Interval{
			{Start: h(10), End: h(8), Status: "major_outage"},
			{Start: h(3), End: now, Status: "degraded_performance"},
		}, got)
	})
	t.Run("Wartung zählt als Intervall, Betrieb und Unbekanntes nicht", func(t *testing.T) {
		got := availability.FromChanges([]availability.StatusChange{
			{At: h(5), Status: "under_maintenance"},
			{At: h(4), Status: "group"},
			{At: h(3), Status: "operational"},
		}, now)
		assert.Equal(t, []availability.Interval{{Start: h(5), End: h(4), Status: "under_maintenance"}}, got)
	})
	t.Run("Eintrag in der Zukunft zählt nicht und kappt den Vorgänger auf jetzt", func(t *testing.T) {
		got := availability.FromChanges([]availability.StatusChange{
			{At: h(2), Status: "partial_outage"},
			{At: now.Add(time.Hour), Status: "major_outage"},
		}, now)
		assert.Equal(t, []availability.Interval{{Start: h(2), End: now, Status: "partial_outage"}}, got)
	})
	t.Run("leer", func(t *testing.T) {
		assert.Empty(t, availability.FromChanges(nil, now))
	})
	t.Run("gleiche Zeit: Länge 0 ergibt kein Intervall", func(t *testing.T) {
		got := availability.FromChanges([]availability.StatusChange{
			{At: h(2), Status: "major_outage"},
			{At: h(2), Status: "operational"},
		}, now)
		assert.Empty(t, got)
		// ... und färbt den Tag nicht: 100 % verfügbar, Status operational.
		days := availability.Compute(got, h(30), now, 1)
		assert.Equal(t, "operational", days[0].Status)
	})
	t.Run("Nachfolger liegt vor dem Beginn: kein Intervall, nie negativ", func(t *testing.T) {
		got := availability.FromChanges([]availability.StatusChange{
			{At: h(2), Status: "major_outage"},
			{At: h(5), Status: "operational"},
		}, now)
		assert.Empty(t, got)
	})
}
