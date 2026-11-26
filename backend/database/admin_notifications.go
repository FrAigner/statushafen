package database

import (
	"github.com/fraigner/statushafen/logging"

	"gorm.io/gorm"
)

// CreateAdminNotificationIndexes creates the indexes of the
// message center. AutoMigrate cannot create partial indexes:
//   - idx_admin_notification_dedup: at most one OPEN notification per DedupKey
//     (empty key = done, superseded or without aggregation). This
//     makes aggregation concurrency-safe: if two requests create
//     the same notification at the same time, the second fails with a
//     unique violation and counts up instead.
//   - idx_notification_read_user: one read status per message and user.
//
// Idempotent via IF NOT EXISTS.
func CreateAdminNotificationIndexes(db *gorm.DB) {
	statements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_admin_notification_dedup ON admin_notifications (dedup_key) WHERE dedup_key <> ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_read_user ON notification_reads (notification_id, user_id)`,
	}
	for _, sql := range statements {
		if err := db.Exec(sql).Error; err != nil {
			logging.Fatal("Failed to create admin notification index", "error", err)
		}
	}
}
