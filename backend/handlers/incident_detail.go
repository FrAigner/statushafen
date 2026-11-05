package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/permissions"

	"github.com/gin-gonic/gin"
)

// Wiederholte Literale (go:S1192).
const (
	errInvalidIncidentID   = "Invalid incident ID"
	errIncidentNotFound    = "Incident not found"
	whereIDAndStatusPageID = "id = ? AND status_page_id = ?"
	whereIncidentIDEquals  = "incident_id = ?"
	// Fehlermeldungen der Statusprüfung.
	errInvalidIncidentSeverity  = "Ungültiger Schweregrad: erlaubt sind degraded_performance, partial_outage, major_outage"
	errInvalidIncidentStatus    = "Ungültiger Status: erlaubt sind investigating, identified, monitoring, resolved"
	errInvalidMaintenanceStatus = "Ungültiger Status für Wartungsfenster: erlaubt sind scheduled, in_progress, resolved"
)

// ─────────────────────────────────────────────────────────────────────────────
// Incident-Detail: Timeline (IncidentUpdate) + Post-Mortem
//
// Routen (main.go):
//   // öffentlich lesbar wie die Incident-Liste:
//   r.GET("/api/pages/:page_id/incidents/:id", h.GetIncidentDetailHandler)
//   // schreibend, unter incAdmin (RequirePermission "incidents:write"):
//   incAdmin.POST("/:id/updates", h.AddIncidentUpdateHandler)
//   incAdmin.PUT("/:id/postmortem", h.UpsertPostMortemHandler)
// ─────────────────────────────────────────────────────────────────────────────

type incidentDetail struct {
	IncidentResponse
	Updates    []incidentUpdateView `json:"updates"`
	PostMortem *models.PostMortem   `json:"post_mortem"`
}

