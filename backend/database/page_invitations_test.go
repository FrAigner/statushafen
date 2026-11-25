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

// Review F3: duplicate rows per (page, address) are cleaned up before the
// unique index (the most recent, i.e. highest ID, stays: it carries the
// most recently sent link and the current lock), the run is idempotent
// and the index also applies against tombstones.
func TestCreatePageInvitationUniqueIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "inv.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.PageInvitation{}))

	later := time.Now().Add(time.Hour)
	mk := func(page uint, email, hash string) models.PageInvitation {
		inv := models.PageInvitation{StatusPageID: page, Email: email, RoleID: 1, TokenHash: hash, ExpiresAt: later, InvitedByID: 1}
		require.NoError(t, db.Create(&inv).Error)
		return inv
	}
	old := mk(1, "a@x.example", "h1")
	newest := mk(1, "a@x.example", "h2")
	require.NoError(t, db.Delete(&newest).Error) // most recent row is a tombstone
	single := mk(1, "b@x.example", "h3")
	otherPage := mk(2, "a@x.example", "h4")

	CreatePageInvitationUniqueIndex(db)
	CreatePageInvitationUniqueIndex(db) // idempotent

	var ids []uint
	require.NoError(t, db.Unscoped().Model(&models.PageInvitation{}).Order("id").Pluck("id", &ids).Error)
	require.Equal(t, []uint{newest.ID, single.ID, otherPage.ID}, ids, "älteres Duplikat %d weg, Rest bleibt", old.ID)

	// The index also applies against the tombstone.
	dup := models.PageInvitation{StatusPageID: 1, Email: "a@x.example", RoleID: 1, TokenHash: "h5", ExpiresAt: later, InvitedByID: 1}
	require.Error(t, db.Create(&dup).Error)
	// Different page and different address are free.
	mk(3, "a@x.example", "h6")
	mk(1, "c@x.example", "h7")
}
