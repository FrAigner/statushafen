package handlers

import (
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/statushistory"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The maintenance scheduler writes exactly one entry per transition.
func TestMaintenanceScheduler_SchreibtStatushistorie(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	comp, inc := seedTriggerTestFixtures(db, "hist-scheduler")

	h.runMaintenanceTick()
	h.runMaintenanceTick() // second tick: nothing left to do
	var hist []models.ComponentStatusChange
	require.NoError(t, db.Where("component_id = ?", comp.ID).Order("id").Find(&hist).Error)
	require.Len(t, hist, 1)
	assert.Equal(t, "operational", hist[0].OldStatus)
	assert.Equal(t, "under_maintenance", hist[0].NewStatus)
	assert.Equal(t, models.StatusSourceMaintenance, hist[0].Source)

	// End is due: back to operational, one more entry.
	past := time.Now().Add(-time.Minute)
	require.NoError(t, db.Model(&models.Incident{}).Where("id = ?", inc.ID).Update("scheduled_end_at", past).Error)
	h.runMaintenanceTick()
	require.NoError(t, db.Where("component_id = ?", comp.ID).Order("id").Find(&hist).Error)
	require.Len(t, hist, 2)
	assert.Equal(t, "under_maintenance", hist[1].OldStatus)
	assert.Equal(t, "operational", hist[1].NewStatus)
}

// If the history entry fails, the transition aborts and the status remains.
func TestMaintenanceScheduler_HistorienfehlerBrichtUebergangAb(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	comp, inc := seedTriggerTestFixtures(db, "hist-scheduler-fehler")
	require.NoError(t, db.Migrator().DropTable(&models.ComponentStatusChange{}))

	var reloaded models.Incident
	db.Preload(preloadIncidentComponentsComponent).First(&reloaded, inc.ID)
	reloaded.Status = "in_progress"
	assert.False(t, h.applyMaintenanceStatusTransition(&reloaded, "under_maintenance"))
	requireComponentStatus(t, db, comp.ID, "operational")
}

func TestPruneStatusHistory_EinmalAmTagUndMitFehlerlog(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	c := models.Component{StatusPageID: 1, Name: "x", Status: "operational"}
	require.NoError(t, db.Create(&c).Error)
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	alt := now.Add(-statushistory.Retention - 48*time.Hour)
	for i, st := range []string{"major_outage", "operational"} {
		require.NoError(t, db.Create(&models.ComponentStatusChange{ComponentID: c.ID, NewStatus: st, ChangedAt: alt.Add(time.Duration(i) * time.Hour)}).Error)
	}

	h.pruneStatusHistory(now)
	var n int64
	db.Model(&models.ComponentStatusChange{}).Count(&n)
	assert.EqualValues(t, 1, n, "der ältere Eintrag fiel weg, der Ausgangsstatus blieb")

	// Within 24 h nothing runs again (the timestamp holds the lock).
	require.NoError(t, db.Create(&models.ComponentStatusChange{ComponentID: c.ID, NewStatus: "major_outage", ChangedAt: alt.Add(-time.Hour)}).Error)
	h.pruneStatusHistory(now.Add(time.Hour))
	db.Model(&models.ComponentStatusChange{}).Count(&n)
	assert.EqualValues(t, 2, n)
	h.pruneStatusHistory(now.Add(25 * time.Hour))
	db.Model(&models.ComponentStatusChange{}).Count(&n)
	assert.EqualValues(t, 1, n)

	// Error: is logged, the tick continues.
	require.NoError(t, db.Migrator().DropTable(&models.ComponentStatusChange{}))
	assert.NotPanics(t, func() { h.pruneStatusHistory(now.Add(60 * time.Hour)) })
}

// Error paths of setComponentStatus: if the lock query
// or the save fails, the status stays unchanged, also in the passed object.
func TestSetComponentStatus_FehlerpfadeLassenStatusUnveraendert(t *testing.T) {
	db := testutils.SetupTestDB()
	comp := models.Component{StatusPageID: 1, Name: "x", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)

	t.Run("Speichern scheitert", func(t *testing.T) {
		withBlockedTableWrites(t, db, "components", func() {
			c := comp
			require.Error(t, setComponentStatus(db, &c, "major_outage", models.StatusSourceManual))
			assert.Equal(t, "operational", c.Status, "Objekt wird zurückgesetzt")
		})
		requireComponentStatus(t, db, comp.ID, "operational")
		var n int64
		db.Model(&models.ComponentStatusChange{}).Count(&n)
		assert.Zero(t, n)
	})

	t.Run("Sperr-Abfrage scheitert", func(t *testing.T) {
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail_lock", func(tx *gorm.DB) {
			if _, ok := tx.Statement.Clauses["FOR"]; ok {
				_ = tx.AddError(gorm.ErrInvalidDB)
			}
		}))
		defer func() { _ = db.Callback().Query().Remove("test:fail_lock") }()
		c := comp
		require.Error(t, setComponentStatus(db, &c, "major_outage", models.StatusSourceManual))
		requireComponentStatus(t, db, comp.ID, "operational")
	})
}

// Public availability loads at most
// maxHistoryRowsPerComponent rows per component (the newest) and flags this.
func TestStatusHistoryIntervals_ObergrenzeJeKomponente(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "P", Slug: "p-781"}
	require.NoError(t, db.Create(&page).Error)
	flatter := models.Component{StatusPageID: page.ID, Name: "Flatter", Status: "operational"}
	ruhig := models.Component{StatusPageID: page.ID, Name: "Ruhig", Status: "operational"}
	require.NoError(t, db.Create(&flatter).Error)
	require.NoError(t, db.Create(&ruhig).Error)

	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	start := now.Add(-80 * 24 * time.Hour)
	n := maxHistoryRowsPerComponent + 500
	batch := make([]models.ComponentStatusChange, 0, n+2)
	for i := 0; i < n; i++ {
		st := "major_outage"
		if i%2 == 1 {
			st = "operational"
		}
		batch = append(batch, models.ComponentStatusChange{ComponentID: flatter.ID, NewStatus: st, Source: "webhook", ChangedAt: start.Add(time.Duration(i) * time.Minute)})
	}
	batch = append(batch,
		models.ComponentStatusChange{ComponentID: ruhig.ID, NewStatus: "major_outage", ChangedAt: now.Add(-3 * time.Hour)},
		models.ComponentStatusChange{ComponentID: ruhig.ID, OldStatus: "major_outage", NewStatus: "operational", ChangedAt: now.Add(-2 * time.Hour)})
	require.NoError(t, db.CreateInBatches(&batch, 1000).Error)

	hist, truncated, err := h.statusHistoryIntervals([]models.Component{flatter, ruhig}, now.Add(-90*24*time.Hour), now)
	require.NoError(t, err)
	assert.Equal(t, []uint{flatter.ID}, truncated)
	// Every second entry is an outage: at most half of the upper limit.
	assert.LessOrEqual(t, len(hist[flatter.ID]), maxHistoryRowsPerComponent/2+1)
	assert.Greater(t, len(hist[flatter.ID]), maxHistoryRowsPerComponent/2-2, "die neuesten Zeilen bleiben erhalten")
	// The quiet component is complete and unchanged.
	require.Len(t, hist[ruhig.ID], 1)
	assert.Equal(t, time.Hour, hist[ruhig.ID][0].End.Sub(hist[ruhig.ID][0].Start))
}
