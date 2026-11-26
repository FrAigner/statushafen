package handlers

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"
	"github.com/fraigner/statushafen/richtext"
	"github.com/fraigner/statushafen/statushistory"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// IncidentResponse flattens IncidentComponents into a sorted
// Components list so that the frontend does not need to know the internal
// join structure (IncidentComponents[].Component).
type IncidentResponse struct {
	models.Incident
	Components []models.Component `json:"Components"`
	// Tags (replacement for the former Country field) - populated manually
	// like Components above, since Incident has no GORM tags relation
	// (explicit IncidentTag join model, see tagsForIncident).
	Tags []models.Tag `json:"Tags"`
}

func (h *Handler) toIncidentResponse(inc models.Incident) IncidentResponse {
	sort.Slice(inc.IncidentComponents, func(i, j int) bool {
		return inc.IncidentComponents[i].SortOrder < inc.IncidentComponents[j].SortOrder
	})
	comps := make([]models.Component, 0, len(inc.IncidentComponents))
	for _, ic := range inc.IncidentComponents {
		comps = append(comps, ic.Component)
	}
	return IncidentResponse{Incident: inc, Components: comps, Tags: h.tagsForIncident(inc.ID)}
}

func (h *Handler) toIncidentResponses(incs []models.Incident) []IncidentResponse {
	out := make([]IncidentResponse, 0, len(incs))
	for _, inc := range incs {
		out = append(out, h.toIncidentResponse(inc))
	}
	return out
}

// componentNames extracts the names for notification texts (an
// incident can now affect several components).
func componentNames(comps []models.Component) string {
	names := make([]string, 0, len(comps))
	for _, c := range comps {
		names = append(names, c.Name)
	}
	return strings.Join(names, ", ")
}

// componentIDsWithOpenIncidents: open, regular incidents per component
// since the switch in componentstatus.OpenIncidentComponentIDs.
func (h *Handler) componentIDsWithOpenIncidents(tenantID uint) map[uint]string {
	return componentstatus.OpenIncidentComponentIDs(h.DB, tenantID)
}

func (h *Handler) GetIncidentsHandler(c *gin.Context) {
	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}

	var page models.StatusPage
	if err := h.DB.First(&page, tenantID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Status page not found", "code": "page_not_found"})
		return
	}
	if !h.requirePageVisible(c, page, "Status page not found", "page_not_found") {
		return
	}

	// Maintenance windows use the same incident model but have their own
	// admin menu entry instead of showing up in the regular incident list.
	// ?is_maintenance=true switches to the maintenance window view,
	// ?is_maintenance=any returns both (history of the public page),
	// the default false leaves the existing incident list unchanged.
	// ?state=open|resolved filters by open/resolved; limit/offset paginate
	// (default 50, at most 200), the list is never unbounded.
	limit, offset, ok := parseIncidentWindow(c)
	if !ok {
		return
	}
	stateScope, ok := incidentStateScope(c.Query("state"))
	if !ok {
		badIncidentWindow(c)
		return
	}
	query := h.DB.Scopes(h.incidentTagFilterScope(c), stateScope).Where("status_page_id = ?", tenantID)
	switch c.Query("is_maintenance") {
	case "true":
		query = query.Where("is_maintenance = ?", true)
	case "any":
	default:
		query = query.Where("is_maintenance = ?", false)
	}

	incidents := []models.Incident{}
	err := query.
		Preload("IncidentComponents.Component").
		Order(incidentNewestFirst).
		Limit(limit).Offset(offset).
		Find(&incidents).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch incidents", "code": "server_error"})
		return
	}

	h.stripHiddenComponentsForCaller(c, tenantID, incidents)
	c.JSON(http.StatusOK, h.toIncidentResponses(incidents))
}

// stripHiddenComponentsForCaller removes archived and invisible
// components from the incidents' assignments if the
// caller may not manage them: the incident routes are anonymously readable.
func (h *Handler) stripHiddenComponentsForCaller(c *gin.Context, pageID uint, incidents []models.Incident) {
	if h.callerMaySeeHiddenComponents(c, pageID) {
		return
	}
	var comps []models.Component
	if err := h.DB.Select("id", "parent_id", "visible", "archived_at").Where("status_page_id = ?", pageID).Find(&comps).Error; err != nil {
		// Fail closed: better to name no components than to reveal hidden ones.
		for i := range incidents {
			incidents[i].IncidentComponents = nil
		}
		return
	}
	stripHiddenIncidentComponents(incidents, hiddenComponentIDs(comps))
}

type createIncidentReq struct {
	Title        string `json:"Title" binding:"required"`
	Message      string `json:"Message" binding:"required"`
	Status       string `json:"Status"`
	Severity     string `json:"Severity"` // empty = degraded_performance
	ComponentIDs []uint `json:"ComponentIDs" binding:"required,min=1"`
	TagIDs       []uint `json:"TagIDs"`
	// Maintenance windows: for IsMaintenance the status is managed by the
	// scheduler, not dictated by the client.
	IsMaintenance       bool       `json:"IsMaintenance"`
	ScheduledStartAt    *time.Time `json:"ScheduledStartAt"`
	ScheduledEndAt      *time.Time `json:"ScheduledEndAt"`
	ReminderLeadMinutes int        `json:"ReminderLeadMinutes"`
	// Event times of regular incidents.
	StartedAt  *time.Time `json:"StartedAt"`
	ResolvedAt *time.Time `json:"ResolvedAt"`
}

// deriveMaintenanceStatus derives the status of a maintenance window from its
// times: end <= now -> resolved, start <= now < end ->
// in_progress, otherwise scheduled. Same boundaries as the scheduler.
func deriveMaintenanceStatus(start, end, now time.Time) string {
	if !end.After(now) {
		return "resolved"
	}
	if !start.After(now) {
		return "in_progress"
	}
	return "scheduled"
}

// validateIncidentTimes checks StartedAt/ResolvedAt of a regular incident:
// ResolvedAt only with status resolved, end not before start.
func validateIncidentTimes(status string, startedAt, resolvedAt *time.Time) string {
	if resolvedAt == nil {
		return ""
	}
	if !models.IsResolvedIncidentStatus(status) {
		return "ResolvedAt ist nur bei Status resolved erlaubt"
	}
	if startedAt != nil && resolvedAt.Before(*startedAt) {
		return "ResolvedAt darf nicht vor StartedAt liegen"
	}
	return ""
}

