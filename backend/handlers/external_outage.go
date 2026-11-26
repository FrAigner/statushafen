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

// Notification on a permanent outage of an external
// source. The polling service (externalstatus) detects the transitions and calls
// SourceOutage; recipients, mail text and delivery live here - the
// service must not import handlers, the central delivery function
// deliverMail lives here, though.

const (
	auditActionOutageEmailSent    = "external_source.outage_email_sent"
	auditActionOutageEmailFailed  = "external_source.outage_email_failed"
	auditActionOutageEmailSkipped = "external_source.outage_email_skipped"
	permComponentsWrite           = "external_sources:update"
	// adminExternalSourcesPath: target of the link in the
	// outage mail, the admin page "Externe Quellen" (route in the frontend).
	adminExternalSourcesPath = "/admin/external-sources"
)

// externalOutageItem: one active outage for the admin banner (part C).
type externalOutageItem struct {
	SourceID      uint       `json:"source_id"`
	SourceName    string     `json:"source_name"`
	BaseURL       string     `json:"base_url"`
	Since         *time.Time `json:"since"`
	LastError     string     `json:"last_error"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	NotifiedAt    *time.Time `json:"notified_at"`
}

// GET /api/pages/:page_id/external-outages (components:read) - all active
// outages of the status page (active sources only), oldest first.
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

// SourceOutage implements externalstatus.OutageNotifier. Delivery runs
// in its own goroutine so a slow SMTP server does not hold up the polling cycle;
// WaitOutageMails waits for it (tests, shutdown).
func (h *Handler) SourceOutage(_ context.Context, ev externalstatus.OutageEvent) {
	h.notifyOutageAdmins(ev)
	h.outageMails.Add(1)
	go func() {
		defer h.outageMails.Done()
		h.sendOutageMails(ev)
	}()
}

// notifyOutageAdmins creates the message for the bell,
// in addition to banner and mail. Outage and reminder share one
// key per source; the all-clear resolves the outage message and
// reports the source as reachable again. The source name is admin input
// and is shown by the frontend as text (never as HTML); URL and
// error text of the third-party site do not belong in it.
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

// WaitOutageMails blocks until all triggered outage mails have been processed.
func (h *Handler) WaitOutageMails() { h.outageMails.Wait() }

// outageRecipients: everyone with components:write on the status page plus Global
// Admins. Viewers (components:read only) get nothing.
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
	// sent=false without error: email globally disabled; deliverMail already wrote the
	// audit entry email.skipped_disabled.
}

// outageText holds the texts of a mail in one language.
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

// outageLabels are the language-dependent labels (the same for all kinds).
var outageLabels = map[string]outageText{
	"de": {since: "Keine Daten seit", errLabel: "Letzter Fehler", noError: "–", link: "Externe Quellen öffnen", page: "Statusseite"},
	"en": {since: "No data since", errLabel: "Last error", noError: "-", link: "Open external sources", page: "Status page"},
}

// outageMailLang chooses German or English by the recipient's
// preferred_language; German if unspecified.
func outageMailLang(preferred string) string {
	if strings.HasPrefix(strings.ToLower(preferred), "en") {
		return "en"
	}
	return "de"
}

// buildOutageMail returns subject and HTML text. Source name and error come
// from admin input or the third-party site and are HTML-escaped in the text; the
// subject stays a header (CR/LF removed, rest checked by hasHeaderInjectionChars).
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
	// Mail text without line breaks: hasHeaderInjectionChars checks the finished body.
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

// oneLine replaces line breaks with spaces: source name and error text
// (third-party site!) end up in subject and mail text, which must not contain CR/LF.
func oneLine(s string) string { return lineBreaks.Replace(s) }

var lineBreaks = strings.NewReplacer("\r", " ", "\n", " ")

// formatOutageTime formats in UTC with a zone label: unambiguous and without
// dependence on the server's time zone database.
func formatOutageTime(t time.Time, lang string) string {
	if lang == "en" {
		return t.UTC().Format("2006-01-02 15:04 MST")
	}
	return t.UTC().Format("02.01.2006 15:04 MST")
}
