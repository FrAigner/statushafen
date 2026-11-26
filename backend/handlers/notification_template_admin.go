package handlers

import (
	"errors"
	htmltemplate "html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/richtext"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Admin CRUD for the NotificationTemplate. Page-scoped
// under /pages/:page_id/notification-templates, the same pages:write
// permission as the status page settings themselves - templates are
// a configuration option of the page and logically located there, not
// with incidents (which have incidents:write).
//
// The data model is deliberately upsert-based: the key is (StatusPageID,
// Kind, Audience, Language), and the UI does not manage a "which template
// has which ID" list, but for a known quadruple writes directly
// PUT and creates/updates - DELETE with the same quadruple resets
// to the hard-coded default (no custom template for it anymore).

// Repeated literals in sampleNotificationTemplateData (go:S1192).
const (
	sampleMaintenanceTitle = "Scheduled DB maintenance"
	sampleMaintenanceComps = "Database Cluster"
)

// validNotificationKinds are the currently editable kind values.
// Incident kinds are the original ones, maintenance kinds were
// added later on.
// General notice kinds are deliberately missing - see the comment in
// notification_template.go at the constants (notices currently send no mails).
var validNotificationKinds = map[string]bool{
	NotificationKindIncidentCreated:      true,
	NotificationKindIncidentUpdated:      true,
	NotificationKindIncidentResolved:     true,
	NotificationKindMaintenanceReminder:  true,
	NotificationKindMaintenanceStarted:   true,
	NotificationKindMaintenanceCompleted: true,
	NotificationKindNoticeCreated:        true,
	NotificationKindNoticeUpdated:        true,
	NotificationKindNoticeEnded:          true,
	NotificationKindSubscriptionConfirm:  true,
	// Reports from external sources.
	NotificationKindExternalIncidentCreated:     true,
	NotificationKindExternalIncidentUpdated:     true,
	NotificationKindExternalIncidentResolved:    true,
	NotificationKindExternalMaintenanceCreated:  true,
	NotificationKindExternalMaintenanceUpdated:  true,
	NotificationKindExternalMaintenanceResolved: true,
}

// validNotificationAudiences are the two target audiences
// (stage 5 of multilingual support) - see NotificationAudienceStaff/
// NotificationAudienceSubscriber in notification_template.go.
var validNotificationAudiences = map[string]bool{
	NotificationAudienceStaff:      true,
	NotificationAudienceSubscriber: true,
}

// isValidNotificationLanguage checks (stage 5 of multilingual support)
// the language parameter against the real language catalog configured per status page
// (StatusPageLanguage) instead of against the
// previous hard-coded en/de convention. This way a page that has
// activated e.g. "fr" no longer collides with the old two-language limit.
//
// If a status page has NO StatusPageLanguage row at all (existing
// pages from before the language catalog migration, or test
// pages created directly in the DB), the check falls back to exactly the previous convention (en/de)
// - this preserves exact existing behavior until a
// real language catalog exists (see database.backfillStatusPageLanguages /
// database.SeedDefaultStatusPageLanguages, which backfill exactly that for every real
// page).
func isValidNotificationLanguage(db *gorm.DB, pageID uint, lang string) bool {
	// Content languages are only de and en.
	if !database.IsContentLanguage(lang) {
		return false
	}
	var configuredCount int64
	if err := db.Model(&models.StatusPageLanguage{}).Where("status_page_id = ?", pageID).Count(&configuredCount).Error; err != nil {
		return lang == "en" || lang == "de"
	}
	if configuredCount == 0 {
		return lang == "en" || lang == "de"
	}
	var enabledCount int64
	db.Model(&models.StatusPageLanguage{}).Where("status_page_id = ? AND language_code = ?", pageID, lang).Count(&enabledCount)
	return enabledCount > 0
}

// notificationTemplateUpsertReq bundles the (deliberately lean) payload for
// create/update - all other fields are derived from the URL parameters
// and, if taken over from the request, would only be a source of
// confusion.
type notificationTemplateUpsertReq struct {
	Subject  string `json:"Subject" binding:"required"`
	BodyHTML string `json:"BodyHTML" binding:"required"`
}

func parseNotificationTemplateKey(c *gin.Context, db *gorm.DB) (pageID uint, kind, audience, language string, ok bool) {
	tenantID, ok := parsePageIDParam(c)
	if !ok {
		return 0, "", "", "", false
	}
	kind = c.Param("kind")
	audience = c.Param("audience")
	language = c.Param("language")
	if !validNotificationKinds[kind] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Kind-Angabe", "code": "invalid_request"})
		return 0, "", "", "", false
	}
	if !validNotificationAudiences[audience] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Audience-Angabe", "code": "invalid_request"})
		return 0, "", "", "", false
	}
	if !isValidNotificationLanguage(db, uint(tenantID), language) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Sprach-Angabe", "code": "invalid_request"})
		return 0, "", "", "", false
	}
	return uint(tenantID), kind, audience, language, true
}

