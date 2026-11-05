package externalstatus

import (
	"context"
	"log/slog"
	"time"

	"github.com/fraigner/statushafen/models"
)

// AdminNotificationRetention: Nachrichten des Admin-Nachrichten-
// centers werden nach dieser Zeit (seit dem letzten Auftreten) gelöscht. Der
// Aufräumlauf hängt am vorhandenen Leader-Takt (maybePrune), wie die
// Aufbewahrung gespiegelter Meldungen.
const AdminNotificationRetention = 90 * 24 * time.Hour

// PruneAdminNotifications löscht Nachrichten, deren letztes Auftreten länger
// als AdminNotificationRetention zurückliegt, samt Lesemarken (auch ohne
// FK-Cascade, z. B. SQLite). Liefert die Anzahl entfernter Nachrichten.
func (s *Service) PruneAdminNotifications(ctx context.Context) (int, error) {
	cutoff := s.now().Add(-AdminNotificationRetention)
	total := 0
	for ctx.Err() == nil {
		var ids []uint
		if err := s.db.Model(&models.AdminNotification{}).Where("last_seen_at < ?", cutoff).
			Order("id").Limit(pruneBatchSize).Pluck("id", &ids).Error; err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}
		if err := s.db.Where("notification_id IN ?", ids).Delete(&models.NotificationRead{}).Error; err != nil {
			return total, err
		}
		if err := s.db.Where("id IN ?", ids).Delete(&models.AdminNotification{}).Error; err != nil {
			return total, err
		}
		total += len(ids)
	}
	return total, nil
}

func (s *Service) pruneAdminNotifications(ctx context.Context) {
	count, err := s.PruneAdminNotifications(ctx)
	if err != nil {
		slog.Error("Admin-Nachrichten: Aufräumen fehlgeschlagen", "error", err)
	}
	if count > 0 {
		slog.Info("Admin-Nachrichten: alte Nachrichten entfernt", "count", count)
	}
}