// incidentValidationCode maps the fixed texts of the status/time checks
// to an error code.
var incidentValidationCodes = map[string]string{
	errInvalidIncidentStatus:                                        "invalid_incident_status",
	errInvalidMaintenanceStatus:                                     "invalid_maintenance_status",
	errInvalidIncidentSeverity:                                      "invalid_incident_severity",
	"Status ist erforderlich":                                       "status_required",
	"ResolvedAt ist nur bei Status resolved erlaubt":                "resolved_at_needs_resolved",
	"ResolvedAt darf nicht vor StartedAt liegen":                    "resolved_before_started",
	"Start- und Endzeitpunkt sind für Wartungsfenster erforderlich": "maintenance_times_required",
	"Endzeitpunkt muss nach dem Startzeitpunkt liegen":              "maintenance_end_before_start",
}

// respondIncidentValidation answers a failed status/time check
// with 400, text and code.
func respondIncidentValidation(c *gin.Context, msg string) {
	code, ok := incidentValidationCodes[msg]
	if !ok {
		code = "invalid_request"
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": code})
}

// resolveCreateIncidentStatus validates/computes the initial status depending
// on whether it is a maintenance window. Extracted from
// CreateIncidentHandler so that its cognitive complexity does not exceed
// the SonarCloud limit (go:S3776).
func resolveCreateIncidentStatus(req *createIncidentReq) (string, bool) {
	if !req.IsMaintenance {
		if req.Severity != "" && !models.IsValidIncidentSeverity(req.Severity) {
			return errInvalidIncidentSeverity, false
		}
		if req.Status == "" {
			return "Status ist erforderlich", false
		}
		if !models.IsValidIncidentStatus(req.Status) {
			return errInvalidIncidentStatus, false
		}
		msg := validateIncidentTimes(req.Status, req.StartedAt, req.ResolvedAt)
		return msg, msg == ""
	}
	if req.ScheduledStartAt == nil || req.ScheduledEndAt == nil {
		return "Start- und Endzeitpunkt sind für Wartungsfenster erforderlich", false
	}
	if !req.ScheduledEndAt.After(*req.ScheduledStartAt) {
		return "Endzeitpunkt muss nach dem Startzeitpunkt liegen", false
	}
	if req.Status == "" {
		req.Status = deriveMaintenanceStatus(*req.ScheduledStartAt, *req.ScheduledEndAt, time.Now())
	} else if !models.IsValidMaintenanceStatus(req.Status) {
		return errInvalidMaintenanceStatus, false
	}
	return "", true
}

// createIncidentWithComponents creates the incident, its component
// assignments and tags in one transaction.
func (h *Handler) createIncidentWithComponents(incident *models.Incident, components []models.Component, tags []models.Tag) error {
	return h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(incident).Error; err != nil {
			return err
		}
		for sortOrder, comp := range components {
			if err := tx.Create(&models.IncidentComponent{IncidentID: incident.ID, ComponentID: comp.ID, SortOrder: sortOrder}).Error; err != nil {
				return err
			}
		}
		return h.replaceIncidentTags(tx, incident.ID, tags)
	})
}

func (h *Handler) CreateIncidentHandler(c *gin.Context) {
	var req createIncidentReq
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxKindBody)
	if err := c.ShouldBindJSON(&req); err != nil {
		if isBodyTooLarge(err) {
			respondRequestTooLarge(c)
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	if !requireTitleLength(c, req.Title) {
		return
	}
	if !requireMessageLength(c, req.Message) {
		return
	}
	if msg, ok := resolveCreateIncidentStatus(&req); !ok {
		respondIncidentValidation(c, msg)
		return
	}

	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}
	// The route has already checked the kind from the body; the kind that
	// the handler actually creates is checked again here independently.
	if req.IsMaintenance && !h.userHasPermissionOnPrivatePage(c, tenantID, permissions.Perm(resourceMaintenance, permissions.Create)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to create maintenance windows", "code": "forbidden"})
		return
	}

	var components []models.Component
	if err := h.DB.Where("id IN ? AND status_page_id = ?", req.ComponentIDs, tenantID).Find(&components).Error; err != nil || len(components) != len(req.ComponentIDs) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Eine oder mehrere Komponenten wurden für diese Statusseite nicht gefunden", "code": "components_not_found"})
		return
	}

	tags, err := h.resolveTagsByIDs(req.TagIDs, tenantID)
	if err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}
	if !h.userHasTagAccess(c, tags) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to create incidents with these tags", "code": "forbidden"})
		return
	}
	if !h.userMayAssignTags(c, tags, nil) {
		return
	}

	incident := models.Incident{
		StatusPageID:        tenantID,
		Title:               req.Title,
		Message:             req.Message,
		Status:              req.Status,
		IsMaintenance:       req.IsMaintenance,
		ScheduledStartAt:    req.ScheduledStartAt,
		ScheduledEndAt:      req.ScheduledEndAt,
		ReminderLeadMinutes: req.ReminderLeadMinutes,
	}
	if !req.IsMaintenance {
		incident.StartedAt = req.StartedAt
		incident.ResolvedAt = req.ResolvedAt
		incident.Severity = cmp.Or(req.Severity, models.IncidentSeverityDegraded)
	}

	if err := h.createIncidentWithComponents(&incident, components, tags); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create incident", "code": "server_error"})
		return
	}
	// a maintenance created already in progress sets its components
	// immediately; the scheduler only touches scheduled/in_progress, past
	// (resolved) maintenances leave the components untouched.
	if incident.IsMaintenance && incident.Status == "in_progress" {
		for _, comp := range components {
			// Groups keep their status "group"; the status is set
			// via the history, so that the change is logged.
			if _, err := statushistory.Set(h.DB, comp.ID, "", componentstatus.UnderMaintenance, models.StatusSourceMaintenance, time.Now()); err != nil {
				slog.Error("Failed to set component under maintenance", "incident_id", incident.ID, "component_id", comp.ID, "error", err)
			}
		}
	}

	h.DispatchNotifications(incident, components, "CREATED")
	h.RecordAudit(c, "incident.created", "incident", "Incident", strconv.FormatUint(uint64(incident.ID), 10), incident.Title, "")

	incident.IncidentComponents = make([]models.IncidentComponent, len(components))
	for i, comp := range components {
		incident.IncidentComponents[i] = models.IncidentComponent{ComponentID: comp.ID, Component: comp, SortOrder: i}
	}
	c.JSON(http.StatusCreated, h.toIncidentResponse(incident))
}

