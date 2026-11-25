package database

import (
	"log/slog"

	"gorm.io/gorm"
)

// legacyExternalProviderStatuspage is the former provider key of the
// external status sources. It is now called "summaryv2" (Summary API v2).
const legacyExternalProviderStatuspage = "statuspage"

// migrateExternalSourceProvider switches sources with the former key
// "statuspage" to "summaryv2". Idempotent: after the first run there is
// no row with the legacy value anymore. Deleted rows (Unscoped) are
// switched too, so the legacy value survives nowhere; UpdateColumn leaves
// updated_at untouched. An error is only logged, startup is not supposed to fail
// because of it.
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
