package database

import (
	"log/slog"

	"gorm.io/gorm"
)

// legacyIncidentStatusCompleted: legacy value from the time before the changeover.
// Maintenance updates could set it unchecked. The constant deliberately lives
// only here: models and rules no longer know it.
const legacyIncidentStatusCompleted = "completed"

// migrateCompletedIncidentStatus switches the legacy value "completed" to
// "resolved" (incidents, maintenances and their timeline updates) and fills
// ResolvedAt, if empty, with the last change of the incident (the
// time at which the legacy value was set). Idempotent: after the first
// run there is no row with the legacy value anymore, another run changes
// nothing. UpdateColumn(s) leaves updated_at untouched; deleted rows
// (Unscoped) are switched too, so the legacy value survives nowhere.
// An error is only logged, startup is not supposed to fail because of it.
func migrateCompletedIncidentStatus(db *gorm.DB) {
	res := db.Unscoped().Table("incidents").Where("status = ?", legacyIncidentStatusCompleted).
		UpdateColumns(map[string]any{
			"status":      "resolved",
			"resolved_at": gorm.Expr("COALESCE(resolved_at, updated_at)"),
		})
	if res.Error != nil {
		slog.Error("Failed to migrate legacy incident status", "error", res.Error)
		return
	}
	upd := db.Unscoped().Table("incident_updates").Where("status = ?", legacyIncidentStatusCompleted).
		UpdateColumn("status", "resolved")
	if upd.Error != nil {
		slog.Error("Failed to migrate legacy incident update status", "error", upd.Error)
		return
	}
	if res.RowsAffected > 0 || upd.RowsAffected > 0 {
		slog.Info("Legacy incident status migrated", "incidents", res.RowsAffected, "updates", upd.RowsAffected)
	}
}
