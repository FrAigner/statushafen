package database

import (
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// indexNames returns the names of the explicit SQLite indexes of a table.
func indexNames(t *testing.T, db *gorm.DB, table string) []string {
	t.Helper()
	var names []string
	require.NoError(t, db.Raw(`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND name NOT LIKE 'idx_%_deleted_at' AND name NOT LIKE 'idx_tags_status_page_id' AND sql IS NOT NULL ORDER BY name`, table).Scan(&names).Error)
	return names
}

// TestCreateSoftDeleteUniqueIndexes_ReplacesLegacyIndexes:
// the old, non-partial indexes (state before the fix) are removed and
// replaced by partial ones; afterwards deleted names and slugs are free again.
func TestCreateSoftDeleteUniqueIndexes_ReplacesLegacyIndexes(t *testing.T) {
	db := setupTagMigrationTestDB(t)
	// Recreate the state before the changeover: slug via gorm uniqueIndex, tags without deleted_at.
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_status_pages_slug ON status_pages (slug)`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_tags_global_name ON tags (LOWER(name)) WHERE status_page_id IS NULL`).Error)
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_tags_scoped_name ON tags (LOWER(name), status_page_id) WHERE status_page_id IS NOT NULL`).Error)

	CreateSoftDeleteUniqueIndexes(db)
	CreateSoftDeleteUniqueIndexes(db) // second run: no-op

	assert.Equal(t, []string{"idx_status_pages_custom_domain_live", "idx_status_pages_slug_active", "idx_status_pages_system_active"}, indexNames(t, db, "status_pages"))
	assert.Equal(t, []string{"idx_tags_global_name_active", "idx_tags_scoped_name_active"}, indexNames(t, db, "tags"))

	page := models.StatusPage{Name: "Statusseite", Slug: "statusseite"}
	require.NoError(t, db.Create(&page).Error)
	assert.Error(t, db.Create(&models.StatusPage{Name: "Dup", Slug: "statusseite"}).Error, "aktiver Slug muss eindeutig bleiben")
	require.NoError(t, db.Delete(&page).Error)
	require.NoError(t, db.Create(&models.StatusPage{Name: "Neu", Slug: "statusseite"}).Error, "gelöschter Slug muss wieder frei sein")

	other := models.StatusPage{Name: "Acme", Slug: "acme"}
	require.NoError(t, db.Create(&other).Error)
	for _, scope := range []*uint{nil, &other.ID} {
		tag := models.Tag{Name: "UI-Test", StatusPageID: scope}
		require.NoError(t, db.Create(&tag).Error)
		assert.Error(t, db.Create(&models.Tag{Name: "ui-test", StatusPageID: scope}).Error, "aktiver Tag-Name muss eindeutig bleiben")
		require.NoError(t, db.Delete(&tag).Error)
		require.NoError(t, db.Create(&models.Tag{Name: "UI-Test", StatusPageID: scope}).Error, "gelöschter Tag-Name muss wieder frei sein")
	}
}

// TestSeedSystemStatusPage_SoftDeletedPageSurvives: a
// soft-deleted system page must not abort the start (formerly: fatal on the
// slug index) and is not silently re-created either.
func TestSeedSystemStatusPage_SoftDeletedPageSurvives(t *testing.T) {
	db := setupTagMigrationTestDB(t)
	CreateSoftDeleteUniqueIndexes(db)
	SeedSystemStatusPage(db)
	require.NoError(t, db.Where("slug = ?", SystemStatusPageSlug).Delete(&models.StatusPage{}).Error)

	SeedSystemStatusPage(db)

	var active, all int64
	require.NoError(t, db.Model(&models.StatusPage{}).Where("slug = ?", SystemStatusPageSlug).Count(&active).Error)
	require.NoError(t, db.Unscoped().Model(&models.StatusPage{}).Where("slug = ?", SystemStatusPageSlug).Count(&all).Error)
	assert.EqualValues(t, 0, active, "die Admin-Entscheidung (gelöscht) muss bestehen bleiben")
	assert.EqualValues(t, 1, all, "es darf keine zweite Systemseite entstehen")
}
