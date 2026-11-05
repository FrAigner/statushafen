package database

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Die Umstellung von "<ressource>:write" auf Create/Read/Update/
// Delete läuft beim Start einmal über alle Rollen (auch benutzerdefinierte und
// soft-gelöschte), ist Prod-sicher und idempotent.

func newPermMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newIntegrityTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.SystemSetting{}))
	return db
}

func permsOf(t *testing.T, db *gorm.DB, id uint) string {
	t.Helper()
	var r models.Role
	require.NoError(t, db.Unscoped().First(&r, id).Error)
	return r.Permissions
}

func TestMigrateRolePermissionsToCRUD(t *testing.T) {
	db := newPermMigrationDB(t)
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	mk := func(name, perms string) models.Role {
		r := models.Role{Name: name, Permissions: perms}
		require.NoError(t, db.Create(&r).Error)
		require.NoError(t, db.Model(&r).UpdateColumn("updated_at", old).Error)
		return r
	}
	editor := mk("Editor", "components:read,incidents:read,incidents:write,notices:read,notices:write")
	custom := mk("Eigene", "components:write,admin_dashboard,foo:bar")
	viewer := mk("Viewer", "components:read,incidents:read,notices:read")
	owner := mk("Page Owner", "pages:write,components:read,components:write,incidents:read,incidents:write,notices:read,notices:write,subscribers:read,subscribers:write,admin_dashboard")
	admin := mk(models.GlobalAdminRoleName, "pages:read,pages:write,users:read,users:write,roles:read,roles:write")
	deleted := mk("Gelöscht", "incidents:write")
	require.NoError(t, db.Delete(&deleted).Error)
	untouched := mk("Neu", "notices:create,notices:read")

	migrateRolePermissionsToCRUD(db)

	has := func(id uint, perm string) bool { return permissions.Has(permsOf(t, db, id), perm) }
	for _, id := range []uint{editor.ID, custom.ID, viewer.ID, owner.ID, admin.ID, deleted.ID, untouched.ID} {
		assert.False(t, permissions.HasLegacyWrite(permsOf(t, db, id)), "kein write mehr in Rolle %d: %s", id, permsOf(t, db, id))
	}

	// Editor: Vorfälle wie bisher schreiben, dazu die gekoppelten Tabs; Komponenten nur lesen.
	assert.True(t, has(editor.ID, "incidents:delete"))
	assert.True(t, has(editor.ID, "maintenance:create"))
	assert.True(t, has(editor.ID, "incident_templates:update"))
	assert.True(t, has(editor.ID, "external_sources:read"), "components:read erbte die externe Quelle")
	assert.False(t, has(editor.ID, "external_sources:update"))
	assert.False(t, has(editor.ID, "components:update"))
	assert.False(t, has(editor.ID, "subscribers:read"))

	// Eigene Rolle mit components:write: Quellen wie bisher, Sonderrecht und Unbekanntes bleiben.
	assert.True(t, has(custom.ID, "external_sources:delete"))
	assert.Contains(t, permsOf(t, db, custom.ID), "foo:bar")
	assert.Contains(t, permsOf(t, db, custom.ID), "admin_dashboard")
	assert.False(t, has(custom.ID, "incidents:read"))

	// Viewer: lesen, inklusive Wartung und Quellen; Vorlagen verlangten write.
	assert.True(t, has(viewer.ID, "maintenance:read"))
	assert.True(t, has(viewer.ID, "external_sources:read"))
	assert.False(t, has(viewer.ID, "incident_templates:read"))
	assert.False(t, has(viewer.ID, "incidents:create"))

	// Page Owner: pages:write bringt die Inhalte-Tabs und SSO.
	for _, res := range []string{"pages", "notification_templates", "languages", "ui_translations", "sso"} {
		for _, act := range permissions.Actions {
			assert.True(t, has(owner.ID, permissions.Perm(res, act)), res+":"+act)
		}
	}

	// Global Admin: alles.
	for _, r := range permissions.Resources {
		for _, act := range permissions.Actions {
			assert.True(t, has(admin.ID, permissions.Perm(r.Key, act)), r.Key+":"+act)
		}
	}

	// Soft-gelöschte Rolle wird mitmigriert (ein Wiederherstellen verliert nichts).
	assert.True(t, has(deleted.ID, "maintenance:delete"))
	assert.Equal(t, "notices:create,notices:read", permsOf(t, db, untouched.ID), "bereits neue Rolle bleibt")

	// updated_at bleibt, die Rollen erscheinen nicht als „gerade geändert“.
	var r models.Role
	require.NoError(t, db.First(&r, editor.ID).Error)
	assert.True(t, r.UpdatedAt.Equal(old), "updated_at wurde verändert: %s", r.UpdatedAt)

	// Backup der alten Strings für einen Rollback.
	var backup models.SystemSetting
	require.NoError(t, db.Where("key = ?", permissionsBackupKey).First(&backup).Error)
	var saved map[string]string
	require.NoError(t, json.Unmarshal([]byte(backup.Value), &saved))
	assert.Equal(t, "components:write,admin_dashboard,foo:bar", saved[strconv.Itoa(int(custom.ID))])
	assert.NotContains(t, saved, strconv.Itoa(int(untouched.ID)), "unveränderte Rollen stehen nicht im Backup")
}

