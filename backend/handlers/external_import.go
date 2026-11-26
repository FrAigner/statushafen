package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/statushistory"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// adopt external building blocks directly into your own component tree
// (create component + link in one step).

const (
	// maxExternalImportItems limits the amount per call (one transaction).
	maxExternalImportItems = 200
	// maxImportedComponentNameLen: name limit of the adopted components
	// (Component.Name has no DB limit; the value matches that of the sources).
	maxImportedComponentNameLen = maxExternalSourceNameLen

	skipAlreadyLinked = "already_linked"
	skipVanished      = "vanished"
	skipEmptyGroup    = "empty_group"
)

type externalImportInput struct {
	ExternalComponentIDs []uint `json:"external_component_ids"`
	// ParentID: own group as target; nil/0 = top level.
	ParentID *uint  `json:"parent_id"`
	TagIDs   []uint `json:"tag_ids"`
}

type externalImportCreated struct {
	ComponentID         uint   `json:"component_id"`
	Name                string `json:"name"`
	ExternalComponentID uint   `json:"external_component_id"`
	IsGroup             bool   `json:"is_group"`
}

type externalImportSkipped struct {
	ExternalComponentID uint   `json:"external_component_id"`
	Reason              string `json:"reason"`
}

// importName shortens the external name to the limit (rune-safe); empty
// yields the external ID, because Component.Name must not be empty.
func importName(ec models.ExternalComponent) string {
	name := strings.TrimSpace(ec.Name)
	if name == "" {
		name = strings.TrimSpace(ec.ExternalID)
	}
	if r := []rune(name); len(r) > maxImportedComponentNameLen {
		name = strings.TrimSpace(string(r[:maxImportedComponentNameLen]))
	}
	return name
}

// importer holds the state of an adoption run within a transaction.
type importer struct {
	h        *Handler
	tx       *gorm.DB
	tenantID uint
	tags     []models.Tag
	linked   map[uint]bool
	created  []externalImportCreated
	skipped  []externalImportSkipped
	links    int
}

func (im *importer) newComponent(name, status string, parentID *uint, sortOrder int) (models.Component, error) {
	comp := models.Component{StatusPageID: im.tenantID, Name: name, Status: status, ParentID: parentID, SortOrder: sortOrder}
	if err := im.tx.Create(&comp).Error; err != nil {
		return comp, err
	}
	if err := statushistory.Record(im.tx, comp.ID, "", comp.Status, models.StatusSourceCreated, time.Now()); err != nil {
		return comp, err
	}
	return comp, im.h.replaceComponentTags(im.tx, comp.ID, im.tags)
}

// importOne creates its own component for an external building block and
// links it (same linking as CreateExternalLinkHandler).
func (im *importer) importOne(ec models.ExternalComponent, parentID *uint, sortOrder int) error {
	comp, err := im.newComponent(importName(ec), "operational", parentID, sortOrder)
	if err != nil {
		return err
	}
	link := models.ExternalComponentLink{StatusPageID: im.tenantID, ComponentID: comp.ID, ExternalComponentID: ec.ID}
	if err := im.tx.Create(&link).Error; err != nil {
		return err
	}
	im.linked[ec.ID] = true
	im.links++
	im.created = append(im.created, externalImportCreated{ComponentID: comp.ID, Name: comp.Name, ExternalComponentID: ec.ID})
	return nil
}

// importGroup creates the group component and adopts the not yet
// linked, current members as children. The group itself is not
// linked (its status results from the children).
func (im *importer) importGroup(g models.ExternalComponent, parentID *uint, sortOrder int) error {
	var members []models.ExternalComponent
	if err := im.tx.Where("external_source_id = ? AND group_external_id = ? AND is_group = ? AND vanished = ?",
		g.ExternalSourceID, g.ExternalID, false, false).
		Order("position asc, name asc, id asc").Find(&members).Error; err != nil {
		return err
	}
	todo := make([]models.ExternalComponent, 0, len(members))
	var linkedMembers []externalImportSkipped
	for _, m := range members {
		if im.linked[m.ID] {
			linkedMembers = append(linkedMembers, externalImportSkipped{ExternalComponentID: m.ID, Reason: skipAlreadyLinked})
			continue
		}
		todo = append(todo, m)
	}
	if len(todo) == 0 {
		// Nothing to adopt: report only the group, not every member individually.
		reason := skipEmptyGroup
		if len(members) > 0 {
			reason = skipAlreadyLinked
		}
		im.skipped = append(im.skipped, externalImportSkipped{ExternalComponentID: g.ID, Reason: reason})
		return nil
	}
	im.skipped = append(im.skipped, linkedMembers...)
	gc, err := im.newComponent(importName(g), componentstatus.Group, parentID, sortOrder)
	if err != nil {
		return err
	}
	im.created = append(im.created, externalImportCreated{ComponentID: gc.ID, Name: gc.Name, ExternalComponentID: g.ID, IsGroup: true})
	for i, m := range todo {
		if err := im.importOne(m, &gc.ID, i+1); err != nil {
			return err
		}
	}
	return nil
}

