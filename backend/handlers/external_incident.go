package handlers

import (
	"context"
	"errors"
	"fmt"
	"html"
	htmltemplate "html/template"
	"log/slog"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Teil B2: Vorfälle und Wartungen externer Quellen an
// Abonnenten der eigenen Statusseite melden und lesend ausliefern. Der
// Abfrage-Dienst (externalstatus) erkennt neue und geänderte Meldungen, die
// gekoppelte Komponenten betreffen, und ruft ExternalIncident; hier liegen
// Empfängerkreis, Mailtext und Versand (der Dienst darf handlers nicht
// importieren, deliverMail liegt hier).

const (
	auditActionIncidentMirrored = "external_incident.mirrored"
	auditActionIncidentNotified = "external_incident.notified"
	auditActionIncidentFailed   = "external_incident.failed"
	auditActionIncidentPruned   = "external_incident.pruned"
	auditTargetExternalIncident = "ExternalIncident"

	// Standardfenster, in dem beendete Meldungen noch gelistet werden.
	externalIncidentDefaultDays = 30
	externalIncidentMaxDays     = 365
	externalIncidentDefaultMax  = 50
	externalIncidentMaxLimit    = 200
	publicExternalIncidentDays  = 14
)

// ExternalIncident implementiert externalstatus.IncidentNotifier. Der Versand
// läuft in einer eigenen Goroutine, damit ein langsamer SMTP-Server den
// Abruf-Takt nicht aufhält; WaitIncidentMails wartet darauf (Tests).
func (h *Handler) ExternalIncident(_ context.Context, ev externalstatus.IncidentEvent) {
	h.incidentMails.Add(1)
	go func() {
		defer h.incidentMails.Done()
		h.dispatchExternalIncident(ev)
	}()
}

// MirroredIncidentsPruned implementiert externalstatus.RetentionReporter
// Audit-Eintrag mit der Anzahl der aufgeräumten Meldungen.
func (h *Handler) MirroredIncidentsPruned(_ context.Context, count int, retention time.Duration) {
	h.RecordSystemAudit(auditActionIncidentPruned, "config", auditTargetExternalIncident, "", "",
		fmt.Sprintf("count=%d retention_days=%d", count, int(retention.Hours()/24)))
}

// WaitIncidentMails blockiert, bis alle angestoßenen Vorfall-Benachrichtigungen verarbeitet sind.
func (h *Handler) WaitIncidentMails() { h.incidentMails.Wait() }

// externalIncidentKind bildet (Art, Anlass) auf den Vorlagen-Kind ab.
func externalIncidentKind(kind string, event externalstatus.IncidentEventKind) string {
	maintenance := kind == models.ExternalIncidentKindMaintenance
	switch event {
	case externalstatus.IncidentCreated:
		if maintenance {
			return NotificationKindExternalMaintenanceCreated
		}
		return NotificationKindExternalIncidentCreated
	case externalstatus.IncidentResolved:
		if maintenance {
			return NotificationKindExternalMaintenanceResolved
		}
		return NotificationKindExternalIncidentResolved
	}
	if maintenance {
		return NotificationKindExternalMaintenanceUpdated
	}
	return NotificationKindExternalIncidentUpdated
}

// extIncidentMail bündelt die pro Ereignis einmal berechneten Werte.
type extIncidentMail struct {
	ev   externalstatus.IncidentEvent
	page models.StatusPage
	cfg  smtpConfig
	lang string // Sprache des eingebauten Standardtexts (je Abonnent gesetzt)
	tmpl *models.NotificationTemplate
	// subTemplate: Vorlage/Sprache für die gewählte Sprache eines Abonnenten
	// "" = Seitenstandardsprache.
	subTemplate func(preferred string) (*models.NotificationTemplate, string)
	// chatLang: effektive Sprache des Abonnenten für Chat-Texte.
	chatLang  func(preferred string) string
	front     string
	compNames string
	compIDs   []uint
	message   string // jüngstes Update, einzeilig
	target    mailAuditTarget
	sent      int
	failed    int
}

func (h *Handler) dispatchExternalIncident(ev externalstatus.IncidentEvent) {
	inc := ev.Incident
	id := strconv.FormatUint(uint64(inc.ID), 10)
	target := mailAuditTarget{Category: "incident", TargetType: auditTargetExternalIncident, TargetID: id, TargetName: inc.Title}
	h.RecordSystemAudit(auditActionIncidentMirrored, target.Category, target.TargetType, id, inc.Title,
		fmt.Sprintf("source=%s kind=%s event=%s status=%s notify=%t", oneLine(ev.Source.Name), inc.Kind, ev.Kind, inc.Status, ev.Notify))
	if !ev.Notify {
		return
	}

	var page models.StatusPage
	if err := h.DB.First(&page, inc.StatusPageID).Error; err != nil {
		slog.Error("Externer Vorfall: Statusseite nicht ladbar", "incident_id", inc.ID, "error", err)
		return
	}
	cfg := loadSMTPConfig()
	cfg.applyPageSender(page)
	m := &extIncidentMail{ev: ev, page: page, cfg: cfg, front: getFrontendURL(), target: target}
	// Vorlage nach dem echten Sprachcode des Abonnenten, sonst der
	// Seitenstandardsprache; Standardtext weiter in de oder en.
	m.subTemplate = h.memoSubscriberTemplate(page.ID, externalIncidentKind(inc.Kind, ev.Kind))
	m.chatLang = h.memoSubscriberChatLang(page.ID)
	names := make([]string, 0, len(ev.Components))
	for _, c := range ev.Components {
		names = append(names, c.Name)
		m.compIDs = append(m.compIDs, c.ID)
	}
	m.compNames = strings.Join(names, ", ")
	if len(inc.Updates) > 0 {
		m.message = oneLine(inc.Updates[0].Body)
	}

	var subs []models.Subscriber
	// nur bestätigte Abos.
	h.DB.Where("status_page_id = ? AND status = ?", page.ID, models.SubscriberStatusActive).
		FindInBatches(&subs, 100, func(_ *gorm.DB, _ int) error {
			for _, sub := range subs {
				h.notifyExternalSubscriber(m, sub)
			}
			return nil
		})
	if m.sent > 0 || m.failed > 0 {
		h.RecordSystemAudit(auditActionIncidentNotified, target.Category, target.TargetType, id, inc.Title,
			fmt.Sprintf("source=%s event=%s sent=%d failed=%d", oneLine(ev.Source.Name), ev.Kind, m.sent, m.failed))
	}
}

// notifyExternalSubscriber meldet an einen Abonnenten, sofern er eine der
// betroffenen Komponenten abonniert hat (Muster notifySubscriber).
func (h *Handler) notifyExternalSubscriber(m *extIncidentMail, sub models.Subscriber) {
	if !SubscriberHasAnyComponent(sub.SubscribedComponentIDs, m.compIDs) {
		return
	}
	if m.page.IsPrivate && !h.subscriberCreatorMayReadPage(sub, m.page.ID) {
		return
	}
	var err error
	if sub.Channel == "google_chat" {
		err = h.sendExternalIncidentWebhook(m, sub)
	} else {
		err = h.sendExternalIncidentMail(m, sub)
	}
	if err == nil {
		m.sent++
		return
	}
	if errors.Is(err, errMailDisabled) {
		return
	}
	m.failed++
	h.RecordSystemAudit(auditActionIncidentFailed, m.target.Category, m.target.TargetType, m.target.TargetID, m.target.TargetName,
		fmt.Sprintf("subscriber_id=%d channel=%s error=%s", sub.ID, sub.Channel, truncateAudit(err.Error())))
}

// errMailDisabled: E-Mail-Versand global abgeschaltet (deliverMail hat den
// Audit-Eintrag email.skipped_disabled geschrieben), weder gesendet noch Fehler.
var errMailDisabled = errors.New("email disabled")

func truncateAudit(s string) string {
	s = oneLine(s)
	if len(s) > 300 {
		return strings.ToValidUTF8(s[:300], "")
	}
	return s
}

func (h *Handler) sendExternalIncidentWebhook(m *extIncidentMail, sub models.Subscriber) error {
	if sub.WebhookURL == "" {
		return errors.New("google_chat subscriber without webhook url")
	}
	inc := m.ev.Incident
	// Beschriftungen in der Sprache des Abonnenten (de/en) bzw. der Seite.
	lang := "de" // wie bisher, wenn keine Sprachwahl vorbereitet ist
	if m.chatLang != nil {
		lang = m.chatLang(sub.Language)
	}
	var text string
	if lang == "en" {
		text = fmt.Sprintf("🌐 **[EXTERNAL] Status Update** (%s)\n**Source:** %s\n**Components:** %s\n**Incident:** %s\n**Status:** %s\n**Details:** %s\n**Link:** %s",
			m.ev.Kind, m.ev.Source.Name, m.compNames, inc.Title, externalStatusLabel(inc.Status, "en"), m.message, inc.URL)
	} else {
		text = fmt.Sprintf("🌐 **[EXTERN] Status Update** (%s)\n**Quelle:** %s\n**Komponenten:** %s\n**Meldung:** %s\n**Status:** %s\n**Details:** %s\n**Link:** %s",
			m.ev.Kind, m.ev.Source.Name, m.compNames, inc.Title, externalStatusLabel(inc.Status, "de"), m.message, inc.URL)
	}
	// SSRF-Schutz wie bei Incidents: postWebhook sendet über webhookTestClient
	// (IP-Prüfung beim Connect); Ratenlimit/Retry zentral, der
	// Fehler enthält nie die URL.
	res := postWebhook(context.Background(), sub.WebhookURL, map[string]any{"text": text}, webhookModeNotify)
	// Admins sehen Ratenlimit und dauerhafte Ablehnung in der Glocke.
	h.reportWebhookProblem(sub.StatusPageID, sub.ID, "google_chat", res)
	return res.Err()
}

func (h *Handler) sendExternalIncidentMail(m *extIncidentMail, sub models.Subscriber) error {
	addr, err := mail.ParseAddress(sub.Email)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	front, unsub := m.front, ""
	var extra []string
	if token, terr := middleware.GenerateSubscriberToken(sub.ID, sub.StatusPageID); terr == nil {
		extra = listUnsubscribeHeaders(m.front, token)
		unsub = m.front + "/unsubscribe?token=" + url.QueryEscape(token)
		if m.page.IsPrivate {
			front = m.front + "/status/" + m.page.Slug + "?token=" + url.QueryEscape(token)
		}
	}
	// Kopie nur für diesen Empfänger: Sprache/Vorlage des Abonnenten, Zähler bleiben unberührt.
	loc := *m
	loc.tmpl, loc.lang = m.subTemplate(sub.Language)
	subject, body := buildExternalIncidentMail(&loc, front, unsub)
	if hasHeaderInjectionChars(subject) || hasHeaderInjectionChars(body) || hasHeaderInjectionChars(m.cfg.From) {
		return errors.New("subject/body/sender contain CR/LF")
	}
	msg := m.cfg.fromHeaders() +
		"To: " + addr.Address + "\n" +
		"Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\n"
	for _, hd := range extra {
		if hasHeaderInjectionChars(hd) {
			return errors.New("extra header contains CR/LF")
		}
		msg += hd + "\n"
	}
	msg += "MIME-Version: 1.0\nContent-Type: text/html; charset=UTF-8\n\n" + body
	sent, err := h.deliverMail(m.cfg, addr.Address, []byte(msg), m.target)
	if err != nil {
		return err
	}
	if !sent {
		return errMailDisabled
	}
	return nil
}

// buildExternalIncidentMail liefert Betreff und Body: die Vorlage der Seite
// (Kind external_*, Audience subscriber), sonst den Standardtext. Alle Werte
// der Fremdseite werden entschärft (HTML-escaped, Zeilenumbrüche im Betreff
// und in Platzhaltern durch Leerzeichen ersetzt, im Standardtext durch <br>).
func buildExternalIncidentMail(m *extIncidentMail, frontendURL, unsubscribeURL string) (subject, body string) {
	inc, src := m.ev.Incident, m.ev.Source
	statusLabel := externalStatusLabel(inc.Status, m.lang)
	srcName := oneLine(src.Name)
	title := oneLine(inc.Title)
	var defSubject string
	if m.lang == "en" {
		defSubject = fmt.Sprintf("[EXTERNAL] %s: %s (%s, source: %s)", m.compNames, title, statusLabel, srcName)
	} else {
		defSubject = fmt.Sprintf("[EXTERN] %s: %s (%s, Quelle: %s)", m.compNames, title, statusLabel, srcName)
	}
	defBody := buildExternalIncidentHTML(m, statusLabel, frontendURL, unsubscribeURL)
	data := NotificationTemplateData{
		IncidentTitle:  title,
		Title:          title,
		Status:         inc.Status,
		StatusLabel:    statusLabel,
		ComponentNames: oneLine(m.compNames),
		Message:        m.message,
		MessageHTML:    htmltemplate.HTML(escapeMultiline(m.message)), // #nosec G203 -- HTML-maskiert, Umbrüche als <br>; Fremdtext wird nicht als Markdown gelesen
		Action:         string(m.ev.Kind),
		Language:       m.lang,
		FrontendURL:    frontendURL,
		UnsubscribeURL: unsubscribeURL,
		FromAddress:    m.cfg.From,
		SourceName:     srcName,
		SourceURL:      inc.URL,
	}
	return applyNotificationTemplate(m.tmpl, data, defSubject, defBody)
}

var htmlLineBreaks = strings.NewReplacer("\r\n", "<br>", "\n", "<br>", "\r", "<br>")

// escapeMultiline escaped HTML und macht Zeilenumbrüche zu <br>: Der Body darf
// kein CR/LF enthalten (Header-Injection-Schutz), Updates der Fremdseite sind
// aber mehrzeilig.
func escapeMultiline(s string) string { return htmlLineBreaks.Replace(html.EscapeString(s)) }

func buildExternalIncidentHTML(m *extIncidentMail, statusLabel, frontendURL, unsubscribeURL string) string {
	inc, src := m.ev.Incident, m.ev.Source
	de := m.lang != "en"
	srcLabel, compLabel, statusText, descLabel, linkLabel, srcLink, unsubLabel, intro := "Source", "Components", "Status", "Latest update", "View status page", "View at the source", "Unsubscribe", ""
	if de {
		srcLabel, compLabel, descLabel, linkLabel, srcLink, unsubLabel = "Quelle", "Komponenten", "Letztes Update", "Statusseite ansehen", "Zur Meldung bei der Quelle", "Abbestellen"
	}
	intro = externalIntro(inc.Kind, m.ev.Kind, de)
	latest := ""
	if len(inc.Updates) > 0 {
		latest = escapeMultiline(inc.Updates[0].Body)
	}
	srcHTML := html.EscapeString(oneLine(src.Name))
	if inc.URL != "" {
		srcHTML += fmt.Sprintf(` (<a href="%s">%s</a>)`, html.EscapeString(inc.URL), srcLink)
	}
	unsub := ""
	if unsubscribeURL != "" {
		unsub = fmt.Sprintf(`<a href="%s" style="color:#8b95a3;font-size:11px;text-decoration:underline;">%s</a>`, html.EscapeString(unsubscribeURL), unsubLabel)
	}
	return fmt.Sprintf(`<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f5f6f8;padding:32px 16px;">`+
		`<div style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #e6e8ec;border-radius:10px;overflow:hidden;">`+
		`<div style="background:#4c6ef5;padding:20px 24px;"><span style="color:#ffffff;font-size:16px;font-weight:600;">Statushafen</span></div>`+
		`<div style="padding:24px;"><h2 style="margin:0 0 12px;font-size:18px;color:#1a2230;">%s</h2>`+
		`<p style="margin:0 0 16px;color:#57616f;font-size:14px;line-height:1.5;">%s</p>`+
		`<table role="presentation" style="width:100%%;border-collapse:collapse;margin-bottom:20px;">`+
		`<tr><td style="padding:6px 0;color:#8b95a3;font-size:13px;width:110px;">%s</td><td style="padding:6px 0;color:#1a2230;font-size:13px;font-weight:600;">%s</td></tr>`+
		`<tr><td style="padding:6px 0;color:#8b95a3;font-size:13px;">%s</td><td style="padding:6px 0;color:#1a2230;font-size:13px;">%s</td></tr>`+
		`<tr><td style="padding:6px 0;color:#8b95a3;font-size:13px;">%s</td><td style="padding:6px 0;color:#1a2230;font-size:13px;">%s</td></tr></table>`+
		`<p style="margin:0 0 4px;color:#8b95a3;font-size:12px;text-transform:uppercase;letter-spacing:0.4px;">%s</p>`+
		`<p style="margin:0 0 20px;color:#1a2230;font-size:14px;line-height:1.6;">%s</p>`+
		`<a href="%s" style="display:inline-block;background:#4c6ef5;color:#ffffff;text-decoration:none;font-size:14px;font-weight:600;padding:10px 20px;border-radius:6px;">%s</a></div>`+
		`<div style="padding:16px 24px;border-top:1px solid #e6e8ec;"><p style="margin:0;color:#8b95a3;font-size:12px;">Statushafen</p>%s</div></div></div>`,
		html.EscapeString(oneLine(inc.Title)), intro,
		srcLabel, srcHTML,
		compLabel, html.EscapeString(oneLine(m.compNames)),
		statusText, html.EscapeString(statusLabel),
		descLabel, latest,
		html.EscapeString(frontendURL), linkLabel, unsub)
}

func externalIntro(kind string, ev externalstatus.IncidentEventKind, de bool) string {
	maintenance := kind == models.ExternalIncidentKindMaintenance
	switch {
	case de && maintenance && ev == externalstatus.IncidentCreated:
		return "Bei einer externen Quelle ist eine Wartung angekündigt, die deine Komponenten betrifft."
	case de && maintenance && ev == externalstatus.IncidentResolved:
		return "Eine Wartung einer externen Quelle ist abgeschlossen."
	case de && maintenance:
		return "Es gibt eine Aktualisierung zu einer Wartung einer externen Quelle."
	case de && ev == externalstatus.IncidentCreated:
		return "Eine externe Quelle meldet einen Vorfall, der deine Komponenten betrifft."
	case de && ev == externalstatus.IncidentResolved:
		return "Ein Vorfall einer externen Quelle ist behoben."
	case de:
		return "Es gibt eine Aktualisierung zu einem Vorfall einer externen Quelle."
	case maintenance && ev == externalstatus.IncidentCreated:
		return "An external source announced a maintenance that affects your components."
	case maintenance && ev == externalstatus.IncidentResolved:
		return "A maintenance of an external source is complete."
	case maintenance:
		return "There is an update on a maintenance of an external source."
	case ev == externalstatus.IncidentCreated:
		return "An external source reports an incident that affects your components."
	case ev == externalstatus.IncidentResolved:
		return "An incident of an external source has been resolved."
	}
	return "There is an update on an incident of an external source."
}

// externalStatusLabel: lokalisierte Bezeichnung eines normalisierten Status;
// unbekannte Werte erscheinen roh.
func externalStatusLabel(status, lang string) string {
	switch status {
	case "in_progress":
		if lang == "en" {
			return "In progress"
		}
		return "In Arbeit"
	case "completed":
		if lang == "en" {
			return "Completed"
		}
		return "Abgeschlossen"
	}
	return statusLabelFor(status, lang)
}

// ---- Lesende API (Teil C/D) ----------------------------------------------

type externalIncidentComponentView struct {
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
}

type externalIncidentOwnComponentView struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type externalIncidentUpdateView struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Body      string    `json:"body"`
	DisplayAt time.Time `json:"display_at"`
}

