// Package statushistory writes the status history of components
// (table component_status_changes).
//
// Every write path of Component.Status goes through this package: Set for pure
// status changes (also conditional ones, "WHERE status = …"), Lock/Record for paths
// that store further columns in the same transaction. An entry is created
// exactly when the status really changes, in the same transaction
// as the change (no entry without a change, no change without an entry).
package statushistory

import (
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Retention is the retention period. Availability shows 90 days; one
// year leaves room for later longer analyses and costs hardly any storage
// (one row per status change). Operator decision open.
const Retention = 365 * 24 * time.Hour

// MergeGap is the length of a compaction window for webhook changes of the same
// component. The window begins with an ANCHOR (source webhook) and
// lasts MergeGap from its time. All further changes within it are compacted into at most TWO
// rows: the anchor carries the WORST status of the window (no
// outage is ever lost), a TRAILER (source webhook_nachlauf) carries the final state,
// if it differs from the worst. Nothing is ever deleted that could
// hide an outage; only the trailer is omitted when the final state is the worst.
// 30 s is longer than any sensible check interval of a monitor (typically
// >= 60 s), so it only merges flapping and limits the table to at most
// 5,760 rows per component and day (instead of 20/min per IP). The status change itself is
// unaffected, only the history is compacted.
//
// Deviation of availability: PESSIMISTIC. The whole window (at most MergeGap) counts
// as the worst status, even if the service ran in between; never as better.
// Changes with normal spacing are not affected. (Previously the switch back
// A→B→A deleted the row entirely: repeated, that yielded 0 rows for a real outage, review B1.)
const MergeGap = 30 * time.Second

const groupStatus = "group"

// Set sets the status of a component and writes the entry, in one
// transaction. from != "" makes the change conditional (only if the status is still
// from, as with self-monitoring). Groups are never touched. Returns
// changed=false if there was nothing to do (same status, condition
// not met, component missing/group).
func Set(db *gorm.DB, componentID uint, from, to, source string, at time.Time) (bool, error) {
	changed := false
	err := db.Transaction(func(tx *gorm.DB) error {
		cur, err := Lock(tx, componentID)
		if err != nil || cur == "" || cur == groupStatus || cur == to || (from != "" && cur != from) {
			return err
		}
		res := tx.Model(&models.Component{}).Where("id = ? AND status = ?", componentID, cur).Update("status", to)
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		changed = true
		return Record(tx, componentID, cur, to, source, at)
	})
	return changed && err == nil, err
}

// Lock reads the current status of the (not deleted) component and locks
// the row until the end of the transaction (Postgres: FOR UPDATE; SQLite
// ignores the clause). If the component is missing, the status is empty.
func Lock(tx *gorm.DB, componentID uint) (string, error) {
	var rows []models.Component
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").
		Where("id = ?", componentID).Limit(1).Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return "", err
	}
	return rows[0].Status, nil
}

// Record writes an entry if the status changes (old != new).
// Groups have no history. The caller invokes it in the transaction
// that writes the status.
func Record(tx *gorm.DB, componentID uint, oldStatus, newStatus, source string, at time.Time) error {
	if oldStatus == newStatus || newStatus == groupStatus || oldStatus == groupStatus {
		return nil
	}
	if source == models.StatusSourceWebhook {
		merged, err := mergeWebhook(tx, componentID, oldStatus, newStatus, at.UTC())
		if err != nil || merged {
			return err
		}
	}
	return tx.Create(&models.ComponentStatusChange{
		ComponentID: componentID, OldStatus: oldStatus, NewStatus: newStatus,
		Source: source, ChangedAt: at.UTC(),
	}).Error
}

