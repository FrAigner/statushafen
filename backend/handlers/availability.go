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

// 90-Tage-Verfügbarkeit für die Statusseite (Redesign Paket 4a).
// nach Schweregrad gewichtet.
//
// Datenquelle: Vorfälle, Wartungsfenster, gespiegelte externe Meldungen und
// seit der Umstellung die Statushistorie der Komponenten (component_status_changes:
// Webhook, manuell, Wartung, Selbstüberwachung). Die Historie beginnt mit dem
// Nachtrag der Bestandsstände; davor gesetzte Ausfälle ohne vorfall fehlen
// weiterhin. Eigene Vorfälle tragen seit der Umstellung einen Schweregrad
// (Incident.Severity, Altdaten degraded_performance, Gewicht 0,3). Externe
// Meldungen bringen ihr `impact` mit (minor/major/critical -> degraded/partial/
// major). Ausfälle, die nur per Webhook/manuell ohne Vorfall gesetzt werden,
// zählen über die Historie mit ihrem Status; überlappende Quellen zählen nach
// dem schlimmsten Status.
// Die Gesamtseite ist der Durchschnitt der sichtbaren Komponenten (versteckte
// Komponenten und ihre Vorfälle zählen nicht).

const (
	availabilityDays     = 90
	availabilityCacheTTL = 60 * time.Second
	// maxHistoryRowsPerComponent begrenzt die Zeilen der Statushistorie je
	// Komponente beim Lesen. Ohne Grenze lädt Dauerflattern
	// (86.400 Zeilen ≈ 0,6 s) unbegrenzt viel. Normal sind es wenige Dutzend
	// Zeilen in 90 Tagen; die Schreib-Verdichtung (statushistory.MergeGap)
	// erlaubt höchstens 2.880 je Tag, 10.000 decken also über 3 Tage
	// Dauerflattern ab und kosten rund 70 ms je Komponente.
	maxHistoryRowsPerComponent = 10000
)

type availabilityDay struct {
	Date            string   `json:"date"`
	Status          string   `json:"status"`
	Availability    *float64 `json:"availability"`
	DowntimeSeconds int64    `json:"downtime_seconds"`
	// Impairments: Dauer je Schweregrad (ungewichtet, schlimmster zuerst).
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
	// HistoryTruncated: Mindestens eine Komponente hat im Fenster
	// mehr Statuswechsel als maxHistoryRowsPerComponent; ausgewertet wurden
	// nur die neuesten, ältere Beeinträchtigungen fehlen.
	HistoryTruncated bool `json:"history_truncated,omitempty"`
}

// availabilityCache hält die Antwort je Seite kurz vor (60 s): Die Statusseite
// wird häufig aufgerufen, die Berechnung läuft über bis zu 90 Tage Vorfälle.
// Zugriffsprüfung passiert VOR dem Cache, der Eintrag enthält nur Seitendaten.
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
	// erst kodieren, dann cachen. Eine nicht kodierbare Antwort
	// (z. B. NaN) bliebe sonst 60 s im Cache. Zudem setzt gin in c.JSON den
	// Status 200 vor dem Kodieren: schlägt es fehl, bekäme der Client 200 mit
	// leerem Body (der Fehler läge nur in c.Errors, Access-Log und
	// Statusüberwachung sähen 200). Darum vorher selbst kodieren und 500 senden.
	body, err := json.Marshal(resp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compute availability", "code": "server_error"})
		return
	}
	h.availCache.put(key, now, resp)
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

// availabilityIncidentRow: Vorfall plus (optionale) Komponente aus einem
// einzigen LEFT JOIN, damit es keine Abfrage pro Vorfall gibt.
type availabilityIncidentRow struct {
	models.Incident
	ComponentID *uint
	LinkID      *uint // incident_components.id; nil = Vorfall ohne Zuordnung
}

