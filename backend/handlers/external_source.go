package handlers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Admin API for external status pages (part A). All
// routes hang under /api/pages/:page_id/... so RequirePermission checks the
// role on exactly this status page; every query additionally filters
// by status_page_id (tenant separation: a foreign ID yields 404).
// Read: components:read, change/link/poll: components:write - a
// source is configuration of the components, a separate permission would need
// a role migration with no visible benefit.

const (
	errExternalSourceNotFound    = "External source not found"
	errExternalComponentNotFound = "External component not found"
	errExternalLinkNotFound      = "External link not found"
	maxExternalSourceNameLen     = 200
	auditTargetExternalSource    = "ExternalSource"
	auditTargetExternalLink      = "ExternalComponentLink"
)

// externalSourceInput: create (Name/BaseURL required) and update (everything
// optional, nil = unchanged). Deliberately a DTO instead of binding into the model
// (see statusPageWritableFields).
type externalSourceInput struct {
	Name                *string `json:"name"`
	BaseURL             *string `json:"base_url"`
	Provider            *string `json:"provider"`
	PollIntervalSeconds *int    `json:"poll_interval_seconds"`
	Active              *bool   `json:"active"`
	// OutageAfterMinutes: period without a successful poll until
	// the outage report, 10 to 1440 minutes.
	OutageAfterMinutes *int `json:"outage_after_minutes"`
}

// externalSourceResponse: source plus counters for the admin list (part C).
type externalSourceResponse struct {
	models.ExternalSource
	ComponentCount int64 `json:"component_count"`
	LinkCount      int64 `json:"link_count"`
	// Outage: outage state of the source for list and banner.
	Outage externalOutageInfo `json:"outage"`
}

// externalOutageInfo: Active = the source is considered failed, Since = start
// of the silence (last successful poll), LastError = last error.
type externalOutageInfo struct {
	Active    bool       `json:"active"`
	Since     *time.Time `json:"since"`
	LastError string     `json:"last_error"`
}

// applyExternalSourceInput applies and validates the fields; the
// error message goes to the client as 400.
func (h *Handler) applyExternalSourceInput(src *models.ExternalSource, in externalSourceInput) string {
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" || len(name) > maxExternalSourceNameLen {
			return fmt.Sprintf("name must be 1-%d characters", maxExternalSourceNameLen)
		}
		src.Name = name
	}
	if in.BaseURL != nil {
		base, err := externalstatus.ValidateBaseURL(*in.BaseURL)
		if err != nil {
			return "invalid base_url: " + err.Error()
		}
		if base != src.BaseURL {
			// Different site: old validators no longer apply.
			src.ETag, src.LastModified = "", ""
		}
		src.BaseURL = base
	}
	if in.Provider != nil {
		if !h.ExternalStatus.SupportsProvider(*in.Provider) {
			return fmt.Sprintf("unsupported provider (supported: %s)", strings.Join(h.ExternalStatus.SupportedProviders(), ", "))
		}
		src.Provider = *in.Provider
	}
	if in.PollIntervalSeconds != nil {
		v := *in.PollIntervalSeconds
		if v < externalstatus.MinPollIntervalSeconds || v > externalstatus.MaxPollIntervalSeconds {
			return fmt.Sprintf("poll_interval_seconds must be between %d and %d", externalstatus.MinPollIntervalSeconds, externalstatus.MaxPollIntervalSeconds)
		}
		src.PollIntervalSeconds = v
	}
	if in.OutageAfterMinutes != nil {
		v := *in.OutageAfterMinutes
		if v < externalstatus.MinOutageAfterMinutes || v > externalstatus.MaxOutageAfterMinutes {
			return fmt.Sprintf("outage_after_minutes must be between %d and %d", externalstatus.MinOutageAfterMinutes, externalstatus.MaxOutageAfterMinutes)
		}
		src.OutageAfterMinutes = v
	}
	if in.Active != nil {
		src.Active = *in.Active
	}
	return ""
}

// duplicateExternalSource reports whether the status page already has another source
// with this base URL (polling the same site twice yields only
// duplicate components).
func (h *Handler) duplicateExternalSource(src models.ExternalSource) bool {
	var n int64
	h.DB.Model(&models.ExternalSource{}).
		Where("status_page_id = ? AND base_url = ? AND id <> ?", src.StatusPageID, src.BaseURL, src.ID).
		Count(&n)
	return n > 0
}

