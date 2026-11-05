package handlers

import (
	"errors"
	htmltemplate "html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/richtext"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Admin-CRUD für die NotificationTemplate. Seiten-scoped
// unter /pages/:page_id/notification-templates, dieselbe pages:write-
// Permission wie die Statusseiten-Einstellungen selbst - Templates sind
// eine Konfigurationsoption der Seite und logisch dort verortet, nicht
// bei Incidents (die haben incidents:write).
//
// Das Datenmodell ist bewusst upsert-basiert: der Key ist (StatusPageID,
// Kind, Audience, Language), und das UI verwaltet keine "welches Template
// hat welche ID"-Liste, sondern schreibt für ein bekanntes Quartett direkt
// PUT und erzeugt/aktualisiert - DELETE mit demselben Quartett setzt zurück
// auf den hartkodierten Default (kein Custom-Template mehr dafür).

// Wiederholte Literale in sampleNotificationTemplateData (go:S1192).
const (
	sampleMaintenanceTitle = "Scheduled DB maintenance"
	sampleMaintenanceComps = "Database Cluster"
)

// validNotificationKinds sind die derzeit editierbaren Kind-Werte.
// Incident-Kinds stammen, Maintenance-Kinds sind
// dazugekommen. General-Notice-Kinds fehlen bewusst - siehe Kommentar
// in notification_template.go bei den Konstanten (Notices versenden
// aktuell keine Mails).
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
	// Meldungen externer Quellen.
	NotificationKindExternalIncidentCreated:     true,
	NotificationKindExternalIncidentUpdated:     true,
	NotificationKindExternalIncidentResolved:    true,
	NotificationKindExternalMaintenanceCreated:  true,
	NotificationKindExternalMaintenanceUpdated:  true,
	NotificationKindExternalMaintenanceResolved: true,
}

// validNotificationAudiences sind die beiden Zielgruppen
// (Stage 5 der Mehrsprachigkeit) - siehe NotificationAudienceStaff/
// NotificationAudienceSubscriber in notification_template.go.
var validNotificationAudiences = map[string]bool{
	NotificationAudienceStaff:      true,
	NotificationAudienceSubscriber: true,
}

// isValidNotificationLanguage prüft (Stage 5 der Mehrsprachigkeit)
// den Language-Parameter gegen den echten, pro Statusseite konfigurierten
// Sprachkatalog (StatusPageLanguage) statt gegen die
// vorherige hartkodierte en/de-Konvention. Damit kollidiert eine Seite, die
// sich z.B. "fr" aktiviert hat, nicht mehr mit dem alten Zwei-Sprachen-Limit.
//
// Hat eine Statusseite noch KEINE einzige StatusPageLanguage-Zeile (Bestands-
// seiten vor der Migration der Sprachkataloge, oder direkt in der DB angelegte Test-
// Seiten), fällt die Prüfung auf genau die bisherige Konvention (en/de)
// zurück - das bewahrt exaktes Bestandsverhalten, bis für die Seite ein
// echter Sprachkatalog vorliegt (siehe database.backfillStatusPageLanguages /
// database.SeedDefaultStatusPageLanguages, die genau das für jede reale
// Seite nachziehen).
func isValidNotificationLanguage(db *gorm.DB, pageID uint, lang string) bool {
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

// notificationTemplateUpsertReq bündelt die (bewusst schlanke) Payload für
// Create/Update - alle anderen Felder sind aus den URL-Parametern
// abgeleitet und stünden bei ihrer Übernahme aus dem Request nur zur
// Verwechslung frei.
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

// GetNotificationTemplatesHandler liefert alle Custom-Templates einer
// Statusseite. Das UI baut sich daraus die Editor-Matrix (Kind × Audience ×
// Language) zusammen; fehlende Einträge = "wird per Default gerendert".
func (h *Handler) GetNotificationTemplatesHandler(c *gin.Context) {
	tenantID, ok := parsePageIDParam(c)
	if !ok {
		return
	}
	var templates []models.NotificationTemplate
	if err := h.DB.Where("status_page_id = ?", tenantID).Order("kind asc, audience asc, language asc").Find(&templates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch notification templates", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, templates)
}

// PutNotificationTemplateHandler ist upsert: existiert für (pageID, kind,
// audience, language) noch kein Eintrag, wird einer erzeugt, sonst
// aktualisiert. Das UI muss dadurch keinen ID-Zustand tracken.
func (h *Handler) PutNotificationTemplateHandler(c *gin.Context) {
	pageID, kind, audience, language, ok := parseNotificationTemplateKey(c, h.DB)
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

// DeleteNotificationTemplateHandler entfernt ein Custom-Template - die
// Statusseite fällt für dieses (Kind, Audience, Language) danach wieder auf
// den hartkodierten Default zurück. 404 wenn keins existiert (idempotent-
// unfreundlich, aber informativer als stiller 200).
func (h *Handler) DeleteNotificationTemplateHandler(c *gin.Context) {
	pageID, kind, audience, language, ok := parseNotificationTemplateKey(c, h.DB)
	if !ok {
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

// PreviewNotificationTemplateHandler rendert das übergebene Subject/Body
// mit realistischen Beispielwerten, damit der Admin vor dem Speichern
// sehen kann, wie eine echte Mail aussieht. Speichert nichts.
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
		// Parse-/Ausführungsfehler der Vorlage: der Text hilft beim Korrigieren.
		if TemplateErrorCode(err) == "" {
			err = newCodedError("template_invalid", err.Error())
		}
		respondCoded(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"subject": subject, "bodyHTML": body})
}

// sampleNotificationTemplateData liefert repräsentative Beispielwerte für
// die Vorschau. Bewusst nicht-generische Werte ("Zahlungs-Gateway"),
// damit der Admin gleich sieht, wie ein Platzhalter mit echtem Text
// wirkt, nicht mit "TITLE"/"MESSAGE"-Dummy.
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
		// Title ist hier der Name der Statusseite.
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
