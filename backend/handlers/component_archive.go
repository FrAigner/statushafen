package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Archive components instead of deleting. Archived components
// keep history, availability, incident assignments and translations and
// can be restored; publicly they are hidden like invisible components
// (hiddenComponentIDs). Status changes via webhook remain
// possible (as with invisible ones) so that a monitoring system does not run into errors.

// descendantComponentIDs returns the IDs of all descendants of rootID in all.
// Visited IDs protect against cycles in legacy data.
func descendantComponentIDs(all []models.Component, rootID uint) []uint {
	children := map[uint][]uint{}
	for _, comp := range all {
		if comp.ParentID != nil {
			children[*comp.ParentID] = append(children[*comp.ParentID], comp.ID)
		}
	}
	seen := map[uint]bool{rootID: true}
	var out []uint
	queue := []uint{rootID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, ch := range children[id] {
			if !seen[ch] {
				seen[ch] = true
				out = append(out, ch)
				queue = append(queue, ch)
			}
		}
	}
	return out
}

// loadComponentForArchive loads the page's component and checks tag access;
// on errors the response is already written (ok=false).
func (h *Handler) loadComponentForArchive(c *gin.Context) (comp models.Component, tenantID uint, ok bool) {
	compID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid component ID", "code": "invalid_id"})
		return comp, 0, false
	}
	tenantID, ok = parseTenantID(c)
	if !ok {
		return comp, 0, false
	}
	if err := h.DB.Where("id = ? AND status_page_id = ?", compID, tenantID).First(&comp).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found", "code": "component_not_found"})
		return comp, 0, false
	}
	if !h.userHasTagAccess(c, h.tagsForComponent(comp.ID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to manage this component", "code": "forbidden"})
		return comp, 0, false
	}
	return comp, tenantID, true
}

// cascadeAccess loads the caller's tag permissions (once, BEFORE the transaction)
// and returns a check for the IDs of the cascade. The component tags come
// with two queries for all IDs so that the row lock is not held during hundreds of
// round trips. Behavior as in userHasTagAccess.
func (h *Handler) cascadeAccess(c *gin.Context) func(db *gorm.DB, ids []uint) (bool, error) {
	userTags, isAdmin := h.userTagsAndAdmin(c)
	return func(db *gorm.DB, ids []uint) (bool, error) {
		if isAdmin || len(userTags) == 0 || len(ids) == 0 {
			return true, nil
		}
		var links []models.ComponentTag
		if err := db.Where("component_id IN ?", ids).Find(&links).Error; err != nil {
			return false, err
		}
		if len(links) == 0 {
			return true, nil
		}
		tagIDs := make([]uint, 0, len(links))
		for _, l := range links {
			tagIDs = append(tagIDs, l.TagID)
		}
		var tags []models.Tag
		if err := db.Where("id IN ?", tagIDs).Find(&tags).Error; err != nil {
			return false, err
		}
		byID := make(map[uint]models.Tag, len(tags))
		for _, tg := range tags {
			byID[tg.ID] = tg
		}
		perComp := map[uint][]models.Tag{}
		for _, l := range links {
			if tg, ok := byID[l.TagID]; ok {
				perComp[l.ComponentID] = append(perComp[l.ComponentID], tg)
			}
		}
		for _, id := range ids {
			if rt := perComp[id]; len(rt) > 0 && !middleware.UserHasAccessToAnyTag(userTags, rt) {
				return false, nil
			}
		}
		return true, nil
	}
}

// errCascadeForbidden aborts the transaction when the tag permissions are missing.
var (
	errCascadeForbidden = errors.New("cascade forbidden")
	errParentArchived   = errors.New("parent archived")
)

// isActiveIn: whether the component id in all is not archived.
func isActiveIn(all []models.Component, id uint) bool {
	for _, x := range all {
		if x.ID == id {
			return x.ArchivedAt == nil
		}
	}
	return false
}

// archivedAtIn: whether the component id in all was archived at exactly the time at.
func archivedAtIn(all []models.Component, id uint, at time.Time) bool {
	for _, x := range all {
		if x.ID == id {
			return x.ArchivedAt != nil && x.ArchivedAt.Equal(at)
		}
	}
	return false
}

// lockedPageComponents reads all components of the page. On Postgres with
// a row lock (FOR NO KEY UPDATE in fixed order: sufficient for archived_at and does not collide with the FK checks of concurrently created incidents, deadlock-free), so that concurrent reparenting/archiving waits until the
// end of the transaction; SQLite does not know the clause and
// serializes writers itself.
func lockedPageComponents(tx *gorm.DB, tenantID uint) ([]models.Component, error) {
	var all []models.Component
	err := tx.Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Where("status_page_id = ?", tenantID).Order("id").Find(&all).Error
	return all, err
}

// hasArchivedAncestor: whether any ancestor of comp is archived (the whole
// chain, not only the direct parent). Visited IDs protect against cycles.
func hasArchivedAncestor(all []models.Component, comp models.Component) bool {
	byID := make(map[uint]models.Component, len(all))
	for _, x := range all {
		byID[x.ID] = x
	}
	seen := map[uint]bool{comp.ID: true}
	for cur := comp.ParentID; cur != nil && !seen[*cur]; {
		seen[*cur] = true
		parent, ok := byID[*cur]
		if !ok {
			return false
		}
		if parent.ArchivedAt != nil {
			return true
		}
		cur = parent.ParentID
	}
	return false
}

