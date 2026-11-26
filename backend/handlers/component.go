package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/statushistory"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// allowedComponentStatuses are the permitted values for Component.Status that
// may be set deliberately via status update (admin UI or monitoring webhook).
// "group" is intentionally excluded - it is a pure
// folder node whose status is always aggregated from its children (see
// buildTree) and must never be set directly.
const componentStatusGroup = "group"

// errComponentIsGroup: status write to a group.
var errComponentIsGroup = errors.New("component is a group; its status is derived from its children")

var allowedComponentStatuses = map[string]bool{
	"operational":          true,
	"degraded_performance": true,
	"partial_outage":       true,
	"major_outage":         true,
	"under_maintenance":    true,
}

// setComponentStatus validates the given status and persists it on
// the component. Central place for the status logic, so that both the
// admin update handler and the monitoring webhook handler
// use the same validation/storage instead of duplicating it.
//
// Saving and the history entry run in ONE transaction; the
// old state is read fresh under a row lock (not from the possibly
// stale component) so that parallel writers do not log a false change
// protokollieren. source: models.StatusSource* (manual/webhook).
func setComponentStatus(db *gorm.DB, component *models.Component, status, source string) error {
	// The status of a group is computed from its children and
	// must not be replaced by any writer (webhook, quick status, admin).
	if component.Status == componentStatusGroup {
		return errComponentIsGroup
	}
	if !allowedComponentStatuses[status] {
		return newCodedError("invalid_component_status", fmt.Sprintf("invalid status: %q", status))
	}
	return db.Transaction(func(tx *gorm.DB) error {
		old, err := statushistory.Lock(tx, component.ID)
		if err != nil {
			return err
		}
		prev := component.Status
		component.Status = status
		if err := tx.Omit("archived_at").Save(component).Error; err != nil {
			component.Status = prev
			return err
		}
		return statushistory.Record(tx, component.ID, old, status, source, time.Now())
	})
}

// statusSeverity/worstStatus have delegated to the componentstatus package since the changeover,
// which is also used by the polling service for external status pages.
func statusSeverity(s string) int { return componentstatus.Severity(s) }

func worstStatus(statuses []string) string { return componentstatus.Worst(statuses...) }

// effectiveComponentStatus: logic lives in componentstatus since the changeover
// (FromOpenIncident/Own), see there.
func effectiveComponentStatus(manualStatus, incidentSeverity string) string {
	return componentstatus.Own(manualStatus, incidentSeverity)
}

// slugRetryAttempts: attempts when creating with an automatic identifier, in case
// a parallel creation assigns the same identifier.
const slugRetryAttempts = 3

// Field lengths in characters (runes, not bytes, because of umlauts).
const (
	maxComponentNameLen        = 60
	maxComponentDescriptionLen = 240
)

// validateComponentTexts checks name and description against the length limits
// and on violation returns the machine-readable error code
// (name_too_long/description_too_long), which the frontend translates at the field.
// nil means "field not sent, do not check". Existing longer values
// are never truncated; they only surface on the next save of the
// field concerned.
func validateComponentTexts(name, description *string) (code, msg string) {
	if name != nil && utf8.RuneCountInString(*name) > maxComponentNameLen {
		return "name_too_long", fmt.Sprintf("Name must not exceed %d characters", maxComponentNameLen)
	}
	if description != nil && utf8.RuneCountInString(*description) > maxComponentDescriptionLen {
		return "description_too_long", fmt.Sprintf("Description must not exceed %d characters", maxComponentDescriptionLen)
	}
	return "", ""
}

// respondComponentTextTooLong answers the length check with 400 and a code.
func respondComponentTextTooLong(c *gin.Context, code, msg string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": code})
}

// hiddenComponentIDs: the public rule is in models.HiddenComponentIDs
// (shared with self-monitoring).
func hiddenComponentIDs(all []models.Component) map[uint]bool {
	return models.HiddenComponentIDs(all)
}