func (h *Handler) buildAvailability(page models.StatusPage, now time.Time, withComponents bool) (availabilityResponse, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	windowStart := today.AddDate(0, 0, -(availabilityDays - 1))

	// Sichtbare Komponenten: versteckte samt Nachfahren zählen
	// weder als Zeile noch für die Gesamtseite.
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

	// Eine Abfrage: Vorfälle im Fenster samt Komponentenzuordnung. Grober
	// SQL-Filter, die genaue Kappung macht availability.Compute.
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

	// Ein Vorfall ohne eine einzige Komponentenzeile betrifft die ganze Seite
	// (alle sichtbaren Komponenten); einer, dessen Komponenten alle gelöscht
	// oder versteckt sind, wird ignoriert.
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
			continue // alle zugeordneten Komponenten gelöscht bzw. fremd
		}
		if r.ComponentID != nil && hidden[*r.ComponentID] {
			continue // betrifft nur eine unsichtbare Komponente
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

	// gespiegelte Meldungen externer Quellen zählen wie eigene
	// Vorfälle, an den eigenen Komponenten, die sie betreffen - mit ihrem
	// Schweregrad.
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

	// Je Blatt-Komponente (Gruppen haben keinen eigenen Status) einmal rechnen;
	// die Gesamtseite ist der Durchschnitt, ihr Status der schlimmste aller Tage.
	var leaves []models.Component
	for _, comp := range comps {
		if comp.Status != componentStatusGroup {
			leaves = append(leaves, comp)
		}
	}

	// Statushistorie der Blätter (Webhook, manuell, Wartung,
	// Selbstüberwachung). Überlappungen mit Vorfällen zählt Compute nach dem
	// schlimmsten Status. Nur Komponenten dieser (sichtbaren) Seite.
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
		// Hinweis, dass bei Dauerflattern nur die neuesten Wechsel einflossen.
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

// allHidden: ids ist nicht leer und jede ID gehört einer unsichtbaren
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

// expandGroup liefert die Komponente selbst bzw. bei einer Gruppe alle
// Nachfahren (ein Vorfall an einer Gruppe betrifft deren Kinder).
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

// incidentInterval bildet einen Vorfall bzw. ein Wartungsfenster auf ein
// Intervall ab. Beginn: StartedAt, sonst CreatedAt. Ende: ResolvedAt; offene
// Vorfälle laufen bis jetzt; behobene ohne ResolvedAt (Altdaten) enden mit der
// letzten Änderung. ok=false: nicht auswertbar (Wartung ohne Zeiten).
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

// externalInterval ist ein Zeitraum einer gespiegelten externen Meldung samt
// den eigenen Komponenten, die sie betrifft.
type externalInterval struct {
	interval     availability.Interval
	componentIDs []uint
}

// externalViewInterval bildet eine Meldung auf ein Intervall ab. Wartungen
// laufen von ScheduledFor bis ScheduledUntil, Vorfälle von StartedAt (sonst der
// letzten Änderung) bis zur Behebung bzw. jetzt.
func externalViewInterval(v externalIncidentView, now time.Time) (availability.Interval, bool) {
	if v.Kind == models.ExternalIncidentKindMaintenance {
		if v.ScheduledFor == nil || v.ScheduledUntil == nil {
			return availability.Interval{}, false
		}
		return availability.Interval{Start: v.ScheduledFor.UTC(), End: v.ScheduledUntil.UTC(), Status: componentstatus.UnderMaintenance}, true
	}
	// Schweregrad aus `impact`. "none" meldet die Fremdseite
	// ausdrücklich als folgenlos.
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

// externalImpactStatus ordnet das `impact` einer externen Meldung einem
// Komponentenstatus zu (Vorschlag): minor ->
// degraded_performance, major -> partial_outage, critical -> major_outage;
// none zählt nicht. Unbekannte oder leere Werte zählen vorsichtig wie bisher als
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

// statusHistoryIntervals liest die Statushistorie der Komponenten im Fenster
// samt dem jeweils letzten Eintrag davor (Ausgangsstatus) und bildet
// Intervalle je Komponente. Zwei indexgestützte Abfragen (component_id,
// changed_at) statt einer je Komponente; die Komponentenliste stammt aus der
// Seite, fremde Komponenten kommen nie vor (Mandantentrennung).
func (h *Handler) statusHistoryIntervals(leaves []models.Component, windowStart, now time.Time) (map[uint][]availability.Interval, []uint, error) {
	ids := make([]uint, len(leaves))
	for i, c := range leaves {
		ids[i] = c.ID
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}
	// je Komponente nur die neuesten maxHistoryRowsPerComponent+1
	// Zeilen (Fensterfunktion); die eine Zeile darüber zeigt nur an, dass
	// gekürzt wurde, und fällt weg. Der erste behaltene Eintrag bestimmt dann
	// den Anfangsstatus.
	var rows []struct {
		models.ComponentStatusChange
		Rn int
	}
	// Ausgangsstatus = neuester Eintrag VOR dem Fenster nach (changed_at, id),
	// nicht nach der höchsten ID.
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
