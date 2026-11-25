package externalstatus

import (
	"context"
	"log/slog"
)

// Causes of a StatusChange.
const (
	CauseFetch  = "fetch"  // Fetching a source changed external statuses
	CauseLink   = "link"   // Link created
	CauseUnlink = "unlink" // Link removed
	CauseSource = "source" // Source changed (active/inactive) or deleted
	CauseOutage = "outage" // Source went down or became reachable again
)

// StatusChange: the effective status of an own component changed because of
// external components. Part B attaches the notifications here; part A only
// logs.
type StatusChange struct {
	StatusPageID uint
	ComponentID  uint
	Previous     string
	Current      string
	Cause        string
	// SourceID: triggering source (0 if unknown).
	SourceID uint
}

// ChangeListener receives StatusChange events. Calls are made synchronously
// from the polling service or the admin handler; slow work (mails) belongs in
// a separate goroutine of the listener.
type ChangeListener interface {
	EffectiveStatusChanged(ctx context.Context, change StatusChange)
}

// LogListener is the default listener in part A: just a log entry.
type LogListener struct{}

// EffectiveStatusChanged implements ChangeListener.
func (LogListener) EffectiveStatusChanged(_ context.Context, ch StatusChange) {
	slog.Info("Effektiver Komponentenstatus geändert (extern)",
		"status_page_id", ch.StatusPageID, "component_id", ch.ComponentID,
		"previous", ch.Previous, "current", ch.Current, "cause", ch.Cause, "source_id", ch.SourceID)
}

// DiffEffective compares two results of EffectiveStatuses and reports every
// component whose status changed to listener. Components that appear in only
// one of the two snapshots (just created/deleted) are not an external event
// and are skipped.
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
