package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/i18n"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ─────────────────────────────────────────────────────────────────────────────
// Translations of an incident's updates (timeline entries).
//
// An update has only a message, no title. The pattern follows the
// incident translations: one PUT per language with state draft or
// confirmed; only the admin sees drafts, public delivery takes
// only confirmed texts.
//
// Routes (main.go):
//
//	admin:      GET /pages/:page_id/incidents/:id/updates/:update_id/translations
//	            PUT /pages/:page_id/incidents/:id/updates/:update_id/translations/:language_code
//	public: GET /api/public/pages/:slug/incidents/:incident_id/update-translations?lang=xx
//
// The admin routes carry :page_id and :id in the path; permissions follow the
// incident (incidents or maintenance, read or update), and the page is checked via the
// page ID as with "post update". If the update is deleted, the foreign key
// (OnDelete:CASCADE) removes its translations as well.
// ─────────────────────────────────────────────────────────────────────────────

// incidentUpdateTranslationView is the admin view of a translation.
type incidentUpdateTranslationView struct {
	LanguageCode string `json:"LanguageCode"`
	Message      string `json:"Message"`
	Status       string `json:"Status"`
	Source       string `json:"Source"`
}

// loadUpdateForTranslation determines the update including its incident from
// :page_id, :id and :update_id. It responds itself on errors (400 invalid ID,
// 404 unknown or foreign page, 403 without tag access) and then returns false.
func (h *Handler) loadUpdateForTranslation(c *gin.Context) (models.Incident, models.IncidentUpdate, bool) {
	var inc models.Incident
	var upd models.IncidentUpdate
	pageID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page_id", "code": "invalid_id"})
		return inc, upd, false
	}
	incID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errInvalidIncidentID, "code": "invalid_id"})
		return inc, upd, false
	}
	updID, err := strconv.ParseUint(c.Param("update_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid update ID", "code": "invalid_id"})
		return inc, upd, false
	}
	if err := h.DB.Where(whereIDAndStatusPageID, incID, pageID).First(&inc).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errIncidentNotFound, "code": "incident_not_found"})
		return inc, upd, false
	}
	if !h.userHasTagAccess(c, h.tagsForIncident(inc.ID)) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Forbidden for this incident", "code": "forbidden"})
		return inc, upd, false
	}
	if err := h.DB.Where("id = ? AND incident_id = ?", updID, inc.ID).First(&upd).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Update not found", "code": "update_not_found"})
		return inc, upd, false
	}
	return inc, upd, true
}

// GetIncidentUpdateTranslationsHandler returns all translations of an update
// including drafts (admin).
func (h *Handler) GetIncidentUpdateTranslationsHandler(c *gin.Context) {
	_, upd, ok := h.loadUpdateForTranslation(c)
	if !ok {
		return
	}
	var rows []models.IncidentUpdateTranslation
	if err := h.DB.Where("update_id = ? AND language_code IN ?", upd.ID, h.deliveredTranslationCodes()).Order("language_code").Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}
	out := make([]incidentUpdateTranslationView, 0, len(rows))
	for _, r := range rows {
		out = append(out, incidentUpdateTranslationView{LanguageCode: r.LanguageCode, Message: r.Message, Status: r.Status, Source: r.Source})
	}
	c.JSON(http.StatusOK, out)
}

// SaveIncidentUpdateTranslationHandler saves the translation of an update
// for exactly one language: create or change, state draft or confirmed.
// The default language of the page is the update itself and has no translation.
func (h *Handler) SaveIncidentUpdateTranslationHandler(c *gin.Context) {
	inc, upd, ok := h.loadUpdateForTranslation(c)
	if !ok {
		return
	}
	var req struct {
		Message string `json:"message" binding:"required"`
		Status  string `json:"status" binding:"required,oneof=draft confirmed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "code": "invalid_request"})
		return
	}
	if !requireMessageLength(c, req.Message) {
		return
	}
	code := strings.ToLower(strings.TrimSpace(c.Param("language_code")))
	if !h.requireAdditionalLanguage(c, inc.StatusPageID, code) {
		return
	}

	var trans models.IncidentUpdateTranslation
	err := h.DB.Where("update_id = ? AND language_code = ?", upd.ID, code).First(&trans).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}
	trans.UpdateID = upd.ID
	trans.LanguageCode = code
	trans.Message = req.Message
	trans.Status = req.Status
	if trans.Source == "" {
		trans.Source = "manual"
	}
	if err := h.DB.Save(&trans).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save translation", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, incidentUpdateTranslationView{LanguageCode: trans.LanguageCode, Message: trans.Message, Status: trans.Status, Source: trans.Source})
}

// publicUpdateTranslationsResponse: per update ID (as a string) the
// message in the requested language, otherwise in the default language. Updates without a
// confirmed translation are missing - the frontend then shows the original.
type publicUpdateTranslationsResponse struct {
	Language        string            `json:"language"`
	DefaultLanguage string            `json:"default_language"`
	Updates         map[string]string `json:"updates"`
}

// GetPublicIncidentUpdateTranslationsHandler returns the confirmed
// translations of an incident's updates for the status page. Access like the
// dashboard (authorizePublicPageRead); the incident must belong to the page.
// Language selection like GetPublicTranslationsHandler (empty lang = default language).
func (h *Handler) GetPublicIncidentUpdateTranslationsHandler(c *gin.Context) {
	var page models.StatusPage
	if err := h.DB.Where(slugWhereClause, c.Param("slug")).First(&page).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Status page not found", "code": "page_not_found"})
		return
	}
	if !h.authorizePublicPageRead(c, page) {
		return
	}
	incID, err := strconv.ParseUint(c.Param("incident_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errInvalidIncidentID, "code": "invalid_id"})
		return
	}
	var inc models.Incident
	if err := h.DB.Select("id").Where(whereIDAndStatusPageID, incID, page.ID).First(&inc).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": errIncidentNotFound, "code": "incident_not_found"})
		return
	}

	lang, def, ok := h.resolvePublicLanguage(c, page)
	if !ok {
		return
	}

	resp := publicUpdateTranslationsResponse{Language: lang, DefaultLanguage: def, Updates: map[string]string{}}
	wanted := []string{lang}
	if def != "" && def != lang {
		wanted = append(wanted, def)
	}
	var rows []models.IncidentUpdateTranslation
	if err := h.DB.Scopes(i18n.ScopeConfirmedOnly).
		Where("update_id IN (?) AND language_code IN ?", h.DB.Model(&models.IncidentUpdate{}).Select("id").Where(whereIncidentIDEquals, inc.ID), wanted).
		Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}
	byID := map[uint]map[string]publicTitleText{}
	ids := make([]uint, 0, len(rows))
	for _, r := range rows {
		if byID[r.UpdateID] == nil {
			ids = append(ids, r.UpdateID)
		}
		addTranslation(byID, r.UpdateID, r.LanguageCode, publicTitleText{Text: r.Message})
	}
	for _, id := range ids {
		if t, ok := pickTranslation(byID, id, lang, def); ok {
			resp.Updates[strconv.FormatUint(uint64(id), 10)] = t.Text
		}
	}
	c.JSON(http.StatusOK, resp)
}
