package componentstatus

import (
	"path/filepath"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Rangfolge vollständig, inkl. der Einordnung von Wartung und unknown.
func TestSeverityOrder(t *testing.T) {
	order := []string{Unknown, Operational, UnderMaintenance, DegradedPerformance, PartialOutage, MajorOutage}
	for i := 1; i < len(order); i++ {
		assert.Greater(t, Severity(order[i]), Severity(order[i-1]), "%s muss schlimmer sein als %s", order[i], order[i-1])
	}
	assert.Equal(t, 0, Severity(Group))
	assert.Equal(t, 0, Severity(Unknown))
	assert.Equal(t, 0, Severity("irgendwas"))
	assert.Equal(t, 1, Severity(Operational))
	assert.Equal(t, 5, Severity(MajorOutage))
}

func TestWorst(t *testing.T) {
	assert.Equal(t, Operational, Worst())
	assert.Equal(t, Operational, Worst(Unknown, Group))
	assert.Equal(t, UnderMaintenance, Worst(Operational, UnderMaintenance))
	assert.Equal(t, DegradedPerformance, Worst(UnderMaintenance, DegradedPerformance))
	assert.Equal(t, MajorOutage, Worst(MajorOutage, PartialOutage, Operational))
	// Gleichstand: der erste gewinnt (stabil).
	assert.Equal(t, PartialOutage, Worst(PartialOutage, PartialOutage))
}

func TestOwnAndFromOpenIncident(t *testing.T) {
	assert.Equal(t, Operational, FromOpenIncident(""))
	assert.Equal(t, DegradedPerformance, FromOpenIncident(DegradedPerformance))
	assert.Equal(t, PartialOutage, FromOpenIncident(PartialOutage))
	assert.Equal(t, MajorOutage, FromOpenIncident(MajorOutage))
	assert.Equal(t, DegradedPerformance, FromOpenIncident("kaputt"), "unbekannt = vorsichtig degraded")
	assert.Equal(t, DegradedPerformance, FromIncidentSeverity(""), "Altdaten")
	assert.Equal(t, PartialOutage, FromIncidentSeverity(PartialOutage))

	assert.Equal(t, PartialOutage, Own(Operational, PartialOutage))
	assert.Equal(t, MajorOutage, Own(UnderMaintenance, MajorOutage))
	assert.Equal(t, MajorOutage, Own(MajorOutage, DegradedPerformance), "schlechterer manueller Status bleibt")
	assert.Equal(t, MajorOutage, Own(PartialOutage, MajorOutage))
	assert.Equal(t, DegradedPerformance, Own(UnderMaintenance, DegradedPerformance), "bisherige Rangfolge")
	assert.Equal(t, UnderMaintenance, Own(UnderMaintenance, ""))
	assert.Equal(t, PartialOutage, Own(PartialOutage, ""))
	assert.Equal(t, Operational, Own(Operational, ""))
}

func TestOpenIncidentComponentIDs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cs.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.StatusPage{}, &models.Component{}, &models.Incident{}, &models.IncidentComponent{}))
	page := models.StatusPage{Name: "P", Slug: "p"}
	require.NoError(t, db.Create(&page).Error)
	a := models.Component{StatusPageID: page.ID, Name: "A"}
	b := models.Component{StatusPageID: page.ID, Name: "B"}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Create(&b).Error)
	open := models.Incident{StatusPageID: page.ID, Title: "t", Message: "m", Status: "investigating", Severity: PartialOutage}
	resolved := models.Incident{StatusPageID: page.ID, Title: "t", Message: "m", Status: "resolved"}
	require.NoError(t, db.Create(&open).Error)
	require.NoError(t, db.Create(&resolved).Error)
	require.NoError(t, db.Create(&models.IncidentComponent{IncidentID: open.ID, ComponentID: a.ID}).Error)
	require.NoError(t, db.Create(&models.IncidentComponent{IncidentID: resolved.ID, ComponentID: b.ID}).Error)

	assert.Equal(t, map[uint]string{a.ID: PartialOutage}, OpenIncidentComponentIDs(db, page.ID))

	// Mehrere offene Vorfälle: der schlimmste Schweregrad gewinnt.
	worse := models.Incident{StatusPageID: page.ID, Title: "t", Message: "m", Status: "monitoring", Severity: MajorOutage}
	milder := models.Incident{StatusPageID: page.ID, Title: "t", Message: "m", Status: "identified", Severity: DegradedPerformance}
	require.NoError(t, db.Create(&worse).Error)
	require.NoError(t, db.Create(&milder).Error)
	require.NoError(t, db.Create(&models.IncidentComponent{IncidentID: worse.ID, ComponentID: a.ID}).Error)
	require.NoError(t, db.Create(&models.IncidentComponent{IncidentID: milder.ID, ComponentID: a.ID}).Error)
	require.NoError(t, db.Create(&models.IncidentComponent{IncidentID: milder.ID, ComponentID: b.ID}).Error)
	assert.Equal(t, map[uint]string{a.ID: MajorOutage, b.ID: DegradedPerformance}, OpenIncidentComponentIDs(db, page.ID))
	assert.Empty(t, OpenIncidentComponentIDs(db, page.ID+1))
}