// externalIncidentView ist die Auslieferung einer gespiegelten Meldung.
type externalIncidentView struct {
	ID                 uint                               `json:"id"`
	SourceID           uint                               `json:"source_id"`
	SourceName         string                             `json:"source_name"`
	SourceURL          string                             `json:"source_url"`
	Kind               string                             `json:"kind"`
	ExternalID         string                             `json:"external_id"`
	Title              string                             `json:"title"`
	Status             string                             `json:"status"`
	RawStatus          string                             `json:"raw_status"`
	Impact             string                             `json:"impact"`
	Closed             bool                               `json:"closed"`
	URL                string                             `json:"url"`
	StartedAt          *time.Time                         `json:"started_at"`
	UpdatedAt          *time.Time                         `json:"updated_at"`
	ResolvedAt         *time.Time                         `json:"resolved_at"`
	ScheduledFor       *time.Time                         `json:"scheduled_for"`
	ScheduledUntil     *time.Time                         `json:"scheduled_until"`
	AffectedComponents []externalIncidentOwnComponentView `json:"affected_components"`
	ExternalComponents []externalIncidentComponentView    `json:"external_components"`
	Updates            []externalIncidentUpdateView       `json:"updates"`
}

type externalIncidentQuery struct {
	kind     string // "" = alle
	state    string // "" | "open" | "closed"
	sourceID uint
	days     int
	limit    int
	now      time.Time // Bezugszeit; leer = jetzt (Tests, Verfügbarkeit)
}