func (h *Handler) externalSourceResponse(src models.ExternalSource) externalSourceResponse {
	resp := externalSourceResponse{ExternalSource: src}
	resp.Outage = externalOutageInfo{Active: src.OutageSince != nil, Since: src.OutageSince, LastError: src.LastError}
	h.DB.Model(&models.ExternalComponent{}).Where("external_source_id = ?", src.ID).Count(&resp.ComponentCount)
	h.DB.Model(&models.ExternalComponentLink{}).
		Where("external_component_id IN (?)", h.DB.Model(&models.ExternalComponent{}).Select("id").Where("external_source_id = ?", src.ID)).
		Count(&resp.LinkCount)
	return resp
}

// loadExternalSource loads :id of the status page :page_id; responds itself with 400/404.
func (h *Handler) loadExternalSource(c *gin.Context) (models.ExternalSource, bool) {
	var src models.ExternalSource
	tenantID, ok := parseTenantID(c)
	if !ok {
		return src, false
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid source ID", "code": "invalid_id"})
		return src, false
	}
	if err := h.DB.Where("id = ? AND status_page_id = ?", id, tenantID).First(&src).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errExternalSourceNotFound, "code": "external_source_not_found"})
		return src, false
	}
	return src, true
}

// effectiveSnapshot returns the effective statuses of a page for
// event detection. Best effort: on a DB error there is no event
// (a nil previous state is not reported by DiffEffective), the action itself continues.
func (h *Handler) effectiveSnapshot(pageID uint) map[uint]string {
	m, err := externalstatus.EffectiveStatuses(h.DB, pageID)
	if err != nil {
		slog.Error("Effektive Status nicht berechenbar", "status_page_id", pageID, "error", err)
	}
	return m
}

// notifyEffectiveDiff compares with before and reports changes to the
// service's listener (part B attaches notifications there).
func (h *Handler) notifyEffectiveDiff(c *gin.Context, before map[uint]string, base externalstatus.StatusChange) {
	externalstatus.DiffEffective(c.Request.Context(), h.ExternalStatus.Listener(), before, h.effectiveSnapshot(base.StatusPageID), base)
}

// GET /api/pages/:page_id/external-sources
func (h *Handler) GetExternalSourcesHandler(c *gin.Context) {
	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}
	var sources []models.ExternalSource
	if err := h.DB.Where(whereStatusPageIDEquals, tenantID).Order("name asc, id asc").Find(&sources).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch external sources", "code": "server_error"})
		return
	}
	resp := make([]externalSourceResponse, 0, len(sources))
	for _, src := range sources {
		resp = append(resp, h.externalSourceResponse(src))
	}
	c.JSON(http.StatusOK, resp)
}

// GET /api/pages/:page_id/external-sources/:id
func (h *Handler) GetExternalSourceHandler(c *gin.Context) {
	src, ok := h.loadExternalSource(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, h.externalSourceResponse(src))
}

// POST /api/pages/:page_id/external-sources
func (h *Handler) CreateExternalSourceHandler(c *gin.Context) {
	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}
	var in externalSourceInput
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == nil || in.BaseURL == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and base_url are required", "code": "external_source_fields_required"})
		return
	}
	src := models.ExternalSource{
		StatusPageID:        tenantID,
		Provider:            externalstatus.ProviderSummaryV2,
		PollIntervalSeconds: externalstatus.DefaultPollIntervalSeconds,
		OutageAfterMinutes:  externalstatus.DefaultOutageAfterMinutes,
		Active:              true,
	}
	if msg := h.applyExternalSourceInput(&src, in); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": "invalid_request"})
		return
	}
	if h.duplicateExternalSource(src) {
		c.JSON(http.StatusConflict, gin.H{"error": "This status page already has a source with this base_url", "code": "external_source_exists"})
		return
	}
	if err := h.DB.Create(&src).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create external source", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "external_source.created", "config", auditTargetExternalSource, strconv.FormatUint(uint64(src.ID), 10), src.Name, src.BaseURL)
	c.JSON(http.StatusCreated, h.externalSourceResponse(src))
}