// withoutHiddenComponents removes invisible components including descendants
// from the flat list, for all public views.
func withoutHiddenComponents(all []models.Component) []models.Component {
	hidden := hiddenComponentIDs(all)
	if len(hidden) == 0 {
		return all
	}
	out := make([]models.Component, 0, len(all))
	for _, comp := range all {
		if !hidden[comp.ID] {
			out = append(out, comp)
		}
	}
	return out
}

// callerMaySeeHiddenComponents: Global Admin or a role on the page that
// reads or writes components or incidents (the incident and maintenance
// forms also need invisible components for selection).
func (h *Handler) callerMaySeeHiddenComponents(c *gin.Context, pageID uint) bool {
	for _, perm := range []string{"components:read", "incidents:read", "maintenance:read", "external_sources:read"} {
		if h.userHasPermissionOnPrivatePage(c, pageID, perm) {
			return true
		}
	}
	return false
}

// stripHiddenIncidentComponents removes invisible components from the
// incident assignments so that their name does not leak publicly via
// an incident. The incident itself stays visible.
func stripHiddenIncidentComponents(incidents []models.Incident, hidden map[uint]bool) {
	if len(hidden) == 0 {
		return
	}
	for i := range incidents {
		kept := make([]models.IncidentComponent, 0, len(incidents[i].IncidentComponents))
		for _, ic := range incidents[i].IncidentComponents {
			if !hidden[ic.ComponentID] {
				kept = append(kept, ic)
			}
		}
		incidents[i].IncidentComponents = kept
	}
}

// buildTree builds a tree from a flat list and aggregates statuses upward
func buildTree(all []models.Component, parentID *uint) []models.Component {
	// non-nil so that empty lists are serialized as [] instead of null.
	nodes := []models.Component{}
	for i := range all {
		comp := all[i]
		if (parentID == nil && comp.ParentID == nil) ||
			(parentID != nil && comp.ParentID != nil && *comp.ParentID == *parentID) {
			// load children recursively
			comp.Children = buildTree(all, &comp.ID)
			// mark group before the status is replaced by the
			// child status. Otherwise the admin cannot tell a group
			// from a normal component.
			comp.IsGroup = comp.Status == componentStatusGroup
			// Aggregate status: if group, pass up the worst child status
			if comp.IsGroup && len(comp.Children) > 0 {
				// archived children do not count toward the status of the
				// active view; a group that is itself archived aggregates all.
				childStatuses := make([]string, 0, len(comp.Children))
				childEffective := make([]string, 0, len(comp.Children))
				for _, ch := range comp.Children {
					if ch.ArchivedAt != nil && comp.ArchivedAt == nil {
						continue
					}
					childStatuses = append(childStatuses, ch.Status)
					childEffective = append(childEffective, ch.EffectiveStatus)
				}
				if len(childStatuses) > 0 {
					comp.Status = worstStatus(childStatuses)
					// the effective status (if annotated) likewise.
					if comp.EffectiveStatus != "" {
						comp.EffectiveStatus = worstStatus(childEffective)
					}
				}
			}
			nodes = append(nodes, comp)
		}
	}
	// Sort by SortOrder
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].SortOrder < nodes[j].SortOrder
	})
	return nodes
}

func (h *Handler) GetComponentsHandler(c *gin.Context) {
	tenantIDStr := c.Param("page_id")
	if tenantIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "page_id parameter missing", "code": "invalid_request"})
		return
	}

	tenantID, err := strconv.ParseUint(tenantIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page_id parameter", "code": "invalid_id"})
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

	all := []models.Component{}
	// id as second criterion so that equal SortOrder (e.g. 0 for
	// older components) is sorted stably by creation; Postgres otherwise guarantees
	// no order on ties.
	if err := h.DB.Where("status_page_id = ?", tenantID).Order("sort_order asc, id asc").Find(&all).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch components", "code": "server_error"})
		return
	}
	// This route is also readable anonymously (main.go). Invisible
	// components are seen only by those who manage the page; everyone else gets the
	// filtered tree as on the public status page.
	if !h.callerMaySeeHiddenComponents(c, page.ID) {
		all = withoutHiddenComponents(all)
	}
	tagsByComponent := h.tagsByComponentOfPage(page.ID)
	for i := range all {
		all[i].Tags = tagsByComponent.of(all[i].ID)
	}
	// Status remains the stored value (admin form),
	// effective_status/external_sources are added.
	h.annotateEffectiveStatus(page.ID, all, h.componentIDsWithOpenIncidents(page.ID))

	tree := buildTree(all, nil)
	c.JSON(http.StatusOK, tree)
}

