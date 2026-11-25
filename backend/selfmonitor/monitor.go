// Package selfmonitor monitors Statushafen itself and sets the
// result as component status on the system page.
//
// Flow per tick: every due check (Check) returns ok/failed/not
// checkable. Only after Threshold consecutive failures (debouncing) is the
// associated component set to the check's outage status; if the
// outage persists for IncidentAfter, the monitor additionally creates an incident. On
// recovery the component goes back to "operational" and the incident is
// closed. Every step is in the audit log (actor "system").
//
// The monitor only touches a component if it is "operational" (e.g.
// no overwriting of a maintenance or a manually set status) or
// it set it to outage itself. This "belongs to the monitor" is stored
// durably in SystemSetting (key selfmonitor.failing.<slug>), so that a
// restart in the middle of an outage leaves nothing orphaned.
package selfmonitor

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/statushistory"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const recoveredReason = "Die automatische Selbstüberwachung meldet: wieder erreichbar."

const (
	// DefaultInterval is the loop's tick.
	DefaultInterval = time.Minute
	// DefaultThreshold: consecutive failures before the status flips.
	DefaultThreshold = 3
	// DefaultIncidentAfter: how long the outage must persist until an incident is created.
	DefaultIncidentAfter = 15 * time.Minute

	failingKeyPrefix  = "selfmonitor.failing."
	incidentKeyPrefix = "selfmonitor.incident."
	systemActor       = "system"
)

// Result is the result of a check. Skip means "not checkable" (e.g.
// sending switched off, measurement file missing): the state stays unchanged.
type Result struct {
	OK   bool
	Skip bool
	// Off: the check is switched off (e.g. mail sending off). Unlike Skip,
	// the monitor releases an outage it set: otherwise
	// status, marker and open incident would remain forever.
	Off    bool
	Detail string
}

// Check is a check together with the component it controls.
type Check struct {
	// Slug of the component on the system page (database.CapabilitySlug).
	Slug string
	Name string
	// FailedStatus is the component status on failure; also the
	// severity of the automatic incident (degraded_performance|partial_outage|major_outage).
	FailedStatus string
	// Every: check only every n-th tick (0/1 = every time).
	Every int
	Run   func(ctx context.Context) Result
}

// Monitor runs the checks and writes the results.
type Monitor struct {
	DB     *gorm.DB
	Checks []Check
	// Enabled returns the switch; a read error is NOT "off":
	// the tick is skipped, nothing is released. nil = always on.
	Enabled       func() (bool, error)
	Threshold     int
	IncidentAfter time.Duration
	Interval      time.Duration
	Now           func() time.Time

	mu    sync.Mutex
	fails map[string]int
	tick  int
}

// New returns a monitor with the default values.
func New(db *gorm.DB, checks []Check, enabled func() (bool, error)) *Monitor {
	return &Monitor{
		DB: db, Checks: checks, Enabled: enabled,
		Threshold: DefaultThreshold, IncidentAfter: DefaultIncidentAfter,
		Interval: DefaultInterval, Now: time.Now,
		fails: map[string]int{},
	}
}

