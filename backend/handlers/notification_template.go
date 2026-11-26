package handlers

import (
	"errors"
	"fmt"
	htmltemplate "html/template"
	"log/slog"
	"strings"

	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// Template engine + placeholder system for notification mails.
//
// The former fmt.Sprintf-based subject/body strings are replaced by
// html/template as soon as a NotificationTemplate is stored for the combination
// (StatusPage, Kind, Language).
// If no custom template exists, the existing default renderer
// (buildNotificationEmailHTML) applies, so that behavior does not change bit for bit
// for all pages without their own configuration.
//
// html/template escapes placeholder values automatically (regression protection). Sender validation against SMTP header injection is still done
// by sendAlertEmail via hasHeaderInjectionChars on the fully
// rendered subject/body - a manipulated template that smuggles in CR/LF
// is therefore still discarded there.

// Notification template kinds. Deliberately typed constants instead of
// magic strings, so that a typo when looking up the template
// is caught at compile time.
// Incident kinds are the original ones, maintenance kinds were added
// (analogous to the three DispatchNotifications actions in maintenance_scheduler.go:
// MAINTENANCE_REMINDER / MAINTENANCE_STARTED / MAINTENANCE_COMPLETED).
//
// General notice kinds are deliberately NOT included: GeneralNotice endpoints
// currently send no mails (see handlers/general_notice.go), the
// specification therefore phrases this part explicitly as conditional ("if
// these are sent by mail"). If mail dispatch for notices is added
// later, the corresponding kinds will be added here.
const (
	NotificationKindIncidentCreated      = "incident_created"
	NotificationKindIncidentUpdated      = "incident_updated"
	NotificationKindIncidentResolved     = "incident_resolved"
	NotificationKindMaintenanceReminder  = "maintenance_reminder"
	NotificationKindMaintenanceStarted   = "maintenance_started"
	NotificationKindMaintenanceCompleted = "maintenance_completed"
	// Notice kinds: come into play when a GeneralNotice
	// is created/updated/ended with NotifySubscribers=true and
	// is sent through DispatchNoticeNotifications.
	NotificationKindNoticeCreated = "notice_created"
	NotificationKindNoticeUpdated = "notice_updated"
	NotificationKindNoticeEnded   = "notice_ended"
	// Confirmation mail of the double opt-in for email subscriptions
	// (Audience subscriber, placeholder {{.ConfirmURL}}).
	NotificationKindSubscriptionConfirm = "subscription_confirm"
	// Part B2: reports from external sources to subscribers
	// (Audience subscriber, placeholders {{.SourceName}}/{{.SourceURL}}).
	NotificationKindExternalIncidentCreated     = "external_incident_created"
	NotificationKindExternalIncidentUpdated     = "external_incident_updated"
	NotificationKindExternalIncidentResolved    = "external_incident_resolved"
	NotificationKindExternalMaintenanceCreated  = "external_maintenance_created"
	NotificationKindExternalMaintenanceUpdated  = "external_maintenance_updated"
	NotificationKindExternalMaintenanceResolved = "external_maintenance_resolved"
)

// Notification audience values (stage 5 of multilingual support):
// replace the previous convention in which the audience was encoded implicitly via
// Language=en ("staff") or Language=de ("subscriber") (see
// the comment on models.NotificationTemplate). NotificationAudienceStaff
// reaches internal users (Admin/Editor), NotificationAudienceSubscriber the
// public subscribers.
const (
	NotificationAudienceStaff      = "staff"
	NotificationAudienceSubscriber = "subscriber"
)

// NotificationTemplateData bundles the placeholders available to a template.
// Field names are deliberately descriptive so that
// templates stay readable ({{.IncidentTitle}} instead of {{.T}}).
//
// All fields are raw values - html/template escapes them automatically
// when rendering. No field may be escaped in advance here, otherwise it would be
// escaped twice and the user would see &amp;lt; instead of <.
type NotificationTemplateData struct {
	// IncidentTitle is kept for backward compatibility with templates from
	// earlier versions. Newly recommended: Title (works for
	// incidents AND notices alike). Both fields hold
	// the same value in incident dispatches.
	IncidentTitle  string
	Title          string
	Status         string
	StatusLabel    string
	Level          string // only filled for notice kinds: info/warning/critical
	ComponentNames string
	Tags           string
	// Message remains the raw text (Markdown source, as before for legacy templates).
	// MessageHTML is the formatted version as
	// finished, already sanitized mail HTML (richtext.ToMailHTML) and
	// is output unescaped by html/template.
	Message        string
	MessageHTML    htmltemplate.HTML
	Action         string
	Language       string
	FrontendURL    string
	UnsubscribeURL string
	// ConfirmURL: confirmation link of the double opt-in, only filled for
	// kind subscription_confirm.
	ConfirmURL  string
	FromAddress string
	// SourceName/SourceURL: name of the external source and link to
	// the original report, only filled for the external_* kinds.
	SourceName string
	SourceURL  string
}

// incidentTemplateDataOpts bundles the inputs for newIncidentTemplateData,
// so that the parameter list does not exceed the SonarCloud limit of 7.
type incidentTemplateDataOpts struct {
	Title       string
	StatusRaw   string
	CompNames   string
	TagNames    string
	Message     string
	MessageHTML string
	Action      string
	Language    string
	FrontendURL string
	FromAddress string
}

// newIncidentTemplateData builds the placeholders from an incident +
// accompanying meta information (language, URLs, sender). Deliberately a helper
// instead of two almost identical struct literals in DispatchNotifications -
// this way the record can be created per language with one call
// and per-recipient fields (UnsubscribeURL/FrontendURL) are then
// only set on the return value.
func newIncidentTemplateData(opts incidentTemplateDataOpts) NotificationTemplateData {
	return NotificationTemplateData{
		IncidentTitle:  opts.Title,
		Title:          opts.Title,
		Status:         opts.StatusRaw,
		StatusLabel:    statusLabelFor(opts.StatusRaw, opts.Language),
		ComponentNames: opts.CompNames,
		Tags:           opts.TagNames,
		Message:        opts.Message,
		MessageHTML:    htmltemplate.HTML(opts.MessageHTML), // #nosec G203 -- Ergebnis von richtext.ToMailHTML, siehe dort
		Action:         opts.Action,
		Language:       opts.Language,
		FrontendURL:    opts.FrontendURL,
		FromAddress:    opts.FromAddress,
	}
}

// statusLabelFor returns the localized label for the status ("Investigating"/
// "Untersuchung"/...) with a fallback to the raw value for unknown statuses.
// Passed to templates as the precomputed placeholder {{.StatusLabel}}
// so that authors do not have to rebuild the language switch
// themselves.
func statusLabelFor(status, lang string) string {
	meta, ok := incidentStatusMeta[status]
	if !ok {
		return status
	}
	if lang == "de" {
		return meta.labelDE
	}
	return meta.labelEN
}

// resolveIncidentKind maps the DispatchNotifications actions
// ("CREATED"/"UPDATED"/...) to a NotificationTemplate kind. An
// UPDATED event with final status "resolved" becomes the standalone
// "incident_resolved" kind, so that admins can customize the resolution mail separately
// from the regular status update.
func resolveIncidentKind(action, status string) string {
	switch action {
	case "CREATED":
		return NotificationKindIncidentCreated
	case "UPDATED":
		if models.IsResolvedIncidentStatus(status) {
			return NotificationKindIncidentResolved
		}
		return NotificationKindIncidentUpdated
	case "MAINTENANCE_REMINDER":
		return NotificationKindMaintenanceReminder
	case "MAINTENANCE_STARTED":
		return NotificationKindMaintenanceStarted
	case "MAINTENANCE_COMPLETED":
		return NotificationKindMaintenanceCompleted
	}
	// Unknown action: lookupNotificationTemplate skips the
	// DB query for an empty kind (see there) - the caller then gets
	// the hard-coded default renderer.
	return ""
}

// lookupNotificationTemplate loads the stored custom template for
// (statusPageID, kind, audience, language), if present. Nil return =
// none, in which case the renderer falls back to the default.
func (h *Handler) lookupNotificationTemplate(statusPageID uint, kind, audience, language string) *models.NotificationTemplate {
	if kind == "" {
		return nil
	}
	var tmpl models.NotificationTemplate
	err := h.DB.Where("status_page_id = ? AND kind = ? AND audience = ? AND language = ?", statusPageID, kind, audience, language).First(&tmpl).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			slog.Error("Failed to load notification template", "status_page_id", statusPageID, "kind", kind, "audience", audience, "language", language, "error", err)
		}
		return nil
	}
	return &tmpl
}