// activeSourceIDs: Quellen der Seite, die abgefragt werden (aktiv, nicht gelöscht).
func (h *Handler) activeSourceIDs(pageID uint) *gorm.DB {
	return h.DB.Model(&models.ExternalSource{}).Select("id").Where("status_page_id = ? AND active = ?", pageID, true)
}

// externalIncidentViews liefert die gespiegelten Meldungen der Seite, die
// mindestens eine gekoppelte Komponente betreffen. Offene zuerst, dann nach
// letzter Änderung; beendete nur innerhalb von q.days.
func (h *Handler) externalIncidentViews(pageID uint, q externalIncidentQuery) ([]externalIncidentView, error) {
	db := h.DB.Where("status_page_id = ? AND external_source_id IN (?)", pageID, h.activeSourceIDs(pageID))
	if q.kind != "" {
		db = db.Where("kind = ?", q.kind)
	}
	if q.sourceID != 0 {
		db = db.Where("external_source_id = ?", q.sourceID)
	}
	switch q.state {
	case "open":
		db = db.Where("closed = ?", false)
	case "closed":
		db = db.Where("closed = ?", true)
	}
	now := q.now
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.AddDate(0, 0, -q.days)
	db = db.Where("closed = ? OR COALESCE(resolved_at, external_updated_at, last_seen_at) >= ?", false, cutoff)
	var rows []models.ExternalIncident
	err := db.Preload("Components").Preload("Updates", func(d *gorm.DB) *gorm.DB { return d.Order("display_at desc, id desc") }).
		Order("closed asc, external_updated_at desc, id desc").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	own, err := externalstatus.OwnComponentMap(h.DB, pageID, 0)
	if err != nil {
		return nil, err
	}
	var sources []models.ExternalSource
	if err := h.DB.Where("status_page_id = ?", pageID).Find(&sources).Error; err != nil {
		return nil, err
	}
	srcByID := make(map[uint]models.ExternalSource, len(sources))
	for _, s := range sources {
		srcByID[s.ID] = s
	}
	out := make([]externalIncidentView, 0, len(rows))
	for _, r := range rows {
		v := newExternalIncidentView(r, srcByID[r.ExternalSourceID], own[r.ExternalSourceID])
		if len(v.AffectedComponents) == 0 {
			continue
		}
		out = append(out, v)
		if len(out) >= q.limit {
			break
		}
	}
	return out, nil
}

