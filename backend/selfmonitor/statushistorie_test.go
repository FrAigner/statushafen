package selfmonitor

import (
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (f *fixture) history(t *testing.T) []models.ComponentStatusChange {
	t.Helper()
	var h []models.ComponentStatusChange
	require.NoError(t, f.db.Where("component_id = ?", f.comp.ID).Order("id").Find(&h).Error)
	return h
}

// Die Selbstüberwachung setzt den Status über die Historie: genau
// ein Eintrag beim Kippen und einer bei der Erholung, Quelle selfmonitor.
func TestMonitor_SchreibtStatushistorieBeiStoerungUndErholung(t *testing.T) {
	f := newFixture(t)
	f.fail()
	f.run(3)
	require.Equal(t, "major_outage", f.status(t))
	f.run(2) // weitere Fehlschläge ändern nichts
	h := f.history(t)
	require.Len(t, h, 1)
	assert.Equal(t, "operational", h[0].OldStatus)
	assert.Equal(t, "major_outage", h[0].NewStatus)
	assert.Equal(t, models.StatusSourceSelfMonitor, h[0].Source)
	assert.True(t, f.now.Equal(h[0].ChangedAt), "Zeit der Monitor-Uhr")

	f.res = &Result{OK: true}
	f.run(3)
	require.Equal(t, "operational", f.status(t))
	h = f.history(t)
	require.Len(t, h, 2)
	assert.Equal(t, "major_outage", h[1].OldStatus)
	assert.Equal(t, "operational", h[1].NewStatus)
}

// Setzt der Admin zwischenzeitlich einen anderen Status, schreibt der Monitor
// weder Status noch Historie (bedingtes Update).
func TestMonitor_FremderStatusSchreibtKeineHistorie(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.db.Model(&f.comp).Update("status", "under_maintenance").Error)
	f.fail()
	f.run(4)
	assert.Equal(t, "under_maintenance", f.status(t))
	assert.Empty(t, f.history(t))
}

// Verschobene Komponente: das Zurücksetzen durch den Monitor ist protokolliert.
func TestMonitor_VerschobeneKomponenteSchreibtHistorieBeimZuruecksetzen(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	verschiebe(t, f)
	f.run(1)
	require.Equal(t, "operational", f.status(t))
	h := f.history(t)
	require.Len(t, h, 2)
	assert.Equal(t, "operational", h[1].NewStatus)
	assert.Equal(t, models.StatusSourceSelfMonitor, h[1].Source)
}
