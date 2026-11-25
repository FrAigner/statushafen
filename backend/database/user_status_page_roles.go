package database

import (
	"log/slog"

	"github.com/fraigner/statushafen/logging"

	"gorm.io/gorm"
)

// dedupeUserStatusPageRoles cleans up duplicate rows per (account,
// page) before the unique index. The row that has been EFFECTIVE SO FAR is kept: the one with
// the smallest ID, because RequirePermission and actorPagePermissions read via
// First() (by ID). This way the backfill changes no permission (no silent
// widening or revoking). All other active rows are soft-deleted;
// no time comparison, hence also robust with NULL in updated_at. Idempotent.
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

// CreateUserStatusPageRoleUniqueIndex creates the partial
// unique index (user_id, status_page_id) for non-deleted rows: an
// account has exactly one role per page, even with simultaneous adding.
// Duplicates are cleaned up beforehand. Idempotent (IF NOT EXISTS); raw SQL, because
// AutoMigrate cannot do partial indexes.
func CreateUserStatusPageRoleUniqueIndex(db *gorm.DB) {
	dedupeUserStatusPageRoles(db)
	sql := `CREATE UNIQUE INDEX IF NOT EXISTS idx_user_status_page_role_active ON user_status_page_roles (user_id, status_page_id) WHERE deleted_at IS NULL`
	if err := db.Exec(sql).Error; err != nil {
		logging.Fatal("Failed to create idx_user_status_page_role_active", "error", err)
	}
}
