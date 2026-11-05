package availability_test

import (
	"testing"
	"time"

	"github.com/fraigner/statushafen/availability"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Review: Webhook-Flattern erzeugt Zehntausende Historieneinträge.
// Die Berechnung muss dafür (fast) linear bleiben, die Verfügbarkeit ist
// öffentlich. Vorher: 86.400 Einträge über 3 Tage = ca. 6,5 s CPU je Komponente.
func TestCompute_FlatternBleibtSchnell(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	start := now.Add(-3 * 24 * time.Hour)
	var changes []availability.StatusChange
	statuses := []string{"major_outage", "operational"}
	for i := 0; i < 86400; i++ { // eine Sekunde Abstand, abwechselnd
		changes = append(changes, availability.StatusChange{At: start.Add(time.Duration(i) * 3 * time.Second), Status: statuses[i%2]})
	}
	began := time.Now()
	ivs := availability.FromChanges(changes, now)
	days := availability.Compute(ivs, start.Add(-time.Hour), now, 90)
	elapsed := time.Since(began)
	require.Len(t, days, 90)
	assert.Less(t, elapsed, 1500*time.Millisecond, "86.400 Wechsel dürfen die Berechnung nicht lahmlegen")
	t.Logf("86.400 Wechsel: %v", elapsed)
	// Halb der Zeit ein Ausfall: grob 50 % an den Flatter-Tagen.
	d := days[89-1]
	require.NotNil(t, d.Availability)
	assert.InDelta(t, 50, *d.Availability, 1)
}
