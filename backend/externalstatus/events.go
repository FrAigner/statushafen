package externalstatus

import (
	"context"
	"log/slog"
)

// Ursachen eines StatusChange.
const (
	CauseFetch  = "fetch"  // Abruf einer Quelle hat externe Status geändert
	CauseLink   = "link"   // Kopplung angelegt
	CauseUnlink = "unlink" // Kopplung entfernt
	CauseSource = "source" // Quelle geändert (aktiv/inaktiv) oder gelöscht
	CauseOutage = "outage" // Quelle ausgefallen bzw. wieder erreichbar
)

// StatusChange: Der effektive Status einer eigenen Komponente hat sich durch
// externe Komponenten geändert. Teil B hängt hier die
// Benachrichtigungen an; Teil A loggt nur.
type StatusChange struct {
	StatusPageID uint
	ComponentID  uint
	Previous     string
	Current      string
	Cause        string
	// SourceID: auslösende Quelle (0, wenn unbekannt).
	SourceID uint
}

// ChangeListener empfängt StatusChange-Ereignisse. Aufrufe kommen synchron
// aus dem Abfrage-Dienst bzw. dem Admin-Handler; langsame Arbeit (Mails)
// gehört in eine eigene Goroutine des Listeners.
type ChangeListener interface {
	EffectiveStatusChanged(ctx context.Context, change StatusChange)
}

// LogListener ist der Standard-Listener in Teil A: nur ein Logeintrag.
type LogListener struct{}

// EffectiveStatusChanged implementiert ChangeListener.
func (LogListener) EffectiveStatusChanged(_ context.Context, ch StatusChange) {
	slog.Info("Effektiver Komponentenstatus geändert (extern)",
		"status_page_id", ch.StatusPageID, "component_id", ch.ComponentID,
		"previous", ch.Previous, "current", ch.Current, "cause", ch.Cause, "source_id", ch.SourceID)
}

// DiffEffective vergleicht zwei Ergebnisse von EffectiveStatuses und meldet
// jede Komponente, deren Status sich geändert hat, an listener. Komponenten,
// die nur in einem der beiden Stände vorkommen (gerade angelegt/gelöscht),
// sind kein externes Ereignis und werden übergangen.
func DiffEffective(ctx context.Context, listener ChangeListener, before, after map[uint]string, base StatusChange) {
	for id, cur := range after {
		prev, ok := before[id]
		if !ok || prev == cur {
			continue
		}
		ch := base
		ch.ComponentID, ch.Previous, ch.Current = id, prev, cur
		listener.EffectiveStatusChanged(ctx, ch)
	}
}
