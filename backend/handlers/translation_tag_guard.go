package handlers

import (
	"net/http"
	"strconv"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// Tag-Wächter für die Übersetzungsrouten von Vorfällen und Komponenten.
//
// Die Tag-Einschränkung (userHasTagAccess) gilt für Vorfälle und Komponenten
// selbst. Die Übersetzungsrouten tragen die Ressource nur als Pfad-Parameter
// und laden sie in ihren Handlern ohne Tag-Prüfung; ohne diesen Wächter ließen
// sich fremd getaggte Inhalte darüber lesen, anlegen, veröffentlichen
// (confirm) und löschen. Die Middleware läuft nach der Rechteprüfung der Route
// und lädt die Tags der Zielressource zentral, statt jeden Handler einzeln zu
// ändern; eine neue Route ohne Wächter fällt im Strukturtest in main_*_test.go
// auf.
//
// Hinweis zu DELETE /api/pages/:page_id/external-sources/:id: Das Löschen einer
// externen Quelle entfernt die Kopplungen aller verknüpften Komponenten. Der
// Handler prüft deshalb selbst, dass der Nutzer auf jede davon Tag-Zugriff hat
// (sonst 403), statt die Quelle als seitenweite Ressource ungeprüft zu löschen.
// ─────────────────────────────────────────────────────────────────────────────

// tagGuard prüft, ob der Aufrufer auf die Ressource zugreifen darf, deren ID
// resolve aus dem Pfad-Parameter param ableitet (ok=false: Ressource unbekannt).
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

// incidentExists/componentExists lösen die ID auf sich selbst auf, aber nur für
// vorhandene Datensätze: Eine unbekannte Ressource trägt keine Tags und würde
// sonst als frei zugänglich durchgehen.
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

// IncidentTagGuard prüft die Tags des Vorfalls aus :incident_id.
func (h *Handler) IncidentTagGuard() gin.HandlerFunc {
	return h.tagGuard("incident_id", incidentExists, incidentTags, msgTagForbiddenIncident)
}

// IncidentTranslationTagGuard prüft die Tags des Vorfalls hinter :translation_id.
func (h *Handler) IncidentTranslationTagGuard() gin.HandlerFunc {
	return h.tagGuard("translation_id", incidentIDOfTranslation, incidentTags, msgTagForbiddenIncident)
}

// ComponentTagGuard prüft die Tags der Komponente aus :component_id.
func (h *Handler) ComponentTagGuard() gin.HandlerFunc {
	return h.tagGuard("component_id", componentExists, componentTags, msgTagForbiddenComponent)
}

// ComponentTranslationTagGuard prüft die Tags der Komponente hinter :translation_id.
func (h *Handler) ComponentTranslationTagGuard() gin.HandlerFunc {
	return h.tagGuard("translation_id", componentIDOfTranslation, componentTags, msgTagForbiddenComponent)
}
