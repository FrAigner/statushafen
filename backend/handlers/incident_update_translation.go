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
// Übersetzungen der Updates eines Vorfalls (Timeline-Einträge).
//
// Ein Update hat nur eine Nachricht, keinen Titel. Das Muster folgt den
// Vorfall-Übersetzungen: je Sprache ein PUT mit Stand draft oder
// confirmed; Entwürfe sieht nur der Admin, die öffentliche Auslieferung nimmt
// ausschließlich bestätigte Texte.
//
// Routen (main.go):
//
//	admin:      GET /pages/:page_id/incidents/:id/updates/:update_id/translations
//	            PUT /pages/:page_id/incidents/:id/updates/:update_id/translations/:language_code
//	öffentlich: GET /api/public/pages/:slug/incidents/:incident_id/update-translations?lang=xx
//
// Die Admin-Routen tragen :page_id und :id im Pfad; die Rechte folgen dem
// Vorfall (incidents bzw. maintenance, read bzw. update), die Seite wird wie bei
// "Update posten" über die Seiten-ID geprüft. Löscht man das Update, entfernt
// der Fremdschlüssel (OnDelete:CASCADE) seine Übersetzungen mit.
// ─────────────────────────────────────────────────────────────────────────────

// incidentUpdateTranslationView ist die Admin-Sicht auf eine Übersetzung.
type incidentUpdateTranslationView struct {
	LanguageCode string `json:"LanguageCode"`
	Message      string `json:"Message"`
	Status       string `json:"Status"`
	Source       string `json:"Source"`
}

// loadUpdateForTranslation ermittelt aus :page_id, :id und :update_id das
// Update samt Vorfall. Sie antwortet selbst bei Fehlern (400 ungültige ID,
// 404 unbekannt oder fremde Seite, 403 ohne Tag-Zugriff) und liefert dann false.
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

// GetIncidentUpdateTranslationsHandler liefert alle Übersetzungen eines Updates
// inklusive Entwürfen (Admin).
func (h *Handler) GetIncidentUpdateTranslationsHandler(c *gin.Context) {
	_, upd, ok := h.loadUpdateForTranslation(c)
	if !ok {
		return
	}
	var rows []models.IncidentUpdateTranslation
	if err := h.DB.Where("update_id = ?", upd.ID).Order("language_code").Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error", "code": "server_error"})
		return
	}
	out := make([]incidentUpdateTranslationView, 0, len(rows))
	for _, r := range rows {
		out = append(out, incidentUpdateTranslationView{LanguageCode: r.LanguageCode, Message: r.Message, Status: r.Status, Source: r.Source})
	}
	c.JSON(http.StatusOK, out)
}

// SaveIncidentUpdateTranslationHandler speichert die Übersetzung eines Updates
// für genau eine Sprache: anlegen oder ändern, Stand draft oder confirmed.
// Die Standardsprache der Seite ist das Update selbst und hat keine Übersetzung.
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

// publicUpdateTranslationsResponse: je Update-ID (als Zeichenkette) die
// Nachricht in der gewünschten Sprache, sonst der Standardsprache. Updates ohne
// bestätigte Übersetzung fehlen - das Frontend zeigt dann das Original.
type publicUpdateTranslationsResponse struct {
	Language        string            `json:"language"`
	DefaultLanguage string            `json:"default_language"`
	Updates         map[string]string `json:"updates"`
}

// GetPublicIncidentUpdateTranslationsHandler liefert die bestätigten
// Übersetzungen der Updates eines Vorfalls für die Statusseite. Zugriff wie das
// Dashboard (authorizePublicPageRead); der Vorfall muss zur Seite gehören.
// Sprachwahl wie GetPublicTranslationsHandler (lang leer = Standardsprache).
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