func (h *Handler) archiveNow() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// ArchiveComponentHandler archives a component including descendants (a
// group takes its members along). Already archived entries keep
// their timestamp; a second call changes nothing (idempotent).
func (h *Handler) ArchiveComponentHandler(c *gin.Context) {
	comp, tenantID, ok := h.loadComponentForArchive(c)
	if !ok {
		return
	}
	if comp.ArchivedAt != nil {
		c.JSON(http.StatusOK, comp)
		return
	}
	now := h.archiveNow()
	allowed := h.cascadeAccess(c)
	changed := true
	// Reading, checking and writing in ONE transaction, otherwise a
	// concurrently attached active element would remain active under the archived group.
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		all, err := lockedPageComponents(tx, tenantID)
		if err != nil {
			return err
		}
		// The root comes from the locked state: if it has been
		// archived in the meantime, the call stays without writing and audit (idempotent).
		root, found := componentIn(all, comp.ID)
		if !found {
			return gorm.ErrRecordNotFound
		}
		comp = root
		if root.ArchivedAt != nil {
			changed = false
			return nil
		}
		// Only the components that actually change (still active) need a tag permission.
		var ids []uint
		for _, id := range append(descendantComponentIDs(all, comp.ID), comp.ID) {
			if isActiveIn(all, id) {
				ids = append(ids, id)
			}
		}
		ok, err := allowed(tx, ids)
		if err != nil {
			return err
		}
		if !ok {
			return errCascadeForbidden
		}
		return tx.Model(&models.Component{}).
			Where("id IN ? AND status_page_id = ? AND archived_at IS NULL", ids, tenantID).
			Update("archived_at", now).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found", "code": "component_not_found"})
		return
	}
	if errors.Is(err, errCascadeForbidden) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to manage all components of this group", "code": "forbidden"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to archive component", "code": "server_error"})
		return
	}
	if !changed {
		c.JSON(http.StatusOK, comp)
		return
	}
	comp.ArchivedAt = &now
	h.RecordAudit(c, "component.archived", "config", "Component", strconv.FormatUint(uint64(comp.ID), 10), comp.Name, "")
	c.JSON(http.StatusOK, comp)
}

// componentIn finds the component id in all.
func componentIn(all []models.Component, id uint) (models.Component, bool) {
	for _, x := range all {
		if x.ID == id {
			return x, true
		}
	}
	return models.Component{}, false
}

// RestoreComponentHandler restores a component. If it is in an
// archived group, that is only possible via the group (409
// parent_archived). For a group, the members come back that were archived
// together with it (same timestamp); members archived individually earlier
// stay archived.
func (h *Handler) RestoreComponentHandler(c *gin.Context) {
	comp, tenantID, ok := h.loadComponentForArchive(c)
	if !ok {
		return
	}
	if comp.ArchivedAt == nil {
		c.JSON(http.StatusOK, comp)
		return
	}
	allowed := h.cascadeAccess(c)
	changed := true
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		all, err := lockedPageComponents(tx, tenantID)
		if err != nil {
			return err
		}
		// Root from the locked state (timestamp and parent may have changed since
		// loading).
		root, found := componentIn(all, comp.ID)
		if !found {
			return gorm.ErrRecordNotFound
		}
		comp = root
		if root.ArchivedAt == nil {
			changed = false
			return nil
		}
		if hasArchivedAncestor(all, root) {
			return errParentArchived
		}
		// Only the components that actually come back (the root and
		// those archived together with it) need a tag permission.
		at := *root.ArchivedAt
		ids := []uint{comp.ID}
		for _, id := range descendantComponentIDs(all, comp.ID) {
			if archivedAtIn(all, id, at) {
				ids = append(ids, id)
			}
		}
		ok, err := allowed(tx, ids)
		if err != nil {
			return err
		}
		if !ok {
			return errCascadeForbidden
		}
		return tx.Model(&models.Component{}).
			Where("id IN ? AND status_page_id = ? AND (id = ? OR archived_at = ?)", ids, tenantID, comp.ID, at).
			Update("archived_at", nil).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Component not found", "code": "component_not_found"})
		return
	}
	if errors.Is(err, errParentArchived) {
		c.JSON(http.StatusConflict, gin.H{"error": "Parent group is archived: restore the group first", "code": "parent_archived"})
		return
	}
	if errors.Is(err, errCascadeForbidden) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden: You are not authorized to manage all components of this group", "code": "forbidden"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to restore component", "code": "server_error"})
		return
	}
	if !changed {
		c.JSON(http.StatusOK, comp)
		return
	}
	comp.ArchivedAt = nil
	h.RecordAudit(c, "component.restored", "config", "Component", strconv.FormatUint(uint64(comp.ID), 10), comp.Name, "")
	c.JSON(http.StatusOK, comp)
}
