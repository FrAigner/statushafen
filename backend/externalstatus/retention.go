package externalstatus

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/models"
)

// Aufbewahrung gespiegelter Meldungen. Beendete Vorfälle und
// Wartungen (Closed) wachsen sonst unbegrenzt, obwohl die API sie nur im
// Fenster days (Standard 30, öffentlich 14) zeigt. Offene Meldungen werden
// nie entfernt.
const (
	// RetentionDaysEnv überschreibt die Aufbewahrungsdauer (Tage seit
	// Beendigung); ungültige oder nicht positive Werte ergeben den Default.
	RetentionDaysEnv = "EXTERNAL_INCIDENT_RETENTION_DAYS"
	// DefaultRetentionDays: Betreiberentscheidung.
	DefaultRetentionDays = 90

	// pruneBatchSize begrenzt die Zeilen je DELETE (kurze Transaktionen).
	pruneBatchSize = 200
	// pruneEvery: Der Aufräumlauf braucht nicht jeden 15-s-Takt.
	pruneEvery = time.Hour
)

// RetentionFromEnv liest RetentionDaysEnv.
func RetentionFromEnv(getenv func(string) string) time.Duration {
	days, err := strconv.Atoi(strings.TrimSpace(getenv(RetentionDaysEnv)))
	if err != nil || days <= 0 {
		days = DefaultRetentionDays
	}
	return time.Duration(days) * 24 * time.Hour
}

// RetentionReporter ist optional auf dem IncidentNotifier: Er bekommt die
// Anzahl entfernter Meldungen (Audit-Eintrag).
type RetentionReporter interface {
	MirroredIncidentsPruned(ctx context.Context, count int, retention time.Duration)
}

// endedExpr: Zeitpunkt der Beendigung. Fremdseiten liefern nicht immer
// resolved_at; dann zählt das letzte Update der Fremdseite, zuletzt der
// letzte Abruf, der eine Änderung sah.
const endedExpr = "COALESCE(resolved_at, external_updated_at, last_seen_at)"

// endedAt entspricht endedExpr für eine Feed-Meldung.
func endedAt(it Incident, now time.Time) time.Time {
	if it.ResolvedAt != nil {
		return *it.ResolvedAt
	}
	if it.UpdatedAt != nil {
		return *it.UpdatedAt
	}
	return now
}

// pastRetention: beendete Meldung, die älter als die Aufbewahrung ist. Der
// Feed liefert sie weiter (letzte 50 Einträge); ohne diesen Filter würde sie
// nach dem Aufräumen bei jedem Abruf neu angelegt.
func (s *Service) pastRetention(it Incident, now time.Time) bool {
	return it.Closed && endedAt(it, now).Before(now.Add(-s.retention))
}

// PruneMirrored entfernt beendete gespiegelte Meldungen, die länger als die
// Aufbewahrung beendet sind, samt Updates und Komponenten (endgültig, in
// Batches). Liefert die Anzahl entfernter Meldungen.
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

// maybePrune läuft im Leader-Takt (Advisory-Sperre gehalten), höchstens
// einmal je pruneEvery.
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
