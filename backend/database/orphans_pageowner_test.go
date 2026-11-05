package database

import (
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoveOrphanedRoleAndUserReferences: DefaultRoleID auf eine
// gelöschte Rolle wird NULL, UserTag und UserSSOIdentity gelöschter Benutzer
// verschwinden; alles andere bleibt, der zweite Lauf ändert nichts.
func TestRemoveOrphanedRoleAndUserReferences(t *testing.T) {
	db := newIntegrityTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.UserTag{}, &models.UserSSOIdentity{}))

	roleOK := models.Role{Name: "Ok", Permissions: "x"}
	roleGone := models.Role{Name: "Gone", Permissions: "x"}
	userOK := models.User{Email: "ok@example.com"}
	userGone := models.User{Email: "gone@example.com"}
	tag := models.Tag{Name: "t"}
	for _, v := range []any{&roleOK, &roleGone, &userOK, &userGone, &tag} {
		require.NoError(t, db.Create(v).Error)
	}
	pageKeep := models.StatusPage{Name: "A", Slug: "a", DefaultRoleID: &roleOK.ID}
	pageOrphan := models.StatusPage{Name: "B", Slug: "b", DefaultRoleID: &roleGone.ID}
	pageNone := models.StatusPage{Name: "C", Slug: "c"}
	for _, v := range []any{&pageKeep, &pageOrphan, &pageNone} {
		require.NoError(t, db.Create(v).Error)
	}
	for _, u := range []uint{userOK.ID, userGone.ID} {
		require.NoError(t, db.Create(&models.UserTag{UserID: u, TagID: tag.ID}).Error)
		require.NoError(t, db.Create(&models.UserSSOIdentity{UserID: u, StatusPageID: pageKeep.ID, Issuer: "i", Subject: "s" + strconv.Itoa(int(u))}).Error)
	}
	require.NoError(t, db.Delete(&roleGone).Error)
	require.NoError(t, db.Delete(&userGone).Error)

	removeOrphanedRoleAndUserReferences(db)
	removeOrphanedRoleAndUserReferences(db)

	var keep, orphan, none models.StatusPage
	require.NoError(t, db.First(&keep, pageKeep.ID).Error)
	require.NoError(t, db.First(&orphan, pageOrphan.ID).Error)
	require.NoError(t, db.First(&none, pageNone.ID).Error)
	require.NotNil(t, keep.DefaultRoleID)
	assert.Equal(t, roleOK.ID, *keep.DefaultRoleID)
	assert.Nil(t, orphan.DefaultRoleID)
	assert.Nil(t, none.DefaultRoleID)

	var tags []models.UserTag
	require.NoError(t, db.Unscoped().Find(&tags).Error)
	require.Len(t, tags, 1)
	assert.Equal(t, userOK.ID, tags[0].UserID)
	var ids []models.UserSSOIdentity
	require.NoError(t, db.Unscoped().Find(&ids).Error)
	require.Len(t, ids, 1)
	assert.Equal(t, userOK.ID, ids[0].UserID)
}

// Fehlende Tabellen brechen den Start nicht ab (nur Warnung).
func TestRemoveOrphanedRoleAndUserReferences_ErrorIsNotFatal(t *testing.T) {
	db := newIntegrityTestDB(t)
	assert.NotPanics(t, func() { removeOrphanedRoleAndUserReferences(db) })
}

