package database

import (
	"log/slog"

	"github.com/fraigner/statushafen/logging"

	"gorm.io/gorm"
)

// CreatePageInvitationUniqueIndex creates (security review,
// finding F3) the unique index (status_page_id, email) over ALL rows, also
// over softly deleted "tombstones". Without it, two simultaneous
// invitations of the same address (Postgres, READ COMMITTED) bypass the 30-minute lock,
// because both read "no row" beforehand. Deliberately NOT a partial index
// (deleted_at IS NULL): the lock depends precisely on the tombstones, and the
// replacement path (replaceablePendingInvitation) permanently deletes all rows of the
// address beforehand, so it fits the full index. The address is stored
// normalized (trim/lowercase) when inviting.
//
// Existing data: duplicate rows per (page, address) make the index creation
// fail. The most recent one (highest ID) is kept: it carries the most recently
// sent link and the current lock; older rows are replaced by it
// (a new invitation deletes all older ones of the address anyway). These
// are short-lived invitation data, not admin configuration. Idempotent
// (second run: nothing to delete, IF NOT EXISTS); raw SQL like the other
// index helpers, because a named key over two columns is wanted.
func CreatePageInvitationUniqueIndex(db *gorm.DB) {
	res := db.Exec(`DELETE FROM page_invitations WHERE EXISTS (
  SELECT 1 FROM page_invitations o
  WHERE o.status_page_id = page_invitations.status_page_id
    AND o.email = page_invitations.email
    AND o.id > page_invitations.id)`)
	if res.Error != nil {
		logging.Fatal("Failed to dedupe page_invitations", "error", res.Error)
	}
	if res.RowsAffected > 0 {
		slog.Info("Doppelte Einladungen bereinigt", "count", res.RowsAffected)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_page_invitation_page_email ON page_invitations (status_page_id, email)`).Error; err != nil {
		logging.Fatal("Failed to create idx_page_invitation_page_email", "error", err)
	}
}