type updateIncidentReq struct {
	Title               string       `json:"title"`
	Message             string       `json:"message" binding:"required"`
	Status              string       `json:"status" binding:"required"`
	Severity            *string      `json:"Severity"` // nil = unchanged
	ComponentIDs        []uint       `json:"ComponentIDs"`
	TagIDs              *[]uint      `json:"TagIDs"` // nil = leave unchanged, [] = remove tags
	ScheduledStartAt    *time.Time   `json:"ScheduledStartAt"`
	ScheduledEndAt      *time.Time   `json:"ScheduledEndAt"`
	ReminderLeadMinutes *int         `json:"ReminderLeadMinutes"`
	StartedAt           OptionalTime `json:"StartedAt"`  // missing = unchanged, null = reset
	ResolvedAt          OptionalTime `json:"ResolvedAt"` // missing = unchanged, null = reset
}

// OptionalTime distinguishes a missing JSON field (Set=false, value stays)
// from an explicit null (Set=true, Time=nil, value is cleared).
// The edit form sends a cleared field as null.
type OptionalTime struct {
	Set  bool
	Time *time.Time
}

// UnmarshalJSON is called by encoding/json only if the field is present in the
// body (also for null), so it is enough to set Set here.
func (o *OptionalTime) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Time = nil
		return nil
	}
	var t time.Time
	if err := json.Unmarshal(b, &t); err != nil {
		return err
	}
	o.Time = &t
	return nil
}

// applyIncidentStatus sets the status of a regular incident and enforces the
// rule "an open incident has no end" in one place:
// every status except resolved clears ResolvedAt. Maintenance windows manage their
// times themselves and are left untouched.
func applyIncidentStatus(incident *models.Incident, status string) {
	incident.Status = status
	if incident.IsMaintenance {
		return
	}
	if !models.IsResolvedIncidentStatus(status) {
		incident.ResolvedAt = nil
	} else if incident.ResolvedAt == nil {
		// Closing an incident records the end, regardless of
		// the path taken (update timeline or edit).
		now := time.Now()
		incident.ResolvedAt = &now
	}
}

// validateIncidentStatusChange checks a new status against the allowed
// set (incident or maintenance). The unchanged existing value remains allowed,
// so that editing old records with an unknown status does not fail.
func validateIncidentStatusChange(incident models.Incident, status string) string {
	if status == incident.Status {
		return ""
	}
	if incident.IsMaintenance && !models.IsValidMaintenanceStatus(status) {
		return errInvalidMaintenanceStatus
	}
	if !incident.IsMaintenance && !models.IsValidIncidentStatus(status) {
		return errInvalidIncidentStatus
	}
	return ""
}

// resolveUpdateIncidentTags resolves req.TagIDs (if set) to tags and
// checks access permission for them. ok=true with newTags=nil means
// "TagIDs was nil, leave tags unchanged".
func (h *Handler) resolveUpdateIncidentTags(c *gin.Context, req updateIncidentReq, tenantID uint, current []models.Tag) (newTags []models.Tag, ok bool) {
	if req.TagIDs == nil {
		return nil, true
	}
	newTags, err := h.resolveTagsByIDs(*req.TagIDs, tenantID)
	if err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return nil, false
	}
	if !h.userHasTagAccess(c, newTags) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to move incidents to these tags", "code": "forbidden"})
		return nil, false
	}
	if !h.userMayAssignTags(c, newTags, current) {
		return nil, false
	}
	return newTags, true
}

// applyIncidentUpdateFields copies the simple fields from req to
// incident. The maintenance window schedule stays changeable before the
// automatic start; the status itself is managed by the scheduler for
// maintenance windows, the frontend sends the unchanged current
// status along here.
func applyIncidentUpdateFields(incident *models.Incident, req updateIncidentReq) {
	if req.Title != "" {
		incident.Title = req.Title
	}
	incident.Message = req.Message
	applyIncidentStatus(incident, req.Status)
	if !incident.IsMaintenance {
		if req.StartedAt.Set {
			incident.StartedAt = req.StartedAt.Time
		}
		if req.ResolvedAt.Set {
			incident.ResolvedAt = req.ResolvedAt.Time
		}
		if req.Severity != nil {
			incident.Severity = *req.Severity
		}
		return
	}
	if req.ScheduledStartAt != nil {
		incident.ScheduledStartAt = req.ScheduledStartAt
	}
	if req.ScheduledEndAt != nil {
		incident.ScheduledEndAt = req.ScheduledEndAt
	}
	if req.ReminderLeadMinutes != nil {
		incident.ReminderLeadMinutes = *req.ReminderLeadMinutes
	}
}