func newExternalIncidentView(r models.ExternalIncident, src models.ExternalSource, own map[string][]externalstatus.OwnComponent) externalIncidentView {
	srcURL := src.PageURL
	if srcURL == "" {
		srcURL = src.BaseURL
	}
	v := externalIncidentView{
		ID: r.ID, SourceID: r.ExternalSourceID, SourceName: src.Name, SourceURL: srcURL,
		Kind: r.Kind, ExternalID: r.ExternalID, Title: r.Title, Status: r.Status, RawStatus: r.RawStatus,
		Impact: r.Impact, Closed: r.Closed, URL: r.URL, StartedAt: r.StartedAt, UpdatedAt: r.ExternalUpdatedAt,
		ResolvedAt: r.ResolvedAt, ScheduledFor: r.ScheduledFor, ScheduledUntil: r.ScheduledUntil,
		AffectedComponents: []externalIncidentOwnComponentView{},
		ExternalComponents: make([]externalIncidentComponentView, 0, len(r.Components)),
		Updates:            make([]externalIncidentUpdateView, 0, len(r.Updates)),
	}
	seen := map[uint]bool{}
	for _, c := range r.Components {
		v.ExternalComponents = append(v.ExternalComponents, externalIncidentComponentView{ExternalID: c.ComponentExternalID, Name: c.Name})
		for _, o := range own[c.ComponentExternalID] {
			if !seen[o.ID] {
				seen[o.ID] = true
				v.AffectedComponents = append(v.AffectedComponents, externalIncidentOwnComponentView{ID: o.ID, Name: o.Name})
			}
		}
	}
	for _, u := range r.Updates {
		v.Updates = append(v.Updates, externalIncidentUpdateView{ID: u.ExternalID, Status: u.Status, Body: u.Body, DisplayAt: u.DisplayAt})
	}
	return v
}

