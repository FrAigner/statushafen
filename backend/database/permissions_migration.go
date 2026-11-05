package database

import (
	"encoding/json"
	"log/slog"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Umstellung des Rechtemodells von "<ressource>:read|write" auf
// "<ressource>:create|read|update|delete" je Tab.
const (
	// permissionsMigrationKey markiert (SystemSetting), dass die Umstellung
	// gelaufen ist. Ohne Marker würde ein späterer Start jede Rolle erneut
	// koppeln (z. B. components:read -> external_sources:read) und damit
	// Rechte zurückholen, die ein Admin inzwischen bewusst entzogen hat.
	permissionsMigrationKey = "permissions.crud_migrated"
	// permissionsBackupKey hält die Rechte-Strings aller geänderten Rollen
	// vor der Umstellung (JSON {"<rollen-id>": "<alt>"}), damit ein Rollback
	// auf eine ältere App-Version ohne DB-Backup möglich bleibt.
	// Er wird nur angelegt und nie überschrieben.
	permissionsBackupKey = "permissions.crud_backup"
)

// migrateRolePermissionsToCRUD ersetzt in ALLEN Rollen (auch benutzerdefinierte
// und soft-gelöschte) "<ressource>:write" durch create+read+update+delete und
// koppelt die Tabs, die bisher an eine andere Ressource hingen, 1:1 so, dass
// niemand Rechte gewinnt oder verliert (permissions.MigrateLegacy). Die Rolle
// "Global Admin" bekommt zusätzlich alle Rechte.
//
// Prod-sicher: läuft in einer Transaktion samt Backup und Marker, genau einmal
// (Marker), ändert updated_at nicht und bricht den Start bei einem Fehler nicht
// ab - es gibt dann nur eine Warnung, und der nächste Start versucht es erneut.
// Bis dahin gelten Alt-Strings über die Kompatibilität in permissions.Has weiter.
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
			// UpdateColumn: updated_at bleibt, die Rolle erscheint nicht als „gerade geändert“.
			if err := tx.Unscoped().Model(&models.Role{}).Where("id = ?", r.ID).UpdateColumn("permissions", migrated).Error; err != nil {
				return err
			}
		}
		// Eine Map aus Zahlen und Texten lässt sich immer serialisieren.
		raw, _ := json.Marshal(backup)
		// Nur anlegen: Läuft die Umstellung nach gelöschtem oder
		// zurückgespieltem Marker erneut, würde ein Upsert den vollständigen
		// Ursprungsstand durch die dann (fast) leere Sicherung ersetzen.
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

// createSettingIfMissing legt den Schlüssel an und lässt einen vorhandenen
// Wert unverändert.
func createSettingIfMissing(tx *gorm.DB, key, value string) error {
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoNothing: true}).
		Create(&models.SystemSetting{Key: key, Value: value}).Error
}