func (h *Handler) CreateComponentHandler(c *gin.Context) {
	type CreateReq struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		// TagIDs replaces the former free-text Country field. Empty
		// means "no tag" - unlike the old Country default, this does NOT
		// fall back automatically to "global" (fail-closed).
		TagIDs []uint `json:"tag_ids"`
		// Status on creation (empty = operational) and parent component.
		Status   string `json:"status"`
		ParentID *uint  `json:"parent_id"`
		// true creates a group. A group has no status of its own
		// (it is computed from the children, see buildTree); a
		// status sent along is therefore an error instead of silently ignored.
		IsGroup bool `json:"is_group"`
		// nil (field missing) = visible.
		Visible *bool `json:"visible"`
		// empty = automatically from the name.
		Slug string `json:"slug"`
	}
	var req CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}
	if code, msg := validateComponentTexts(&req.Name, &req.Description); code != "" {
		respondComponentTextTooLong(c, code, msg)
		return
	}

	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}
	if !h.checkNewComponentSlug(c, tenantID, req.Slug) {
		return
	}

	status, err := initialComponentStatus(req.IsGroup, req.Status)
	if err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}
	var parentID *uint
	if req.ParentID != nil && *req.ParentID != 0 {
		if err := h.validateParent(tenantID, 0, *req.ParentID); err != nil {
			respondCoded(c, http.StatusBadRequest, err)
			return
		}
		parentID = req.ParentID
	}

	tags, err := h.resolveTagsByIDs(req.TagIDs, tenantID)
	if err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}
	if !h.userHasTagAccess(c, tags) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to create components with these tags", "code": "forbidden"})
		return
	}
	if !h.userMayAssignTags(c, tags, nil) {
		return
	}

	comp := models.Component{
		StatusPageID: tenantID,
		Name:         req.Name,
		Slug:         req.Slug,
		Description:  req.Description,
		Status:       status,
		ParentID:     parentID,
		SortOrder:    h.nextSiblingSortOrder(tenantID, parentID),
		Visible:      req.Visible == nil || *req.Visible,
	}
	err = h.createComponent(&comp, tags, req.Visible)
	if isUniqueViolation(err) { // Race after the pre-check
		respondSlugTaken(c)
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create component", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "component.created", "config", "Component", strconv.FormatUint(uint64(comp.ID), 10), comp.Name, "")
	comp.Tags = tags
	comp.IsGroup = req.IsGroup
	c.JSON(http.StatusCreated, comp)
}

// createComponent creates the component with its tags in one transaction.
// GORM replaces the zero value false on Create with the tag default true; an
// entry created as invisible is therefore corrected afterwards. If the caller
// gave no identifier, BeforeCreate assigns one; if it collides in
// a race with a parallel creation (unique index), it is retried with a newly
// computed identifier.
func (h *Handler) createComponent(comp *models.Component, tags []models.Tag, visible *bool) error {
	autoSlug := comp.Slug == ""
	var err error
	for attempt := 0; attempt < slugRetryAttempts; attempt++ {
		err = h.DB.Transaction(func(tx *gorm.DB) error {
			if autoSlug {
				comp.Slug = ""
			}
			if err := tx.Create(comp).Error; err != nil {
				return err
			}
			// Initial status as first entry of the history.
			if err := statushistory.Record(tx, comp.ID, "", comp.Status, models.StatusSourceCreated, time.Now()); err != nil {
				return err
			}
			if visible != nil && !*visible {
				if err := tx.Model(comp).Update("visible", false).Error; err != nil {
					return err
				}
				comp.Visible = false
			}
			return h.replaceComponentTags(tx, comp.ID, tags)
		})
		if !autoSlug || !isUniqueViolation(err) {
			return err
		}
		comp.ID = 0
	}
	return err
}