// Zweiter Lauf: nichts ändert sich, auch keine Rechte, die ein Admin
// inzwischen bewusst entzogen hat (Marker statt erneutem Koppeln).
func TestMigrateRolePermissionsToCRUD_Idempotent(t *testing.T) {
	db := newPermMigrationDB(t)
	viewer := models.Role{Name: "Viewer", Permissions: "components:read,incidents:read"}
	require.NoError(t, db.Create(&viewer).Error)

	migrateRolePermissionsToCRUD(db)
	first := permsOf(t, db, viewer.ID)
	require.Contains(t, first, "external_sources:read")

	// Der Admin entzieht die Quellen wieder.
	require.NoError(t, db.Model(&viewer).UpdateColumn("permissions", "components:read,incidents:read,maintenance:read").Error)
	migrateRolePermissionsToCRUD(db)
	assert.Equal(t, "components:read,incidents:read,maintenance:read", permsOf(t, db, viewer.ID), "Marker verhindert erneutes Koppeln")

	var markers int64
	require.NoError(t, db.Model(&models.SystemSetting{}).Where("key = ?", permissionsMigrationKey).Count(&markers).Error)
	assert.EqualValues(t, 1, markers)
}

// Leere Datenbank (Neuinstallation): kein Fehler, Marker gesetzt.
func TestMigrateRolePermissionsToCRUD_LeereDatenbank(t *testing.T) {
	db := newPermMigrationDB(t)
	migrateRolePermissionsToCRUD(db)
	var n int64
	require.NoError(t, db.Model(&models.SystemSetting{}).Where("key = ?", permissionsMigrationKey).Count(&n).Error)
	assert.EqualValues(t, 1, n)
}

