package externalstatus

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/models"
)

// Retention of mirrored reports. Finished incidents and
// maintenances (Closed) would otherwise grow without bound, although the API
// only shows them in the window days (default 30, public 14). Open reports are
// never removed.
const (
	// RetentionDaysEnv overrides the retention period (days since
	// completion); invalid or non-positive values yield the default.
	RetentionDaysEnv = "EXTERNAL_INCIDENT_RETENTION_DAYS"
	// DefaultRetentionDays: operator decision.
	DefaultRetentionDays = 90

	// pruneBatchSize limits the rows per DELETE (short transactions).
	pruneBatchSize = 200
	// pruneEvery: the cleanup run does not need every 15 s tick.
	pruneEvery = time.Hour
)

// RetentionFromEnv reads RetentionDaysEnv.
func RetentionFromEnv(getenv func(string) string) time.Duration {
	days, err := strconv.Atoi(strings.TrimSpace(getenv(RetentionDaysEnv)))
	if err != nil || days <= 0 {
		days = DefaultRetentionDays
	}
	return time.Duration(days) * 24 * time.Hour
}

// RetentionReporter is optional on the IncidentNotifier: it receives the
// number of removed reports (audit entry).
type RetentionReporter interface {
	MirroredIncidentsPruned(ctx context.Context, count int, retention time.Duration)
}

// endedExpr: time of completion. Third-party sites do not always supply
// resolved_at; then the third-party site's last update counts, finally the
// last fetch that saw a change.
const endedExpr = "COALESCE(resolved_at, external_updated_at, last_seen_at)"

// endedAt corresponds to endedExpr for a feed report.
func endedAt(it Incident, now time.Time) time.Time {
	if it.ResolvedAt != nil {
		return *it.ResolvedAt
	}
	if it.UpdatedAt != nil {
		return *it.UpdatedAt
	}
	return now
}

// pastRetention: finished report that is older than the retention. The
// feed keeps delivering it (last 50 entries); without this filter it would be
// re-created on every fetch after cleanup.
func (s *Service) pastRetention(it Incident, now time.Time) bool {
	return it.Closed && endedAt(it, now).Before(now.Add(-s.retention))
}

// PruneMirrored removes finished mirrored reports that have been finished
// longer than the retention, together with updates and components (permanently,
// in batches). Returns the number of removed reports.
func (s *Service) PruneMirrored(ctx context.Context) (int, error) {
	cutoff := s.now().Add(-s.retention)
	total := 0
	for ctx.Err() == nil {
		var ids []uint
		if err := s.db.Unscoped().Model(&models.ExternalIncident{}).
			Where("closed = ? AND "+endedExpr+" < ?", true, cutoff).
			Order("id").Limit(pruneBatchSize).Pluck("id", &ids).Error; err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}
		if err := s.db.Unscoped().Where("external_incident_id IN ?", ids).Delete(&models.ExternalIncidentUpdate{}).Error; err != nil {
			return total, err
		}
		if err := s.db.Unscoped().Where("external_incident_id IN ?", ids).Delete(&models.ExternalIncidentComponent{}).Error; err != nil {
			return total, err
		}
		if err := s.db.Unscoped().Where("id IN ?", ids).Delete(&models.ExternalIncident{}).Error; err != nil {
			return total, err
		}
		total += len(ids)
	}
	return total, nil
}

// maybePrune runs on the leader tick (advisory lock held), at most
// once per pruneEvery.
func (s *Service) maybePrune(ctx context.Context) {
	now := s.now()
	s.mu.Lock()
	if !s.lastPrune.IsZero() && now.Sub(s.lastPrune) < pruneEvery {
		s.mu.Unlock()
		return
	}
	s.lastPrune = now
	n := s.incidents
	s.mu.Unlock()
	s.pruneAdminNotifications(ctx)
	count, err := s.PruneMirrored(ctx)
	if err != nil {
		slog.Error("Externe Statusseiten: Aufräumen gespiegelter Meldungen fehlgeschlagen", "error", err)
	}
	if count == 0 {
		return
	}
	slog.Info("Externe Statusseiten: beendete gespiegelte Meldungen entfernt", "count", count, "retention", s.retention)
	if r, ok := n.(RetentionReporter); ok {
		r.MirroredIncidentsPruned(ctx, count, s.retention)
	}
}
