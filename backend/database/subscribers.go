package database

import (
	"log/slog"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// backfillSubscriberStatus marks (double opt-in) all subscriptions without
// status as confirmed. The subscriptions date from the time before double opt-in
// and are supposed to keep their notifications. Normally the
// column default "active" already fills all existing rows during AutoMigrate; the backfill
// catches rows with NULL/empty status (e.g. column created without default).
//
// Idempotent and admin-safe (pattern: backfillStatusPageLanguages): it
// touches only rows without status. "pending" stays untouched, otherwise
// every restart would undermine the double opt-in. An error is only logged,
// not fatal: the column has the default "active" anyway.
// backfillMembershipOrigin marks memberships from before joined_via existed as
// legacy (their origin is unknown, they may stem from an auto-join). New rows
// always get a value (UserStatusPageRole.BeforeCreate), so it touches only
// rows from before and can run on every start.
func backfillMembershipOrigin(db *gorm.DB) {
	err := db.Model(&models.UserStatusPageRole{}).Unscoped().
		Where("joined_via IS NULL OR joined_via = ''").
		UpdateColumn("joined_via", models.JoinedViaLegacy).Error
	if err != nil {
		slog.Error("Failed to backfill membership origin", "error", err)
	}
}

func backfillSubscriberStatus(db *gorm.DB) {
	err := db.Model(&models.Subscriber{}).Unscoped().
		Where("status IS NULL OR status = ''").
		UpdateColumn("status", models.SubscriberStatusActive).Error
	if err != nil {
		slog.Error("Failed to backfill subscriber status", "error", err)
	}
}
