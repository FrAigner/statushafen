package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/fraigner/statushafen/availability"
	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// 90-day availability for the status page (redesign package 4a),
// weighted by severity.
//
// Data source: incidents, maintenance windows, mirrored external reports and,
// since the changeover, the components' status history (component_status_changes:
// webhook, manual, maintenance, self-monitoring). The history begins with the
// backfill of the existing states; outages set before that without an incident are still
// missing. Own incidents have carried a severity since the changeover
// (Incident.Severity, legacy data degraded_performance, weight 0.3). External
// reports bring their `impact` along (minor/major/critical -> degraded/partial/
// major). Outages set only via webhook/manual without an incident
// count via the history with their status; overlapping sources count by
// the worst status.
// The overall page is the average of the visible components (hidden
// components and their incidents do not count).

const (
	availabilityDays     = 90
	availabilityCacheTTL = 60 * time.Second
	// maxHistoryRowsPerComponent limits the rows of the status history per
	// component when reading. Without a limit, permanent flapping loads
	// (86,400 rows ≈ 0.6 s) an unbounded amount. Normally there are a few dozen
	// rows in 90 days; the write compaction (statushistory.MergeGap)
	// allows at most 2,880 per day, so 10,000 cover over 3 days of
	// permanent flapping and cost about 70 ms per component.
	maxHistoryRowsPerComponent = 10000
)

type availabilityDay struct {
	Date            string   `json:"date"`
	Status          string   `json:"status"`
	Availability    *float64 `json:"availability"`
	DowntimeSeconds int64    `json:"downtime_seconds"`
	// Impairments: duration per severity (unweighted, worst first).
	Impairments []availabilityImpairment `json:"impairments"`
}

type availabilityImpairment struct {
	Status  string `json:"status"`
	Seconds int64  `json:"seconds"`
}

type availabilityComponent struct {
	ID            uint              `json:"id"`
	Name          string            `json:"name"`
	UptimePercent *float64          `json:"uptime_percent"`
	Days          []availabilityDay `json:"days"`
}

type availabilityResponse struct {
	GeneratedAt   time.Time               `json:"generated_at"`
	From          string                  `json:"from"`
	To            string                  `json:"to"`
	UptimePercent *float64                `json:"uptime_percent"`
	Days          []availabilityDay       `json:"days"`
	Components    []availabilityComponent `json:"components,omitempty"`
	// HistoryTruncated: At least one component has more status changes
	// in the window than maxHistoryRowsPerComponent; only the newest were
	// evaluated, older impairments are missing.
	HistoryTruncated bool `json:"history_truncated,omitempty"`
}

// availabilityCache keeps the response per page briefly (60 s): The status page
// is requested often, and the calculation runs over up to 90 days of incidents.
// The access check happens BEFORE the cache; the entry contains only page data.
type availabilityCache struct {
	mu      sync.Mutex
	entries map[availabilityCacheKey]availabilityCacheEntry
}

type availabilityCacheKey struct {
	pageID     uint
	components bool
}

type availabilityCacheEntry struct {
	at   time.Time
	resp availabilityResponse
}

func (c *availabilityCache) get(k availabilityCacheKey, now time.Time) (availabilityResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok || now.Sub(e.at) >= availabilityCacheTTL || now.Before(e.at) {
		return availabilityResponse{}, false
	}
	return e.resp, true
}

func (c *availabilityCache) put(k availabilityCacheKey, now time.Time, r availabilityResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[availabilityCacheKey]availabilityCacheEntry{}
	}
	c.entries[k] = availabilityCacheEntry{at: now, resp: r}
}

// GET /api/public/pages/:slug/availability[?include=components]
func (h *Handler) GetPublicAvailabilityHandler(c *gin.Context) {
	var page models.StatusPage
	if err := h.DB.Where(slugWhereClause, c.Param("slug")).First(&page).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Status page not found", "code": "page_not_found"})
		return
	}
	if !h.authorizePublicPageRead(c, page) {
		return
	}

	withComponents := c.Query("include") == "components"
	now := time.Now().UTC()
	if h.Now != nil {
		now = h.Now().UTC()
	}
	key := availabilityCacheKey{pageID: page.ID, components: withComponents}
	if resp, ok := h.availCache.get(key, now); ok {
		c.JSON(http.StatusOK, resp)
		return
	}
	resp, err := h.buildAvailability(page, now, withComponents)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compute availability", "code": "server_error"})
		return
	}
	h.respondAvailability(c, key, now, resp)
}

