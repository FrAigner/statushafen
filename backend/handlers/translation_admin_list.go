package handlers

import (
	"net/http"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// Admin-Listen der Übersetzungen mit allen Status.
//
// Die öffentlichen Routen GET /api/{incidents,components,notices}/:id/translations
// liefern bewusst nur status = 'confirmed' (Entwürfe sind nicht öffentlich).
// Die Admin-Übersetzungsseiten luden ihre Tabelle aber genau darüber - neu
// angelegte Übersetzungen (immer 'draft') verschwanden nach dem Speichern,
// "Bestätigen" erschien nie. Statt die öffentliche Route um einen Admin-Modus
// zu erweitern (dann hinge "Entwurf sichtbar?" an einer Abfrage im Handler),
// gibt es eine eigene Route .../translations/all - nach dem Muster von
// GET /api/pages/:page_id/notices/all. Die Mandantentrennung übernimmt die
// vorgeschaltete *PageScope-Middleware (Seite aus der Ressource, 404 für
// unbekannte IDs), RequirePermission prüft dann *:read auf genau dieser Seite
// (fremde Seite → 403). Lesen reicht, weil die Seite auch für reine
// Leserollen (Viewer) Entwürfe anzeigt; die Schreibknöpfe blendet das
// Frontend über canWrite aus, die Schreibrouten verlangen weiter *:write.
// ─────────────────────────────────────────────────────────────────────────────

// listAllTranslations lädt alle Übersetzungen (Entwurf und bestätigt) des
// Elternobjekts, dessen ID parse aus dem Pfad liest, sortiert nach Sprache.
func listAllTranslations[T any](h *Handler, c *gin.Context, parse func(*gin.Context) (uint, bool), parentColumn string) {
	parentID, ok := parse(c)
	if !ok {
		return
	}
	translations := []T{}
	if err := h.DB.Where(parentColumn+" = ?", parentID).Order("language_code").Find(&translations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, translations)
}

// GetAllIncidentTranslationsHandler liefert alle Übersetzungen eines Vorfalls
// für den Admin-Bereich.
func (h *Handler) GetAllIncidentTranslationsHandler(c *gin.Context) {
	listAllTranslations[models.IncidentTranslation](h, c, parseIncidentID, "incident_id")
}

// GetAllComponentTranslationsHandler liefert alle Übersetzungen einer
// Komponente für den Admin-Bereich.
func (h *Handler) GetAllComponentTranslationsHandler(c *gin.Context) {
	listAllTranslations[models.ComponentTranslation](h, c, parseComponentID, "component_id")
}

// GetAllGeneralNoticeTranslationsHandler liefert alle Übersetzungen einer
// Ankündigung für den Admin-Bereich.
func (h *Handler) GetAllGeneralNoticeTranslationsHandler(c *gin.Context) {
	listAllTranslations[models.GeneralNoticeTranslation](h, c, parseNoticeID, "general_notice_id")
}