// Start starts the loop (first tick after one interval, so that the
// own HTTP server is already listening at startup) and returns the stop function.
func (m *Monitor) Start(ctx context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(m.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.RunOnce(ctx)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// neutralizeEvery: every n-th tick (minutes) cleans up incident texts of hidden
// components (review).
const neutralizeEvery = 10

// RunOnce is one tick.
func (m *Monitor) RunOnce(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tick++
	if m.tick%neutralizeEvery == 0 {
		// Review: if a component is hidden/deleted/moved only after a regular
		// recovery, the incident no longer belongs to the monitor
		// and would remain public with names until the next start. Before the
		// switch check, because the texts are public (like the backfill at
		// startup); idempotent and one query round per 10 ticks. Errors: logged,
		// nothing written, the tick continues.
		if err := NeutralizeHiddenIncidents(m.DB); err != nil {
			slog.Error("Selbstüberwachung: Vorfalltexte nicht bereinigt", "error", err)
		}
	}
	if m.Enabled != nil {
		on, err := m.Enabled()
		if err != nil {
			// A flaky DB must not release a real outage.
			slog.Warn("Selbstüberwachung: Schalter nicht lesbar, Takt entfällt", "error", err)
			return
		}
		if !on {
			m.fails = map[string]int{} // do not carry the counter across a switch-off
			m.releaseAll(offReason)
			return
		}
	}
	var page models.StatusPage
	if err := m.DB.Where("is_system = ?", true).First(&page).Error; err != nil {
		slog.Warn("Selbstüberwachung: keine Systemseite", "error", err)
		return
	}
	for _, c := range m.Checks {
		if c.Every > 1 && (m.tick-1)%c.Every != 0 {
			continue
		}
		m.apply(page.ID, c, c.Run(ctx))
	}
}

// offReason is the message in the incident when the monitor releases an outage because of
// a switch-off (instead of because of recovery).
const offReason = "Automatische Verfolgung beendet."

// hiddenReason: closing text when the component was archived/hidden,
// deliberately without the component name, the incident is public.
const hiddenReason = "Automatische Verfolgung beendet. Die Komponente wird nicht mehr angezeigt."

// releaseAll releases all outages set by the monitor when
// monitoring was switched off. Without markers it costs only one query
// per check.
func (m *Monitor) releaseAll(reason string) {
	var page models.StatusPage
	if err := m.DB.Where("is_system = ?", true).First(&page).Error; err != nil {
		return
	}
	for _, c := range m.Checks {
		_, owned, err := m.getValue(failingKeyPrefix + c.Slug)
		if err != nil {
			logReadError(failingKeyPrefix+c.Slug, err)
			continue
		}
		if !owned {
			continue
		}
		var comp models.Component
		if err := m.DB.Where("status_page_id = ? AND slug = ?", page.ID, c.Slug).First(&comp).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				m.releaseOrphan(page.ID, c) // component deleted
			}
			continue
		}
		m.recover(c, comp, reason)
	}
}

func (m *Monitor) apply(pageID uint, c Check, res Result) {
	var comp models.Component
	if err := m.DB.Where("status_page_id = ? AND slug = ?", pageID, c.Slug).First(&comp).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Component deleted by the admin. If the monitor had occupied it,
			// marker and open incident would otherwise remain forever.
			// Also on "not checkable" (Skip), otherwise the orphan remains
			// as long as the check never yields a result again.
			m.releaseOrphan(pageID, c)
		}
		return
	}
	hidden, ok := m.publiclyHidden(pageID, c, comp)
	if !ok {
		return
	}
	if hidden {
		// Publicly hidden components (archived, invisible
		// or a descendant thereof, same rule as the status page) are not
		// evaluated: a public incident names the check name (c.Name)
		// and belongs to a component that does not exist publicly. An
		// ongoing own outage is released (also on Skip); without ownership
		// nothing happens.
		m.fails[c.Slug] = 0
		m.recover(c, comp, hiddenReason)
		return
	}
	if res.Skip {
		return
	}
	if res.Off {
		m.fails[c.Slug] = 0
		m.recover(c, comp, offReason)
		return
	}
	if res.OK {
		m.fails[c.Slug] = 0
		m.recover(c, comp, recoveredReason)
		return
	}
	m.fails[c.Slug]++
	if m.fails[c.Slug] >= m.Threshold {
		m.fail(pageID, c, comp, res.Detail)
	}
}

// publiclyHidden applies the public rule (models.HiddenComponentIDs, incl.
// descendants) to the components of the system page. ok=false: list not
// readable, the step is skipped (the next tick repeats it).
func (m *Monitor) publiclyHidden(pageID uint, c Check, comp models.Component) (hidden, ok bool) {
	var all []models.Component
	if err := m.DB.Where("status_page_id = ?", pageID).Find(&all).Error; err != nil {
		slog.Error("Selbstüberwachung: Komponentenliste nicht lesbar, Schritt entfällt", "slug", c.Slug, "error", err)
		return false, false
	}
	return models.HiddenComponentIDs(all)[comp.ID], true
}