// initialComponentStatus returns the initial status of a new component or
// group. Groups get the marker status "group" and must
// not bring their own status; components: empty = operational, otherwise
// the value must be permitted.
func initialComponentStatus(isGroup bool, requested string) (string, error) {
	if isGroup {
		if requested != "" {
			return "", newCodedError("group_has_no_status", "a group has no status of its own")
		}
		return componentStatusGroup, nil
	}
	if requested == "" {
		return "operational", nil
	}
	if !allowedComponentStatuses[requested] {
		return "", newCodedError("invalid_component_status", fmt.Sprintf("invalid status: %q", requested))
	}
	return requested, nil
}

// nextSiblingSortOrder returns the SortOrder for a new component under
// parentID (nil = top level): one more than the highest of its siblings.
// Previously every new component got the default 0 and thus ended up
// before all siblings with a maintained order (seed: 1, 2, 3 …), instead of
// at the end as expected.
func (h *Handler) nextSiblingSortOrder(tenantID uint, parentID *uint) int {
	q := h.DB.Model(&models.Component{}).Where("status_page_id = ?", tenantID)
	if parentID == nil {
		q = q.Where("parent_id IS NULL")
	} else {
		q = q.Where("parent_id = ?", *parentID)
	}
	var maxOrder int
	// On a read error maxOrder stays 0; the order is then only
	// cosmetically wrong, creation should not fail because of it.
	q.Select("COALESCE(MAX(sort_order), 0)").Scan(&maxOrder)
	return maxOrder + 1
}

type componentUpdateReq struct {
	Name string `json:"name"`
	// no longer binding:"required", because groups do not have to
	// send a status; for normal components the handler checks itself.
	Status string `json:"status"`
	// nil (field missing) = unchanged, "" = clear description.
	Description *string `json:"description"`
	TagIDs      *[]uint `json:"tag_ids"` // nil = leave unchanged, [] = remove tags
	// nil = unchanged, 0 = root, otherwise ID of the parent component.
	ParentID *uint `json:"parent_id"`
	// nil = unchanged.
	Visible *bool `json:"visible"`
	// the identifier is immutable; only the unchanged
	// value is allowed (the form sends it along), anything else yields 400.
	Slug *string `json:"slug"`
}

// applyParentChange sets the new parent component (0 = top level) and
// writes the response on errors (false). An archived
// component stays in its group (409), otherwise reparenting would bypass the rule
// "restore only via the group"; an unchanged assignment (the
// form sends it along) is allowed and not rechecked.
func (h *Handler) applyParentChange(c *gin.Context, component *models.Component, tenantID, parentID uint) bool {
	return h.applyParentChangeDB(c, h.DB, component, tenantID, parentID)
}

// applyParentChangeDB is like applyParentChange, but reads via db (
// inside the transaction with row lock).
func (h *Handler) applyParentChangeDB(c *gin.Context, db *gorm.DB, component *models.Component, tenantID, parentID uint) bool {
	// read fresh; if the component was archived since loading,
	// the current state applies (otherwise an archived element would end up on top).
	var fresh models.Component
	if err := db.Select("id", "archived_at").First(&fresh, component.ID).Error; err == nil {
		component.ArchivedAt = fresh.ArchivedAt
	}
	if component.ArchivedAt != nil {
		if sameParent(component.ParentID, parentID) {
			return true
		}
		c.JSON(http.StatusConflict, gin.H{"error": "Archived component cannot be moved: restore it first", "code": "component_archived"})
		return false
	}
	if parentID == 0 {
		component.ParentID = nil
		return true
	}
	if err := validateParentDB(db, tenantID, component.ID, parentID); err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return false
	}
	component.ParentID = &parentID
	return true
}

// sameParent: whether the requested parent_id (0 = top level) equals the current one.
func sameParent(current *uint, requested uint) bool {
	if current == nil {
		return requested == 0
	}
	return *current == requested
}

// validateParent checks whether parentID is suitable as parent component of
// compID (0 = new component): it must exist on the same status page
// (tenant separation), must not be the component itself, and must
// not create a cycle (parent is a descendant of compID).
func (h *Handler) validateParent(tenantID, compID, parentID uint) error {
	return validateParentDB(h.DB, tenantID, compID, parentID)
}