// TestSeedPageOwnerRole: legt die Rolle mit pages:write an,
// ist idempotent und überschreibt weder eine vom Betreiber angepasste noch
// eine bewusst gelöschte Rolle gleichen Namens.
func TestSeedPageOwnerRole(t *testing.T) {
	db := newIntegrityTestDB(t)
	seedPageOwnerRole(db)
	seedPageOwnerRole(db)

	var roles []models.Role
	require.NoError(t, db.Where("name = ?", PageOwnerRoleName).Find(&roles).Error)
	require.Len(t, roles, 1)
	// CRUD statt write, mit allen früher gekoppelten Tabs.
	for _, p := range []string{"pages:create", "pages:update", "pages:delete", "components:update", "external_sources:update",
		"maintenance:create", "incident_templates:delete", "notification_templates:update", "languages:update",
		"ui_translations:update", "sso:update", "subscribers:delete", "admin_dashboard"} {
		assert.Contains(t, roles[0].Permissions, p)
	}
	assert.NotContains(t, roles[0].Permissions, ":write")
	assert.False(t, roles[0].GlobalOnly())

	t.Run("angepasste Rolle bleibt unverändert", func(t *testing.T) {
		db := newIntegrityTestDB(t)
		require.NoError(t, db.Create(&models.Role{Name: PageOwnerRoleName, Permissions: "custom"}).Error)
		seedPageOwnerRole(db)
		var r models.Role
		require.NoError(t, db.Where("name = ?", PageOwnerRoleName).First(&r).Error)
		assert.Equal(t, "custom", r.Permissions)
	})
	t.Run("gelöschte Rolle wird nicht neu angelegt", func(t *testing.T) {
		db := newIntegrityTestDB(t)
		r := models.Role{Name: PageOwnerRoleName, Permissions: "custom"}
		require.NoError(t, db.Create(&r).Error)
		require.NoError(t, db.Delete(&r).Error)
		seedPageOwnerRole(db)
		var n int64
		require.NoError(t, db.Model(&models.Role{}).Where("name = ?", PageOwnerRoleName).Count(&n).Error)
		assert.Zero(t, n)
	})
	t.Run("umbenannte Rolle wird nach dem ersten Seed nicht neu angelegt", func(t *testing.T) {
		db := newIntegrityTestDB(t)
		seedPageOwnerRole(db)
		require.NoError(t, db.Model(&models.Role{}).Where("name = ?", PageOwnerRoleName).Update("name", "Seiten-Chef").Error)
		seedPageOwnerRole(db)
		var n int64
		require.NoError(t, db.Model(&models.Role{}).Count(&n).Error)
		assert.EqualValues(t, 1, n, "Umbenennen darf keine zweite Standardrolle erzeugen")
		require.NoError(t, db.Model(&models.Role{}).Where("name = ?", PageOwnerRoleName).Count(&n).Error)
		assert.Zero(t, n)
	})
	t.Run("fehlende Merker-Tabelle ist nicht fatal und legt nichts an", func(t *testing.T) {
		db := newIntegrityTestDB(t)
		require.NoError(t, db.Exec(`DROP TABLE system_settings`).Error)
		assert.NotPanics(t, func() { seedPageOwnerRole(db) })
		var n int64
		require.NoError(t, db.Model(&models.Role{}).Count(&n).Error)
		assert.Zero(t, n)
	})
	t.Run("Merker nicht setzbar ist nicht fatal", func(t *testing.T) {
		db := newIntegrityTestDB(t)
		require.NoError(t, db.Exec(`CREATE TRIGGER no_marker BEFORE INSERT ON system_settings BEGIN SELECT RAISE(ABORT, 'nein'); END`).Error)
		assert.NotPanics(t, func() { seedPageOwnerRole(db) })
		var n int64
		require.NoError(t, db.Model(&models.Role{}).Count(&n).Error)
		assert.EqualValues(t, 1, n)
	})
	t.Run("soft-gelöschter Merker legt nach Umbenennen keine zweite Rolle an", func(t *testing.T) {
		db := newIntegrityTestDB(t)
		seedPageOwnerRole(db)
		require.NoError(t, db.Model(&models.Role{}).Where("name = ?", PageOwnerRoleName).Update("name", "Seiten-Chef").Error)
		require.NoError(t, db.Where("key = ?", pageOwnerSeededKey).Delete(&models.SystemSetting{}).Error)
		seedPageOwnerRole(db)
		var n int64
		require.NoError(t, db.Model(&models.Role{}).Count(&n).Error)
		assert.EqualValues(t, 1, n)
	})
	t.Run("Prüfungsfehler ist nicht fatal", func(t *testing.T) {
		db := newIntegrityTestDB(t)
		require.NoError(t, db.Exec(`DROP TABLE roles`).Error)
		assert.NotPanics(t, func() { seedPageOwnerRole(db) })
	})
	t.Run("Anlegefehler ist nicht fatal und legt nichts an", func(t *testing.T) {
		db := newIntegrityTestDB(t)
		require.NoError(t, db.Exec(`DROP TABLE roles`).Error)
		require.NoError(t, db.Exec(`CREATE TABLE roles (id integer primary key, created_at datetime, updated_at datetime,
			deleted_at datetime, name text, permissions text, pflicht text NOT NULL)`).Error)
		assert.NotPanics(t, func() { seedPageOwnerRole(db) })
		var n int64
		require.NoError(t, db.Table("roles").Count(&n).Error)
		assert.Zero(t, n)
		require.NoError(t, db.Unscoped().Model(&models.SystemSetting{}).Where("key = ?", pageOwnerSeededKey).Count(&n).Error)
		assert.Zero(t, n, "Merker erst nach erfolgreichem Anlegen")
	})
}