func (m *Monitor) fail(pageID uint, c Check, comp models.Component, detail string) {
	now := m.Now()
	since, owned, err := m.getTime(failingKeyPrefix + c.Slug)
	if err != nil {
		logReadError(failingKeyPrefix+c.Slug, err) // step is skipped, the next tick repeats it
		return
	}
	if !owned {
		if comp.Status != componentstatus.Operational {
			return // set manually (maintenance, admin): do not overwrite
		}
		// Status and ownership marker in ONE transaction. If the
		// marker fails, the component would otherwise remain in outage without an owner.
		err = m.DB.Transaction(func(tx *gorm.DB) error {
			// Conditional update: a status set by the admin in the meantime
			// (e.g. maintenance) is not overwritten.
			// Status and history entry via statushistory (conditional).
			changed, err := statushistory.Set(tx, comp.ID, componentstatus.Operational, c.FailedStatus, models.StatusSourceSelfMonitor, now)
			if err != nil {
				return err
			}
			if !changed {
				return errStaleStatus
			}
			return setValue(tx, failingKeyPrefix+c.Slug, now.UTC().Format(time.RFC3339))
		})
		if errors.Is(err, errStaleStatus) {
			return
		}
		if err != nil {
			slog.Error("Selbstüberwachung: Status nicht gesetzt", "slug", c.Slug, "error", err)
			return
		}
		m.audit("selfmonitor.component_failed", comp, c.FailedStatus+": "+detail)
		since = now
	}
	_, has, err := m.getValue(incidentKeyPrefix + c.Slug)
	if err != nil {
		// A read error is NOT "no marker", otherwise a
		// second incident would arise and the first would remain publicly open.
		logReadError(incidentKeyPrefix+c.Slug, err)
		return
	}
	if has || now.Sub(since) < m.IncidentAfter {
		return
	}
	m.openIncident(pageID, c, comp, since, detail)
}

// incidentTitle and incidentMessagePrefix form the title and message of the
// automatic incident. neutralizeIncident recognizes from them the
// unchanged texts of the monitor, hence in ONE place.
func incidentTitle(c Check) string { return "Störung: " + c.Name }

func incidentMessagePrefix(c Check) string { return incidentMessageHead + c.Name + " (" }

// incidentMessageHead is the fixed beginning of the message (the backfill
// reads the check name back from it).
const incidentMessageHead = "Die automatische Selbstüberwachung meldet eine Störung bei "

func (m *Monitor) openIncident(pageID uint, c Check, comp models.Component, since time.Time, detail string) {
	msg := incidentMessagePrefix(c) + detail + ")."
	inc := models.Incident{
		StatusPageID: pageID, Title: incidentTitle(c), Message: msg,
		Status: "investigating", Severity: c.FailedStatus, StartedAt: &since,
	}
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		// Review: read the outage marker again in the transaction (row lock
		// in Postgres, SQLite ignores it): if another replica has meanwhile
		// saved the recovery, no incident is created that nobody would ever close.
		var own models.SystemSetting
		lerr := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).Where("key = ?", failingKeyPrefix+c.Slug).First(&own).Error
		if errors.Is(lerr, gorm.ErrRecordNotFound) {
			return errNotOwned
		}
		if lerr != nil {
			return lerr
		}
		if err := tx.Create(&inc).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID}).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.IncidentUpdate{IncidentID: inc.ID, Status: "investigating", Message: msg, AuthorEmail: systemActor}).Error; err != nil {
			return err
		}
		// The incident marker belongs in the same transaction. If
		// it fails, a new incident would otherwise arise on every tick.
		// Insert with DO NOTHING instead of upsert: if the marker already exists
		// (second replica, skipped read), it is rolled back instead of
		// orphaning the first incident.
		mark := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoNothing: true}).
			Create(&models.SystemSetting{Key: incidentKeyPrefix + c.Slug, Value: strconv.FormatUint(uint64(inc.ID), 10)})
		if mark.Error != nil {
			return mark.Error
		}
		if mark.RowsAffected == 0 {
			return errIncidentExists
		}
		return nil
	})
	if errors.Is(err, errNotOwned) {
		return
	}
	if errors.Is(err, errIncidentExists) {
		slog.Warn("Selbstüberwachung: Vorfall-Marke existiert bereits, kein zweiter Vorfall", "slug", c.Slug)
		return
	}
	if err != nil {
		slog.Error("Selbstüberwachung: Vorfall nicht angelegt", "slug", c.Slug, "error", err)
		return
	}
	m.audit("selfmonitor.incident_opened", comp, "Vorfall "+strconv.FormatUint(uint64(inc.ID), 10))
}

