package database

import (
	"github.com/fraigner/statushafen/logging"

	"gorm.io/gorm"
)

// CreateAdminNotificationIndexes legt die Indizes des
// Nachrichtencenters an. AutoMigrate kann keine partiellen Indizes:
//   - idx_admin_notification_dedup: höchstens eine OFFENE Meldung je DedupKey
//     (leerer Schlüssel = erledigt, abgelöst oder ohne Zusammenfassung). Das
//     macht das Zusammenfassen konkurrenzsicher: Legen zwei Anfragen
//     gleichzeitig dieselbe Meldung an, scheitert die zweite mit einer
//     Unique-Verletzung und zählt stattdessen hoch.
//   - idx_notification_read_user: ein Lesestatus je Nachricht und Benutzer.
//
// Idempotent per IF NOT EXISTS.
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