// PUT /api/pages/:page_id/external-sources/:id
func (h *Handler) UpdateExternalSourceHandler(c *gin.Context) {
	src, ok := h.loadExternalSource(c)
	if !ok {
		return
	}
	var in externalSourceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	if msg := h.applyExternalSourceInput(&src, in); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": "invalid_request"})
		return
	}
	if h.duplicateExternalSource(src) {
		c.JSON(http.StatusConflict, gin.H{"error": "This status page already has a source with this base_url", "code": "external_source_exists"})
		return
	}
	// Active/inactive changes whether the linked components count.
	before := h.effectiveSnapshot(src.StatusPageID)
	if err := h.DB.Select("Name", "BaseURL", "Provider", "PollIntervalSeconds", "OutageAfterMinutes", "Active", "ETag", "LastModified").Save(&src).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update external source", "code": "server_error"})
		return
	}
	h.notifyEffectiveDiff(c, before, externalstatus.StatusChange{StatusPageID: src.StatusPageID, Cause: externalstatus.CauseSource, SourceID: src.ID})
	h.RecordAudit(c, "external_source.updated", "config", auditTargetExternalSource, strconv.FormatUint(uint64(src.ID), 10), src.Name, src.BaseURL)
	c.JSON(http.StatusOK, h.externalSourceResponse(src))
}

// DELETE /api/pages/:page_id/external-sources/:id - deletes (soft) the source,
// its external components and their links in one transaction.
// Because the links of all linked components disappear in the process, the
// same tag restriction applies as when deleting a single link
// (DeleteExternalLinkHandler): the call is only allowed if the user has
// tag access to every linked component, otherwise 403 and nothing is deleted.
// Global Admins and users without tags are not restricted by this.
func (h *Handler) DeleteExternalSourceHandler(c *gin.Context) {
	src, ok := h.loadExternalSource(c)
	if !ok {
		return
	}
	var linkedComponentIDs []uint
	if err := h.DB.Model(&models.ExternalComponentLink{}).
		Joins("JOIN external_components ON external_components.id = external_component_links.external_component_id").
		Where("external_components.external_source_id = ?", src.ID).
		Distinct().Pluck("external_component_links.component_id", &linkedComponentIDs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete external source", "code": "server_error"})
		return
	}
	for _, componentID := range linkedComponentIDs {
		if !h.userHasTagAccess(c, h.tagsForComponent(componentID)) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to manage all components linked to this source", "code": "forbidden"})
			return
		}
	}
	before := h.effectiveSnapshot(src.StatusPageID)
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		compIDs := tx.Model(&models.ExternalComponent{}).Select("id").Where("external_source_id = ?", src.ID)
		if err := tx.Where("external_component_id IN (?)", compIDs).Delete(&models.ExternalComponentLink{}).Error; err != nil {
			return err
		}
		if err := deleteMirroredIncidents(tx, src.ID); err != nil {
			return err
		}
		if err := tx.Where("external_source_id = ?", src.ID).Delete(&models.ExternalComponent{}).Error; err != nil {
			return err
		}
		return tx.Delete(&src).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete external source", "code": "server_error"})
		return
	}
	h.notifyEffectiveDiff(c, before, externalstatus.StatusChange{StatusPageID: src.StatusPageID, Cause: externalstatus.CauseSource, SourceID: src.ID})
	h.RecordAudit(c, "external_source.deleted", "config", auditTargetExternalSource, strconv.FormatUint(uint64(src.ID), 10), src.Name, src.BaseURL)
	c.JSON(http.StatusOK, gin.H{"message": "External source deleted"})
}

// POST /api/pages/:page_id/external-sources/:id/fetch - "Jetzt abrufen".
// Always responds 200 with the result (ok/error) so the UI can show
// the third-party site's error; 4xx/5xx only for own errors.
//
// If a poll is already running for the source (background cycle, another
// click or another instance), the endpoint responds immediately with
// result {ok:false, error:"fetch_in_progress"}, without waiting - see
// externalstatus.Service.poll.
func (h *Handler) FetchExternalSourceHandler(c *gin.Context) {
	src, ok := h.loadExternalSource(c)
	if !ok {
		return
	}
	res := h.ExternalStatus.FetchNow(c.Request.Context(), src)
	h.DB.First(&src, src.ID)
	details := "ok"
	if !res.OK {
		details = res.Error
	}
	h.RecordAudit(c, "external_source.fetched", "config", auditTargetExternalSource, strconv.FormatUint(uint64(src.ID), 10), src.Name, details)
	c.JSON(http.StatusOK, gin.H{"result": res, "source": h.externalSourceResponse(src)})
}