// errStaleStatus: the status has changed since reading (admin).
var errStaleStatus = errors.New("komponentenstatus zwischenzeitlich geändert")

// errNotOwned: the outage marker is now missing (recovery/release saved).
var errNotOwned = errors.New("störungs-marke fehlt")

// errIncidentExists: the incident marker already exists.
var errIncidentExists = errors.New("vorfall-marke existiert bereits")

// orphanReason is the message in the incident when the component was deleted.
const orphanReason = "Die überwachte Komponente ist nicht mehr vorhanden. Automatische Verfolgung beendet."

// releaseOrphan cleans up when the component was deleted while the
// monitor owned it: close the incident, delete both markers, all
// in ONE transaction; a failure is repeated on the next tick.
func (m *Monitor) releaseOrphan(pageID uint, c Check) {
	var incID string
	released, closed := false, false
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		_, owned, err := readValue(tx, failingKeyPrefix+c.Slug)
		if err != nil || !owned {
			return err
		}
		released = true
		raw, has, err := readValue(tx, incidentKeyPrefix+c.Slug)
		if err != nil {
			return err
		}
		if has {
			incID = raw
			var rerr error
			if closed, rerr = resolveIncident(tx, pageID, m.Now(), raw, orphanReason); rerr != nil {
				return rerr
			}
			// The incident of a deleted or moved
			// component also remains public in the history (as).
			if err := neutralizeIncident(tx, pageID, raw, c); err != nil {
				return err
			}
			if err := tx.Unscoped().Where("key = ?", incidentKeyPrefix+c.Slug).Delete(&models.SystemSetting{}).Error; err != nil {
				return err
			}
		}
		return tx.Unscoped().Where("key = ?", failingKeyPrefix+c.Slug).Delete(&models.SystemSetting{}).Error
	})
	if err != nil {
		slog.Error("Selbstüberwachung: Aufräumen nicht gespeichert", "slug", c.Slug, "incident", incID, "error", err)
		return
	}
	if released {
		comp, where := m.identify(pageID, c.Slug, incID)
		details := "Komponente nicht mehr auf der Systemseite"
		switch where {
		case compDeleted:
			details = "Komponente gelöscht"
		case compMoved:
			details = "Komponente auf eine andere Seite verschoben"
		}
		if closed {
			details += "; Vorfall " + incID + " geschlossen"
		}
		if where == compMoved {
			// The component is known by ID and still carries the
			// outage status the monitor set: that belongs to it, it resets
			// it conditionally (like recover). Any other status stays.
			changed, serr := statushistory.Set(m.DB, comp.ID, c.FailedStatus, componentstatus.Operational, models.StatusSourceSelfMonitor, m.Now())
			if serr == nil && changed {
				details += "; Status zurückgesetzt"
			} else {
				details += "; Status unverändert"
			}
		}
		m.audit("selfmonitor.component_released", comp, details)
	}
}

const (
	compUnknown = iota // identity not known
	compDeleted        // deleted on the system page
	compMoved          // moved to another page
)

// identify determines what became of the component. Only the
// ID from the marked incident (IncidentComponent) is exact; on foreign pages
// it is NOT guessed by slug alone, since a foreign component may by chance
// carry the same slug there. Without an incident only the (soft) deleted component
// of the system page remains; otherwise the identity is unknown (slug serves as name).
func (m *Monitor) identify(pageID uint, slug, incID string) (models.Component, int) {
	if id, err := strconv.ParseUint(incID, 10, 64); err == nil {
		var link models.IncidentComponent
		var comp models.Component
		if m.DB.Where("incident_id = ?", id).Order("id").First(&link).Error == nil &&
			m.DB.Unscoped().First(&comp, link.ComponentID).Error == nil {
			if comp.StatusPageID != pageID {
				return comp, compMoved
			}
			return comp, compDeleted
		}
	}
	comp := models.Component{Name: slug}
	// Order id DESC: with several deleted components with the same slug, the most recent.
	if m.DB.Unscoped().Where("status_page_id = ? AND slug = ?", pageID, slug).Order("id DESC").First(&comp).Error == nil {
		return comp, compDeleted
	}
	return models.Component{Name: slug}, compUnknown
}

