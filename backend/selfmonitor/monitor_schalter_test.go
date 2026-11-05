package selfmonitor

import (
	"errors"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
)

// C: Ein Lesefehler beim Schalter ist nicht "aus": Status, Marker
// und Vorfall bleiben unverändert, auch der Zähler läuft nicht weiter.
func TestMonitor_SchalterLesefehlerGibtNichtsFrei(t *testing.T) {
	f := newFixture(t)
	offeneStoerung(t, f)
	f.m.Enabled = func() (bool, error) { return false, errors.New("db wackelt") }
	f.run(3)
	assert.Equal(t, "major_outage", f.status(t))
	assert.EqualValues(t, 1, f.count(t, &models.Incident{}, "status <> ?", "resolved"))
	assert.EqualValues(t, 2, f.count(t, &models.SystemSetting{}, "key LIKE ?", "selfmonitor.%"))

	f.m.Enabled = func() (bool, error) { return false, nil }
	f.run(1)
	assert.Equal(t, "operational", f.status(t), "ausdrücklich aus gibt weiterhin frei")
}
