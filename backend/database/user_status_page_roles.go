package database

import (
	"log/slog"

	"github.com/fraigner/statushafen/logging"

	"gorm.io/gorm"
)

// dedupeUserStatusPageRoles bereinigt Doppelzeilen je (Konto,
// Seite) vor dem Unique-Index. Behalten wird die BISHER WIRKSAME Zeile: die mit
// der kleinsten ID, denn RequirePermission und actorPagePermissions lesen per
// First() (nach ID). So ändert der Backfill keine Berechtigung (kein stilles
// Ausweiten oder Entziehen). Alle weiteren aktiven Zeilen werden soft-gelöscht;
// kein Zeitvergleich, daher auch mit NULL in updated_at robust. Idempotent.
func dedupeUserStatusPageRoles(db *gorm.DB) {
	sql := `UPDATE user_status_page_roles SET deleted_at = CURRENT_TIMESTAMP
WHERE deleted_at IS NULL AND EXISTS (
  SELECT 1 FROM user_status_page_roles o
  WHERE o.user_id = user_status_page_roles.user_id
    AND o.status_page_id = user_status_page_roles.status_page_id
    AND o.deleted_at IS NULL
    AND o.id < user_status_page_roles.id)`
	res := db.Exec(sql)
	if res.Error != nil {
		logging.Fatal("Failed to dedupe user_status_page_roles", "error", res.Error)
	}
	if res.RowsAffected > 0 {
		slog.Info("Doppelte Seitenrollen bereinigt", "count", res.RowsAffected)
	}
}

// CreateUserStatusPageRoleUniqueIndex legt den partiellen
// Unique-Index (user_id, status_page_id) für nicht gelöschte Zeilen an: Ein
// Konto hat je Seite genau eine Rolle, auch bei gleichzeitigem Hinzufügen.
// Vorher werden Duplikate bereinigt. Idempotent (IF NOT EXISTS); Raw-SQL, weil
// AutoMigrate keine partiellen Indizes kann.
func CreateUserStatusPageRoleUniqueIndex(db *gorm.DB) {
	dedupeUserStatusPageRoles(db)
	sql := `CREATE UNIQUE INDEX IF NOT EXISTS idx_user_status_page_role_active ON user_status_page_roles (user_id, status_page_id) WHERE deleted_at IS NULL`
	if err := db.Exec(sql).Error; err != nil {
		logging.Fatal("Failed to create idx_user_status_page_role_active", "error", err)
	}
}