// externalComponentResponse: external component plus IDs of the linked
// own components (part C uses this to show the link state in the search).
type externalComponentResponse struct {
	models.ExternalComponent
	LinkedComponentIDs []uint `json:"linked_component_ids"`
}

// GET /api/pages/:page_id/external-sources/:id/components
//
// Query: q (substring in name or group name, case-insensitive),
// group (external group ID), status (normalized status),
// include_vanished=true (default: hide vanished).
// Response: {"groups": [groups], "components": [all hits, groups included]}.
func (h *Handler) GetExternalComponentsHandler(c *gin.Context) {
	src, ok := h.loadExternalSource(c)
	if !ok {
		return
	}
	q := h.DB.Where("external_source_id = ?", src.ID)
	if c.Query("include_vanished") != "true" {
		q = q.Where("vanished = ?", false)
	}
	if search := strings.TrimSpace(c.Query("q")); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		q = q.Where("LOWER(name) LIKE ? OR LOWER(group_name) LIKE ?", like, like)
	}
	if group := c.Query("group"); group != "" {
		q = q.Where("group_external_id = ? OR external_id = ?", group, group)
	}
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}
	var comps []models.ExternalComponent
	if err := q.Order("group_name asc, position asc, name asc, id asc").Find(&comps).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch external components", "code": "server_error"})
		return
	}

	linked := h.linkedComponentIDsByExternal(src.StatusPageID)
	groups := []models.ExternalComponent{}
	items := make([]externalComponentResponse, 0, len(comps))
	for _, ec := range comps {
		if ec.IsGroup {
			groups = append(groups, ec)
		}
		ids := linked[ec.ID]
		if ids == nil {
			ids = []uint{}
		}
		items = append(items, externalComponentResponse{ExternalComponent: ec, LinkedComponentIDs: ids})
	}
	c.JSON(http.StatusOK, gin.H{"groups": groups, "components": items})
}

func (h *Handler) linkedComponentIDsByExternal(pageID uint) map[uint][]uint {
	var links []models.ExternalComponentLink
	h.DB.Where(whereStatusPageIDEquals, pageID).Order("component_id").Find(&links)
	out := map[uint][]uint{}
	for _, l := range links {
		out[l.ExternalComponentID] = append(out[l.ExternalComponentID], l.ComponentID)
	}
	return out
}

// externalLinkResponse: one link with the names of both sides.
type externalLinkResponse struct {
	ID                    uint   `json:"id"`
	ComponentID           uint   `json:"component_id"`
	ComponentName         string `json:"component_name"`
	ExternalComponentID   uint   `json:"external_component_id"`
	ExternalComponentName string `json:"external_component_name"`
	GroupName             string `json:"group_name"`
	SourceID              uint   `json:"source_id"`
	SourceName            string `json:"source_name"`
	Status                string `json:"status"`
	Vanished              bool   `json:"vanished"`
	// IsGroup: The link points to a whole external group;
	// Status/Worst are then the worst of its current members.
	IsGroup bool                  `json:"is_group"`
	Worst   *models.ExternalWorst `gorm:"-" json:"worst,omitempty"`
	// Warnings: "overlaps_group" if the same component links an external group
	// and one of its members. Allowed and without effect on the
	// status (maximum), the admin UI only points it out.
	Warnings []string `gorm:"-" json:"warnings"`
	// ExternalID/GroupExternalID only for the overlap check (not delivered).
	ExternalID      string `json:"-"`
	GroupExternalID string `json:"-"`
}

// warningOverlapsGroup: double link of an external group and one of its members.
const warningOverlapsGroup = "overlaps_group"

// annotateLinkWarnings sets Warnings: per own component and source, a
// warning on every link that is part of an overlap (the group and
// the member).
func annotateLinkWarnings(rows []externalLinkResponse) {
	type key struct {
		component, source uint
		group             string
	}
	groups := map[key]bool{}
	for _, r := range rows {
		if r.IsGroup {
			groups[key{r.ComponentID, r.SourceID, r.ExternalID}] = true
		}
	}
	for i := range rows {
		rows[i].Warnings = []string{}
		r := rows[i]
		overlap := (r.IsGroup && hasMemberLink(rows, r)) || (!r.IsGroup && groups[key{r.ComponentID, r.SourceID, r.GroupExternalID}])
		if overlap {
			rows[i].Warnings = append(rows[i].Warnings, warningOverlapsGroup)
		}
	}
}

