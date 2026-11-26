package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoveOrphanedUserStatusPageRoles_ErrorIsNotFatal: if the table is missing,
// the cleanup only aborts (warning), startup continues.
func TestRemoveOrphanedUserStatusPageRoles_ErrorIsNotFatal(t *testing.T) {
	db := newIntegrityTestDB(t)
	require.NoError(t, db.Exec(`DROP TABLE user_status_page_roles`).Error)
	assert.NotPanics(t, func() { removeOrphanedUserStatusPageRoles(db) })
}
