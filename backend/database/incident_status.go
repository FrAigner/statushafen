package database

import (
	"log/slog"

	"gorm.io/gorm"
)

// legacyIncidentStatusCompleted: Altwert aus der Zeit vor der Umstellung.
// Wartungs-Updates konnten ihn ungeprüft setzen. Die Konstante lebt bewusst
// nur noch hier: models und Regeln kennen ihn nicht mehr.
const legacyIncidentStatusCompleted = "completed"

// migrateCompletedIncidentStatus stellt den Altwert "completed" auf
// "resolved" um (Vorfälle, Wartungen und deren Timeline-Updates) und füllt
// ResolvedAt, falls leer, mit der letzten Änderung des Vorfalls (der
// Zeitpunkt, an dem der Altwert gesetzt wurde). Idempotent: Nach dem ersten
// Lauf gibt es keine Zeile mit dem Altwert mehr, ein weiterer Lauf ändert
// nichts. UpdateColumn(s) lässt updated_at unberührt; gelöschte Zeilen
// (Unscoped) werden mit umgestellt, damit der Altwert nirgends überlebt.
// Ein Fehler wird nur geloggt, der Start soll nicht daran scheitern.
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
