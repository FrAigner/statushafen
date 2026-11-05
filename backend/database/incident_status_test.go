package database

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Der Altwert "completed" wird auf "resolved" umgestellt.

func newIncidentStatusDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "status.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.StatusPage{}, &models.Incident{}, &models.IncidentUpdate{}))
	return db
}

func TestMigrateCompletedIncidentStatus(t *testing.T) {
	db := newIncidentStatusDB(t)
	page := models.StatusPage{Name: "P", Slug: "p"}
	require.NoError(t, db.Create(&page).Error)
	geaendert := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	fruehere := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

	mk := func(status string, maintenance bool, resolved *time.Time) models.Incident {
		inc := models.Incident{StatusPageID: page.ID, Title: status, Message: "m", Status: status, IsMaintenance: maintenance, ResolvedAt: resolved}
		require.NoError(t, db.Create(&inc).Error)
		require.NoError(t, db.Model(&inc).UpdateColumn("updated_at", geaendert).Error)
		return inc
	}
	wartung := mk("completed", true, nil)
	vorfall := mk("completed", false, &fruehere)
	offen := mk("monitoring", false, nil)
	behoben := mk("resolved", false, nil)
	geloescht := mk("completed", false, nil)
	require.NoError(t, db.Delete(&geloescht).Error)
	require.NoError(t, db.Create(&models.IncidentUpdate{IncidentID: wartung.ID, Status: "completed", Message: "fertig"}).Error)
	require.NoError(t, db.Create(&models.IncidentUpdate{IncidentID: offen.ID, Status: "monitoring", Message: "läuft"}).Error)

	migrateCompletedIncidentStatus(db)

	get := func(id uint) models.Incident {
		var inc models.Incident
		require.NoError(t, db.Unscoped().First(&inc, id).Error)
		return inc
	}
	w := get(wartung.ID)
	assert.Equal(t, "resolved", w.Status)
	require.NotNil(t, w.ResolvedAt, "ResolvedAt wird gefüllt")
	assert.True(t, w.ResolvedAt.Equal(geaendert), "mit der letzten Änderung: %v", w.ResolvedAt)
	assert.True(t, w.UpdatedAt.Equal(geaendert), "updated_at bleibt unberührt")
	assert.True(t, w.IsMaintenance)

	v := get(vorfall.ID)
	assert.Equal(t, "resolved", v.Status)
	assert.True(t, v.ResolvedAt.Equal(fruehere), "vorhandenes ResolvedAt bleibt")

	assert.Equal(t, "resolved", get(geloescht.ID).Status, "auch gelöschte Zeilen verlieren den Altwert")
	assert.Equal(t, "monitoring", get(offen.ID).Status)
	assert.Nil(t, get(offen.ID).ResolvedAt, "andere Vorfälle bleiben unverändert")
	assert.Equal(t, "resolved", get(behoben.ID).Status)
	assert.Nil(t, get(behoben.ID).ResolvedAt)

	var updStatus []string
	require.NoError(t, db.Model(&models.IncidentUpdate{}).Order("id").Pluck("status", &updStatus).Error)
	assert.Equal(t, []string{"resolved", "monitoring"}, updStatus)

	// Idempotent: zweiter Lauf ändert nichts.
	var before, after []models.Incident
	require.NoError(t, db.Unscoped().Order("id").Find(&before).Error)
	migrateCompletedIncidentStatus(db)
	require.NoError(t, db.Unscoped().Order("id").Find(&after).Error)
	assert.Equal(t, before, after)
}

func TestMigrateCompletedIncidentStatus_FehlerSindNichtFatal(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "leer.db")), &gorm.Config{})
	require.NoError(t, err)
	out := captureLog(t)
	migrateCompletedIncidentStatus(db)
	assert.Contains(t, out.String(), "Failed to migrate legacy incident status")

	// Nur die Updates-Tabelle fehlt.
	db = newIncidentStatusDB(t)
	require.NoError(t, db.Migrator().DropTable(&models.IncidentUpdate{}))
	out = captureLog(t)
	migrateCompletedIncidentStatus(db)
	assert.Contains(t, out.String(), "Failed to migrate legacy incident update status")
}