func validateParentDB(db *gorm.DB, tenantID, compID, parentID uint) error {
	if parentID == compID {
		return newCodedError("parent_is_self", "component cannot be its own parent")
	}
	var parent models.Component
	if err := db.Where("id = ? AND status_page_id = ?", parentID, tenantID).First(&parent).Error; err != nil {
		return newCodedError("parent_not_found", "parent component not found")
	}
	// nothing can be moved into an archived group.
	if parent.ArchivedAt != nil {
		return newCodedError("parent_archived", "parent group is archived")
	}
	// Walk upward from the parent component; if compID is encountered, it would be a cycle.
	// Visited IDs prevent infinite loops with an already existing cycle
	// (legacy issue, direct DB intervention) that does not contain compID.
	visited := map[uint]bool{parent.ID: true}
	for cur := parent.ParentID; cur != nil; {
		if *cur == compID {
			return newCodedError("parent_cycle", "parent would create a cycle")
		}
		if visited[*cur] {
			return newCodedError("parent_cycle", "parent chain is cyclic")
		}
		visited[*cur] = true
		var anc models.Component
		// If an ancestor is missing (deleted), anc.ParentID stays nil and the loop ends.
		db.Select("id", "parent_id").First(&anc, *cur)
		cur = anc.ParentID
	}
	return nil
}

// resolveComponentUpdateTags resolves req.TagIDs (if set) to tags and
// checks the access permission on them. ok=true with newTags=nil means
// "TagIDs was nil, leave tags unchanged". Extracted from
// UpdateComponentHandler so that its cognitive complexity does not
// exceed the SonarCloud limit (go:S3776).
func (h *Handler) resolveComponentUpdateTags(c *gin.Context, req componentUpdateReq, tenantID uint, current []models.Tag) (newTags []models.Tag, ok bool) {
	if req.TagIDs == nil {
		return nil, true
	}
	newTags, err := h.resolveTagsByIDs(*req.TagIDs, tenantID)
	if err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return nil, false
	}
	if !h.userHasTagAccess(c, newTags) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to move components to these tags", "code": "forbidden"})
		return nil, false
	}
	if !h.userMayAssignTags(c, newTags, current) {
		return nil, false
	}
	return newTags, true
}

// saveComponentUpdate saves the changed component. Normal components
// go through setComponentStatus (validation + saving), groups keep
// their status "group". Extracted so that the cognitive
// complexity of UpdateComponentHandler does not rise further (go:S3776).
func (h *Handler) saveComponentUpdate(db *gorm.DB, component *models.Component, isGroup bool, status string) error {
	if isGroup {
		return db.Omit("archived_at").Save(component).Error
	}
	return setComponentStatus(db, component, status, models.StatusSourceManual)
}

// errParentChangeRejected aborts the transaction when the response has already been written.
var errParentChangeRejected = errors.New("parent change rejected")

// saveWithParentChange: page lock, parent check and saving in
// one transaction. false = response has been written.
func (h *Handler) saveWithParentChange(c *gin.Context, component *models.Component, tenantID, parentID uint, isGroup bool, status string) bool {
	var saveErr error
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockedPageComponents(tx, tenantID); err != nil {
			return err
		}
		if !h.applyParentChangeDB(c, tx, component, tenantID, parentID) {
			return errParentChangeRejected
		}
		saveErr = h.saveComponentUpdate(tx, component, isGroup, status)
		return saveErr
	})
	switch {
	case err == nil:
		return true
	case errors.Is(err, errParentChangeRejected):
	case saveErr != nil:
		respondCoded(c, http.StatusBadRequest, saveErr)
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update component", "code": "server_error"})
	}
	return false
}

