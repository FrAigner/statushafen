package database

import (
	"log/slog"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// backfillSubscriberStatus markiert (Double-Opt-in) alle Abos ohne
// Status als bestätigt. Die Abos stammen aus der Zeit vor dem Double-Opt-in
// und sollen ihre Benachrichtigungen behalten. Normalerweise füllt schon der
// Spalten-Default "active" beim AutoMigrate alle Bestandszeilen; der Backfill
// fängt Zeilen mit NULL/leerem Status ab (z.B. Spalte ohne Default angelegt).
//
// Idempotent und admin-sicher (Muster: backfillStatusPageLanguages): Er
// berührt nur Zeilen ohne Status. "pending" bleibt unangetastet, sonst würde
// jeder Neustart das Double-Opt-in aushebeln. Ein Fehler wird nur geloggt,
// nicht fatal: Die Spalte hat ohnehin den Default "active".
func backfillSubscriberStatus(db *gorm.DB) {
	err := db.Model(&models.Subscriber{}).Unscoped().
		Where("status IS NULL OR status = ''").
		UpdateColumn("status", models.SubscriberStatusActive).Error
	if err != nil {
		slog.Error("Failed to backfill subscriber status", "error", err)
	}
}
