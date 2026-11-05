package database

import (
	"path/filepath"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupExternalProviderTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "provider.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.StatusPage{}, &models.ExternalSource{}))
	return db
}

// Der Anbieter-Schlüssel hieß früher "statuspage" und heißt jetzt "summaryv2".
// Bestehende Quellen werden beim Start umgestellt (auch soft-gelöschte, damit der
// Altwert nirgends überlebt); ein zweiter Lauf ändert nichts.
func TestMigrateExternalSourceProvider(t *testing.T) {
	db := setupExternalProviderTestDB(t)
	page := models.StatusPage{Name: "P", Slug: "p"}
	require.NoError(t, db.Create(&page).Error)

	alt := models.ExternalSource{StatusPageID: page.ID, Name: "Alt", BaseURL: "https://alt.example.com", Provider: "statuspage"}
	neu := models.ExternalSource{StatusPageID: page.ID, Name: "Neu", BaseURL: "https://neu.example.com", Provider: "summaryv2"}
	geloescht := models.ExternalSource{StatusPageID: page.ID, Name: "Weg", BaseURL: "https://weg.example.com", Provider: "statuspage"}
	require.NoError(t, db.Create(&alt).Error)
	require.NoError(t, db.Create(&neu).Error)
	require.NoError(t, db.Create(&geloescht).Error)
	require.NoError(t, db.Delete(&geloescht).Error)

	migrateExternalSourceProvider(db)

	var rows []models.ExternalSource
	require.NoError(t, db.Unscoped().Order("id").Find(&rows).Error)
	require.Len(t, rows, 3)
	for _, r := range rows {
		assert.Equal(t, "summaryv2", r.Provider, r.Name)
	}

	// Idempotent: nichts mehr zu tun, nichts ändert sich.
	migrateExternalSourceProvider(db)
	var n int64
	require.NoError(t, db.Unscoped().Model(&models.ExternalSource{}).Where("provider = ?", "statuspage").Count(&n).Error)
	assert.Zero(t, n)
}

func TestMigrateExternalSourceProvider_FehlerWirdNurGeloggt(t *testing.T) {
	db := setupExternalProviderTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&models.ExternalSource{}))
	assert.NotPanics(t, func() { migrateExternalSourceProvider(db) })
}