// run first adopts all selected groups, then the individual building blocks (so
// members of a selected group are not created twice).
func (im *importer) run(sortOrder int, parentID *uint, selection []models.ExternalComponent) error {
	im.created = []externalImportCreated{}
	im.skipped = []externalImportSkipped{}
	for _, ec := range selection {
		if !ec.IsGroup {
			continue
		}
		if ec.Vanished {
			im.skipped = append(im.skipped, externalImportSkipped{ExternalComponentID: ec.ID, Reason: skipVanished})
			continue
		}
		before := len(im.created)
		if err := im.importGroup(ec, parentID, sortOrder); err != nil {
			return err
		}
		if len(im.created) > before {
			sortOrder++
		}
	}
	for _, ec := range selection {
		switch {
		case ec.IsGroup:
			continue
		case ec.Vanished:
			im.skipped = append(im.skipped, externalImportSkipped{ExternalComponentID: ec.ID, Reason: skipVanished})
		case im.linked[ec.ID]:
			im.skipped = append(im.skipped, externalImportSkipped{ExternalComponentID: ec.ID, Reason: skipAlreadyLinked})
		default:
			if err := im.importOne(ec, parentID, sortOrder); err != nil {
				return err
			}
			sortOrder++
		}
	}
	return nil
}

// importTarget checks the target: own group of the same page (or top level).
func (h *Handler) importTarget(tenantID uint, in externalImportInput) (*uint, error) {
	if in.ParentID == nil || *in.ParentID == 0 {
		return nil, nil
	}
	if err := h.validateParent(tenantID, 0, *in.ParentID); err != nil {
		return nil, err
	}
	var parent models.Component
	if err := h.DB.Select("id", "status").First(&parent, *in.ParentID).Error; err != nil || parent.Status != componentstatus.Group {
		return nil, newCodedError("parent_not_group", "parent must be a group")
	}
	return in.ParentID, nil
}

// loadImportSelection loads the selected external building blocks of the source
// (without duplicates, in request order). Foreign/unknown IDs yield false.
func (h *Handler) loadImportSelection(src models.ExternalSource, ids []uint) ([]models.ExternalComponent, bool) {
	seen := map[uint]bool{}
	uniq := make([]uint, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	var rows []models.ExternalComponent
	if err := h.DB.Where("external_source_id = ? AND id IN ?", src.ID, uniq).Find(&rows).Error; err != nil || len(rows) != len(uniq) {
		return nil, false
	}
	byID := map[uint]models.ExternalComponent{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	out := make([]models.ExternalComponent, 0, len(uniq))
	for _, id := range uniq {
		out = append(out, byID[id])
	}
	return out, true
}

// linkedExternalIDs: all external building blocks of the page that are already linked.
func linkedExternalIDs(tx *gorm.DB, pageID uint) (map[uint]bool, error) {
	var ids []uint
	if err := tx.Model(&models.ExternalComponentLink{}).Where(whereStatusPageIDEquals, pageID).Pluck("external_component_id", &ids).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// POST /api/pages/:page_id/external-sources/:id/import
// {external_component_ids, parent_id?, tag_ids?}
//
// Per selected building block: create its own component (name = external name,
// operational) under parent_id and link it; a group is adopted as a
// group component with its current members. Already
// linked and vanished building blocks are skipped (double-click
// protection). All in one transaction. Response 201 (at least one component
// created) or 200 (nothing to do): {created, skipped}.
func (h *Handler) ImportExternalComponentsHandler(c *gin.Context) {
	src, ok := h.loadExternalSource(c)
	if !ok {
		return
	}
	var in externalImportInput
	if err := c.ShouldBindJSON(&in); err != nil || len(in.ExternalComponentIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "external_component_ids are required", "code": "invalid_request"})
		return
	}
	if len(in.ExternalComponentIDs) > maxExternalImportItems {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Too many components (maximum " + strconv.Itoa(maxExternalImportItems) + ")", "code": "too_many_components"})
		return
	}
	tenantID := src.StatusPageID
	parentID, err := h.importTarget(tenantID, in)
	if err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}
	tags, err := h.resolveTagsByIDs(in.TagIDs, tenantID)
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
	selection, ok := h.loadImportSelection(src, in.ExternalComponentIDs)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": errExternalComponentNotFound, "code": "external_component_not_found"})
		return
	}

	before := h.effectiveSnapshot(tenantID)
	im := &importer{h: h, tenantID: tenantID, tags: tags}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		linked, err := linkedExternalIDs(tx, tenantID)
		if err != nil {
			return err
		}
		im.tx, im.linked = tx, linked
		return im.run(h.nextSiblingSortOrder(tenantID, parentID), parentID, selection)
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to import components", "code": "server_error"})
		return
	}
	if len(im.created) == 0 {
		c.JSON(http.StatusOK, gin.H{"created": im.created, "skipped": im.skipped})
		return
	}
	h.notifyEffectiveDiff(c, before, externalstatus.StatusChange{StatusPageID: tenantID, Cause: externalstatus.CauseLink, SourceID: src.ID})
	for _, cr := range im.created {
		h.RecordAudit(c, "component.created", "config", "Component", strconv.FormatUint(uint64(cr.ComponentID), 10), cr.Name, "")
	}
	h.RecordAudit(c, "external_link.imported", "config", auditTargetExternalSource, strconv.FormatUint(uint64(src.ID), 10), src.Name,
		strconv.Itoa(len(im.created))+" components, "+strconv.Itoa(im.links)+" links")
	c.JSON(http.StatusCreated, gin.H{"created": im.created, "skipped": im.skipped})
}