func (h *Handler) respondAvailability(c *gin.Context, key availabilityCacheKey, now time.Time, resp availabilityResponse) {
	// Encode first, then cache. A response that cannot be encoded
	// (e.g. NaN) would otherwise stay in the cache for 60 s. Also, gin sets the
	// status 200 in c.JSON before encoding: if it fails, the client would get 200 with
	// an empty body (the error would only be in c.Errors, and the access log and
	// status monitoring would see 200). So encode ourselves first and send 500.
	body, err := json.Marshal(resp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compute availability", "code": "server_error"})
		return
	}
	h.availCache.put(key, now, resp)
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

// availabilityIncidentRow: incident plus (optional) component from a
// single LEFT JOIN, so there is no query per incident.
type availabilityIncidentRow struct {
	models.Incident
	ComponentID *uint
	LinkID      *uint // incident_components.id; nil = incident without assignment
}

func (h *Handler) buildAvailability(page models.StatusPage, now time.Time, withComponents bool) (availabilityResponse, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	windowStart := today.AddDate(0, 0, -(availabilityDays - 1))

	// Visible components: hidden ones including descendants count
	// neither as a row nor for the overall page.
	var comps []models.Component
	if err := h.DB.Where(whereStatusPageIDEquals, page.ID).Order("sort_order asc, id asc").Find(&comps).Error; err != nil {
		return availabilityResponse{}, err
	}
	hidden := hiddenComponentIDs(comps)
	comps = withoutHiddenComponents(comps)
	children := map[uint][]uint{}
	byID := map[uint]models.Component{}
	for _, comp := range comps {
		byID[comp.ID] = comp
		if comp.ParentID != nil {
			children[*comp.ParentID] = append(children[*comp.ParentID], comp.ID)
		}
	}

	// One query: incidents in the window including component assignment. Coarse
	// SQL filter; the exact clamping is done by availability.Compute.
	var rows []availabilityIncidentRow
	err := h.DB.Table("incidents").
		Select("incidents.*, c.id AS component_id, ic.id AS link_id").
		Joins("LEFT JOIN incident_components ic ON ic.incident_id = incidents.id AND ic.deleted_at IS NULL").
		Joins("LEFT JOIN components c ON c.id = ic.component_id AND c.deleted_at IS NULL AND c.status_page_id = incidents.status_page_id").
		Where("incidents.deleted_at IS NULL AND incidents.status_page_id = ?", page.ID).
		Where("(incidents.is_maintenance = ? AND (incidents.status NOT IN ? OR incidents.updated_at >= ? OR incidents.resolved_at >= ?)) OR (incidents.is_maintenance = ? AND incidents.scheduled_end_at >= ?)",
			false, models.ResolvedIncidentStatuses, windowStart, windowStart, true, windowStart).
		Scan(&rows).Error
	if err != nil {
		return availabilityResponse{}, err
	}

	// An incident without a single component row affects the whole page
	// (all visible components); one whose components are all deleted
	// or hidden is ignored.
	var pageWide []availability.Interval
	perComp := map[uint][]availability.Interval{}
	dataStart := page.CreatedAt.UTC()
	touch := func(iv availability.Interval) {
		if iv.Start.Before(dataStart) {
			dataStart = iv.Start
		}
	}
	seenIncident := map[uint]bool{}
	for _, r := range rows {
		iv, ok := incidentInterval(r.Incident, now)
		if !ok {
			continue
		}
		if r.ComponentID == nil && r.LinkID != nil {
			continue // all assigned components deleted or foreign
		}
		if r.ComponentID != nil && hidden[*r.ComponentID] {
			continue // affects only an invisible component
		}
		switch {
		case r.ComponentID != nil:
			for _, id := range expandGroup(byID, children, *r.ComponentID) {
				perComp[id] = append(perComp[id], iv)
				touch(iv)
			}
		case r.LinkID == nil && !seenIncident[r.ID]:
			seenIncident[r.ID] = true
			pageWide = append(pageWide, iv)
			touch(iv)
		}
	}

	// Mirrored reports from external sources count like own
	// incidents, on the own components they affect - with their
	// severity.
	extRows, err := h.externalAvailabilityIntervals(page.ID, now)
	if err != nil {
		return availabilityResponse{}, err
	}
	for _, e := range extRows {
		if allHidden(e.componentIDs, hidden) {
			continue
		}
		for _, cid := range e.componentIDs {
			for _, id := range expandGroup(byID, children, cid) {
				perComp[id] = append(perComp[id], e.interval)
				touch(e.interval)
			}
		}
	}

	// Compute once per leaf component (groups have no status of their own);
	// the overall page is the average, its status the worst of all days.
	var leaves []models.Component
	for _, comp := range comps {
		if comp.Status != componentStatusGroup {
			leaves = append(leaves, comp)
		}
	}

	// Status history of the leaves (webhook, manual, maintenance,
	// self-monitoring). Compute counts overlaps with incidents by the
	// worst status. Only components of this (visible) page.
	hist, truncated, err := h.statusHistoryIntervals(leaves, windowStart, now)
	if err != nil {
		return availabilityResponse{}, err
	}
	if len(truncated) > 0 {
		slog.Warn("Statushistorie für die Verfügbarkeit gekürzt (Flattern)", "page_id", page.ID, "component_ids", truncated, "limit", maxHistoryRowsPerComponent)
	}
	for id, ivs := range hist {
		for _, iv := range ivs {
			perComp[id] = append(perComp[id], iv)
			touch(iv)
		}
	}
	pageIntervals := append([]availability.Interval(nil), pageWide...)
	compDays := make([][]availability.Day, len(leaves))
	for i, comp := range leaves {
		ivs := append(append([]availability.Interval(nil), perComp[comp.ID]...), pageWide...)
		pageIntervals = append(pageIntervals, perComp[comp.ID]...)
		compDays[i] = availability.Compute(ivs, dataStart, now, availabilityDays)
	}
	pageDays := availability.Merge(availability.Compute(pageIntervals, dataStart, now, availabilityDays), compDays)

	resp := availabilityResponse{
		GeneratedAt:   now,
		From:          windowStart.Format("2006-01-02"),
		To:            today.Format("2006-01-02"),
		UptimePercent: availability.Summary(pageDays),
		Days:          toAvailabilityDays(pageDays),
		// Note that with permanent flapping only the newest changes were included.
		HistoryTruncated: len(truncated) > 0,
	}
	if withComponents {
		resp.Components = make([]availabilityComponent, 0, len(leaves))
		for i, comp := range leaves {
			resp.Components = append(resp.Components, availabilityComponent{
				ID: comp.ID, Name: comp.Name, UptimePercent: availability.Summary(compDays[i]), Days: toAvailabilityDays(compDays[i]),
			})
		}
	}
	return resp, nil
}

// allHidden: ids is not empty and every ID belongs to an invisible
// Komponente.
func allHidden(ids []uint, hidden map[uint]bool) bool {
	if len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		if !hidden[id] {
			return false
		}
	}
	return true
}