// renderNotificationTemplate parses and renders the subject and body of a
// custom template. On error (parse/execute error): empty strings +
// error - the caller then decides to fall back to the default.
//
// html/template escapes placeholder values automatically, which is the
// real reason for html/template instead of text/template here.
func renderNotificationTemplate(tmpl *models.NotificationTemplate, data NotificationTemplateData) (subject, body string, err error) {
	subject, err = executeStringTemplate("subject", tmpl.Subject, data)
	if err != nil {
		return "", "", fmt.Errorf("render subject: %w", err)
	}
	body, err = executeStringTemplate("body", tmpl.BodyHTML, data)
	if err != nil {
		return "", "", fmt.Errorf("render body: %w", err)
	}
	return subject, body, nil
}

// executeStringTemplate parses, checks (length and permitted constructs) and
// renders a template with bounded output size and runtime. The check
// runs on every execution, because even already stored templates from the
// time before the limits must no longer execute anything impermissible.
func executeStringTemplate(name, raw string, data NotificationTemplateData) (string, error) {
	maxIn, maxOut := MaxTemplateBodyLen, maxTemplateBodyOutput
	if name == "subject" {
		maxIn, maxOut = MaxTemplateSubjectLen, maxTemplateSubjectOutput
	}
	t, err := parseAllowedTemplate(name, raw, maxIn)
	if err != nil {
		return "", err
	}
	return runWithTimeout(templateExecTimeout, func() (string, error) {
		w := &limitedWriter{max: maxOut}
		if err := t.Execute(w, data); err != nil {
			if w.exceeded {
				return "", errTemplateOutputTooLarge()
			}
			return "", err
		}
		return w.buf.String(), nil
	})
}