// replaceIncidentComponents replaces the component assignment (newComponents,
// loaded beforehand via resolveIncidentComponents) of an incident, unless empty, and returns the
// current components (unchanged if componentIDs was empty).
func (h *Handler) replaceIncidentComponents(c *gin.Context, incident models.Incident, newComponents []models.Component, current []models.Component) ([]models.Component, bool) {
	if len(newComponents) == 0 {
		return current, true
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(whereIncidentIDEquals, incident.ID).Delete(&models.IncidentComponent{}).Error; err != nil {
			return err
		}
		for sortOrder, comp := range newComponents {
			if err := tx.Create(&models.IncidentComponent{IncidentID: incident.ID, ComponentID: comp.ID, SortOrder: sortOrder}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update component links", "code": "server_error"})
		return nil, false
	}
	return newComponents, true
}

// resolveIncidentComponents loads the components of the page named in the update.
// It runs before any write access: a reference to an unknown or
// foreign component answers the request with 404 without anything
// having been changed on the incident.
func (h *Handler) resolveIncidentComponents(c *gin.Context, componentIDs []uint, tenantID uint) ([]models.Component, bool) {
	if len(componentIDs) == 0 {
		return nil, true
	}
	var comps []models.Component
	if err := h.DB.Where("id IN ? AND status_page_id = ?", componentIDs, tenantID).Find(&comps).Error; err != nil || len(comps) != len(componentIDs) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Eine oder mehrere Komponenten wurden für diese Statusseite nicht gefunden", "code": "components_not_found"})
		return nil, false
	}
	return comps, true
}

func (h *Handler) UpdateIncidentHandler(c *gin.Context) {
	incIDStr := c.Param("id")
	incID, err := strconv.ParseUint(incIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid incident ID", "code": "invalid_id"})
		return
	}

	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}

	var incident models.Incident
	if err := h.DB.Preload("IncidentComponents.Component").Where(whereIDAndStatusPageID, incID, tenantID).First(&incident).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errIncidentNotFound, "code": "incident_not_found"})
		return
	}

	if !h.userHasTagAccess(c, h.tagsForIncident(incident.ID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to update this incident", "code": "forbidden"})
		return
	}

	var req updateIncidentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	if !requireTitleLength(c, req.Title) {
		return
	}
	if !requireMessageLength(c, req.Message) {
		return
	}

	newTags, ok := h.resolveUpdateIncidentTags(c, req, tenantID, h.tagsForIncident(incident.ID))
	if !ok {
		return
	}

	if msg := validateIncidentStatusChange(incident, req.Status); msg != "" {
		respondIncidentValidation(c, msg)
		return
	}
	if req.Severity != nil && !incident.IsMaintenance && !models.IsValidIncidentSeverity(*req.Severity) {
		respondIncidentValidation(c, errInvalidIncidentSeverity)
		return
	}
	applyIncidentUpdateFields(&incident, req)
	if !incident.IsMaintenance {
		if msg := validateIncidentTimes(incident.Status, incident.StartedAt, incident.ResolvedAt); msg != "" {
			respondIncidentValidation(c, msg)
			return
		}
	}

	newComponents, ok := h.resolveIncidentComponents(c, req.ComponentIDs, tenantID)
	if !ok {
		return
	}

	if err := h.DB.Save(&incident).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update incident", "code": "server_error"})
		return
	}

	components := make([]models.Component, len(incident.IncidentComponents))
	for i, ic := range incident.IncidentComponents {
		components[i] = ic.Component
	}
	components, ok = h.replaceIncidentComponents(c, incident, newComponents, components)
	if !ok {
		return
	}

	if req.TagIDs != nil {
		if err := h.replaceIncidentTags(h.DB, incident.ID, newTags); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tags", "code": "server_error"})
			return
		}
	}

	h.DispatchNotifications(incident, components, "UPDATED")
	h.RecordAudit(c, "incident.status_changed", "incident", "Incident", strconv.FormatUint(uint64(incident.ID), 10), incident.Title, "")

	incident.IncidentComponents = make([]models.IncidentComponent, len(components))
	for i, comp := range components {
		incident.IncidentComponents[i] = models.IncidentComponent{ComponentID: comp.ID, Component: comp, SortOrder: i}
	}
	c.JSON(http.StatusOK, h.toIncidentResponse(incident))
}