// logReadError reports a read error on a marker (the error text is sanitized by the
// logger, see logging rules).
func logReadError(key string, err error) {
	slog.Error("Selbstüberwachung: Marke nicht lesbar, Schritt entfällt", "key", key, "error", err)
}

// recover resets the status, closes the incident and deletes both markers in
// ONE transaction. Otherwise status "operational" and marker
// "failing" would remain contradictory. If anything fails, everything is retried
// on the next tick; the audit message is only produced after the commit.
func (m *Monitor) recover(c Check, comp models.Component, reason string) {
	_, owned, rerr := m.getValue(failingKeyPrefix + c.Slug)
	if rerr != nil {
		logReadError(failingKeyPrefix+c.Slug, rerr)
		return
	}
	if !owned {
		return
	}
	var incID string
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		// Re-review: the same lock as in openIncident first (same
		// order, no deadlock): a second replica reads the incident marker
		// only after the commit of this recovery.
		if _, _, lerr := readValueLocked(tx, failingKeyPrefix+c.Slug); lerr != nil {
			return lerr
		}
		if comp.Status == c.FailedStatus { // if the admin has meanwhile set something else, it stays
			if _, err := statushistory.Set(tx, comp.ID, c.FailedStatus, componentstatus.Operational, models.StatusSourceSelfMonitor, m.Now()); err != nil {
				return err
			}
		}
		var inc models.SystemSetting
		ierr := tx.Unscoped().Where("key = ?", incidentKeyPrefix+c.Slug).First(&inc).Error
		if ierr != nil && !errors.Is(ierr, gorm.ErrRecordNotFound) {
			return ierr
		}
		if ierr == nil {
			incID = inc.Value
			if _, err := resolveIncident(tx, comp.StatusPageID, m.Now(), inc.Value, reason); err != nil {
				return err
			}
			if reason == hiddenReason {
				// The incident remains in the public history, only the
				// assignment to the component is hidden by the page. Title and message
				// would otherwise keep naming the check name.
				if err := neutralizeIncident(tx, comp.StatusPageID, inc.Value, c); err != nil {
					return err
				}
			}
			if err := tx.Unscoped().Where("key = ?", incidentKeyPrefix+c.Slug).Delete(&models.SystemSetting{}).Error; err != nil {
				return err
			}
		}
		return tx.Unscoped().Where("key = ?", failingKeyPrefix+c.Slug).Delete(&models.SystemSetting{}).Error
	})
	if err != nil {
		slog.Error("Selbstüberwachung: Erholung nicht gespeichert", "slug", c.Slug, "incident", incID, "error", err)
		return
	}
	details := ""
	if reason == offReason {
		details = "Überwachung abgeschaltet" // distinguishable from a real recovery
	}
	if reason == hiddenReason {
		details = "Komponente archiviert oder ausgeblendet"
	}
	m.audit("selfmonitor.component_recovered", comp, details)
}

// resolveIncident closes the incident. A DB error is returned (the
// caller then keeps the incident marker); "0 rows" (admin already closed or
// deleted) is success.
// Only incidents of the system page (pageID). The return value says whether
// this call actually closed the incident.
func resolveIncident(tx *gorm.DB, pageID uint, now time.Time, rawID, reason string) (bool, error) {
	// Bit size 0 = size of uint: values that do not fit in uint are rejected here
	// instead of being truncated at uint(id) below (32-bit).
	id, err := strconv.ParseUint(rawID, 10, 0)
	if err != nil {
		return false, nil //nolint:nilerr // invalid identifier: nothing to close, marker is removed
	}
	res := tx.Model(&models.Incident{}).Where("id = ? AND status_page_id = ? AND status <> ?", id, pageID, "resolved").
		Updates(map[string]any{"status": "resolved", "resolved_at": now})
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil // already closed or deleted by the admin
	}
	if err := tx.Create(&models.IncidentUpdate{IncidentID: uint(id), Status: "resolved", AuthorEmail: systemActor,
		Message: reason}).Error; err != nil {
		return false, err // in Postgres the error would abort the transaction anyway
	}
	return true, nil
}

// neutral texts for incidents of a hidden component,
// without check names and without error detail.
const (
	neutralTitle   = "Störung: automatische Überwachung"
	neutralMessage = "Die automatische Selbstüberwachung meldete eine Störung."
)