// expandGroup returns the component itself or, for a group, all
// descendants (an incident on a group affects its children).
func expandGroup(byID map[uint]models.Component, children map[uint][]uint, id uint) []uint {
	comp, ok := byID[id]
	if !ok {
		return nil
	}
	if comp.Status != componentStatusGroup {
		return []uint{id}
	}
	var out []uint
	for _, ch := range children[id] {
		out = append(out, expandGroup(byID, children, ch)...)
	}
	return out
}

// incidentInterval maps an incident or maintenance window to an
// interval. Start: StartedAt, otherwise CreatedAt. End: ResolvedAt; open
// incidents run until now; resolved ones without ResolvedAt (legacy data) end with the
// last change. ok=false: not evaluable (maintenance without times).
func incidentInterval(inc models.Incident, now time.Time) (availability.Interval, bool) {
	if inc.IsMaintenance {
		if inc.ScheduledStartAt == nil || inc.ScheduledEndAt == nil {
			return availability.Interval{}, false
		}
		return availability.Interval{Start: inc.ScheduledStartAt.UTC(), End: inc.ScheduledEndAt.UTC(), Status: componentstatus.UnderMaintenance}, true
	}
	start := inc.CreatedAt
	if inc.StartedAt != nil {
		start = *inc.StartedAt
	}
	end := now
	switch {
	case inc.ResolvedAt != nil:
		end = *inc.ResolvedAt
	case models.IsResolvedIncidentStatus(inc.Status):
		end = inc.UpdatedAt
	}
	if end.After(now) {
		end = now
	}
	if end.Before(start) {
		end = start
	}
	return availability.Interval{Start: start.UTC(), End: end.UTC(), Status: componentstatus.FromIncidentSeverity(inc.Severity)}, true
}

func toAvailabilityDays(days []availability.Day) []availabilityDay {
	out := make([]availabilityDay, len(days))
	for i, d := range days {
		imp := make([]availabilityImpairment, len(d.Impairments))
		for j, v := range d.Impairments {
			imp[j] = availabilityImpairment{Status: v.Status, Seconds: v.Seconds}
		}
		out[i] = availabilityDay{Date: d.Date, Status: d.Status, Availability: d.Availability, DowntimeSeconds: d.DowntimeSeconds, Impairments: imp}
	}
	return out
}

// externalInterval is a period of a mirrored external report together with
// the own components it affects.
type externalInterval struct {
	interval     availability.Interval
	componentIDs []uint
}