// mergeWebhook compacts (see MergeGap): if a window exists (latest row is
// a webhook anchor or a trailer with its anchor, and the change comes less than MergeGap
// after the anchor), the anchor is set to the worse status and the trailer is brought to
// the final state (created, overwritten or removed if the final state equals the anchor).
// The caller holds the row lock (Lock). Returns false if no window exists
// (the caller then creates a new anchor).
func mergeWebhook(tx *gorm.DB, componentID uint, oldStatus, newStatus string, at time.Time) (bool, error) {
	var last []models.ComponentStatusChange
	if err := tx.Where("component_id = ?", componentID).Order("changed_at DESC, id DESC").Limit(2).Find(&last).Error; err != nil {
		return false, err
	}
	if len(last) == 0 {
		return false, nil
	}
	var anchor models.ComponentStatusChange
	var tail *models.ComponentStatusChange
	switch last[0].Source {
	case models.StatusSourceWebhook:
		anchor = last[0]
	case models.StatusSourceWebhookNachlauf:
		// A trailer always belongs to the anchor before it; if that is missing, the state is unclear: no merge.
		if len(last) < 2 || last[1].Source != models.StatusSourceWebhook || last[1].NewStatus != last[0].OldStatus {
			return false, nil
		}
		anchor = last[1]
		tail = &last[0]
	default:
		return false, nil
	}
	aktuell := anchor.NewStatus
	if tail != nil {
		aktuell = tail.NewStatus
	}
	d := at.Sub(anchor.ChangedAt)
	if aktuell != oldStatus || d < 0 || d >= MergeGap {
		return false, nil
	}
	schlimmster := componentstatus.Worst(anchor.NewStatus, newStatus)
	if schlimmster != anchor.NewStatus {
		if err := tx.Model(&models.ComponentStatusChange{}).Where("id = ?", anchor.ID).Update("new_status", schlimmster).Error; err != nil {
			return true, err
		}
	}
	switch {
	case newStatus == schlimmster:
		// Final state is the worst: a trailer is not needed.
		if tail != nil {
			return true, tx.Delete(&models.ComponentStatusChange{}, tail.ID).Error
		}
		return true, nil
	case tail != nil:
		return true, tx.Model(&models.ComponentStatusChange{}).Where("id = ?", tail.ID).
			Updates(map[string]any{"old_status": schlimmster, "new_status": newStatus, "changed_at": at}).Error
	default:
		return true, tx.Create(&models.ComponentStatusChange{
			ComponentID: componentID, OldStatus: schlimmster, NewStatus: newStatus,
			Source: models.StatusSourceWebhookNachlauf, ChangedAt: at,
		}).Error
	}
}

// Backfill enters the current status for every component without history.
// The timestamp is that of the backfill, NOT the creation: when an
// existing status really began is unknown, and backdating would
// count a current outage as downtime over weeks. Idempotent
// (NOT EXISTS), groups and deleted components are left out.
func Backfill(db *gorm.DB, now time.Time) error {
	return db.Exec(`INSERT INTO component_status_changes (component_id, old_status, new_status, source, changed_at, created_at)
SELECT c.id, '', c.status, ?, ?, ? FROM components c
WHERE c.deleted_at IS NULL AND c.status <> ? AND NOT EXISTS (SELECT 1 FROM component_status_changes h WHERE h.component_id = c.id)`,
		models.StatusSourceBackfill, now.UTC(), now.UTC(), groupStatus).Error
}

// Prune deletes entries older than the retention period, but keeps for each
// component the newest of them: it is the initial status at the start
// of the analysis window. Returns the number of deleted rows.
func Prune(db *gorm.DB, now time.Time) (int64, error) {
	cutoff := now.UTC().Add(-Retention)
	// The newest old entry per component by (changed_at, id) is kept.
	res := db.Exec(`DELETE FROM component_status_changes WHERE changed_at < ? AND EXISTS (
SELECT 1 FROM component_status_changes n WHERE n.component_id = component_status_changes.component_id AND n.changed_at < ?
AND (n.changed_at > component_status_changes.changed_at OR (n.changed_at = component_status_changes.changed_at AND n.id > component_status_changes.id)))`, cutoff, cutoff)
	return res.RowsAffected, res.Error
}