func parseExternalIncidentQuery(c *gin.Context) (externalIncidentQuery, string) {
	q := externalIncidentQuery{days: externalIncidentDefaultDays, limit: externalIncidentDefaultMax}
	q.kind = c.Query("kind")
	if q.kind != "" && q.kind != models.ExternalIncidentKindIncident && q.kind != models.ExternalIncidentKindMaintenance {
		return q, "kind must be incident or maintenance"
	}
	q.state = c.Query("state")
	if q.state != "" && q.state != "open" && q.state != "closed" {
		return q, "state must be open or closed"
	}
	if v := c.Query("source_id"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil || n == 0 {
			return q, "invalid source_id"
		}
		q.sourceID = uint(n)
	}
	if v := c.Query("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > externalIncidentMaxDays {
			return q, "days must be between 1 and " + strconv.Itoa(externalIncidentMaxDays)
		}
		q.days = n
	}
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > externalIncidentMaxLimit {
			return q, "limit must be between 1 and " + strconv.Itoa(externalIncidentMaxLimit)
		}
		q.limit = n
	}
	return q, ""
}

// GET /api/pages/:page_id/external-incidents (components:read) - gespiegelte
// Vorfälle und Wartungen, die gekoppelte Komponenten der Seite betreffen.
// Filter: kind, state (open|closed), source_id, days (Fenster für beendete,
// Standard 30), limit (Standard 50).
func (h *Handler) GetExternalIncidentsHandler(c *gin.Context) {
	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}
	q, msg := parseExternalIncidentQuery(c)
	if msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg, "code": "invalid_request"})
		return
	}
	items, err := h.externalIncidentViews(tenantID, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch external incidents", "code": "server_error"})
		return
	}
	c.JSON(http.StatusOK, items)
}