// GetNotificationTemplatesHandler returns all custom templates of a
// status page. The UI builds the editor matrix (kind x audience x
// language) from them; missing entries = "rendered by default".
func (h *Handler) GetNotificationTemplatesHandler(c *gin.Context) {
	tenantID, ok := parsePageIDParam(c)
	if !ok {
		return
	}
	var templates []models.NotificationTemplate
	q := h.DB.Where("status_page_id = ?", tenantID)
	// Switch off: only templates in the page default language are shown.
	if !h.translationEnabled() {
		q = q.Where("language = ?", h.pageDefaultLanguageCode(uint(tenantID)))
	}
	if err := q.Order("kind asc, audience asc, language asc").Find(&templates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch notification templates", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, templates)
}

// PutNotificationTemplateHandler is an upsert: if no entry exists for (pageID, kind,
// audience, language), one is created, otherwise it is
// updated. This way the UI does not have to track any ID state.
func (h *Handler) PutNotificationTemplateHandler(c *gin.Context) {
	pageID, kind, audience, language, ok := parseNotificationTemplateKey(c, h.DB)
	if !ok || !h.requireTemplateLanguageEnabled(c, pageID, language) {
		return
	}
	var req notificationTemplateUpsertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Felder", "code": "invalid_request"})
		return
	}
	if err := ValidateNotificationTemplateForKind(kind, req.Subject, req.BodyHTML); err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}

	var tmpl models.NotificationTemplate
	err := h.DB.Where("status_page_id = ? AND kind = ? AND audience = ? AND language = ?", pageID, kind, audience, language).First(&tmpl).Error
	created := false
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification template", "code": "server_error"})
			return
		}
		tmpl = models.NotificationTemplate{StatusPageID: pageID, Kind: kind, Audience: audience, Language: language}
		created = true
	}
	tmpl.Subject = req.Subject
	tmpl.BodyHTML = req.BodyHTML
	if err := h.DB.Save(&tmpl).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save notification template", "code": "server_error"})
		return
	}
	auditAction := "notification_template.updated"
	if created {
		auditAction = "notification_template.created"
	}
	h.RecordAudit(c, auditAction, "config", "NotificationTemplate", strconv.FormatUint(uint64(tmpl.ID), 10), kind+"/"+audience+"/"+language, "")
	c.JSON(http.StatusOK, tmpl)
}

// DeleteNotificationTemplateHandler removes a custom template - the
// status page then falls back to the hard-coded default for this (kind, audience, language)
// again. 404 if none exists (idempotency-
// unfriendly, but more informative than a silent 200).
func (h *Handler) DeleteNotificationTemplateHandler(c *gin.Context) {
	pageID, kind, audience, language, ok := parseNotificationTemplateKey(c, h.DB)
	if !ok || !h.requireTemplateLanguageEnabled(c, pageID, language) {
		return
	}
	var tmpl models.NotificationTemplate
	if err := h.DB.Where("status_page_id = ? AND kind = ? AND audience = ? AND language = ?", pageID, kind, audience, language).First(&tmpl).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Notification template not found", "code": "template_not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load notification template", "code": "server_error"})
		return
	}
	if err := h.DB.Delete(&tmpl).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete notification template", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "notification_template.deleted", "config", "NotificationTemplate", strconv.FormatUint(uint64(tmpl.ID), 10), kind+"/"+audience+"/"+language, "")
	c.JSON(http.StatusOK, gin.H{"message": "Notification template deleted"})
}

// PreviewNotificationTemplateHandler renders the given subject/body
// with realistic sample values, so that the admin can see before saving
// what a real mail looks like. Stores nothing.
func (h *Handler) PreviewNotificationTemplateHandler(c *gin.Context) {
	_, kind, _, language, ok := parseNotificationTemplateKey(c, h.DB)
	if !ok {
		return
	}
	var req notificationTemplateUpsertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Felder", "code": "invalid_request"})
		return
	}

	if err := ValidateNotificationTemplateForKind(kind, req.Subject, req.BodyHTML); err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}
	sample := models.NotificationTemplate{Subject: req.Subject, BodyHTML: req.BodyHTML, Kind: kind, Language: language}
	subject, body, err := renderNotificationTemplate(&sample, sampleNotificationTemplateData(kind, language))
	if err != nil {
		// Parse/execution error of the template: the text helps with fixing it.
		if TemplateErrorCode(err) == "" {
			err = newCodedError("template_invalid", err.Error())
		}
		respondCoded(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"subject": subject, "bodyHTML": body})
}