// neutralizeIncident replaces title and messages of the automatic incident
// with neutral texts. Only what the monitor wrote and
// is unchanged: the original message is that of the opening update (first
// update by the actor "system" with the monitor's prefix); comparison is
// exact, so text added by an admin stays (review), as do
// a changed title and updates by the admin. The condition is in the UPDATE
// itself: an admin edit between read and write is not overwritten.
// Translations of replaced texts are deleted, they also name the name
// and no longer match the text. Errors abort the transaction, the
// next tick repeats the release.
func neutralizeIncident(tx *gorm.DB, pageID uint, rawID string, c Check) error {
	id, err := strconv.ParseUint(rawID, 10, 64)
	if err != nil {
		return nil //nolint:nilerr // invalid identifier: nothing to neutralize (like resolveIncident)
	}
	var inc models.Incident
	if err := tx.Where("id = ? AND status_page_id = ?", id, pageID).First(&inc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // deleted by the admin
		}
		return err
	}
	var updates []models.IncidentUpdate
	if err := tx.Where("incident_id = ? AND author_email = ?", inc.ID, systemActor).Order("id").Find(&updates).Error; err != nil {
		return err
	}
	original := ""
	for _, u := range updates {
		if strings.HasPrefix(u.Message, incidentMessagePrefix(c)) {
			original = u.Message
			break
		}
	}
	titled, err := replaceIf(tx, &models.Incident{}, inc.ID, "title", incidentTitle(c), neutralTitle)
	if err != nil {
		return err
	}
	messaged, err := replaceIf(tx, &models.Incident{}, inc.ID, "message", original, neutralMessage)
	if err != nil {
		return err
	}
	if titled || messaged {
		if err := tx.Where("incident_id = ?", inc.ID).Delete(&models.IncidentTranslation{}).Error; err != nil {
			return err
		}
	}
	for _, u := range updates {
		replaced, err := replaceIf(tx, &models.IncidentUpdate{}, u.ID, "message", original, neutralMessage)
		if err != nil {
			return err
		}
		if !replaced {
			continue
		}
		if err := tx.Where("update_id = ?", u.ID).Delete(&models.IncidentUpdateTranslation{}).Error; err != nil {
			return err
		}
	}
	return nil
}

// replaceIf sets col to to, but only as long as it still holds exactly want
// (review: condition in the UPDATE, no overwriting of an
// admin edit in the meantime). An empty want never replaces anything.
func replaceIf(tx *gorm.DB, model any, id uint, col, want, to string) (bool, error) {
	res := tx.Model(model).Where("id = ? AND "+col+" = ? AND "+col+" <> ''", id, want).Update(col, to)
	return res.RowsAffected > 0, res.Error
}

func (m *Monitor) audit(action string, comp models.Component, details string) {
	entry := models.AuditLog{Action: action, Category: "system", TargetType: "Component",
		TargetID: strconv.FormatUint(uint64(comp.ID), 10), TargetName: comp.Name, Details: details, ActorEmail: systemActor}
	if err := m.DB.Create(&entry).Error; err != nil {
		slog.Error("Selbstüberwachung: Audit-Eintrag fehlgeschlagen", "action", action, "error", err)
	}
}

// getValue returns the value of a marker. A read error is returned and
// is NOT "no marker".
func (m *Monitor) getValue(key string) (string, bool, error) { return readValue(m.DB, key) }

// readValue also reads soft-deleted rows (review): ON CONFLICT (key)
// also hits them, "no marker" would otherwise be a silent permanent rollback.
func readValue(db *gorm.DB, key string) (string, bool, error) { return readValueWith(db, key, false) }

// readValueLocked reads with a row lock (FOR UPDATE in Postgres, SQLite ignores it).
func readValueLocked(tx *gorm.DB, key string) (string, bool, error) {
	return readValueWith(tx, key, true)
}

func readValueWith(db *gorm.DB, key string, lock bool) (string, bool, error) {
	var s models.SystemSetting
	q := db.Unscoped()
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Where("key = ?", key).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return s.Value, true, nil
}