func (h *Handler) DeleteIncidentHandler(c *gin.Context) {
	idStr := c.Param("id")
	incID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid incident ID", "code": "invalid_id"})
		return
	}

	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}

	var inc models.Incident
	if err := h.DB.Where("id = ? AND status_page_id = ?", incID, tenantID).First(&inc).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Incident not found", "code": "incident_not_found"})
		return
	}

	if !h.userHasTagAccess(c, h.tagsForIncident(inc.ID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to delete this incident", "code": "forbidden"})
		return
	}

	// Explicit cleanup of the join rows: Incident uses soft delete
	// (gorm.Model → UPDATE instead of DELETE), a DB-side OnDelete:CASCADE would
	// never fire in that case.
	if err := h.DB.Where("incident_id = ?", inc.ID).Delete(&models.IncidentComponent{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clean up component links", "code": "server_error"})
		return
	}

	if err := h.DB.Delete(&inc).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete incident", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "incident.deleted", "incident", "Incident", strconv.FormatUint(uint64(inc.ID), 10), inc.Title, "")
	c.JSON(http.StatusOK, gin.H{"message": "Incident deleted successfully"})
}

// headerInjectionPattern matches CR or LF, the characters that would let a
// crafted incident field or email address inject extra SMTP headers/recipients
// into the raw message built in sendAlertEmail (CodeQL go/email-injection,
// alerts dismissed as won't-fix: the guard below is not recognized as a
// barrier by the query's static taint tracking, but does close the risk at
// runtime).
var headerInjectionPattern = regexp.MustCompile(`[\r\n]`)

// foldBodyLines folds CR/LF (also CRLF) in the mail body into one space each;
// in HTML a line break is just whitespace anyway (breaks are made with <br>/<p>).
func foldBodyLines(s string) string {
	return bodyLineBreakPattern.ReplaceAllString(s, " ")
}

var bodyLineBreakPattern = regexp.MustCompile(`\r\n|\r|\n`)

// hasHeaderInjectionChars reports whether s contains CR/LF and must not be
// embedded raw into an SMTP header or message body.
func hasHeaderInjectionChars(s string) bool {
	return headerInjectionPattern.MatchString(s)
}

// incidentStatusMeta mirrors the STATUS_META color/label mapping used on the
// public status page (frontend/src/views/public/StatusPage.vue) and the admin
// incident views, so notification emails visually match the app instead of
// showing a raw, unstyled status string. Colors are the flat hex values of
// the corresponding --status-* CSS custom properties (frontend/src/assets/tokens.css) -
// email clients can't resolve CSS variables, so the values are inlined directly.
type incidentStatusMetaEntry struct {
	labelEN string
	labelDE string
	color   string
	bg      string
}

var incidentStatusMeta = map[string]incidentStatusMetaEntry{
	"investigating": {"Investigating", "Untersuchung", "#d64b45", "#fdecea"},
	"identified":    {"Identified", "Identifiziert", "#e0723c", "#fdf1e8"},
	"monitoring":    {"Monitoring", "Beobachtung", "#c98a1e", "#fdf3e0"},
	"resolved":      {"Resolved", "Behoben", "#2c9c6a", "#e9f7f0"},
	"scheduled":     {"Scheduled", "Geplant", "#3d7edb", "#eaf2fc"},
}

// notificationEmailParams bundles the already HTML-escaped, ready-to-embed
// values for buildNotificationEmailHTML. Escaping happens once in the caller
// (DispatchNotifications) rather than here, to keep the escaping
// guarantee in a single, auditable place.
type notificationEmailParams struct {
	lang        string // "en" or "de"
	actionVerb  string // lower-cased action, e.g. "created", "updated", "maintenance_reminder"
	compNames   string // HTML-escaped
	title       string // HTML-escaped
	status      string // raw incident status, looked up in incidentStatusMeta (not itself rendered raw)
	country     string // HTML-escaped
	message     string // finished, single-line mail HTML from richtext.ToMailHTML
	frontendURL string
	// unsubscribeURL: only set for subscriber mails (empty for
	// internal users who have no subscriber token) - renders an
	// additional unsubscribe link in the footer if not empty.
	unsubscribeURL string
}

// rawNotificationEmailTemplate is the multi-line source of the notification
// email body, kept readable/indented here for maintainability. It is
// compacted to a single line (see notificationEmailTemplate below) before use,
// since sendAlertEmail's hasHeaderInjectionChars guard rejects the composed
// body outright if it contains ANY \r/\n - a deliberate defense against
// SMTP-header-injection via attacker-controlled incident fields (Title/
// Message aren't newline-stripped by html.EscapeString). That check must see
// only newlines that came from actual attacker input, not from this template's
// own formatting, so the static markup is flattened first and the %s
// placeholders are substituted afterwards - any \r/\n hiding inside a
// substituted value therefore still survives into the final string and still
// trips the guard, exactly as before this template existed.
const rawNotificationEmailTemplate = `<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f5f6f8;padding:32px 16px;">
  <div style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #e6e8ec;border-radius:10px;overflow:hidden;">
    <div style="background:#4c6ef5;padding:20px 24px;">
      <span style="color:#ffffff;font-size:16px;font-weight:600;">Statushafen</span>
    </div>
    <div style="padding:24px;">
      <h2 style="margin:0 0 12px;font-size:18px;color:#1a2230;">%s</h2>
      <p style="margin:0 0 16px;color:#57616f;font-size:14px;line-height:1.5;">%s</p>
      <table role="presentation" style="width:100%%;border-collapse:collapse;margin-bottom:20px;">
        <tr>
          <td style="padding:6px 0;color:#8b95a3;font-size:13px;width:110px;">%s</td>
          <td style="padding:6px 0;color:#1a2230;font-size:13px;font-weight:600;">%s</td>
        </tr>
        <tr>
          <td style="padding:6px 0;color:#8b95a3;font-size:13px;">%s</td>
          <td style="padding:6px 0;">
            <span style="display:inline-block;padding:2px 10px;border-radius:12px;background:%s;color:%s;font-size:12px;font-weight:600;">%s</span>
          </td>
        </tr>
      </table>
      <p style="margin:0 0 4px;color:#8b95a3;font-size:12px;text-transform:uppercase;letter-spacing:0.4px;">%s</p>
      <div style="margin:0 0 20px;color:#1a2230;font-size:14px;line-height:1.6;">%s</div>
      <a href="%s" style="display:inline-block;background:#4c6ef5;color:#ffffff;text-decoration:none;font-size:14px;font-weight:600;padding:10px 20px;border-radius:6px;">%s</a>
    </div>
    <div style="padding:16px 24px;border-top:1px solid #e6e8ec;">
      <p style="margin:0;color:#8b95a3;font-size:12px;">Statushafen</p>
      %s
    </div>
  </div>
</div>`

// notificationEmailTemplate is rawNotificationEmailTemplate with its own
// formatting newlines/tabs stripped - computed once at package init, see
// rawNotificationEmailTemplate's comment for why this matters.
var notificationEmailTemplate = strings.NewReplacer("\n", "", "\t", "").Replace(rawNotificationEmailTemplate)

// buildNotificationEmailHTML renders a branded, inline-styled HTML email body.
// Inline styles are required (not a <style> block or external stylesheet) -
// most webmail clients (Gmail, Outlook.com) strip both. Kept intentionally
// simple/table-free beyond one small meta table, since this is still a static
// Go template, not a user-customizable one - the planned
// admin-configurable template system will eventually replace it.
func buildNotificationEmailHTML(p notificationEmailParams) string {
	meta, known := incidentStatusMeta[p.status]
	statusLabel := html.EscapeString(p.status)
	color, bg := "#8b95a3", "#f1f2f4"
	if known {
		color, bg = meta.color, meta.bg
		if p.lang == "de" {
			statusLabel = meta.labelDE
		} else {
			statusLabel = meta.labelEN
		}
	}

	var heading, actionSentence, titleLabel, statusLabelText, descLabel, linkLabel, unsubscribeLabel string
	if p.lang == "de" {
		heading = "Status-Update für " + p.compNames
		actionSentence = fmt.Sprintf("Ein Vorfall wurde %s für <strong>%s</strong> (Region: %s).", p.actionVerb, p.compNames, p.country)
		titleLabel, statusLabelText, descLabel, linkLabel = "Titel", "Status", "Beschreibung", "Statusseite ansehen"
		unsubscribeLabel = "Abbestellen"
	} else {
		heading = "Status update for " + p.compNames
		actionSentence = fmt.Sprintf("An incident was %s for <strong>%s</strong> (Region: %s).", p.actionVerb, p.compNames, p.country)
		titleLabel, statusLabelText, descLabel, linkLabel = "Title", "Status", "Description", "View status page"
		unsubscribeLabel = "Unsubscribe"
	}

	unsubscribeHTML := ""
	if p.unsubscribeURL != "" {
		unsubscribeHTML = fmt.Sprintf(`<a href="%s" style="color:#8b95a3;font-size:11px;text-decoration:underline;">%s</a>`, p.unsubscribeURL, unsubscribeLabel)
	}

	return fmt.Sprintf(notificationEmailTemplate,
		heading, actionSentence,
		titleLabel, p.title,
		statusLabelText, bg, color, statusLabel,
		descLabel, p.message,
		p.frontendURL, linkLabel,
		unsubscribeHTML)
}

// sendAlertEmail sends a single incident-alert email over the configured SMTP
// connection. recipientEmail is validated via mail.ParseAddress, and
// subjectText/bodyHTML/smtpCfg.From are rejected outright if they contain
// CR/LF, closing the header-injection sink CodeQL flags on the raw msg string
// below (go/email-injection) - callers are expected to have already validated
// the incident fields that feed subjectText/bodyHTML via hasHeaderInjectionChars.
// extraHeaders: optional, complete
// header lines like List-Unsubscribe; they must not contain CR/LF either.
func (h *Handler) sendAlertEmail(smtpCfg smtpConfig, recipientEmail, subjectText, bodyHTML, logLabel string, incID uint, incTitle string, extraHeaders ...string) {
	addr, err := mail.ParseAddress(recipientEmail)
	if err != nil {
		slog.Error("Skipping notification: invalid recipient address", "email", recipientEmail, "error", err)
		return
	}
	recipient := addr.Address

	// The body may be multi-line (templates, multi-line
	// {{.Message}}); it is folded into one line instead of
	// discarding the mail. Subject and sender are headers and stay strict.
	bodyHTML = foldBodyLines(bodyHTML)
	if hasHeaderInjectionChars(subjectText) || hasHeaderInjectionChars(bodyHTML) || hasHeaderInjectionChars(smtpCfg.From) {
		slog.Error("Skipping notification: subject/body/sender contain invalid CR/LF characters", "email", recipient)
		return
	}
	extra := ""
	for _, header := range extraHeaders {
		if hasHeaderInjectionChars(header) {
			slog.Error("Skipping notification: extra header contains invalid CR/LF characters", "email", recipient)
			return
		}
		extra += header + "\n"
	}

	subject := "Subject: " + subjectText + "\n"
	mime := "MIME-Version: 1.0\nContent-Type: text/html; charset=UTF-8\n\n"
	// smtpCfg.From is already in the full "Display Name <address>" format
	// (SMTP_FROM env var, see loadSMTPConfig) - additional wrapping here
	// produced nested angle brackets (invalid RFC 5322 From header),
	// which many recipients (including Outlook.com) silently discard.
	msg := smtpCfg.fromHeaders() +
		"To: " + recipient + "\n" +
		subject + extra + mime + bodyHTML

	incIDStr := strconv.FormatUint(uint64(incID), 10)
	sent, err := h.deliverMail(smtpCfg, recipient, []byte(msg), mailAuditTarget{Category: "incident", TargetType: "Incident", TargetID: incIDStr, TargetName: incTitle})
	if err != nil {
		slog.Error("Failed to send email to "+logLabel, "email", recipient, "smtp_addr", smtpCfg.addr(), "error", err)
		h.RecordSystemAudit("notification.email_failed", "incident", "Incident", incIDStr, incTitle,
			fmt.Sprintf("recipient=%s smtp_addr=%s error=%s", recipient, smtpCfg.addr(), err.Error()))
		return
	}
	if sent {
		slog.Info("Real Email sent to "+logLabel+" via SMTP", "email", recipient, "smtp_addr", smtpCfg.addr())
	}
}

// DispatchNotifications sends real SMTP emails and calls the Google Chat webhook.
// comps: all affected components (previously a single Component).
// notificationDispatchContext bundles all values that DispatchNotifications
// computes once and that the individual recipient
// notification paths (internal users, subscribers, global webhook)
// need. Extracted from DispatchNotifications so that its
// cognitive complexity does not exceed the SonarCloud limit (go:S3776).
type notificationDispatchContext struct {
	inc          models.Incident
	action       string
	compNames    string
	tagNames     string
	componentIDs []uint
	page         models.StatusPage
	smtpCfg      smtpConfig
	frontendURL  string
	incidentTags []models.Tag
	// staffMail returns subject and text for an internal user in their
	// UI language (User.PreferredLanguage); empty/unknown = English.
	staffMail func(preferred string) (subject, body string)
	// subscriberMail returns the template, placeholder data and default text parameters
	// in a subscriber's language; "" = site default language.
	subscriberMail func(preferred string) incidentSubscriberMail
	// chatLang: effective language (de/en) of the subscriber for chat texts,
	// independent of existing mail templates.
	chatLang       func(preferred string) string
	defaultSubject string
}

// subscriberLang is the mail/text language (de/en) for a subscriber; without a
// prepared language choice it stays at the previous English.
func (ctx notificationDispatchContext) subscriberLang(preferred string) string {
	if ctx.chatLang == nil {
		return "en"
	}
	return ctx.chatLang(preferred)
}

// incidentSubscriberMail is the language-dependent part of the subscriber mail.
type incidentSubscriberMail struct {
	tmpl       *models.NotificationTemplate
	dataBase   NotificationTemplateData
	bodyParams notificationEmailParams
}

// buildNotificationDispatchContext computes once all values needed for
// sending (SMTP config, templates, HTML-escaped fields) before the
// recipient loops access them.
func (h *Handler) buildNotificationDispatchContext(inc models.Incident, action, compNames string, componentIDs []uint) notificationDispatchContext {
	smtpCfg := loadSMTPConfig()
	// a status page can store its own sender
	// (StatusPage.NotificationFromAddress), which is then used instead of the global
	// SMTP_FROM. Only the From field is overridden - host/
	// port/auth stay the central SMTP connection, since only the sender
	// is tenant-specific, not the entire mail delivery. If nothing is
	// stored, a page-specific default from
	// Slug + Domain (resolveNotificationFromAddress).
	// is needed to know whether the status page is private
	// (then the "view status page" link in the subscriber mail gets an
	// access token) and for the slug-specific link itself.
	var page models.StatusPage
	h.DB.First(&page, inc.StatusPageID)
	smtpCfg.applyPageSender(page)
	frontendURL := getFrontendURL()
	incidentTags := h.tagsForIncident(inc.ID)
	tagNames := tagNameList(incidentTags)

	// bodyEN/bodyDE are text/html email bodies - title, message,
	// component names and tags come from user input (incident form)
	// and must therefore be HTML-escaped before being inserted here.
	// Without that, an editor with incidents:write could inject arbitrary HTML/markup into
	// the mails sent to subscribers/users. The subjects
	// stay unescaped because they are inserted into an SMTP header (not HTML) -
	// there hasHeaderInjectionChars already protects against CR/LF.
	escCompNames := html.EscapeString(compNames)
	escTitle := html.EscapeString(inc.Title)
	escTagNames := html.EscapeString(tagNames)
	// The message is restricted Markdown; richtext.ToMailHTML
	// escapes all text and produces only fixed tags (no raw HTML) and is
	// always single-line, so it passes the CR/LF guard of sendAlertEmail.
	messageHTML := richtext.ToMailHTML(inc.Message)

	// a custom template can be stored per status page.
	// Lookup once per language before the recipient loops, so that the
	// render paths below only have to apply the custom template per recipient
	// or return the prepared default.
	notifKind := resolveIncidentKind(action, inc.Status)
	// Internal users get the mail in their UI language
	// (de/en); template (audience staff) in that language, otherwise the English
	// template of the page, otherwise the built-in text in their language. Cached per
	// language because the loop runs over all users.
	subTemplate := h.memoSubscriberTemplate(inc.StatusPageID, notifKind)
	actionVerb := strings.ToLower(action)

	defaultSubject := fmt.Sprintf("[ALERT] %s: %s (%s)", compNames, inc.Title, inc.Status)
	staffCache := map[string][2]string{}
	fixedLang := h.fixedStaffMailLang(inc.StatusPageID)
	staffMail := func(preferred string) (string, string) {
		lang := staffMailLang(preferred)
		// Switch off: everyone gets the page default language.
		if fixedLang != "" {
			lang = fixedLang
		}
		if r, ok := staffCache[lang]; ok {
			return r[0], r[1]
		}
		tmpl, tmplLang := h.staffNotificationTemplate(inc.StatusPageID, notifKind, lang)
		data := newIncidentTemplateData(incidentTemplateDataOpts{
			Title: inc.Title, StatusRaw: inc.Status, CompNames: compNames,
			TagNames: tagNames, Message: inc.Message, MessageHTML: messageHTML, Action: actionVerb,
			Language: tmplLang, FrontendURL: frontendURL, FromAddress: smtpCfg.From,
		})
		defaultBody := buildNotificationEmailHTML(notificationEmailParams{
			lang:        lang,
			actionVerb:  actionVerb,
			compNames:   escCompNames,
			title:       escTitle,
			status:      inc.Status,
			country:     escTagNames,
			message:     messageHTML,
			frontendURL: frontendURL,
		})
		subject, body := applyNotificationTemplate(tmpl, data, defaultSubject, defaultBody)
		staffCache[lang] = [2]string{subject, body}
		return subject, body
	}

	// The subject stays the same for all subscribers, whereas the body is
	// built fresh per subscriber (see notifySubscriberEmail) - since the switch it contains
	// an individual unsubscribe token and, for private
	// pages, an individual access token in the "view status page" link.
	subscriberMail := func(preferred string) incidentSubscriberMail {
		tmpl, subLang := subTemplate(preferred)
		return incidentSubscriberMail{
			tmpl: tmpl,
			dataBase: newIncidentTemplateData(incidentTemplateDataOpts{
				Title: inc.Title, StatusRaw: inc.Status, CompNames: compNames,
				TagNames: tagNames, Message: inc.Message, MessageHTML: messageHTML, Action: actionVerb,
				Language: subLang, FromAddress: smtpCfg.From,
			}),
			bodyParams: notificationEmailParams{
				lang:       subLang,
				actionVerb: actionVerb,
				compNames:  escCompNames,
				title:      escTitle,
				status:     inc.Status,
				country:    escTagNames,
				message:    messageHTML,
			},
		}
	}

	return notificationDispatchContext{
		inc:            inc,
		action:         action,
		compNames:      compNames,
		tagNames:       tagNames,
		componentIDs:   componentIDs,
		page:           page,
		smtpCfg:        smtpCfg,
		frontendURL:    frontendURL,
		incidentTags:   incidentTags,
		staffMail:      staffMail,
		subscriberMail: subscriberMail,
		chatLang:       h.memoSubscriberChatLang(inc.StatusPageID),
		defaultSubject: defaultSubject,
	}
}

// notifyInternalUsers notifies only users who are either
// global admins or have a role on exactly this status page.
func (h *Handler) notifyInternalUsers(ctx notificationDispatchContext) {
	var users []models.User
	h.DB.Where(
		"is_global_admin = ? OR id IN (?)",
		true,
		h.DB.Model(&models.UserStatusPageRole{}).Select("user_id").Where(whereStatusPageIDEquals, ctx.inc.StatusPageID),
	).FindInBatches(&users, 100, func(tx *gorm.DB, batch int) error {
		for _, user := range users {
			// Tag-based successor of UserHasAccessToCountry.
			// Same "only restrict if both sides have tags" logic
			// as userHasTagAccess: global admins, users without their own tags and
			// incidents without their own tag bypass the check - the already
			// completed page-role filtering above (is_global_admin OR
			// UserStatusPageRole) is the actual access control, tags
			// are only an additional, optional narrowing.
			userTags := h.tagsForUser(user.ID)
			if user.IsGlobalAdmin || len(userTags) == 0 || len(ctx.incidentTags) == 0 || middleware.UserHasAccessToAnyTag(userTags, ctx.incidentTags) {
				subject, body := ctx.staffMail(user.PreferredLanguage)
				h.sendAlertEmail(ctx.smtpCfg, user.Email, subject, body, "internal user", ctx.inc.ID, ctx.inc.Title)
			}
		}
		return nil
	})
}

// incidentGoogleChatPayload builds the Google Chat message text that is used both
// for individual subscriber webhooks and for the global fallback webhook.
// The message goes out as plain text (Markdown
// becomes readable text, links as "Text (URL)"), not as raw Markdown.
//
// lang (de/en) selects the fixed labels; subscribers get
// their chosen language or that of the page, the global webhook stays English.
func incidentGoogleChatPayload(ctx notificationDispatchContext, lang string) map[string]interface{} {
	if lang == "de" {
		return map[string]interface{}{
			"text": fmt.Sprintf("🚨 **[ALERT] Status-Update** (%s)\n**Komponente:** %s (Tags: %s)\n**Vorfall:** %s\n**Status:** %s\n**Details:** %s",
				ctx.action, ctx.compNames, ctx.tagNames, ctx.inc.Title, statusLabelFor(ctx.inc.Status, "de"), richtext.ToPlainText(ctx.inc.Message)),
		}
	}
	return map[string]interface{}{
		"text": fmt.Sprintf("🚨 **[ALERT] Status Update** (%s)\n**Component:** %s (Tags: %s)\n**Incident:** %s\n**Status:** %s\n**Details:** %s",
			ctx.action, ctx.compNames, ctx.tagNames, ctx.inc.Title, ctx.inc.Status, richtext.ToPlainText(ctx.inc.Message)),
	}
}

// notifySubscriberGoogleChat sends the Google Chat webhook message to
// a single subscriber.
func (h *Handler) notifySubscriberGoogleChat(ctx notificationDispatchContext, sub models.Subscriber) {
	if sub.WebhookURL == "" {
		slog.Error("Subscriber has google_chat channel but no WebhookURL", "subscriber_id", sub.ID)
		return
	}
	// SSRF protection (late addition): postWebhook sends via
	// webhookTestClient so that the resolved IP is checked against the blocklist
	// again right before connecting (see safeDialContext in
	// subscriber.go) - a normal http.Client would resolve the host again during
	// the actual request and would thus be vulnerable to
	// DNS rebinding between subscription creation and incident dispatch.
	// Rate limit/retry/logging centrally there.
	res := postWebhook(context.Background(), sub.WebhookURL, incidentGoogleChatPayload(ctx, ctx.subscriberLang(sub.Language)), webhookModeNotify)
	if !res.OK() {
		slog.Error("Failed to send webhook to subscriber", "subscriber_id", sub.ID, "status", res.Status, "class", res.Class)
		// Admins see rate limiting and permanent rejection in the bell.
		h.reportWebhookProblem(sub.StatusPageID, sub.ID, "google_chat", res)
		return
	}
	slog.Info("Webhook triggered for subscriber", "subscriber_id", sub.ID, "response_status", res.Status)
}

// notifySubscriberEmail builds the individual (token/unsubscribe link)
// mail content for an email subscriber and sends it.
func (h *Handler) notifySubscriberEmail(ctx notificationDispatchContext, sub models.Subscriber) {
	mail := ctx.subscriberMail(sub.Language)
	body := mail.bodyParams
	body.frontendURL = ctx.frontendURL
	perSubData := mail.dataBase
	perSubData.FrontendURL = ctx.frontendURL
	var extraHeaders []string
	if token, err := middleware.GenerateSubscriberToken(sub.ID, sub.StatusPageID); err == nil {
		extraHeaders = listUnsubscribeHeaders(ctx.frontendURL, token)
		body.unsubscribeURL = ctx.frontendURL + "/unsubscribe?token=" + url.QueryEscape(token)
		perSubData.UnsubscribeURL = body.unsubscribeURL
		// for private pages the previous, non-slug-specific
		// frontendURL link is not enough - without login (which a mere
		// subscriber does not have) it would only lead to a login
		// prompt. The token in the link instead grants direct read access
		// to exactly this page (see subscriberTokenGrantsPageAccess).
		if ctx.page.IsPrivate {
			body.frontendURL = ctx.frontendURL + "/status/" + ctx.page.Slug + "?token=" + url.QueryEscape(token)
			perSubData.FrontendURL = body.frontendURL
		}
	} else {
		slog.Error("Failed to generate subscriber token for notification email", "subscriber_id", sub.ID, "error", err)
	}
	perSubSubject, perSubBody := applyNotificationTemplate(mail.tmpl, perSubData, ctx.defaultSubject, buildNotificationEmailHTML(body))
	h.sendAlertEmail(ctx.smtpCfg, sub.Email, perSubSubject, perSubBody, "public subscriber", ctx.inc.ID, ctx.inc.Title, extraHeaders...)
}

// notifySubscriber notifies a single subscriber via
// the channel chosen at subscription time, provided they subscribed to one of the affected
// components.
func (h *Handler) notifySubscriber(ctx notificationDispatchContext, sub models.Subscriber) {
	if !SubscriberHasAnyComponent(sub.SubscribedComponentIDs, ctx.componentIDs) {
		return
	}
	// Incidents of private pages only to subscriptions whose
	// creator may still see the page (see subscriberCreatorMayReadPage).
	if ctx.page.IsPrivate && !h.subscriberCreatorMayReadPage(sub, ctx.page.ID) {
		slog.Info("Skipping subscriber on private page without authorized creator", "subscriber_id", sub.ID)
		return
	}
	if sub.Channel == "google_chat" {
		h.notifySubscriberGoogleChat(ctx, sub)
		return
	}
	h.notifySubscriberEmail(ctx, sub)
}

// notifySubscribers notifies all email/webhook
// subscribers of the affected status page.
func (h *Handler) notifySubscribers(ctx notificationDispatchContext) {
	var subscribers []models.Subscriber
	// unconfirmed (pending) subscriptions get no notifications.
	h.DB.Where("status_page_id = ? AND status = ?", ctx.inc.StatusPageID, models.SubscriberStatusActive).FindInBatches(&subscribers, 100, func(tx *gorm.DB, batch int) error {
		for _, sub := range subscribers {
			h.notifySubscriber(ctx, sub)
		}
		return nil
	})
}

// dispatchGlobalGoogleChatWebhook additionally sends to the (optional)
// process-wide Google Chat webhook from GOOGLE_CHAT_WEBHOOK_URL, independent
// of subscriber-specific webhooks. The webhook belongs to the operator and
// sees incidents of all tenants; therefore only incidents of public
// pages go into it, whose content anyone can read anyway. Incidents of private
// pages stay with the tenant. If the page is not loaded (ID 0, e.g. after
// a database error), nothing is sent (fail closed).
func dispatchGlobalGoogleChatWebhook(ctx notificationDispatchContext) {
	webhookURL := os.Getenv("GOOGLE_CHAT_WEBHOOK_URL")
	if webhookURL == "" {
		return
	}
	if ctx.page.ID == 0 || ctx.page.IsPrivate {
		return
	}
	res := postWebhook(context.Background(), webhookURL, incidentGoogleChatPayload(ctx, "en"), webhookModeNotify)
	if !res.OK() {
		slog.Error("Webhook sending failed", "status", res.Status, "class", res.Class)
		return
	}
	slog.Info("Webhook triggered", "response_status", res.Status)
}

// DispatchNotifications sends real SMTP emails and calls the Google Chat webhook.
// comps: all affected components (previously a single Component).
func (h *Handler) DispatchNotifications(inc models.Incident, comps []models.Component, action string) {
	compNames := componentNames(comps)
	componentIDs := make([]uint, len(comps))
	for i, c := range comps {
		componentIDs[i] = c.ID
	}
	h.dispatchWG.Add(1)
	go func() {
		defer h.dispatchWG.Done()
		slog.Info("Dispatching real notifications for incident",
			"incident_id", inc.ID,
			"incident_title", inc.Title,
			"action", action,
		)

		ctx := h.buildNotificationDispatchContext(inc, action, compNames, componentIDs)
		h.notifyInternalUsers(ctx)
		h.notifySubscribers(ctx)
		dispatchGlobalGoogleChatWebhook(ctx)
	}()
}

// WaitNotifications blocks until all dispatches started with DispatchNotifications
// have finished. Tests wait with it before they reset replaced variables;
// it is not needed in production.
func (h *Handler) WaitNotifications() { h.dispatchWG.Wait() }

func SubscriberHasComponent(subscribedIDs string, targetID uint) bool {
	if subscribedIDs == "all" {
		return true
	}
	ids := strings.Split(subscribedIDs, ",")
	targetStr := fmt.Sprintf("%d", targetID)
	for _, id := range ids {
		if strings.TrimSpace(id) == targetStr {
			return true
		}
	}
	return false
}

// SubscriberHasAnyComponent: an incident can now affect several
// components - a subscriber is notified if they subscribed to
// at least one of them.
func SubscriberHasAnyComponent(subscribedIDs string, targetIDs []uint) bool {
	for _, id := range targetIDs {
		if SubscriberHasComponent(subscribedIDs, id) {
			return true
		}
	}
	return false
}
