package database

import (
	"encoding/json"
	"log/slog"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Changeover of the permission model from "<resource>:read|write" to
// "<resource>:create|read|update|delete" per tab.
const (
	// permissionsMigrationKey marks (SystemSetting) that the changeover
	// has run. Without the marker a later start would couple every role again
	// (e.g. components:read -> external_sources:read) and thereby bring back
	// permissions that an admin has deliberately revoked in the meantime.
	permissionsMigrationKey = "permissions.crud_migrated"
	// permissionsBackupKey holds the permission strings of all changed roles
	// before the changeover (JSON {"<role-id>": "<old>"}), so a rollback
	// to an older app version remains possible without a DB backup.
	// It is only created and never overwritten.
	permissionsBackupKey = "permissions.crud_backup"
)

// migrateRolePermissionsToCRUD replaces in ALL roles (also custom
// and soft-deleted ones) "<resource>:write" with create+read+update+delete and
// couples the tabs that previously depended on another resource 1:1 so that
// nobody gains or loses permissions (permissions.MigrateLegacy). The role
// "Global Admin" additionally gets all permissions.
//
// Prod-safe: runs in one transaction including backup and marker, exactly once
// (marker), does not change updated_at and does not abort the start on an error
// - there is then only a warning, and the next start tries again.
// Until then legacy strings continue to apply via the compatibility in permissions.Has.
func migrateRolePermissionsToCRUD(db *gorm.DB) {
	var done int64
	if err := db.Unscoped().Model(&models.SystemSetting{}).Where("key = ?", permissionsMigrationKey).Count(&done).Error; err != nil {
		slog.Warn("Rechte-Umstellung (CRUD): Marker nicht lesbar, übersprungen", "error", err)
		return
	}
	if done > 0 {
		return
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		var roles []models.Role
		if err := tx.Unscoped().Find(&roles).Error; err != nil {
			return err
		}
		backup := map[uint]string{}
		for _, r := range roles {
			migrated := permissions.MigrateLegacy(r.Permissions)
			if r.Name == models.GlobalAdminRoleName {
				migrated = permissions.Union(migrated, permissions.FullAccess())
			}
			if migrated == r.Permissions {
				continue
			}
			backup[r.ID] = r.Permissions
			// UpdateColumn: updated_at stays, the role does not appear as "just changed".
			if err := tx.Unscoped().Model(&models.Role{}).Where("id = ?", r.ID).UpdateColumn("permissions", migrated).Error; err != nil {
				return err
			}
		}
		// A map of numbers and texts can always be serialized.
		raw, _ := json.Marshal(backup)
		// Only create: if the changeover runs again after a deleted or
		// restored marker, an upsert would replace the complete
		// original state with the then (almost) empty backup.
		if err := createSettingIfMissing(tx, permissionsBackupKey, string(raw)); err != nil {
			return err
		}
		if err := upsertSetting(tx, permissionsMigrationKey, "1"); err != nil {
			return err
		}
		slog.Info("Rechte auf Create/Read/Update/Delete umgestellt", "roles_changed", len(backup))
		return nil
	})
	if err != nil {
		slog.Warn("Rechte-Umstellung (CRUD) fehlgeschlagen, nächster Start versucht es erneut", "error", err)
	}
}

func upsertSetting(tx *gorm.DB, key, value string) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&models.SystemSetting{Key: key, Value: value}).Error
}

// createSettingIfMissing creates the key and leaves an existing
// value unchanged.
func createSettingIfMissing(tx *gorm.DB, key, value string) error {
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoNothing: true}).
		Create(&models.SystemSetting{Key: key, Value: value}).Error
}