// sampleNotificationTemplateData returns representative sample values for
// the preview. Deliberately non-generic values ("Zahlungs-Gateway"),
// so that the admin immediately sees how a placeholder with real text
// looks, not with "TITLE"/"MESSAGE" dummies.
func sampleNotificationTemplateData(kind, language string) NotificationTemplateData {
	action := "created"
	statusRaw := "investigating"
	title := "Payment gateway degraded"
	message := "We are investigating elevated error rates on the payment API."
	compNames := "Payment API, Billing UI"
	switch kind {
	case NotificationKindIncidentUpdated:
		action = "updated"
		statusRaw = "identified"
		message = "The root cause has been identified. Working on rollback."
	case NotificationKindIncidentResolved:
		action = "updated"
		statusRaw = "resolved"
		message = "The incident has been resolved. All systems are back to normal."
	case NotificationKindMaintenanceReminder:
		action = "maintenance_reminder"
		statusRaw = "scheduled"
		title = sampleMaintenanceTitle
		message = "This is a reminder that a scheduled maintenance window starts in 30 minutes."
		compNames = sampleMaintenanceComps
	case NotificationKindMaintenanceStarted:
		action = "maintenance_started"
		statusRaw = "in_progress"
		title = sampleMaintenanceTitle
		message = "The scheduled maintenance window has started. Some services may be briefly unavailable."
		compNames = sampleMaintenanceComps
	case NotificationKindMaintenanceCompleted:
		action = "maintenance_completed"
		statusRaw = "resolved"
		title = sampleMaintenanceTitle
		message = "The maintenance window is complete and all services are back to normal."
		compNames = sampleMaintenanceComps
	case NotificationKindNoticeCreated:
		action = "created"
		statusRaw = ""
		title = "Neue Datenschutzrichtlinie ab 01.08."
		message = "Wir haben unsere Datenschutzrichtlinie überarbeitet. Bitte lies die neue Version durch."
		compNames = ""
	case NotificationKindNoticeUpdated:
		action = "updated"
		statusRaw = ""
		title = "Ankündigung: Neue Datenschutzrichtlinie"
		message = "Wir haben die Ankündigung mit weiteren Details aktualisiert."
		compNames = ""
	case NotificationKindNoticeEnded:
		action = "ended"
		statusRaw = ""
		title = "Ankündigung beendet"
		message = "Der Hinweis wird ab sofort nicht mehr auf der Statusseite angezeigt."
		compNames = ""
	}
	data := NotificationTemplateData{
		IncidentTitle:  title,
		Title:          title,
		Status:         statusRaw,
		StatusLabel:    statusLabelFor(statusRaw, language),
		ComponentNames: compNames,
		Tags:           "eu-west, production",
		Message:        message,
		MessageHTML:    htmltemplate.HTML(richtext.ToMailHTML(message)), // #nosec G203 -- Ergebnis von richtext.ToMailHTML
		Action:         action,
		Language:       language,
		FrontendURL:    "https://status.example.com/status/acme",
		UnsubscribeURL: "https://status.example.com/unsubscribe?token=SAMPLE_TOKEN",
		FromAddress:    "Statushafen <notifications@example.com>",
	}
	switch kind {
	case NotificationKindExternalIncidentCreated, NotificationKindExternalIncidentUpdated, NotificationKindExternalIncidentResolved,
		NotificationKindExternalMaintenanceCreated, NotificationKindExternalMaintenanceUpdated, NotificationKindExternalMaintenanceResolved:
		data.SourceName = "Example Cloud"
		data.SourceURL = "https://status.example.com/incidents/fx0000000001"
		data.Action = strings.TrimPrefix(strings.TrimPrefix(kind, "external_incident_"), "external_maintenance_")
	case NotificationKindNoticeCreated, NotificationKindNoticeUpdated, NotificationKindNoticeEnded:
		data.Level = "warning"
	case NotificationKindSubscriptionConfirm:
		// Title here is the name of the status page.
		data = NotificationTemplateData{
			Title:       "Acme Status",
			Language:    language,
			FrontendURL: "https://status.example.com/status/acme",
			ConfirmURL:  "https://status.example.com/subscribe/confirm?token=SAMPLE_TOKEN",
			FromAddress: data.FromAddress,
		}
	}
	return data
}