// incidentUpdateView ist ein Timeline-Eintrag in der Detail-Antwort.
// GetIncidentDetailHandler ist öffentlich und
// lieferte author_email an jeden Besucher - damit waren die E-Mail-Adressen
// des Teams öffentlich. AuthorEmail wird nur noch für Nutzer mit
// incidents:write auf der Seite gesetzt (Admin-Ansicht), sonst fällt das Feld
// per omitempty ganz weg.
type incidentUpdateView struct {
	ID          uint      `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	IncidentID  uint      `json:"incident_id"`
	Status      string    `json:"status"`
	Message     string    `json:"message"`
	AuthorEmail string    `json:"author_email,omitempty"`
}

func toIncidentUpdateViews(updates []models.IncidentUpdate, withAuthor bool) []incidentUpdateView {
	out := make([]incidentUpdateView, 0, len(updates))
	for _, u := range updates {
		v := incidentUpdateView{ID: u.ID, CreatedAt: u.CreatedAt, IncidentID: u.IncidentID, Status: u.Status, Message: u.Message}
		if withAuthor {
			v.AuthorEmail = u.AuthorEmail
		}
		out = append(out, v)
	}
	return out
}

func (h *Handler) GetIncidentDetailHandler(c *gin.Context) {
	tenantID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page_id", "code": "invalid_id"})
		return
	}
	incID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errInvalidIncidentID, "code": "invalid_id"})
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

	var inc models.Incident
	if err := h.DB.Scopes(h.incidentTagFilterScope(c)).
		Preload("IncidentComponents.Component").
		Where(whereIDAndStatusPageID, incID, tenantID).
		First(&inc).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errIncidentNotFound, "code": "incident_not_found"})
		return
	}

	// Nicht mehr "irgendein valides JWT" (getEmail(c) != ""),
	// sondern nur eine Rolle mit incidents:write auf GENAU dieser page_id
	// darf Post-Mortem-Entwürfe und die Autoren-E-Mails der
	// Timeline sehen - dieselbe Berechtigung, die auch zum Schreiben nötig
	// ist (UpsertPostMortemHandler/AddIncidentUpdateHandler).
	canEdit := h.userHasPermissionOnPrivatePage(c, page.ID, permissions.Perm(incidentResource(inc.IsMaintenance), permissions.Update))

	// archivierte und unsichtbare Komponenten nur für Verwalter.
	one := []models.Incident{inc}
	h.stripHiddenComponentsForCaller(c, page.ID, one)
	inc = one[0]

	detail := incidentDetail{IncidentResponse: h.toIncidentResponse(inc)}
	var updates []models.IncidentUpdate
	h.DB.Where(whereIncidentIDEquals, inc.ID).Order("created_at ASC").Find(&updates)
	detail.Updates = toIncidentUpdateViews(updates, canEdit)

	var pm models.PostMortem
	if err := h.DB.Preload("Actions").Where(whereIncidentIDEquals, inc.ID).First(&pm).Error; err == nil {
		// "Irgendeine Rolle" (frühere Fassung: userHasRoleOnPrivatePage)
		// reichte nicht: nach Auto-Join per DefaultRoleID hat JEDER
		// eingeloggte User irgendeine Rolle, auch ein reiner Read-Only-Viewer
		// (per unabhängigem Review gefunden).
		if pm.PublishedAt != nil || canEdit {
			detail.PostMortem = &pm
		}
	}

	c.JSON(http.StatusOK, detail)
}

func (h *Handler) AddIncidentUpdateHandler(c *gin.Context) {
	tenantID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page_id", "code": "invalid_id"})
		return
	}
	incID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errInvalidIncidentID, "code": "invalid_id"})
		return
	}

	var inc models.Incident
	if err := h.DB.Preload("IncidentComponents.Component").Where(whereIDAndStatusPageID, incID, tenantID).First(&inc).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errIncidentNotFound, "code": "incident_not_found"})
		return
	}
	if !h.userHasTagAccess(c, h.tagsForIncident(inc.ID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden for this incident", "code": "forbidden"})
		return
	}

	type req struct {
		Status  string `json:"status" binding:"required"`
		Message string `json:"message" binding:"required"`
	}
	var body req
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	if !requireMessageLength(c, body.Message) {
		return
	}
	// Status gegen die erlaubte Menge prüfen (Vorfall bzw.
	// Wartung); vorher wurde jeder String übernommen (auch completed).
	if msg := validateIncidentStatusChange(inc, body.Status); msg != "" {
		respondIncidentValidation(c, msg)
		return
	}

	update := models.IncidentUpdate{
		IncidentID:  inc.ID,
		Status:      body.Status,
		Message:     body.Message,
		AuthorEmail: getEmail(c),
	}
	if err := h.DB.Create(&update).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add update", "code": "server_error"})
		return
	}

	// Incident-Hauptstatus mitziehen und Benachrichtigungen auslösen.
	// Incident.Message bleibt die Erstmeldung und wird nie durch ein
	// Update überschrieben; die Update-Nachricht steht nur in IncidentUpdate.
	// Die Benachrichtigung bekommt eine Kopie mit der Update-Nachricht, die
	// nicht gespeichert wird.
	applyIncidentStatus(&inc, body.Status)
	h.DB.Save(&inc)
	notifyInc := inc
	notifyInc.Message = body.Message
	comps := make([]models.Component, len(inc.IncidentComponents))
	for i, ic := range inc.IncidentComponents {
		comps[i] = ic.Component
	}
	h.DispatchNotifications(notifyInc, comps, "UPDATED")
	h.RecordAudit(c, "incident.update_added", "incident", "Incident",
		strconv.FormatUint(uint64(inc.ID), 10), inc.Title, "")

	c.JSON(http.StatusCreated, update)
}

func (h *Handler) UpsertPostMortemHandler(c *gin.Context) {
	tenantID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page_id", "code": "invalid_id"})
		return
	}
	incID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errInvalidIncidentID, "code": "invalid_id"})
		return
	}

	var inc models.Incident
	if err := h.DB.Where(whereIDAndStatusPageID, incID, tenantID).First(&inc).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errIncidentNotFound, "code": "incident_not_found"})
		return
	}
	if !h.userHasTagAccess(c, h.tagsForIncident(inc.ID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden for this incident", "code": "forbidden"})
		return
	}

	type actionReq struct {
		Text  string `json:"text"`
		Owner string `json:"owner"`
		State string `json:"state"`
	}
	type req struct {
		Summary    string      `json:"summary"`
		RootCause  string      `json:"root_cause"`
		Impact     string      `json:"impact"`
		Resolution string      `json:"resolution"`
		Detection  string      `json:"detection"`
		Publish    bool        `json:"publish"`
		Actions    []actionReq `json:"actions"`
	}
	var body req
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}

	var pm models.PostMortem
	h.DB.Where(whereIncidentIDEquals, inc.ID).First(&pm) // legt an, falls nicht vorhanden
	pm.IncidentID = inc.ID
	pm.Summary = body.Summary
	pm.RootCause = body.RootCause
	pm.Impact = body.Impact
	pm.Resolution = body.Resolution
	pm.Detection = body.Detection
	if body.Publish && pm.PublishedAt == nil {
		now := time.Now()
		pm.PublishedAt = &now
	}
	if err := h.DB.Save(&pm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save post-mortem", "code": "server_error"})
		return
	}

	// Action-Items neu setzen (einfachste konsistente Strategie: ersetzen).
	h.DB.Where("post_mortem_id = ?", pm.ID).Delete(&models.ActionItem{})
	for _, a := range body.Actions {
		if a.Text == "" {
			continue
		}
		state := a.State
		if state == "" {
			state = "open"
		}
		h.DB.Create(&models.ActionItem{PostMortemID: pm.ID, Text: a.Text, Owner: a.Owner, State: state})
	}

	action := "postmortem.saved"
	if body.Publish {
		action = "postmortem.published"
	}
	h.RecordAudit(c, action, "incident", "Incident",
		strconv.FormatUint(uint64(inc.ID), 10), inc.Title, "")

	h.DB.Preload("Actions").First(&pm, pm.ID)
	c.JSON(http.StatusOK, pm)
}
