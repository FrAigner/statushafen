package database

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Duplicate page roles are cleaned up before the unique index; the
// previously effective row (smallest ID) is kept.
func TestCreateUserStatusPageRoleUniqueIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "usr.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.StatusPage{}, &models.Role{}, &models.User{}, &models.UserStatusPageRole{}))

	page := models.StatusPage{Name: "P", Slug: "p"}
	other := models.StatusPage{Name: "Q", Slug: "q"}
	roleA, roleB := models.Role{Name: "A"}, models.Role{Name: "B"}
	u := models.User{Email: "u@x.example"}
	for _, v := range []any{&page, &other, &roleA, &roleB, &u} {
		require.NoError(t, db.Create(v).Error)
	}
	// So far First() made the row with the smallest ID (viewer) effective, even if
	// a more recent (higher ID) one carries the owner role: the backfill must not change the effective
	// role. A row with NULL in updated_at must not break anything.
	viewerRow := models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: roleA.ID}
	ownerRow := models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: roleB.ID}
	single := models.UserStatusPageRole{UserID: u.ID, StatusPageID: other.ID, RoleID: roleB.ID}
	for _, v := range []*models.UserStatusPageRole{&viewerRow, &ownerRow, &single} {
		require.NoError(t, db.Create(v).Error)
	}
	require.NoError(t, db.Model(&models.UserStatusPageRole{}).Where("id = ?", viewerRow.ID).UpdateColumn("updated_at", time.Now().Add(-time.Hour)).Error)
	require.NoError(t, db.Model(&models.UserStatusPageRole{}).Where("id = ?", ownerRow.ID).UpdateColumn("updated_at", gorm.Expr("NULL")).Error)

	CreateUserStatusPageRoleUniqueIndex(db)
	CreateUserStatusPageRoleUniqueIndex(db) // idempotent

	var rows []models.UserStatusPageRole
	require.NoError(t, db.Where("status_page_id = ?", page.ID).Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, roleA.ID, rows[0].RoleID, "die bisher wirksame Zeile (kleinste ID) bleibt")
	var n int64
	require.NoError(t, db.Model(&models.UserStatusPageRole{}).Where("status_page_id = ?", other.ID).Count(&n).Error)
	require.EqualValues(t, 1, n, "Einzelzeile bleibt")

	// Index applies; soft-deleted rows do not block.
	dup := models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: roleB.ID}
	require.Error(t, db.Create(&dup).Error)
	require.NoError(t, db.Delete(&rows[0]).Error)
	require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: roleB.ID}).Error)
}
