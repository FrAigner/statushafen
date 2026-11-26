package handlers

import (
	"net/http"
	"strconv"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// Page scope for the translation write routes.
//
// The routes POST /api/{incidents,components,notices}/:id/translations and
// PATCH/DELETE /api/*-translations/:translation_id carry no :page_id in the
// path. middleware.RequirePermission requires it, however, and therefore rejected
// every non-global admin with 400 - page admins could not maintain
// translations. Instead of restructuring the routes (frontend and spec
// stay unchanged), this middleware derives the page from the resource
// and sets :page_id before RequirePermission runs. Because the page comes from
// the resource itself, tenant membership is checked automatically:
// anyone with rights only on page A gets 403 for resources of page B.
// Unknown (or deleted) resources yield 404, as the
// handlers themselves did before.
// ─────────────────────────────────────────────────────────────────────────────

// pageIDLookup returns the StatusPageID of the resource with the given ID
// or ok=false if it does not exist.
type pageIDLookup func(h *Handler, id uint) (uint, bool)

// scopeToResourcePage parses the path parameter param, uses lookup to determine
// the associated status page, and appends it as :page_id to the parameters.
func (h *Handler) scopeToResourcePage(param string, lookup pageIDLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param(param), 10, 32)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Invalid " + param + " parameter", "code": "invalid_id"})
			return
		}
		pageID, ok := lookup(h, uint(id))
		if !ok {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "Not found", "code": "not_found"})
			return
		}
		c.Params = append(c.Params, gin.Param{Key: "page_id", Value: strconv.FormatUint(uint64(pageID), 10)})
		c.Next()
	}
}

func incidentPageID(h *Handler, id uint) (uint, bool) {
	var inc models.Incident
	if err := h.DB.Select("id", "status_page_id").First(&inc, id).Error; err != nil {
		return 0, false
	}
	return inc.StatusPageID, true
}

func componentPageID(h *Handler, id uint) (uint, bool) {
	var comp models.Component
	if err := h.DB.Select("id", "status_page_id").First(&comp, id).Error; err != nil {
		return 0, false
	}
	return comp.StatusPageID, true
}

func noticePageID(h *Handler, id uint) (uint, bool) {
	var notice models.GeneralNotice
	if err := h.DB.Select("id", "status_page_id").First(&notice, id).Error; err != nil {
		return 0, false
	}
	return notice.StatusPageID, true
}

func incidentTranslationPageID(h *Handler, id uint) (uint, bool) {
	var tr models.IncidentTranslation
	if err := h.DB.Select("id", "incident_id").First(&tr, id).Error; err != nil {
		return 0, false
	}
	return incidentPageID(h, tr.IncidentID)
}

func componentTranslationPageID(h *Handler, id uint) (uint, bool) {
	var tr models.ComponentTranslation
	if err := h.DB.Select("id", "component_id").First(&tr, id).Error; err != nil {
		return 0, false
	}
	return componentPageID(h, tr.ComponentID)
}

func noticeTranslationPageID(h *Handler, id uint) (uint, bool) {
	var tr models.GeneralNoticeTranslation
	if err := h.DB.Select("id", "general_notice_id").First(&tr, id).Error; err != nil {
		return 0, false
	}
	return noticePageID(h, tr.GeneralNoticeID)
}

// IncidentPageScope sets :page_id from :incident_id.
func (h *Handler) IncidentPageScope() gin.HandlerFunc {
	return h.scopeToResourcePage("incident_id", incidentPageID)
}

// ComponentPageScope sets :page_id from :component_id.
func (h *Handler) ComponentPageScope() gin.HandlerFunc {
	return h.scopeToResourcePage("component_id", componentPageID)
}

// NoticePageScope sets :page_id from :notice_id.
func (h *Handler) NoticePageScope() gin.HandlerFunc {
	return h.scopeToResourcePage("notice_id", noticePageID)
}

// IncidentTranslationPageScope sets :page_id from :translation_id
// (translation → incident → page).
func (h *Handler) IncidentTranslationPageScope() gin.HandlerFunc {
	return h.scopeToResourcePage("translation_id", incidentTranslationPageID)
}

// ComponentTranslationPageScope sets :page_id from :translation_id
// (translation → component → page).
func (h *Handler) ComponentTranslationPageScope() gin.HandlerFunc {
	return h.scopeToResourcePage("translation_id", componentTranslationPageID)
}

// NoticeTranslationPageScope sets :page_id from :translation_id
// (translation → notice → page).
func (h *Handler) NoticeTranslationPageScope() gin.HandlerFunc {
	return h.scopeToResourcePage("translation_id", noticeTranslationPageID)
}
