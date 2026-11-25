package externalstatus

import (
	"context"
	"log/slog"
	"time"

	"github.com/fraigner/statushafen/models"
)

// Notification about a lasting outage of an
// external source.
//
// Outage = no successful fetch for OutageAfterMinutes (a 304 counts as
// success). A single error or the backoff alone triggers nothing. The state
// lives in the columns outage_since/outage_notified_at/outage_reminded_at of
// the source and is claimed via a conditional UPDATE ("only if not yet set")
// before a mail goes out. As a result, neither the poll tick nor a restart nor
// a second instance sends the same mail twice; in the worst case (crash
// between claiming and sending) a mail is dropped instead of being sent twice.

const (
	// DefaultOutageAfterMinutes is the per-source default, Min/Max the allowed
	// values (admin API).
	DefaultOutageAfterMinutes = 60
	MinOutageAfterMinutes     = 10
	MaxOutageAfterMinutes     = 1440
	// OutageReminderAfter: reminder if the outage still persists this long after
	// the first notification. Exactly one.
	OutageReminderAfter = 24 * time.Hour
)

// OutageKind distinguishes the three mails.
type OutageKind string

// The three outage notifications.
const (
	OutageStarted   OutageKind = "started"
	OutageReminder  OutageKind = "reminder"
	OutageRecovered OutageKind = "recovered"
)

// OutageEvent describes a state change to be reported. Source is a
// snapshot; for OutageRecovered it still carries the start of the ended
// outage in OutageSince.
type OutageEvent struct {
	Kind   OutageKind
	Source models.ExternalSource
}

// OutageNotifier sends the notifications (mail). The call is made
// synchronously from the polling service; slow work belongs in a separate
// goroutine of the recipient.
type OutageNotifier interface {
	SourceOutage(ctx context.Context, ev OutageEvent)
}

// OutageAfter returns a source's grace period, at least MinOutageAfterMinutes
// (legacy data and tests without a set value fall back to the default).
func OutageAfter(src models.ExternalSource) time.Duration {
	m := src.OutageAfterMinutes
	if m <= 0 {
		m = DefaultOutageAfterMinutes
	}
	return time.Duration(max(m, MinOutageAfterMinutes)) * time.Minute
}

// outageReference is the point in time from which the radio silence is
// counted: the last success, for a source without any success its creation.
func outageReference(src models.ExternalSource) time.Time {
	if src.LastSuccessAt != nil {
		return *src.LastSuccessAt
	}
	return src.CreatedAt
}

// evaluateOutage checks a source and sets or clears the outage state;
// on a transition the effective status of the linked components changes
// (a stale status no longer counts), which DiffEffective reports.
// Best effort: DB errors are logged, the next tick tries again.
func (s *Service) evaluateOutage(ctx context.Context, id uint) {
	var src models.ExternalSource
	if err := s.db.First(&src, id).Error; err != nil {
		return
	}
	now := s.now()
	ref := outageReference(src)
	down := !now.Before(ref.Add(OutageAfter(src)))
	switch {
	case down:
		if src.OutageSince == nil && !s.setOutage(ctx, &src, ref) {
			return
		}
		s.notifyOutage(ctx, src, now)
	case src.OutageSince != nil:
		s.clearOutage(ctx, src)
	}
}

// setOutage claims the transition into the outage. false = already set by
// someone else, or DB error.
func (s *Service) setOutage(ctx context.Context, src *models.ExternalSource, since time.Time) bool {
	before := s.snapshot(src)
	res := s.db.Model(&models.ExternalSource{}).Where("id = ? AND outage_since IS NULL", src.ID).Update("outage_since", since)
	if res.Error != nil || res.RowsAffected == 0 {
		return false
	}
	src.OutageSince = &since
	s.diffAfterOutageChange(ctx, src, before)
	return true
}

// clearOutage ends the outage and reports the all-clear - but only if an
// outage mail went out before.
func (s *Service) clearOutage(ctx context.Context, src models.ExternalSource) {
	before := s.snapshot(&src)
	res := s.db.Model(&models.ExternalSource{}).Where("id = ? AND outage_since IS NOT NULL", src.ID).
		Updates(map[string]any{"outage_since": nil, "outage_notified_at": nil, "outage_reminded_at": nil})
	if res.Error != nil || res.RowsAffected == 0 {
		return
	}
	s.diffAfterOutageChange(ctx, &src, before)
	if src.OutageNotifiedAt != nil {
		s.emitOutage(ctx, OutageRecovered, src)
	}
}

// snapshot returns the page's effective statuses before a change; on a DB
// error nil (then there is no event, see DiffEffective).
func (s *Service) snapshot(src *models.ExternalSource) map[uint]string {
	m, err := EffectiveStatuses(s.db, src.StatusPageID)
	if err != nil {
		slog.Error("Externe Statusseite: Status vor Ausfall-Wechsel nicht berechenbar", "source_id", src.ID, "error", err)
	}
	return m
}

func (s *Service) diffAfterOutageChange(ctx context.Context, src *models.ExternalSource, before map[uint]string) {
	after, err := EffectiveStatuses(s.db, src.StatusPageID)
	if err != nil {
		slog.Error("Externe Statusseite: Status nach Ausfall-Wechsel nicht berechenbar", "source_id", src.ID, "error", err)
		return
	}
	DiffEffective(ctx, s.listener, before, after, StatusChange{StatusPageID: src.StatusPageID, Cause: CauseOutage, SourceID: src.ID})
}

// notifyOutage sends (at most once) the outage mail and, after
// OutageReminderAfter, (at most once) the reminder. The timestamps are
// claimed via a conditional UPDATE before sending.
func (s *Service) notifyOutage(ctx context.Context, src models.ExternalSource, now time.Time) {
	if src.OutageNotifiedAt == nil {
		if s.claim(src.ID, "outage_notified_at", now) {
			src.OutageNotifiedAt = &now
			s.emitOutage(ctx, OutageStarted, src)
		}
		return
	}
	if src.OutageRemindedAt == nil && !now.Before(src.OutageNotifiedAt.Add(OutageReminderAfter)) {
		if s.claim(src.ID, "outage_reminded_at", now) {
			src.OutageRemindedAt = &now
			s.emitOutage(ctx, OutageReminder, src)
		}
	}
}

// claim sets column if still empty and the outage still exists.
func (s *Service) claim(id uint, column string, now time.Time) bool {
	res := s.db.Model(&models.ExternalSource{}).
		Where("id = ? AND outage_since IS NOT NULL AND "+column+" IS NULL", id).Update(column, now)
	if res.Error != nil {
		slog.Error("Externe Statusseite: Ausfall-Zeitstempel nicht gespeichert", "source_id", id, "error", res.Error)
		return false
	}
	return res.RowsAffected == 1
}

func (s *Service) emitOutage(ctx context.Context, kind OutageKind, src models.ExternalSource) {
	slog.Warn("Externe Statusseite: Ausfall-Meldung", "source_id", src.ID, "kind", string(kind), "error", src.LastError)
	s.mu.Lock()
	n := s.outage
	s.mu.Unlock()
	if n != nil {
		n.SourceOutage(ctx, OutageEvent{Kind: kind, Source: src})
	}
}

// SetOutageNotifier sets the recipient of the outage events. The handler
// is created after the service (it needs it), hence set afterwards.
func (s *Service) SetOutageNotifier(n OutageNotifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outage = n
}
