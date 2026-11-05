package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoveOrphanedUserStatusPageRoles_ErrorIsNotFatal: Fehlt die Tabelle,
// bricht die Bereinigung nur ab (Warnung), der Start läuft weiter.
func TestRemoveOrphanedUserStatusPageRoles_ErrorIsNotFatal(t *testing.T) {
	db := newIntegrityTestDB(t)
	require.NoError(t, db.Exec(`DROP TABLE user_status_page_roles`).Error)
	assert.NotPanics(t, func() { removeOrphanedUserStatusPageRoles(db) })
}
