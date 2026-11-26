package handlers

import (
	"net/http"
	"strconv"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// Tag guard for the translation routes of incidents and components.
//
// The tag restriction (userHasTagAccess) applies to incidents and components
// themselves. The translation routes carry the resource only as a path parameter
// and load it in their handlers without a tag check; without this guard,
// content tagged for others could be read, created, published
// (confirm) and deleted through them. The middleware runs after the route's permission check
// and loads the tags of the target resource centrally instead of changing every
// handler individually; a new route without a guard is caught by the structure test
// in main_*_test.go.
//
// Note on DELETE /api/pages/:page_id/external-sources/:id: deleting an
// external source removes the couplings of all linked components. The
// handler therefore checks itself that the user has tag access to each of them
// (otherwise 403), instead of deleting the source unchecked as a page-wide resource.
// ─────────────────────────────────────────────────────────────────────────────

// tagGuard checks whether the caller may access the resource whose ID
// resolve derives from the path parameter param (ok=false: resource unknown).
func (h *Handler) tagGuard(param string, resolve func(h *Handler, id uint) (uint, bool), tagsOf func(h *Handler, id uint) []models.Tag, msg string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := strconv.ParseUint(c.Param(param), 10, 32)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid " + param + " parameter", "code": "invalid_id"})
			return
		}
		resourceID, ok := resolve(h, uint(raw))
		if !ok {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Not found", "code": "not_found"})
			return
		}
		if !h.userHasTagAccess(c, tagsOf(h, resourceID)) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": msg, "code": "forbidden"})
			return
		}
		c.Next()
	}
}

// incidentExists/componentExists resolve the ID to itself, but only for
// existing records: an unknown resource carries no tags and would
// otherwise pass as freely accessible.
func incidentExists(h *Handler, id uint) (uint, bool) {
	_, ok := incidentPageID(h, id)
	return id, ok
}

func componentExists(h *Handler, id uint) (uint, bool) {
	_, ok := componentPageID(h, id)
	return id, ok
}

func incidentIDOfTranslation(h *Handler, id uint) (uint, bool) {
	var tr models.IncidentTranslation
	if err := h.DB.Select("id", "incident_id").First(&tr, id).Error; err != nil {
		return 0, false
	}
	return tr.IncidentID, true
}

func componentIDOfTranslation(h *Handler, id uint) (uint, bool) {
	var tr models.ComponentTranslation
	if err := h.DB.Select("id", "component_id").First(&tr, id).Error; err != nil {
		return 0, false
	}
	return tr.ComponentID, true
}

func incidentTags(h *Handler, id uint) []models.Tag  { return h.tagsForIncident(id) }
func componentTags(h *Handler, id uint) []models.Tag { return h.tagsForComponent(id) }

const (
	msgTagForbiddenIncident  = "Forbidden for this incident"
	msgTagForbiddenComponent = "Forbidden for this component"
)

// IncidentTagGuard checks the tags of the incident from :incident_id.
func (h *Handler) IncidentTagGuard() gin.HandlerFunc {
	return h.tagGuard("incident_id", incidentExists, incidentTags, msgTagForbiddenIncident)
}

// IncidentTranslationTagGuard checks the tags of the incident behind :translation_id.
func (h *Handler) IncidentTranslationTagGuard() gin.HandlerFunc {
	return h.tagGuard("translation_id", incidentIDOfTranslation, incidentTags, msgTagForbiddenIncident)
}

// ComponentTagGuard checks the tags of the component from :component_id.
func (h *Handler) ComponentTagGuard() gin.HandlerFunc {
	return h.tagGuard("component_id", componentExists, componentTags, msgTagForbiddenComponent)
}

// ComponentTranslationTagGuard checks the tags of the component behind :translation_id.
func (h *Handler) ComponentTranslationTagGuard() gin.HandlerFunc {
	return h.tagGuard("translation_id", componentIDOfTranslation, componentTags, msgTagForbiddenComponent)
}