// hasMemberLink: The component of group also links a member of this group.
func hasMemberLink(rows []externalLinkResponse, group externalLinkResponse) bool {
	for _, r := range rows {
		if !r.IsGroup && r.ComponentID == group.ComponentID && r.SourceID == group.SourceID && r.GroupExternalID == group.ExternalID {
			return true
		}
	}
	return false
}

func (h *Handler) externalLinks(pageID uint) ([]externalLinkResponse, error) {
	rows := []externalLinkResponse{}
	err := h.DB.Table("external_component_links AS l").
		Select(`l.id AS id, l.component_id AS component_id, c.name AS component_name,
			ec.id AS external_component_id, ec.name AS external_component_name, ec.group_name AS group_name,
			es.id AS source_id, es.name AS source_name, ec.status AS status, ec.vanished AS vanished,
			ec.is_group AS is_group, ec.external_id AS external_id, ec.group_external_id AS group_external_id`).
		Joins("JOIN components c ON c.id = l.component_id AND c.deleted_at IS NULL").
		Joins("JOIN external_components ec ON ec.id = l.external_component_id AND ec.deleted_at IS NULL").
		Joins("JOIN external_sources es ON es.id = ec.external_source_id AND es.deleted_at IS NULL").
		Where("l.deleted_at IS NULL AND l.status_page_id = ?", pageID).
		Order("c.name, ec.name, l.id").Scan(&rows).Error
	if err != nil {
		return rows, err
	}
	for i := range rows {
		if rows[i].IsGroup {
			// A group's own status does not count, only its members.
			rows[i].GroupName = rows[i].ExternalComponentName
			if rows[i].Status, rows[i].Worst, err = externalstatus.GroupWorst(h.DB, rows[i].SourceID, rows[i].ExternalID); err != nil {
				return rows, err
			}
		}
	}
	annotateLinkWarnings(rows)
	return rows, nil
}

// GET /api/pages/:page_id/external-links
func (h *Handler) GetExternalLinksHandler(c *gin.Context) {
	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}
	rows, err := h.externalLinks(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch external links", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, rows)
}

// resolveLinkTargets checks both ends of a new link for membership
// of the status page (404) and tag access (403); responds itself.
func (h *Handler) resolveLinkTargets(c *gin.Context, tenantID, componentID, externalID uint) (models.Component, models.ExternalComponent, bool) {
	var comp models.Component
	var ext models.ExternalComponent
	if err := h.DB.Where("id = ? AND status_page_id = ?", componentID, tenantID).First(&comp).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found", "code": "component_not_found"})
		return comp, ext, false
	}
	// Pure groups inherit their status from their children (buildTree); a
	// link there would have no unambiguous effect.
	if comp.Status == componentstatus.Group {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Group components cannot be linked; link one of their children", "code": "external_group_not_linkable"})
		return comp, ext, false
	}
	err := h.DB.Preload("ExternalSource").Where("id = ? AND external_source_id IN (?)", externalID,
		h.DB.Model(&models.ExternalSource{}).Select("id").Where(whereStatusPageIDEquals, tenantID)).First(&ext).Error
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errExternalComponentNotFound, "code": "external_component_not_found"})
		return comp, ext, false
	}
	if !h.userHasTagAccess(c, h.tagsForComponent(comp.ID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to manage this component", "code": "forbidden"})
		return comp, ext, false
	}
	return comp, ext, true
}