func (h *Handler) UpdateComponentHandler(c *gin.Context) {
	compIDStr := c.Param("id")
	compID, err := strconv.ParseUint(compIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid component ID", "code": "invalid_id"})
		return
	}

	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}

	var component models.Component
	if err := h.DB.Where("id = ? AND status_page_id = ?", compID, tenantID).First(&component).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found", "code": "component_not_found"})
		return
	}

	if !h.userHasTagAccess(c, h.tagsForComponent(component.ID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to manage this component", "code": "forbidden"})
		return
	}

	var req componentUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}

	// The status of a group is not a stored value but
	// is computed from the children (buildTree). A status sent along
	// is therefore ignored for groups instead of rejected with 400: Older
	// admin states still loaded in the browser send the
	// aggregated child status along when renaming; a 400 would block editing
	// groups there, ignoring keeps it working correctly. A conversion
	// group <-> component is not intended via update (the issue
	// does not require it).
	isGroup := component.Status == componentStatusGroup
	if !isGroup && req.Status == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request fields", "code": "invalid_request"})
		return
	}

	if req.Slug != nil && *req.Slug != component.Slug {
		respondSlugImmutable(c)
		return
	}
	if code, msg := validateComponentTexts(&req.Name, req.Description); code != "" {
		respondComponentTextTooLong(c, code, msg)
		return
	}
	if req.Name != "" {
		component.Name = req.Name
	}
	if req.Visible != nil {
		component.Visible = *req.Visible
	}
	newTags, ok := h.resolveComponentUpdateTags(c, req, tenantID, h.tagsForComponent(component.ID))
	if !ok {
		return
	}
	if req.Description != nil {
		component.Description = *req.Description
	}
	if req.ParentID != nil && !sameParent(component.ParentID, *req.ParentID) {
		// A parent change checks and saves under the lock
		// of the page in one transaction (otherwise archiving runs in between).
		if !h.saveWithParentChange(c, &component, tenantID, *req.ParentID, isGroup, req.Status) {
			return
		}
	} else {
		if req.ParentID != nil && !h.applyParentChange(c, &component, tenantID, *req.ParentID) {
			return
		}
		if err := h.saveComponentUpdate(h.DB, &component, isGroup, req.Status); err != nil {
			respondCoded(c, http.StatusBadRequest, err)
			return
		}
	}
	component.IsGroup = isGroup
	if req.TagIDs != nil {
		if err := h.replaceComponentTags(h.DB, component.ID, newTags); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update tags", "code": "server_error"})
			return
		}
	}

	h.RecordAudit(c, "component.updated", "config", "Component", strconv.FormatUint(uint64(component.ID), 10), component.Name, "")
	component.Tags = h.tagsForComponent(component.ID)
	c.JSON(http.StatusOK, component)
}

func (h *Handler) DeleteComponentHandler(c *gin.Context) {
	idStr := c.Param("id")
	compID, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid component ID", "code": "invalid_id"})
		return
	}

	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}

	var comp models.Component
	if err := h.DB.Where("id = ? AND status_page_id = ?", compID, tenantID).First(&comp).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found", "code": "component_not_found"})
		return
	}

	if !h.userHasTagAccess(c, h.tagsForComponent(comp.ID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to delete this component", "code": "forbidden"})
		return
	}

	// Do not delete a group with children. The children would
	// otherwise be orphaned (parent_id points nowhere) and fall out of the tree.
	// Machine-readable code so that the frontend translates the message.
	if comp.Status == componentStatusGroup {
		var children int64
		if err := h.DB.Model(&models.Component{}).Where("parent_id = ? AND status_page_id = ?", comp.ID, tenantID).Count(&children).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete component", "code": "server_error"})
			return
		}
		if children > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": "Group is not empty: move or delete its components first", "code": "group_not_empty"})
			return
		}
	}

	// an incident can affect several components - when deleting
	// a component, only remove the assignment, no longer the whole
	// incident (it might still be valid for other components).
	h.DB.Where("component_id = ?", comp.ID).Delete(&models.IncidentComponent{})
	// Also remove links to external components.
	h.DB.Where("component_id = ?", comp.ID).Delete(&models.ExternalComponentLink{})

	if err := h.DB.Delete(&comp).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete component", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "component.deleted", "config", "Component", strconv.FormatUint(uint64(comp.ID), 10), comp.Name, "")
	c.JSON(http.StatusOK, gin.H{"message": "Component deleted successfully"})
}