// GET /api/pages/:page_id/external-incidents/:id (components:read) - eine
// gespiegelte Meldung; 404, wenn sie nicht zu dieser Seite gehört oder keine
// gekoppelte Komponente betrifft.
func (h *Handler) GetExternalIncidentHandler(c *gin.Context) {
	tenantID, ok := parseTenantID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid id parameter", "code": "invalid_id"})
		return
	}
	var row models.ExternalIncident
	err = h.DB.Preload("Components").Preload("Updates", func(d *gorm.DB) *gorm.DB { return d.Order("display_at desc, id desc") }).
		Where("id = ? AND status_page_id = ?", id, tenantID).First(&row).Error
	if err != nil {
		h.externalIncidentNotFoundOr500(c, err)
		return
	}
	var src models.ExternalSource
	if err := h.DB.Where("id = ? AND status_page_id = ?", row.ExternalSourceID, tenantID).First(&src).Error; err != nil {
		h.externalIncidentNotFoundOr500(c, err)
		return
	}
	own, err := externalstatus.OwnComponentMap(h.DB, tenantID, src.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch external incident", "code": "server_error"})
		return
	}
	v := newExternalIncidentView(row, src, own[src.ID])
	if len(v.AffectedComponents) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "External incident not found", "code": "external_incident_not_found"})
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *Handler) externalIncidentNotFoundOr500(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "External incident not found", "code": "external_incident_not_found"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch external incident", "code": "server_error"})
}

// publicExternalIncidents: Auszug fürs öffentliche Dashboard (Teil D): offene
// und kürzlich beendete Meldungen. Fehler werden geloggt, das Dashboard
// bleibt erreichbar (leere Liste).
func (h *Handler) publicExternalIncidents(pageID uint) []externalIncidentView {
	items, err := h.externalIncidentViews(pageID, externalIncidentQuery{days: publicExternalIncidentDays, limit: externalIncidentDefaultMax})
	if err != nil {
		slog.Error("Externe Vorfälle fürs Dashboard nicht ladbar", "page_id", pageID, "error", err)
		return []externalIncidentView{}
	}
	return items
}
