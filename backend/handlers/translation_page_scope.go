package handlers

import (
	"net/http"
	"strconv"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// Seiten-Scope für die Übersetzungs-Schreibrouten.
//
// Die Routen POST /api/{incidents,components,notices}/:id/translations und
// PATCH/DELETE /api/*-translations/:translation_id tragen keine :page_id im
// Pfad. middleware.RequirePermission verlangt sie aber zwingend und wies
// deshalb jeden Nicht-Global-Admin mit 400 ab - Seiten-Admins konnten keine
// Übersetzungen pflegen. Statt die Routen umzubauen (Frontend und Spec
// bleiben unverändert), leitet diese Middleware die Seite aus der Ressource
// ab und setzt :page_id, bevor RequirePermission läuft. Weil die Seite aus
// der Ressource selbst stammt, ist die Mandanten-Zugehörigkeit automatisch
// geprüft: Wer nur auf Seite A Rechte hat, bekommt für Ressourcen von Seite B
// 403. Unbekannte (oder gelöschte) Ressourcen ergeben 404, wie es die
// Handler selbst bisher auch taten.
// ─────────────────────────────────────────────────────────────────────────────

// pageIDLookup liefert die StatusPageID der Ressource mit der gegebenen ID
// oder ok=false, wenn sie nicht existiert.
type pageIDLookup func(h *Handler, id uint) (uint, bool)

// scopeToResourcePage parst den Pfad-Parameter param, ermittelt per lookup
// die zugehörige Statusseite und hängt sie als :page_id an die Parameter an.
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

// IncidentPageScope setzt :page_id aus :incident_id.
func (h *Handler) IncidentPageScope() gin.HandlerFunc {
	return h.scopeToResourcePage("incident_id", incidentPageID)
}

// ComponentPageScope setzt :page_id aus :component_id.
func (h *Handler) ComponentPageScope() gin.HandlerFunc {
	return h.scopeToResourcePage("component_id", componentPageID)
}

// NoticePageScope setzt :page_id aus :notice_id.
func (h *Handler) NoticePageScope() gin.HandlerFunc {
	return h.scopeToResourcePage("notice_id", noticePageID)
}

// IncidentTranslationPageScope setzt :page_id aus :translation_id
// (Übersetzung → Incident → Seite).
func (h *Handler) IncidentTranslationPageScope() gin.HandlerFunc {
	return h.scopeToResourcePage("translation_id", incidentTranslationPageID)
}

// ComponentTranslationPageScope setzt :page_id aus :translation_id
// (Übersetzung → Component → Seite).
func (h *Handler) ComponentTranslationPageScope() gin.HandlerFunc {
	return h.scopeToResourcePage("translation_id", componentTranslationPageID)
}

// NoticeTranslationPageScope setzt :page_id aus :translation_id
// (Übersetzung → Notice → Seite).
func (h *Handler) NoticeTranslationPageScope() gin.HandlerFunc {
	return h.scopeToResourcePage("translation_id", noticeTranslationPageID)
}