// Fehlerfälle brechen den Start nicht ab (Warnung statt Abbruch) und setzen
// keinen Marker, damit der nächste Start es erneut versucht.
func TestMigrateRolePermissionsToCRUD_FehlerSindNichtFatal(t *testing.T) {
	t.Run("Marker nicht lesbar", func(t *testing.T) {
		db := newPermMigrationDB(t)
		require.NoError(t, db.Exec(`DROP TABLE system_settings`).Error)
		assert.NotPanics(t, func() { migrateRolePermissionsToCRUD(db) })
	})
	t.Run("Rollen nicht lesbar: kein Marker", func(t *testing.T) {
		db := newPermMigrationDB(t)
		require.NoError(t, db.Exec(`DROP TABLE roles`).Error)
		assert.NotPanics(t, func() { migrateRolePermissionsToCRUD(db) })
		var n int64
		require.NoError(t, db.Model(&models.SystemSetting{}).Where("key = ?", permissionsMigrationKey).Count(&n).Error)
		assert.Zero(t, n)
	})
	// Schlägt ein Schritt der Transaktion fehl, bleibt alles unverändert (kein halbes Migrieren).
	for name, trigger := range map[string]string{
		"Rolle nicht änderbar":     `CREATE TRIGGER blockiere AFTER UPDATE ON roles BEGIN SELECT RAISE(ABORT, 'gesperrt'); END`,
		"Backup nicht speicherbar": `CREATE TRIGGER blockiere BEFORE INSERT ON system_settings WHEN NEW.key = 'permissions.crud_backup' BEGIN SELECT RAISE(ABORT, 'gesperrt'); END`,
		"Marker nicht speicherbar": `CREATE TRIGGER blockiere BEFORE INSERT ON system_settings WHEN NEW.key = 'permissions.crud_migrated' BEGIN SELECT RAISE(ABORT, 'gesperrt'); END`,
	} {
		t.Run(name, func(t *testing.T) {
			db := newPermMigrationDB(t)
			role := models.Role{Name: "Alt", Permissions: "components:write"}
			require.NoError(t, db.Create(&role).Error)
			require.NoError(t, db.Exec(trigger).Error)
			assert.NotPanics(t, func() { migrateRolePermissionsToCRUD(db) })
			assert.Equal(t, "components:write", permsOf(t, db, role.ID), "Transaktion zurückgerollt")
			var n int64
			require.NoError(t, db.Model(&models.SystemSetting{}).Where("key LIKE 'permissions.crud%'").Count(&n).Error)
			assert.Zero(t, n)
		})
	}
}

// Seeds: gleiche Wirkung wie die früheren Listen, kein write mehr.
func TestSeedRollen_KeinWriteMehrUndGleicheWirkung(t *testing.T) {
	assert.False(t, permissions.HasLegacyWrite(PageOwnerPermissions))
	assert.True(t, permissions.Has(PageOwnerPermissions, "sso:delete"))
	assert.Contains(t, PageOwnerPermissions, permissions.AdminDashboard)
	assert.False(t, permissions.Has(PageOwnerPermissions, "users:read"), "Page Owner ist keine globale Rolle")
}

// Das Rollback-Backup entsteht nur einmal. Ein zweiter Lauf (Marker
// gelöscht oder aus einem alten Dump zurückgespielt) darf den vollständigen
// Ursprungsstand nicht durch die dann leere Sicherung ersetzen.
func TestMigrateRolePermissionsToCRUD_BackupWirdNichtUeberschrieben(t *testing.T) {
	db := newPermMigrationDB(t)
	editor := models.Role{Name: "Editor", Permissions: "incidents:write"}
	require.NoError(t, db.Create(&editor).Error)

	migrateRolePermissionsToCRUD(db)
	var first models.SystemSetting
	require.NoError(t, db.Where("key = ?", permissionsBackupKey).First(&first).Error)
	require.Contains(t, first.Value, "incidents:write")

	require.NoError(t, db.Unscoped().Where("key = ?", permissionsMigrationKey).Delete(&models.SystemSetting{}).Error)
	migrateRolePermissionsToCRUD(db)

	var second models.SystemSetting
	require.NoError(t, db.Where("key = ?", permissionsBackupKey).First(&second).Error)
	assert.Equal(t, first.Value, second.Value, "Backup bleibt der ursprüngliche Stand")
	var markers int64
	require.NoError(t, db.Model(&models.SystemSetting{}).Where("key = ?", permissionsMigrationKey).Count(&markers).Error)
	assert.EqualValues(t, 1, markers, "Marker wird wieder gesetzt")
	assert.NotContains(t, permsOf(t, db, editor.ID), "write")
}