// externalViewInterval maps a report to an interval. Maintenance windows
// run from ScheduledFor to ScheduledUntil, incidents from StartedAt (otherwise the
// last change) until resolution or now.
func externalViewInterval(v externalIncidentView, now time.Time) (availability.Interval, bool) {
	if v.Kind == models.ExternalIncidentKindMaintenance {
		if v.ScheduledFor == nil || v.ScheduledUntil == nil {
			return availability.Interval{}, false
		}
		return availability.Interval{Start: v.ScheduledFor.UTC(), End: v.ScheduledUntil.UTC(), Status: componentstatus.UnderMaintenance}, true
	}
	// Severity from `impact`. "none" is explicitly reported by the external
	// site as without consequences.
	status, counts := externalImpactStatus(v.Impact)
	if !counts {
		return availability.Interval{}, false
	}
	var start time.Time
	switch {
	case v.StartedAt != nil:
		start = *v.StartedAt
	case v.UpdatedAt != nil:
		start = *v.UpdatedAt
	default:
		return availability.Interval{}, false
	}
	end := now
	switch {
	case v.ResolvedAt != nil:
		end = *v.ResolvedAt
	case v.Closed && v.UpdatedAt != nil:
		end = *v.UpdatedAt
	}
	if end.After(now) {
		end = now
	}
	if end.Before(start) {
		end = start
	}
	return availability.Interval{Start: start.UTC(), End: end.UTC(), Status: status}, true
}

// externalImpactStatus maps the `impact` of an external report to a
// component status (suggestion): minor ->
// degraded_performance, major -> partial_outage, critical -> major_outage;
// none does not count. Unknown or empty values count conservatively as before as
// degraded_performance.
func externalImpactStatus(impact string) (string, bool) {
	switch impact {
	case "none":
		return "", false
	case "major":
		return componentstatus.PartialOutage, true
	case "critical":
		return componentstatus.MajorOutage, true
	}
	return componentstatus.DegradedPerformance, true
}

func (h *Handler) externalAvailabilityIntervals(pageID uint, now time.Time) ([]externalInterval, error) {
	views, err := h.externalIncidentViews(pageID, externalIncidentQuery{days: availabilityDays, limit: 1000, now: now})
	if err != nil {
		return nil, err
	}
	var out []externalInterval
	for _, v := range views {
		iv, ok := externalViewInterval(v, now)
		if !ok {
			continue
		}
		e := externalInterval{interval: iv}
		for _, c := range v.AffectedComponents {
			e.componentIDs = append(e.componentIDs, c.ID)
		}
		out = append(out, e)
	}
	return out, nil
}

// statusHistoryIntervals reads the components' status history in the window
// together with the latest entry before it (initial status) and builds
// intervals per component. Two index-backed queries (component_id,
// changed_at) instead of one per component; the component list comes from the
// page, foreign components never occur (tenant separation).
func (h *Handler) statusHistoryIntervals(leaves []models.Component, windowStart, now time.Time) (map[uint][]availability.Interval, []uint, error) {
	ids := make([]uint, len(leaves))
	for i, c := range leaves {
		ids[i] = c.ID
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}
	// per component only the newest maxHistoryRowsPerComponent+1
	// rows (window function); the one row beyond only indicates that
	// truncation occurred, and is dropped. The first retained entry then determines
	// the initial status.
	var rows []struct {
		models.ComponentStatusChange
		Rn int
	}
	// Initial status = newest entry BEFORE the window by (changed_at, id),
	// not by highest ID.
	err := h.DB.Raw(`SELECT id, component_id, old_status, new_status, source, changed_at, created_at, rn FROM (
SELECT s.*, ROW_NUMBER() OVER (PARTITION BY s.component_id ORDER BY s.changed_at DESC, s.id DESC) AS rn FROM (
SELECT * FROM component_status_changes WHERE component_id IN ? AND (changed_at >= ? OR (changed_at < ? AND NOT EXISTS (
SELECT 1 FROM component_status_changes n WHERE n.component_id = component_status_changes.component_id AND n.changed_at < ?
AND (n.changed_at > component_status_changes.changed_at OR (n.changed_at = component_status_changes.changed_at AND n.id > component_status_changes.id))))
)) s) r WHERE rn <= ? ORDER BY component_id, changed_at, id`,
		ids, windowStart, windowStart, windowStart, maxHistoryRowsPerComponent+1).Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}
	byComp := map[uint][]availability.StatusChange{}
	var truncated []uint
	for _, r := range rows {
		if r.Rn > maxHistoryRowsPerComponent {
			truncated = append(truncated, r.ComponentID)
			continue
		}
		byComp[r.ComponentID] = append(byComp[r.ComponentID], availability.StatusChange{At: r.ChangedAt, Status: r.NewStatus})
	}
	out := make(map[uint][]availability.Interval, len(byComp))
	for id, changes := range byComp {
		out[id] = availability.FromChanges(changes, now)
	}
	return out, truncated, nil
}