// POST /api/pages/:page_id/external-links {component_id, external_component_id}
// Idempotent: an existing link yields 200 instead of 201.
func (h *Handler) CreateExternalLinkHandler(c *gin.Context) {
	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}
	var in struct {
		ComponentID         uint `json:"component_id" binding:"required"`
		ExternalComponentID uint `json:"external_component_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "component_id and external_component_id are required", "code": "external_link_fields_required"})
		return
	}
	comp, ext, ok := h.resolveLinkTargets(c, tenantID, in.ComponentID, in.ExternalComponentID)
	if !ok {
		return
	}

	var link models.ExternalComponentLink
	err := h.DB.Where("component_id = ? AND external_component_id = ?", comp.ID, ext.ID).First(&link).Error
	status := http.StatusOK
	if errors.Is(err, gorm.ErrRecordNotFound) {
		before := h.effectiveSnapshot(tenantID)
		link = models.ExternalComponentLink{StatusPageID: tenantID, ComponentID: comp.ID, ExternalComponentID: ext.ID}
		err = h.DB.Create(&link).Error
		status = http.StatusCreated
		if err == nil {
			h.notifyEffectiveDiff(c, before, externalstatus.StatusChange{StatusPageID: tenantID, Cause: externalstatus.CauseLink, SourceID: ext.ExternalSourceID})
			h.RecordAudit(c, "external_link.created", "config", auditTargetExternalLink, strconv.FormatUint(uint64(link.ID), 10), comp.Name, ext.Name)
		}
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to link component", "code": "server_error"})
		return
	}
	// The response comes from the same list as GET so group status and
	// warnings (overlaps_group) are identical.
	rows, err := h.externalLinks(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to link component", "code": "server_error"})
		return
	}
	var resp externalLinkResponse
	for _, row := range rows {
		if row.ID == link.ID {
			resp = row
		}
	}
	c.JSON(status, resp)
}

// DELETE /api/pages/:page_id/external-links/:id
func (h *Handler) DeleteExternalLinkHandler(c *gin.Context) {
	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid link ID", "code": "invalid_id"})
		return
	}
	var link models.ExternalComponentLink
	if err := h.DB.Where("id = ? AND status_page_id = ?", id, tenantID).First(&link).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errExternalLinkNotFound, "code": "external_link_not_found"})
		return
	}
	if !h.userHasTagAccess(c, h.tagsForComponent(link.ComponentID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to manage this component", "code": "forbidden"})
		return
	}
	before := h.effectiveSnapshot(tenantID)
	if err := h.DB.Delete(&link).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to unlink component", "code": "server_error"})
		return
	}
	h.notifyEffectiveDiff(c, before, externalstatus.StatusChange{StatusPageID: tenantID, Cause: externalstatus.CauseUnlink})
	h.RecordAudit(c, "external_link.deleted", "config", auditTargetExternalLink, strconv.FormatUint(uint64(link.ID), 10), "", "")
	c.JSON(http.StatusOK, gin.H{"message": "External link deleted"})
}

// annotateEffectiveStatus sets EffectiveStatus and
// ExternalSources for the list responses (component tree, dashboard).
// Own status = stored status overlaid with open incidents
// (componentstatus.Own); groups get "group" and are aggregated from
// their children in buildTree. If the external data is missing (DB error),
// the effective status stays the own one - the page should not fail because of it.
func (h *Handler) annotateEffectiveStatus(pageID uint, all []models.Component, openComponentIDs map[uint]string) {
	refs, err := externalstatus.LinkedRefs(h.DB, pageID)
	if err != nil {
		slog.Error("Externe Status nicht lesbar", "status_page_id", pageID, "error", err)
	}
	for i := range all {
		if all[i].Status == componentstatus.Group {
			all[i].EffectiveStatus = componentstatus.Group
			continue
		}
		all[i].ExternalSources = refs[all[i].ID]
		all[i].EffectiveStatus = externalstatus.Effective(componentstatus.Own(all[i].Status, openComponentIDs[all[i].ID]), all[i].ExternalSources)
	}
}

// deleteMirroredIncidents removes (soft) the mirrored incidents and
// maintenances of a source including updates and affected components.
func deleteMirroredIncidents(tx *gorm.DB, sourceID uint) error {
	ids := tx.Model(&models.ExternalIncident{}).Select("id").Where("external_source_id = ?", sourceID)
	if err := tx.Where("external_incident_id IN (?)", ids).Delete(&models.ExternalIncidentUpdate{}).Error; err != nil {
		return err
	}
	if err := tx.Where("external_incident_id IN (?)", ids).Delete(&models.ExternalIncidentComponent{}).Error; err != nil {
		return err
	}
	return tx.Where("external_source_id = ?", sourceID).Delete(&models.ExternalIncident{}).Error
}
