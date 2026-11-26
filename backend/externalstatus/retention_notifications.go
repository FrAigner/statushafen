package externalstatus

import (
	"context"
	"log/slog"
	"time"

	"github.com/fraigner/statushafen/models"
)

// AdminNotificationRetention: messages of the admin message
// center are deleted after this time (since the last occurrence). The
// cleanup run hangs off the existing leader tick (maybePrune), like the
// retention of mirrored reports.
const AdminNotificationRetention = 90 * 24 * time.Hour

// PruneAdminNotifications deletes messages whose last occurrence is longer
// ago than AdminNotificationRetention, together with read marks (even without
// FK cascade, e.g. SQLite). Returns the number of removed messages.
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
