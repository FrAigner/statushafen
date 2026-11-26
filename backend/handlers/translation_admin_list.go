package handlers

import (
	"net/http"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// Admin lists of translations with all statuses.
//
// The public routes GET /api/{incidents,components,notices}/:id/translations
// deliberately return only status = 'confirmed' (drafts are not public).
// The admin translation pages, however, loaded their table from exactly those - newly
// created translations (always 'draft') vanished after saving,
// "Bestätigen" never appeared. Instead of extending the public route with an admin mode
// (which would make "draft visible?" depend on a query in the handler),
// there is a dedicated route .../translations/all - following the pattern of
// GET /api/pages/:page_id/notices/all. Tenant separation is handled by the
// upstream *PageScope middleware (page from the resource, 404 for
// unknown IDs), RequirePermission then checks *:read on exactly this page
// (other page → 403). Read suffices because the page also shows drafts for
// read-only roles (viewer); the frontend hides the write buttons
// via canWrite, and the write routes still require *:write.
// ─────────────────────────────────────────────────────────────────────────────

// listAllTranslations loads all translations (draft and confirmed) of the
// parent object whose ID parse reads from the path, sorted by language.
func listAllTranslations[T any](h *Handler, c *gin.Context, parse func(*gin.Context) (uint, bool), parentColumn string) {
	parentID, ok := parse(c)
	if !ok {
		return
	}
	translations := []T{}
	if err := h.DB.Where(parentColumn+" = ?", parentID).Where("language_code IN ?", h.deliveredTranslationCodes()).Order("language_code").Find(&translations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, translations)
}

// GetAllIncidentTranslationsHandler returns all translations of an incident
// for the admin area.
func (h *Handler) GetAllIncidentTranslationsHandler(c *gin.Context) {
	listAllTranslations[models.IncidentTranslation](h, c, parseIncidentID, "incident_id")
}

// GetAllComponentTranslationsHandler returns all translations of a
// component for the admin area.
func (h *Handler) GetAllComponentTranslationsHandler(c *gin.Context) {
	listAllTranslations[models.ComponentTranslation](h, c, parseComponentID, "component_id")
}

// GetAllGeneralNoticeTranslationsHandler returns all translations of a
// notice for the admin area.
func (h *Handler) GetAllGeneralNoticeTranslationsHandler(c *gin.Context) {
	listAllTranslations[models.GeneralNoticeTranslation](h, c, parseNoticeID, "general_notice_id")
}
