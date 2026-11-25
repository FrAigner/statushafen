package database

import (
	"github.com/fraigner/statushafen/logging"

	"gorm.io/gorm"
)

// createUiOverrideUniqueIndexes creates the partial
// unique indexes for UiTranslationOverride. GORM's AutoMigrate cannot create
// partial indexes (see CreateSoftDeleteUniqueIndexes) and a
// simple uniqueIndex tag on (status_page_id, language_code, key) would not
// work as desired with NULL status_page_id (global overrides),
// since SQL treats multiple NULLs in unique indexes as distinct
// - two separate partial indexes solve this cleanly and (unlike a normal unique index)
// remain soft-delete capable, i.e. a
// deleted and re-created override for the same key does not collide
// with the previously deleted row. Idempotent via IF NOT EXISTS.
func createUiOverrideUniqueIndexes(db *gorm.DB) {
	statements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ui_override_page ON ui_translation_overrides (status_page_id, language_code, key) WHERE deleted_at IS NULL AND status_page_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ui_override_global ON ui_translation_overrides (language_code, key) WHERE deleted_at IS NULL AND status_page_id IS NULL`,
	}
	for _, sql := range statements {
		if err := db.Exec(sql).Error; err != nil {
			logging.Fatal("Failed to create ui_translation_overrides unique index", "error", err)
		}
	}
}
