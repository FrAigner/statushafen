package database

import (
	"path/filepath"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Memberships from before joined_via existed become "legacy" (their origin is
// unknown: they may stem from an auto-join); new rows are "assigned" unless
// the auto-join marks them. The backfill runs on every start and must not
// touch rows written since.
func TestBackfillMembershipOrigin_MarksOnlyRowsFromBefore(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "joined.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE user_status_page_roles (id integer primary key autoincrement, created_at datetime, updated_at datetime, deleted_at datetime, user_id integer not null, status_page_id integer not null, role_id integer not null)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO user_status_page_roles (user_id, status_page_id, role_id) VALUES (1, 1, 1)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO user_status_page_roles (user_id, status_page_id, role_id, deleted_at) VALUES (2, 1, 1, CURRENT_TIMESTAMP)`).Error)
	require.NoError(t, db.AutoMigrate(&models.StatusPage{}, &models.Role{}, &models.User{}, &models.UserStatusPageRole{}))

	backfillMembershipOrigin(db)
	require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: 3, StatusPageID: 1, RoleID: 1}).Error)
	require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: 4, StatusPageID: 1, RoleID: 1, JoinedVia: models.JoinedViaAutoJoin}).Error)
	backfillMembershipOrigin(db) // second start

	var got []string
	require.NoError(t, db.Unscoped().Model(&models.UserStatusPageRole{}).Order("user_id").Pluck("joined_via", &got).Error)
	assert.Equal(t, []string{models.JoinedViaLegacy, models.JoinedViaLegacy, models.JoinedViaAssigned, models.JoinedViaAutoJoin}, got)
}

// Without the table the backfill logs the failure instead of stopping the start.
func TestBackfillMembershipOrigin_LogsAFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "empty.db")), &gorm.Config{})
	require.NoError(t, err)
	out := captureLog(t)
	backfillMembershipOrigin(db)
	assert.Contains(t, out.String(), "Failed to backfill membership origin")
}
