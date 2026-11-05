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

// IncidentResponse flacht IncidentComponents zu einer sortierten
// Components-Liste ab, damit das Frontend nicht die interne
// Join-Struktur (IncidentComponents[].Component) kennen muss.
type IncidentResponse struct {
	models.Incident
	Components []models.Component `json:"Components"`
	// Tags (Ersatz für das frühere Country-Feld) - analog zu
	// Components oben manuell befüllt, da Incident keine GORM-Tags-Relation
	// hat (explizites IncidentTag-Join-Modell, siehe tagsForIncident).
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

// componentNames extrahiert die Namen für Notification-Texte (ein
// Incident kann jetzt mehrere Komponenten betreffen).
func componentNames(comps []models.Component) string {
	names := make([]string, 0, len(comps))
	for _, c := range comps {
		names = append(names, c.Name)
	}
	return strings.Join(names, ", ")
}

// componentIDsWithOpenIncidents: offene, normale Vorfälle je Komponente
// seit der Umstellung in componentstatus.OpenIncidentComponentIDs.
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

	// Wartungsfenster nutzen dasselbe Incident-Modell, aber einen
	// eigenen Admin-Menüpunkt statt in der normalen Incident-Liste
	// aufzutauchen. ?is_maintenance=true schaltet auf die Wartungsfenster-Sicht,
	// ?is_maintenance=any liefert beides (Verlauf der öffentlichen Seite),
	// Default false hält die bestehende Incident-Liste unverändert.
	// ?state=open|resolved filtert nach offen/behoben; limit/offset blättern
	// (Standard 50, höchstens 200), die Liste ist nie unbegrenzt.
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

// stripHiddenComponentsForCaller entfernt archivierte und unsichtbare
// Komponenten aus den Zuordnungen der Vorfälle, wenn der
// Aufrufer sie nicht verwalten darf: die Vorfall-Routen sind anonym lesbar.
func (h *Handler) stripHiddenComponentsForCaller(c *gin.Context, pageID uint, incidents []models.Incident) {
	if h.callerMaySeeHiddenComponents(c, pageID) {
		return
	}
	var comps []models.Component
	if err := h.DB.Select("id", "parent_id", "visible", "archived_at").Where("status_page_id = ?", pageID).Find(&comps).Error; err != nil {
		// Fail-closed: lieber keine Komponenten nennen als versteckte preiszugeben.
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
	Severity     string `json:"Severity"` // leer = degraded_performance
	ComponentIDs []uint `json:"ComponentIDs" binding:"required,min=1"`
	TagIDs       []uint `json:"TagIDs"`
	// Wartungsfenster: Status wird bei IsMaintenance vom
	// Scheduler verwaltet, nicht vom Client vorgegeben.
	IsMaintenance       bool       `json:"IsMaintenance"`
	ScheduledStartAt    *time.Time `json:"ScheduledStartAt"`
	ScheduledEndAt      *time.Time `json:"ScheduledEndAt"`
	ReminderLeadMinutes int        `json:"ReminderLeadMinutes"`
	// Ereigniszeiten normaler Vorfälle.
	StartedAt  *time.Time `json:"StartedAt"`
	ResolvedAt *time.Time `json:"ResolvedAt"`
}

// deriveMaintenanceStatus leitet den Status eines Wartungsfensters aus seinen
// Zeiten ab: Ende <= jetzt -> resolved, Start <= jetzt < Ende ->
// in_progress, sonst scheduled. Dieselben Grenzen wie der Scheduler.
func deriveMaintenanceStatus(start, end, now time.Time) string {
	if !end.After(now) {
		return "resolved"
	}
	if !start.After(now) {
		return "in_progress"
	}
	return "scheduled"
}

// validateIncidentTimes prüft StartedAt/ResolvedAt eines normalen Vorfalls
// ResolvedAt nur bei Status resolved, Ende nicht vor Beginn.
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

// incidentValidationCode ordnet den festen Texten der Status-/Zeitprüfungen
// einen Fehlercode zu.
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

// respondIncidentValidation beantwortet eine fehlgeschlagene Status-/Zeitprüfung
// mit 400, Text und Code.
func respondIncidentValidation(c *gin.Context, msg string) {
	code, ok := incidentValidationCodes[msg]
	if !ok {
		code = "invalid_request"
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": code})
}

// resolveCreateIncidentStatus validiert/berechnet den initialen Status je
// nachdem, ob es sich um ein Wartungsfenster handelt. Ausgelagert aus
// CreateIncidentHandler, damit dessen kognitive Komplexität die
// SonarCloud-Grenze (go:S3776) nicht überschreitet.
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

// createIncidentWithComponents legt den Incident, seine Komponenten-
// Zuordnungen und Tags in einer Transaktion an.
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
	// Die Route hat die Art schon aus dem Body geprüft; die Art, die
	// der Handler tatsächlich anlegt, wird hier unabhängig davon erneut geprüft.
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
	// eine bereits laufend angelegte Wartung setzt ihre Komponenten
	// sofort; der Scheduler fasst nur scheduled/in_progress an, vergangene
	// (resolved) Wartungen lassen die Komponenten unberührt.
	if incident.IsMaintenance && incident.Status == "in_progress" {
		for _, comp := range components {
			// Gruppen behalten ihren Status "group".
			// über die Historie, damit der Wechsel protokolliert wird.
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
	Severity            *string      `json:"Severity"` // nil = unverändert
	ComponentIDs        []uint       `json:"ComponentIDs"`
	TagIDs              *[]uint      `json:"TagIDs"` // nil = unverändert lassen, [] = Tags entfernen
	ScheduledStartAt    *time.Time   `json:"ScheduledStartAt"`
	ScheduledEndAt      *time.Time   `json:"ScheduledEndAt"`
	ReminderLeadMinutes *int         `json:"ReminderLeadMinutes"`
	StartedAt           OptionalTime `json:"StartedAt"`  // fehlt = unverändert, null = zurücksetzen
	ResolvedAt          OptionalTime `json:"ResolvedAt"` // fehlt = unverändert, null = zurücksetzen
}

// OptionalTime unterscheidet ein fehlendes JSON-Feld (Set=false, Wert bleibt)
// von einem ausdrücklichen null (Set=true, Time=nil, Wert wird geleert).
// Das Bearbeiten-Formular schickt ein geleertes Feld als null.
type OptionalTime struct {
	Set  bool
	Time *time.Time
}

// UnmarshalJSON wird von encoding/json nur aufgerufen, wenn das Feld im Body
// steht (auch bei null), daher genügt es, Set hier zu setzen.
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

// applyIncidentStatus setzt den Status eines normalen Vorfalls und hält die
// Regel „ein offener Vorfall hat kein Ende" an einer Stelle ein:
// jeder Status außer resolved leert ResolvedAt. Wartungsfenster verwalten ihre
// Zeiten selbst und bleiben unberührt.
func applyIncidentStatus(incident *models.Incident, status string) {
	incident.Status = status
	if incident.IsMaintenance {
		return
	}
	if !models.IsResolvedIncidentStatus(status) {
		incident.ResolvedAt = nil
	} else if incident.ResolvedAt == nil {
		// Schließen eines Vorfalls hält das Ende fest, egal über
		// welchen Weg (Update-Timeline oder Bearbeiten).
		now := time.Now()
		incident.ResolvedAt = &now
	}
}

// validateIncidentStatusChange prüft einen neuen Status gegen die erlaubte
// Menge (Vorfall bzw. Wartung). Der unveränderte Bestandswert bleibt erlaubt,
// damit das Bearbeiten alter Datensätze mit unbekanntem Status nicht scheitert.
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

// resolveUpdateIncidentTags löst req.TagIDs (falls gesetzt) zu Tags auf und
// prüft die Zugriffsberechtigung darauf. ok=true mit newTags=nil bedeutet
// "TagIDs war nil, Tags unverändert lassen".
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

// applyIncidentUpdateFields überträgt die einfachen Felder von req auf
// incident. Der Wartungsfenster-Zeitplan bleibt vor dem automatischen Start
// änderbar; der Status selbst wird für Wartungsfenster vom
// Scheduler verwaltet, das Frontend schickt hier den unveränderten aktuellen
// Status mit.
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

// replaceIncidentComponents ersetzt die Komponenten-Zuordnung (newComponents,
// vorab per resolveIncidentComponents geladen) eines Incidents, sofern nicht leer, und gibt die
// aktuellen Komponenten zurück (unverändert, falls componentIDs leer war).
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

// resolveIncidentComponents lädt die im Update genannten Komponenten der Seite.
// Es läuft vor jedem Schreibzugriff: ein Verweis auf eine unbekannte oder
// fremde Komponente beantwortet die Anfrage mit 404, ohne dass am Vorfall
// etwas verändert wurde.
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

	// Explizites Aufräumen der Join-Zeilen: Incident nutzt Soft-Delete
	// (gorm.Model → UPDATE statt DELETE), ein DB-seitiges OnDelete:CASCADE würde
	// dabei nie feuern.
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

// foldBodyLines faltet CR/LF (auch CRLF) im Mail-Body zu je einem Leerzeichen;
// in HTML ist Zeilenumbruch ohnehin nur Leerraum (Umbrüche setzen <br>/<p>).
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
	message     string // fertiges, einzeiliges Mail-HTML aus richtext.ToMailHTML
	frontendURL string
	// unsubscribeURL: nur für Subscriber-Mails gesetzt (leer bei
	// internen Usern, die kein Subscriber-Token haben) - rendert einen
	// zusätzlichen Abbestellen-Link im Footer, wenn nicht leer.
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
// extraHeaders: optionale, vollständige
// Header-Zeilen wie List-Unsubscribe; auch sie dürfen kein CR/LF enthalten.
func (h *Handler) sendAlertEmail(smtpCfg smtpConfig, recipientEmail, subjectText, bodyHTML, logLabel string, incID uint, incTitle string, extraHeaders ...string) {
	addr, err := mail.ParseAddress(recipientEmail)
	if err != nil {
		slog.Error("Skipping notification: invalid recipient address", "email", recipientEmail, "error", err)
		return
	}
	recipient := addr.Address

	// Der Body darf mehrzeilig sein (Vorlagen, mehrzeilige
	// {{.Message}}); er wird zu einer Zeile gefaltet, statt die Mail zu
	// verwerfen. Betreff und Absender sind Header und bleiben streng.
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
	// smtpCfg.From liegt bereits im vollständigen "Anzeigename <adresse>"-Format vor
	// (SMTP_FROM-Env-Var, siehe loadSMTPConfig) - ein zusätzliches Wrapping hier
	// erzeugte verschachtelte spitze Klammern (ungültiger RFC-5322-From-Header),
	// die viele Empfänger (u.a. Outlook.com) kommentarlos verwerfen.
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
// comps: alle betroffenen Komponenten (vorher eine einzelne Component).
// notificationDispatchContext bündelt alle Werte, die DispatchNotifications
// einmalig berechnet und die anschließend von den einzelnen Empfänger-
// Benachrichtigungspfaden (interne User, Subscriber, globaler Webhook)
// benötigt werden. Ausgelagert aus DispatchNotifications, damit dessen
// kognitive Komplexität die SonarCloud-Grenze (go:S3776) nicht überschreitet.
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
	// staffMail liefert Betreff und Text für einen internen Nutzer in seiner
	// UI-Sprache (User.PreferredLanguage); leer/unbekannt = Englisch.
	staffMail func(preferred string) (subject, body string)
	// subscriberMail liefert Vorlage, Platzhalterdaten und Standardtext-Parameter
	// in der Sprache eines Abonnenten; "" = Seitenstandardsprache.
	subscriberMail func(preferred string) incidentSubscriberMail
	// chatLang: effektive Sprache (de/en) des Abonnenten für Chat-Texte,
	// unabhängig von vorhandenen Mail-Vorlagen.
	chatLang       func(preferred string) string
	defaultSubject string
}

// subscriberLang ist die Mail-/Textsprache (de/en) für einen Abonnenten; ohne
// vorbereitete Sprachwahl bleibt es beim bisherigen Englisch.
func (ctx notificationDispatchContext) subscriberLang(preferred string) string {
	if ctx.chatLang == nil {
		return "en"
	}
	return ctx.chatLang(preferred)
}

// incidentSubscriberMail ist der sprachabhängige Teil der Abonnentenmail.
type incidentSubscriberMail struct {
	tmpl       *models.NotificationTemplate
	dataBase   NotificationTemplateData
	bodyParams notificationEmailParams
}

// buildNotificationDispatchContext berechnet einmalig alle für den Versand
// nötigen Werte (SMTP-Konfig, Templates, HTML-escapte Felder), bevor die
// Empfänger-Schleifen darauf zugreifen.
func (h *Handler) buildNotificationDispatchContext(inc models.Incident, action, compNames string, componentIDs []uint) notificationDispatchContext {
	smtpCfg := loadSMTPConfig()
	// eine Statusseite kann einen eigenen Absender hinterlegen
	// (StatusPage.NotificationFromAddress), der dann statt des globalen
	// SMTP_FROM verwendet wird. Nur das From-Feld wird überschrieben - Host/
	// Port/Auth bleiben die zentrale SMTP-Verbindung, da nur der Absender
	// mandantenspezifisch ist, nicht der komplette Mailversand. Ist nichts
	// hinterlegt, greift statt SMTP_FROM ein seitenspezifischer Default aus
	// Slug + Domain (resolveNotificationFromAddress).
	// wird gebraucht, um zu wissen, ob die Statusseite privat ist
	// (dann bekommt der "Statusseite ansehen"-Link im Subscriber-Mail einen
	// Zugriffs-Token) und für den slug-genauen Link selbst.
	var page models.StatusPage
	h.DB.First(&page, inc.StatusPageID)
	smtpCfg.applyPageSender(page)
	frontendURL := getFrontendURL()
	incidentTags := h.tagsForIncident(inc.ID)
	tagNames := tagNameList(incidentTags)

	// bodyEN/bodyDE sind text/html-E-Mail-Bodies - Titel, Nachricht,
	// Komponentennamen und Tags kommen aus Nutzereingaben (Incident-Formular)
	// und müssen deshalb HTML-escaped werden, bevor sie hier eingesetzt werden.
	// Ohne das könnte ein Editor mit incidents:write beliebiges HTML/Markup in
	// die an Abonnenten/User verschickten Mails einschleusen. Die Subjects
	// bleiben unescaped, da sie in einen SMTP-Header (kein HTML) eingesetzt
	// werden - dort schützt bereits hasHeaderInjectionChars vor CR/LF.
	escCompNames := html.EscapeString(compNames)
	escTitle := html.EscapeString(inc.Title)
	escTagNames := html.EscapeString(tagNames)
	// Die Nachricht ist eingeschränktes Markdown; richtext.ToMailHTML
	// maskiert allen Text und erzeugt nur feste Tags (kein Roh-HTML) und ist
	// immer einzeilig, passiert also den CR/LF-Guard von sendAlertEmail.
	messageHTML := richtext.ToMailHTML(inc.Message)

	// pro Statusseite kann ein Custom-Template hinterlegt sein.
	// Lookup einmal pro Sprache vor den Empfänger-Schleifen, damit die
	// Renderpfade unten pro Empfänger nur noch das Custom-Template
	// anwenden bzw. den vorbereiteten Default zurückgeben.
	notifKind := resolveIncidentKind(action, inc.Status)
	// Interne Nutzer bekommen die Mail in ihrer UI-Sprache
	// (de/en); Vorlage (Audience staff) in dieser Sprache, sonst die englische
	// Vorlage der Seite, sonst der eingebaute Text in ihrer Sprache. Gemerkt je
	// Sprache, weil die Schleife über alle Nutzer läuft.
	subTemplate := h.memoSubscriberTemplate(inc.StatusPageID, notifKind)
	actionVerb := strings.ToLower(action)

	defaultSubject := fmt.Sprintf("[ALERT] %s: %s (%s)", compNames, inc.Title, inc.Status)
	staffCache := map[string][2]string{}
	staffMail := func(preferred string) (string, string) {
		lang := staffMailLang(preferred)
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

	// Der Betreff bleibt für alle Subscriber gleich, der Body dagegen wird
	// pro Subscriber frisch gebaut (siehe notifySubscriberEmail) - er enthält
	// seit der Umstellung einen individuellen Abbestellen-Token und, bei privaten
	// Seiten, einen individuellen Zugriffs-Token im "Statusseite ansehen"-Link.
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

// notifyInternalUsers benachrichtigt nur User, die entweder
// Global Admin sind oder eine Rolle auf genau dieser Statusseite haben.
func (h *Handler) notifyInternalUsers(ctx notificationDispatchContext) {
	var users []models.User
	h.DB.Where(
		"is_global_admin = ? OR id IN (?)",
		true,
		h.DB.Model(&models.UserStatusPageRole{}).Select("user_id").Where(whereStatusPageIDEquals, ctx.inc.StatusPageID),
	).FindInBatches(&users, 100, func(tx *gorm.DB, batch int) error {
		for _, user := range users {
			// Tag-basierter Nachfolger von UserHasAccessToCountry.
			// Dieselbe "nur einschränken, wenn beide Seiten Tags haben"-Logik
			// wie userHasTagAccess: Global-Admins, User ohne eigene Tags und
			// Incidents ohne eigenen Tag umgehen die Prüfung - die bereits
			// erfolgte Seiten-Rollen-Filterung oben (is_global_admin OR
			// UserStatusPageRole) ist die eigentliche Zugriffskontrolle, Tags
			// sind nur eine zusätzliche, optionale Eingrenzung.
			userTags := h.tagsForUser(user.ID)
			if user.IsGlobalAdmin || len(userTags) == 0 || len(ctx.incidentTags) == 0 || middleware.UserHasAccessToAnyTag(userTags, ctx.incidentTags) {
				subject, body := ctx.staffMail(user.PreferredLanguage)
				h.sendAlertEmail(ctx.smtpCfg, user.Email, subject, body, "internal user", ctx.inc.ID, ctx.inc.Title)
			}
		}
		return nil
	})
}

// incidentGoogleChatPayload baut den Google-Chat-Nachrichtentext, der sowohl
// für einzelne Subscriber-Webhooks als auch den globalen Fallback-Webhook
// verwendet wird. Die Nachricht geht als Klartext raus (Markdown
// wird zu lesbarem Text, Links als "Text (URL)"), nicht als rohes Markdown.
//
// lang (de/en) wählt die festen Beschriftungen; Abonnenten bekommen
// ihre gewählte Sprache bzw. die der Seite, der globale Webhook bleibt englisch.
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

// notifySubscriberGoogleChat verschickt die Google-Chat-Webhook-Nachricht an
// einen einzelnen Subscriber.
func (h *Handler) notifySubscriberGoogleChat(ctx notificationDispatchContext, sub models.Subscriber) {
	if sub.WebhookURL == "" {
		slog.Error("Subscriber has google_chat channel but no WebhookURL", "subscriber_id", sub.ID)
		return
	}
	// SSRF-Schutz (Nachzügler): postWebhook sendet über
	// webhookTestClient, damit die aufgelöste IP direkt vor dem Connect erneut
	// gegen die Blockliste geprüft wird (siehe safeDialContext in
	// subscriber.go) - ein normaler http.Client würde den Host beim
	// eigentlichen Request erneut auflösen und wäre damit anfällig für
	// DNS-Rebinding zwischen Abo-Anlage und Incident-Dispatch.
	// Ratenlimit/Retry/Logging zentral dort.
	res := postWebhook(context.Background(), sub.WebhookURL, incidentGoogleChatPayload(ctx, ctx.subscriberLang(sub.Language)), webhookModeNotify)
	if !res.OK() {
		slog.Error("Failed to send webhook to subscriber", "subscriber_id", sub.ID, "status", res.Status, "class", res.Class)
		// Admins sehen Ratenlimit und dauerhafte Ablehnung in der Glocke.
		h.reportWebhookProblem(sub.StatusPageID, sub.ID, "google_chat", res)
		return
	}
	slog.Info("Webhook triggered for subscriber", "subscriber_id", sub.ID, "response_status", res.Status)
}

// notifySubscriberEmail baut den individuellen (Token/Unsubscribe-Link)
// Mailinhalt für einen Email-Subscriber und verschickt ihn.
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
		// bei privaten Seiten reicht der bisherige, nicht-slug-
		// spezifische frontendURL-Link nicht - ohne Login (den ein reiner
		// Abonnent nicht hat) würde er sonst nur auf eine Login-Aufforderung
		// treffen. Der Token im Link gewährt stattdessen direkten Lesezugriff
		// auf genau diese Seite (siehe subscriberTokenGrantsPageAccess).
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

// notifySubscriber benachrichtigt einen einzelnen Subscriber über
// den beim Abo gewählten Kanal, sofern er eine der betroffenen Komponenten
// abonniert hat.
func (h *Handler) notifySubscriber(ctx notificationDispatchContext, sub models.Subscriber) {
	if !SubscriberHasAnyComponent(sub.SubscribedComponentIDs, ctx.componentIDs) {
		return
	}
	// Vorfälle privater Seiten nur an Abos, deren
	// Ersteller die Seite noch sehen darf (siehe subscriberCreatorMayReadPage).
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

// notifySubscribers benachrichtigt alle Email-/Webhook-
// Subscriber der betroffenen Statusseite.
func (h *Handler) notifySubscribers(ctx notificationDispatchContext) {
	var subscribers []models.Subscriber
	// unbestätigte (pending) Abos bekommen keine Benachrichtigungen.
	h.DB.Where("status_page_id = ? AND status = ?", ctx.inc.StatusPageID, models.SubscriberStatusActive).FindInBatches(&subscribers, 100, func(tx *gorm.DB, batch int) error {
		for _, sub := range subscribers {
			h.notifySubscriber(ctx, sub)
		}
		return nil
	})
}

// dispatchGlobalGoogleChatWebhook verschickt zusätzlich an den (optionalen)
// prozessweiten Google-Chat-Webhook aus GOOGLE_CHAT_WEBHOOK_URL, unabhängig
// von Subscriber-spezifischen Webhooks. Der Webhook gehört dem Betreiber und
// sieht Vorfälle aller Mandanten; deshalb gehen nur Vorfälle öffentlicher
// Seiten hinein, deren Inhalt ohnehin jeder lesen kann. Vorfälle privater
// Seiten bleiben beim Mandanten. Ist die Seite nicht geladen (ID 0, etwa nach
// einem Datenbankfehler), wird nichts gesendet (fail closed).
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
// comps: alle betroffenen Komponenten (vorher eine einzelne Component).
func (h *Handler) DispatchNotifications(inc models.Incident, comps []models.Component, action string) {
	compNames := componentNames(comps)
	componentIDs := make([]uint, len(comps))
	for i, c := range comps {
		componentIDs[i] = c.ID
	}
	go func() {
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

// SubscriberHasAnyComponent: ein Incident kann jetzt mehrere
// Komponenten betreffen - ein Subscriber wird benachrichtigt, wenn er
// mindestens eine davon abonniert hat.
func SubscriberHasAnyComponent(subscribedIDs string, targetIDs []uint) bool {
	for _, id := range targetIDs {
		if SubscriberHasComponent(subscribedIDs, id) {
			return true
		}
	}
	return false
}
