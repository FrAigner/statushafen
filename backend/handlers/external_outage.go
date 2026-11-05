package handlers

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"mime"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// Meldung bei dauerhaftem Ausfall einer externen
// Quelle. Der Abfrage-Dienst (externalstatus) erkennt die Übergänge und ruft
// SourceOutage; hier liegen Empfängerkreis, Mailtext und Versand - der
// Dienst darf handlers nicht importieren, die zentrale Versandfunktion
// deliverMail liegt aber hier.

const (
	auditActionOutageEmailSent    = "external_source.outage_email_sent"
	auditActionOutageEmailFailed  = "external_source.outage_email_failed"
	auditActionOutageEmailSkipped = "external_source.outage_email_skipped"
	permComponentsWrite           = "external_sources:update"
	// adminExternalSourcesPath: Ziel des Links in der
	// Ausfall-Mail, die Admin-Seite „Externe Quellen“ (Route im Frontend).
	adminExternalSourcesPath = "/admin/external-sources"
)

// externalOutageItem: ein aktiver Ausfall für das Admin-Banner (Teil C).
type externalOutageItem struct {
	SourceID      uint       `json:"source_id"`
	SourceName    string     `json:"source_name"`
	BaseURL       string     `json:"base_url"`
	Since         *time.Time `json:"since"`
	LastError     string     `json:"last_error"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	NotifiedAt    *time.Time `json:"notified_at"`
}

// GET /api/pages/:page_id/external-outages (components:read) - alle aktiven
// Ausfälle der Statusseite (nur aktive Quellen), älteste zuerst.
func (h *Handler) GetExternalOutagesHandler(c *gin.Context) {
	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}
	var sources []models.ExternalSource
	if err := h.DB.Where("status_page_id = ? AND active = ? AND outage_since IS NOT NULL", tenantID, true).
		Order("outage_since asc, id asc").Find(&sources).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch external outages", "code": "server_error"})
		return
	}
	items := make([]externalOutageItem, 0, len(sources))
	for _, src := range sources {
		items = append(items, externalOutageItem{
			SourceID: src.ID, SourceName: src.Name, BaseURL: src.BaseURL, Since: src.OutageSince,
			LastError: src.LastError, LastSuccessAt: src.LastSuccessAt, NotifiedAt: src.OutageNotifiedAt,
		})
	}
	c.JSON(http.StatusOK, items)
}

// SourceOutage implementiert externalstatus.OutageNotifier. Der Versand läuft
// in einer eigenen Goroutine, damit ein langsamer SMTP-Server den Abruf-Takt
// nicht aufhält; WaitOutageMails wartet darauf (Tests, Herunterfahren).
func (h *Handler) SourceOutage(_ context.Context, ev externalstatus.OutageEvent) {
	h.notifyOutageAdmins(ev)
	h.outageMails.Add(1)
	go func() {
		defer h.outageMails.Done()
		h.sendOutageMails(ev)
	}()
}

// notifyOutageAdmins legt die Nachricht für die Glocke an,
// zusätzlich zu Banner und Mail. Ausfall und Erinnerung teilen sich einen
// Schlüssel je Quelle; die Entwarnung erledigt die Ausfall-Nachricht und
// meldet die Quelle als wieder erreichbar. Der Quellenname ist Admin-Eingabe
// und wird vom Frontend als Text (nie als HTML) angezeigt; URL und
// Fehlertext der Fremdseite gehören nicht hinein.
func (h *Handler) notifyOutageAdmins(ev externalstatus.OutageEvent) {
	src := ev.Source
	pageID := src.StatusPageID
	key := "external_source_outage:" + strconv.FormatUint(uint64(src.ID), 10)
	params := map[string]any{"source": oneLine(src.Name), "source_id": src.ID}
	if ev.Kind == externalstatus.OutageRecovered {
		h.ResolveAdminNotifications(&pageID, key)
		h.NotifyAdmins(&pageID, AdminNotifExternalSourceRecovered, models.AdminNotificationInfo,
			"adminNotifications.external_source_recovered.title", "adminNotifications.external_source_recovered.body",
			params, adminExternalSourcesPath, "external_source_recovered:"+strconv.FormatUint(uint64(src.ID), 10))
		return
	}
	h.NotifyAdmins(&pageID, AdminNotifExternalSourceOutage, models.AdminNotificationError,
		"adminNotifications.external_source_outage.title", "adminNotifications.external_source_outage.body",
		params, adminExternalSourcesPath, key)
}

// WaitOutageMails blockiert, bis alle angestoßenen Ausfall-Mails verarbeitet sind.
func (h *Handler) WaitOutageMails() { h.outageMails.Wait() }

// outageRecipients: alle mit components:write auf der Statusseite plus Global
// Admins. Betrachter (nur components:read) bekommen nichts.
func (h *Handler) outageRecipients(pageID uint) []models.User {
	var roles []models.UserStatusPageRole
	h.DB.Preload("Role").Where(whereStatusPageIDEquals, pageID).Find(&roles)
	ids := make([]uint, 0, len(roles))
	for _, r := range roles {
		if permissionListContains(r.Role.Permissions, permComponentsWrite) {
			ids = append(ids, r.UserID)
		}
	}
	var users []models.User
	h.DB.Where("is_global_admin = ? OR id IN ?", true, ids).Order("id").Find(&users)
	return users
}

func (h *Handler) sendOutageMails(ev externalstatus.OutageEvent) {
	src := ev.Source
	var page models.StatusPage
	h.DB.First(&page, src.StatusPageID)
	cfg := loadSMTPConfig()
	cfg.applyPageSender(page)
	srcID := strconv.FormatUint(uint64(src.ID), 10)
	target := mailAuditTarget{Category: "config", TargetType: auditTargetExternalSource, TargetID: srcID, TargetName: src.Name}

	users := h.outageRecipients(src.StatusPageID)
	if len(users) == 0 {
		h.RecordSystemAudit(auditActionOutageEmailSkipped, target.Category, target.TargetType, srcID, src.Name,
			"kind="+string(ev.Kind)+" reason=no_recipients")
		return
	}
	for _, u := range users {
		h.sendOutageMail(cfg, u, ev, page, target)
	}
}

func (h *Handler) sendOutageMail(cfg smtpConfig, u models.User, ev externalstatus.OutageEvent, page models.StatusPage, target mailAuditTarget) {
	addr, err := mail.ParseAddress(u.Email)
	if err != nil {
		slog.Error("Ausfall-Mail übersprungen: ungültige Empfängeradresse", "email", u.Email, "error", err)
		return
	}
	recipient := addr.Address
	subject, body := buildOutageMail(ev, page, u.PreferredLanguage)
	if hasHeaderInjectionChars(subject) || hasHeaderInjectionChars(body) || hasHeaderInjectionChars(cfg.From) {
		slog.Error("Ausfall-Mail übersprungen: Betreff/Text/Absender enthalten CR/LF", "email", recipient)
		return
	}
	msg := cfg.fromHeaders() +
		"To: " + recipient + "\n" +
		"Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\n" +
		"MIME-Version: 1.0\nContent-Type: text/html; charset=UTF-8\n\n" + body

	sent, err := h.deliverMail(cfg, recipient, []byte(msg), target)
	details := fmt.Sprintf("kind=%s recipient=%s", ev.Kind, recipient)
	switch {
	case err != nil:
		slog.Error("Ausfall-Mail nicht gesendet", "email", recipient, "smtp_addr", cfg.addr(), "error", err)
		h.RecordSystemAudit(auditActionOutageEmailFailed, target.Category, target.TargetType, target.TargetID, target.TargetName,
			details+" error="+err.Error())
	case sent:
		h.RecordSystemAudit(auditActionOutageEmailSent, target.Category, target.TargetType, target.TargetID, target.TargetName, details)
	}
	// sent=false ohne Fehler: E-Mail global abgeschaltet; deliverMail hat den
	// Audit-Eintrag email.skipped_disabled schon geschrieben.
}

// outageText sind die Texte einer Mail in einer Sprache.
type outageText struct {
	subject, heading, intro, since, errLabel, noError, link, page string
}

var outageTexts = map[string]map[externalstatus.OutageKind]outageText{
	"de": {
		externalstatus.OutageStarted: {
			subject: "Externe Quelle „%s“ nicht erreichbar",
			heading: "Externe Quelle nicht erreichbar",
			intro:   "Die externe Statusquelle <b>%s</b> (%s) liefert seit längerer Zeit keine Daten mehr. Ihr zuletzt bekannter Status zählt solange nicht mehr in den Status deiner Komponenten.",
		},
		externalstatus.OutageReminder: {
			subject: "Erinnerung: Externe Quelle „%s“ weiterhin nicht erreichbar",
			heading: "Externe Quelle weiterhin nicht erreichbar",
			intro:   "Die externe Statusquelle <b>%s</b> (%s) ist seit über 24 Stunden nicht erreichbar. Ihr zuletzt bekannter Status zählt weiterhin nicht in den Status deiner Komponenten.",
		},
		externalstatus.OutageRecovered: {
			subject: "Externe Quelle „%s“ wieder erreichbar",
			heading: "Externe Quelle wieder erreichbar",
			intro:   "Die externe Statusquelle <b>%s</b> (%s) liefert wieder Daten. Ihr aktueller Status zählt wieder in den Status deiner Komponenten.",
		},
	},
	"en": {
		externalstatus.OutageStarted: {
			subject: "External source \"%s\" unreachable",
			heading: "External source unreachable",
			intro:   "The external status source <b>%s</b> (%s) has not delivered data for a while. Its last known status no longer counts towards your components' status until it recovers.",
		},
		externalstatus.OutageReminder: {
			subject: "Reminder: external source \"%s\" still unreachable",
			heading: "External source still unreachable",
			intro:   "The external status source <b>%s</b> (%s) has been unreachable for over 24 hours. Its last known status still does not count towards your components' status.",
		},
		externalstatus.OutageRecovered: {
			subject: "External source \"%s\" reachable again",
			heading: "External source reachable again",
			intro:   "The external status source <b>%s</b> (%s) delivers data again. Its current status counts towards your components' status again.",
		},
	},
}

// outageLabels sind die sprachabhängigen Beschriftungen (für alle Arten gleich).
var outageLabels = map[string]outageText{
	"de": {since: "Keine Daten seit", errLabel: "Letzter Fehler", noError: "–", link: "Externe Quellen öffnen", page: "Statusseite"},
	"en": {since: "No data since", errLabel: "Last error", noError: "-", link: "Open external sources", page: "Status page"},
}

// outageMailLang wählt Deutsch oder Englisch nach preferred_language des
// Empfängers; ohne Angabe Deutsch.
func outageMailLang(preferred string) string {
	if strings.HasPrefix(strings.ToLower(preferred), "en") {
		return "en"
	}
	return "de"
}

// buildOutageMail liefert Betreff und HTML-Text. Quellenname und Fehler stammen
// aus Admin-Eingabe bzw. der Fremdseite und werden im Text HTML-escaped; der
// Betreff bleibt ein Header (CR/LF entfernt, Rest prüft hasHeaderInjectionChars).
func buildOutageMail(ev externalstatus.OutageEvent, page models.StatusPage, preferred string) (string, string) {
	lang := outageMailLang(preferred)
	t := outageTexts[lang][ev.Kind]
	l := outageLabels[lang]
	src := ev.Source

	subject := fmt.Sprintf(t.subject, oneLine(src.Name))
	since := "-"
	if src.OutageSince != nil {
		since = formatOutageTime(*src.OutageSince, lang)
	}
	lastErr := l.noError
	if src.LastError != "" {
		lastErr = html.EscapeString(oneLine(src.LastError))
	}
	// Mailtext ohne Zeilenumbrüche: hasHeaderInjectionChars prüft den fertigen Body.
	body := fmt.Sprintf(`<div style="font-family:sans-serif;max-width:560px">`+
		`<h2>%s</h2><p>%s</p>`+
		`<table style="border-collapse:collapse">`+
		`<tr><td style="padding:2px 12px 2px 0;color:#666">%s</td><td>%s</td></tr>`+
		`<tr><td style="padding:2px 12px 2px 0;color:#666">%s</td><td>%s</td></tr>`+
		`<tr><td style="padding:2px 12px 2px 0;color:#666">%s</td><td>%s</td></tr>`+
		`</table><p><a href="%s">%s</a></p></div>`,
		t.heading, fmt.Sprintf(t.intro, html.EscapeString(oneLine(src.Name)), html.EscapeString(oneLine(src.BaseURL))),
		l.page, html.EscapeString(oneLine(page.Name)),
		l.since, since,
		l.errLabel, lastErr,
		html.EscapeString(getFrontendURL()+adminExternalSourcesPath), l.link)
	return subject, body
}

// oneLine ersetzt Zeilenumbrüche durch Leerzeichen: Quellenname und Fehlertext
// (Fremdseite!) landen in Betreff und Mailtext, der keine CR/LF enthalten darf.
func oneLine(s string) string { return lineBreaks.Replace(s) }

var lineBreaks = strings.NewReplacer("\r", " ", "\n", " ")

// formatOutageTime formatiert in UTC mit Zonenangabe: eindeutig und ohne
// Abhängigkeit von der Zeitzonen-Datenbank des Servers.
func formatOutageTime(t time.Time, lang string) string {
	if lang == "en" {
		return t.UTC().Format("2006-01-02 15:04 MST")
	}
	return t.UTC().Format("02.01.2006 15:04 MST")
}
