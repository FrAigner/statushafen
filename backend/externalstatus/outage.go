package externalstatus

import (
	"context"
	"log/slog"
	"time"

	"github.com/fraigner/statushafen/models"
)

// Meldung bei dauerhaftem Ausfall einer
// externen Quelle.
//
// Ausfall = seit OutageAfterMinutes kein erfolgreicher Abruf mehr (ein 304
// zählt als Erfolg). Ein einzelner Fehler oder der Backoff allein löst nichts
// aus. Der Zustand steckt in den Spalten outage_since/outage_notified_at/
// outage_reminded_at der Quelle und wird per bedingtem UPDATE ("nur wenn noch
// nicht gesetzt") beansprucht, bevor eine Mail rausgeht. Dadurch verschickt
// weder der Poll-Takt noch ein Neustart noch eine zweite Instanz dieselbe
// Mail doppelt; im ungünstigsten Fall (Absturz zwischen Beanspruchen und
// Versand) fällt eine Mail aus statt doppelt zu kommen.

const (
	// DefaultOutageAfterMinutes ist der Standard je Quelle, Min/Max die
	// erlaubten Werte (Admin-API).
	DefaultOutageAfterMinutes = 60
	MinOutageAfterMinutes     = 10
	MaxOutageAfterMinutes     = 1440
	// OutageReminderAfter: Erinnerung, wenn der Ausfall so lange nach der
	// ersten Meldung noch anhält. Genau eine.
	OutageReminderAfter = 24 * time.Hour
)

// OutageKind unterscheidet die drei Mails.
type OutageKind string

// Die drei Ausfall-Meldungen.
const (
	OutageStarted   OutageKind = "started"
	OutageReminder  OutageKind = "reminder"
	OutageRecovered OutageKind = "recovered"
)

// OutageEvent beschreibt eine zu meldende Zustandsänderung. Source ist ein
// Schnappschuss; bei OutageRecovered trägt er noch den Beginn des beendeten
// Ausfalls in OutageSince.
type OutageEvent struct {
	Kind   OutageKind
	Source models.ExternalSource
}

// OutageNotifier verschickt die Meldungen (Mail). Der Aufruf kommt
// synchron aus dem Abfrage-Dienst; langsame Arbeit gehört in eine eigene
// Goroutine des Empfängers.
type OutageNotifier interface {
	SourceOutage(ctx context.Context, ev OutageEvent)
}

// OutageAfter liefert die Frist einer Quelle, mindestens MinOutageAfterMinutes
// (Altdaten und Tests ohne gesetzten Wert fallen auf den Standard).
func OutageAfter(src models.ExternalSource) time.Duration {
	m := src.OutageAfterMinutes
	if m <= 0 {
		m = DefaultOutageAfterMinutes
	}
	return time.Duration(max(m, MinOutageAfterMinutes)) * time.Minute
}

// outageReference ist der Zeitpunkt, ab dem die Funkstille gezählt wird: der
// letzte Erfolg, bei einer Quelle ohne jeden Erfolg ihre Anlage.
func outageReference(src models.ExternalSource) time.Time {
	if src.LastSuccessAt != nil {
		return *src.LastSuccessAt
	}
	return src.CreatedAt
}

// evaluateOutage prüft eine Quelle und setzt bzw. löscht den Ausfallzustand;
// bei einem Übergang ändert sich der effektive Status der gekoppelten
// Komponenten (veralteter Status zählt nicht mehr), das meldet DiffEffective.
// Best effort: DB-Fehler werden geloggt, der nächste Takt versucht es erneut.
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

// setOutage beansprucht den Übergang in den Ausfall. false = schon durch
// jemand anderen gesetzt oder DB-Fehler.
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

// clearOutage beendet den Ausfall und meldet die Entwarnung - aber nur, wenn
// zuvor eine Ausfall-Mail rausging.
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

// snapshot liefert die effektiven Status der Seite vor einem Wechsel; bei
// einem DB-Fehler nil (dann gibt es kein Ereignis, siehe DiffEffective).
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

// notifyOutage verschickt (höchstens einmal) die Ausfall-Mail und nach
// OutageReminderAfter (höchstens einmal) die Erinnerung. Die Zeitstempel
// werden vor dem Versand per bedingtem UPDATE beansprucht.
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

// claim setzt column, falls noch leer und der Ausfall noch besteht.
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

// SetOutageNotifier setzt den Empfänger der Ausfall-Ereignisse. Der Handler
// entsteht nach dem Dienst (er braucht ihn), deshalb nachträglich.
func (s *Service) SetOutageNotifier(n OutageNotifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outage = n
}
