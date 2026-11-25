package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// SQLite has no advisory locks and runs in one process -
// TryLock is neutral there (always true), Unlock a no-op. The Postgres path
// lives in pruefeAdvisoryLock (MIGRATION_PG_TEST=1).
func TestAdvisoryLock_SQLiteNeutral(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "lock.db")), &gorm.Config{})
	require.NoError(t, err)
	l := NewAdvisoryLock(db, 1)
	for range 2 {
		ok, err := l.TryLock(context.Background())
		require.NoError(t, err)
		assert.True(t, ok)
	}
	l.Unlock()
	assert.Nil(t, l.conn)
}
