package handlers_test

import (
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nachgetragene Historie: Ein Vorfall vor Anlage der Seite
// zieht den Datenbeginn vor; Wartung ohne Zeiten bleibt unberücksichtigt.
func TestAvailability_NachgetragenerVorfallVorSeitenanlage(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("historie", false)
	require.NoError(t, e.db.Model(&p).Update("created_at", *utc(27, 0)).Error)
	e.incident(p.ID, "resolved", *utc(20, 0), utc(20, 6))
	bad := models.Incident{StatusPageID: p.ID, Title: "W", Message: "m", Status: "scheduled", IsMaintenance: true, ScheduledEndAt: utc(28, 6)}
	require.NoError(t, e.db.Create(&bad).Error)

	_, resp := e.get("/api/public/pages/historie/availability")
	assert.Equal(t, "no_data", day(t, resp.Days, "2026-10-19").Status)
	assert.Equal(t, "degraded_performance", day(t, resp.Days, "2026-10-20").Status)
	assert.Equal(t, "operational", day(t, resp.Days, "2026-10-22").Status, "ab dem ersten Vorfall beobachtet")
	assert.Equal(t, "operational", day(t, resp.Days, "2026-10-28").Status, "Wartung ohne Beginn ignoriert")
}

// Dauerflattern über der Obergrenze: Antwort bleibt 200, neueste
// Wechsel zählen, die Antwort weist auf die Kürzung hin.
func TestAvailability_DauerflatternWirdGekuerztUndGemeldet(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("flatter", false)
	c := e.comp(p.ID, "API", "operational", nil)
	n := 10000 + 100
	rows := make([]models.ComponentStatusChange, 0, n)
	for i := 0; i < n; i++ {
		st := "operational"
		if i%2 == 0 {
			st = "major_outage"
		}
		rows = append(rows, models.ComponentStatusChange{ComponentID: c.ID, NewStatus: st, Source: "webhook", ChangedAt: e.now.Add(-time.Duration(n-i) * time.Minute)})
	}
	require.NoError(t, e.db.CreateInBatches(&rows, 1000).Error)

	w, _ := e.get("/api/public/pages/flatter/availability")
	require.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), `"history_truncated":true`)

	// Ruhige Seite: kein Hinweis.
	q := e.page("ruhig", false)
	e.comp(q.ID, "API", "operational", nil)
	w, _ = e.get("/api/public/pages/ruhig/availability")
	assert.NotContains(t, w.Body.String(), "history_truncated")
}