// ValidateNotificationTemplate checks a template before saving:
// parse errors and CR/LF in the subject (SMTP header injection sink) are
// rejected. The body may contain CR/LF - sendAlertEmail folds it into
// one line before sending.
//
// The explicit CR/LF guard here has a reason: sendAlertEmail does reject
// the same characters at send time (defense in depth remains), but
// then every single mail of this status page would be silently discarded. The
// check at save time prevents that already in the admin UI with a clear
// error message.
func ValidateNotificationTemplate(subject, body string) error {
	return ValidateNotificationTemplateForKind("", subject, body)
}

// ValidateNotificationTemplateForKind additionally checks the mandatory content of the
// kind: the confirmation mail of the double opt-in must always contain the confirmation link
// ({{.ConfirmURL}}), otherwise the template could be abused to send
// arbitrary content to third parties and could not confirm a subscription.
func ValidateNotificationTemplateForKind(kind, subject, body string) error {
	if strings.TrimSpace(subject) == "" {
		return newCodedError("template_subject_empty", "subject darf nicht leer sein")
	}
	if strings.TrimSpace(body) == "" {
		return newCodedError("template_body_empty", "body darf nicht leer sein")
	}
	if hasHeaderInjectionChars(subject) {
		return newCodedError("template_subject_crlf", "subject darf keine CR/LF-Zeichen enthalten (SMTP-Header-Injection)")
	}
	if _, err := parseAllowedTemplate("subject", subject, MaxTemplateSubjectLen); err != nil {
		return err
	}
	_, used, err := parseAllowedTemplateFields("body", body, MaxTemplateBodyLen)
	if err != nil {
		return err
	}
	if kind == NotificationKindSubscriptionConfirm && !used["ConfirmURL"] {
		return errConfirmURLMissing()
	}
	return nil
}

func errConfirmURLMissing() error {
	return newCodedError("template_confirm_url_missing", "Die Vorlage muss den Platzhalter {{.ConfirmURL}} enthalten")
}

// applyNotificationTemplate renders the given custom template OR returns
// the defaults if no template is set or rendering
// fails (the latter is logged - a broken template must not
// prevent mail dispatch entirely). The lookup is deliberately NOT done
// here, so that DispatchNotifications can do it once per language instead of
// per recipient.
func applyNotificationTemplate(
	tmpl *models.NotificationTemplate,
	data NotificationTemplateData,
	defaultSubject string,
	defaultBody string,
) (subject, body string) {
	if tmpl == nil {
		return defaultSubject, defaultBody
	}
	subject, body, err := renderNotificationTemplate(tmpl, data)
	if err != nil {
		slog.Error("Notification template render failed, falling back to default", "template_id", tmpl.ID, "status_page_id", tmpl.StatusPageID, "kind", tmpl.Kind, "audience", tmpl.Audience, "language", tmpl.Language, "error_code", TemplateErrorCode(err))
		return defaultSubject, defaultBody
	}
	return subject, body
}