func (m *Monitor) getTime(key string) (time.Time, bool, error) {
	v, ok, err := m.getValue(key)
	if err != nil || !ok {
		return time.Time{}, false, err
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		// A corrupted value counts as "just started" AND is rewritten.
		// Otherwise every tick would again return "now" and IncidentAfter
		// would never be reached.
		now := m.Now()
		if werr := setValue(m.DB, key, now.UTC().Format(time.RFC3339)); werr != nil {
			slog.Error("Selbstüberwachung: Zeitwert nicht repariert", "key", key, "error", werr)
		}
		return now, true, nil
	}
	return t, true, nil
}

func setValue(db *gorm.DB, key, value string) error {
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at", "deleted_at"}),
	}).Create(&models.SystemSetting{Key: key, Value: value}).Error
}

// NeutralizeHiddenIncidents is the backfill: monitor incidents of the
// system page that are already CLOSED (before the fix when hiding
// or after regular recovery) and whose component is now publicly
// hidden, deleted or moved otherwise keep check names and
// error detail in the public history.
//
// Design decisions:
//   - The incident is recognized by the opening update of the actor "system" with the
//     fixed message beginning; the check name is read from it. This way the
//     backfill needs no list of checks and also runs when a check
//     has meanwhile been dropped. After replacing, the update no longer matches: a
//     second run finds nothing (idempotent), without markers in SystemSetting.
//   - Replacing goes through neutralizeIncident: only unchanged monitor text, texts
//     and updates by the admin stay.
//   - Only incidents of the system page (tenant separation), only closed ones: open ones
//     belong to the monitor, which neutralizes them itself when releasing.
//   - Without a findable assignment or component the incident counts as orphaned and
//     is neutralized (conservative: neutral text never hurts).
//   - All in ONE transaction; every DB error rolls back and is
//     returned (fail closed, the next start repeats the run).
func NeutralizeHiddenIncidents(db *gorm.DB) error {
	var page models.StatusPage
	if err := db.Where("is_system = ?", true).First(&page).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // no system page, nothing to do
		}
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var active []models.Component
		if err := tx.Where("status_page_id = ?", page.ID).Find(&active).Error; err != nil {
			return err
		}
		hidden := models.HiddenComponentIDs(active)
		var openings []models.IncidentUpdate
		if err := tx.Joins("JOIN incidents ON incidents.id = incident_updates.incident_id AND incidents.deleted_at IS NULL").
			Where("incidents.status_page_id = ? AND incidents.status = ?", page.ID, "resolved").
			Where("incident_updates.author_email = ? AND incident_updates.message LIKE ?", systemActor, incidentMessageHead+"%").
			Order("incident_updates.id").Find(&openings).Error; err != nil {
			return err
		}
		seen := map[uint]bool{}
		for _, u := range openings {
			name, _, found := strings.Cut(strings.TrimPrefix(u.Message, incidentMessageHead), " (")
			if seen[u.IncidentID] || !found || name == "" {
				continue
			}
			seen[u.IncidentID] = true
			gone, err := componentGone(tx, page.ID, u.IncidentID, hidden)
			if err != nil {
				return err
			}
			if !gone {
				continue
			}
			if err := neutralizeIncident(tx, page.ID, strconv.FormatUint(uint64(u.IncidentID), 10), Check{Name: name}); err != nil {
				return err
			}
		}
		return nil
	})
}

// componentGone says whether the incident's component no longer
// appears publicly: hidden (hidden, status page rule), deleted, moved to a
// different page or no longer findable at all. The assignment is read
// unscoped, so that a removed assignment does not look like "visible".
func componentGone(tx *gorm.DB, pageID, incidentID uint, hidden map[uint]bool) (bool, error) {
	var link models.IncidentComponent
	if err := tx.Unscoped().Where("incident_id = ?", incidentID).Order("id").First(&link).Error; err != nil {
		return errors.Is(err, gorm.ErrRecordNotFound), onlyReadError(err)
	}
	var comp models.Component
	if err := tx.Unscoped().First(&comp, link.ComponentID).Error; err != nil {
		return errors.Is(err, gorm.ErrRecordNotFound), onlyReadError(err)
	}
	return comp.StatusPageID != pageID || comp.DeletedAt.Valid || hidden[comp.ID], nil
}

// onlyReadError returns err, except "not found" (that is a result here).
func onlyReadError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}
