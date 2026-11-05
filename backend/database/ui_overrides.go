package database

import (
	"github.com/fraigner/statushafen/logging"

	"gorm.io/gorm"
)

// createUiOverrideUniqueIndexes legt die partiellen
// Unique-Indizes für UiTranslationOverride an. GORMs AutoMigrate kann keine
// partiellen Indizes erzeugen (siehe CreateSoftDeleteUniqueIndexes) und ein
// einfacher uniqueIndex-Tag auf (status_page_id, language_code, key) würde
// bei NULL-status_page_id (globale Overrides) nicht wie gewünscht
// funktionieren, da SQL mehrere NULLs in Unique-Indizes als verschieden
// behandelt - zwei getrennte partielle Indizes lösen das sauber und bleiben
// dabei (anders als ein normaler Unique-Index) Soft-Delete-fähig, d.h. ein
// gelöschter und neu angelegter Override für denselben Key kollidiert nicht
// mit der zuvor gelöschten Zeile. Idempotent per IF NOT EXISTS.
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
