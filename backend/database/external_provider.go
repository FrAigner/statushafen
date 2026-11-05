package database

import (
	"log/slog"

	"gorm.io/gorm"
)

// legacyExternalProviderStatuspage ist der frühere Anbieter-Schlüssel der
// externen Statusquellen. Er heißt jetzt "summaryv2" (Summary-API v2).
const legacyExternalProviderStatuspage = "statuspage"

// migrateExternalSourceProvider stellt Quellen mit dem früheren Schlüssel
// "statuspage" auf "summaryv2" um. Idempotent: nach dem ersten Lauf gibt es
// keine Zeile mit dem Altwert mehr. Gelöschte Zeilen (Unscoped) werden mit
// umgestellt, damit der Altwert nirgends überlebt; UpdateColumn lässt
// updated_at unberührt. Ein Fehler wird nur geloggt, der Start soll nicht daran
// scheitern.
func migrateExternalSourceProvider(db *gorm.DB) {
	res := db.Unscoped().Table("external_sources").
		Where("provider = ?", legacyExternalProviderStatuspage).
		UpdateColumn("provider", "summaryv2")
	if res.Error != nil {
		slog.Error("Failed to migrate external source provider", "error", res.Error)
		return
	}
	if res.RowsAffected > 0 {
		slog.Info("External source provider migrated", "rows", res.RowsAffected)
	}
}
