package models

import "time"

// Sources of a status change.
const (
	StatusSourceManual  = "manual"  // admin UI / API
	StatusSourceWebhook = "webhook" // monitoring webhook
	// StatusSourceWebhookNachlauf marks the final-state row of a condensed webhook window
	// The anchor (webhook) carries the worst status, the trailing row (Nachlauf) the final state.
	StatusSourceWebhookNachlauf = "webhook_nachlauf"
	StatusSourceMaintenance     = "maintenance" // maintenance window (start/end)
	StatusSourceSelfMonitor     = "selfmonitor" // self-monitoring
	StatusSourceCreated         = "created"     // initial status on creation
	StatusSourceBackfill        = "backfill"    // one-off backfilled data
)

// ComponentStatusChange: an entry in the status history of a
// component. Component.Status is only the current value; only this table
// makes the history evaluable for the 90-day availability. Pure
// append-only table (no soft delete, no changes): cleanup only via the
// retention period (statushistory.Prune). Deliberately without a foreign key and
// without StatusPageID: components are only soft-deleted, and the page is
// always resolved via the component (tenant separation in the query).
type ComponentStatusChange struct {
	ID          uint `gorm:"primaryKey"`
	ComponentID uint `gorm:"not null;index:idx_csc_component_time,priority:1"`
	// OldStatus is empty for the first entry of a component.
	OldStatus string    `gorm:"not null;default:''"`
	NewStatus string    `gorm:"not null"`
	Source    string    `gorm:"not null;default:''"`
	ChangedAt time.Time `gorm:"not null;index:idx_csc_component_time,priority:2;index:idx_csc_changed_at"`
	CreatedAt time.Time
}
