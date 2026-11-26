package handlers

import (
	"fmt"
	"html"
	htmltemplate "html/template"
	"log/slog"
	"net/url"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/richtext"

	"gorm.io/gorm"
)

// Mail dispatch for General Notices. Deliberately kept lean
// and separate from DispatchNotifications - notices have no
// component assignment and no status lifecycle, only
// Title/Message/Level. The SMTP path itself (loadSMTPConfig +
// sendAlertEmail) is shared, so that From fallback, header injection
// guard and sender config stay identical to the incident path.
//
// Subscriber filtering is weaker than for incidents: a notice
// always applies to the whole status page, not to individual components.
// Therefore all email subscribers of the page receive the notice, regardless of
// which components they subscribed to. Internal users (Admin/Editor)
// are not notified - notices are aimed at the general
// public.

// noticeDispatchContext bundles the values computed once per dispatch
// that every subscriber mail needs - avoids giving sendNoticeToSubscriber
// a long parameter list.
type noticeDispatchContext struct {
	notice      models.GeneralNotice
	action      string
	page        models.StatusPage
	smtpCfg     smtpConfig
	frontendURL string
	// subTemplate: template and language (de/en of the built-in default text) for
	// the subscriber's chosen language; "" = page default.
	subTemplate    func(preferred string) (*models.NotificationTemplate, string)
	escTitle       string
	messageHTML    string // richtext.ToMailHTML(notice.Message)
	defaultSubject string
}

// sendNoticeToSubscriber builds the individual (token/unsubscribe link)
// mail content for a subscriber and sends it. Extracted from
// DispatchNoticeNotifications so that its cognitive complexity does not exceed the
// SonarCloud limit (go:S3776).
func (h *Handler) sendNoticeToSubscriber(ctx noticeDispatchContext, sub models.Subscriber) {
	// Notices of private pages only to subscriptions whose
	// creator may still see the page (see subscriberCreatorMayReadPage).
	if ctx.page.IsPrivate && !h.subscriberCreatorMayReadPage(sub, ctx.page.ID) {
		slog.Info("Skipping notice subscriber on private page without authorized creator", "subscriber_id", sub.ID)
		return
	}
	perSubURL := ctx.frontendURL
	perSubUnsub := ""
	var extraHeaders []string
	if token, err := middleware.GenerateSubscriberToken(sub.ID, sub.StatusPageID); err == nil {
		extraHeaders = listUnsubscribeHeaders(ctx.frontendURL, token)
		perSubUnsub = ctx.frontendURL + "/unsubscribe?token=" + url.QueryEscape(token)
		if ctx.page.IsPrivate {
			perSubURL = ctx.frontendURL + "/status/" + ctx.page.Slug + "?token=" + url.QueryEscape(token)
		}
	} else {
		slog.Error("Failed to generate subscriber token for notice notification", "subscriber_id", sub.ID, "error", err)
	}

	tmplSub, lang := ctx.subTemplate(sub.Language)
	data := newNoticeTemplateData(ctx.notice, ctx.messageHTML, ctx.action, lang, perSubURL, ctx.smtpCfg.From)
	data.UnsubscribeURL = perSubUnsub
	defaultBody := buildNoticeEmailHTML(lang, ctx.notice.Level, ctx.escTitle, ctx.messageHTML, perSubURL, perSubUnsub)
	subject, body := applyNotificationTemplate(tmplSub, data, ctx.defaultSubject, defaultBody)
	h.sendAlertEmail(ctx.smtpCfg, sub.Email, subject, body, "notice subscriber", ctx.notice.ID, ctx.notice.Title, extraHeaders...)
}

// DispatchNoticeNotifications sends the notice by mail to all
// email subscribers of the status page. action: "created" | "updated" |
// "ended" - maps to the corresponding kinds.
func (h *Handler) DispatchNoticeNotifications(notice models.GeneralNotice, action string) {
	if !notice.NotifySubscribers {
		return
	}
	go func() {
		slog.Info("Dispatching notice notifications", "notice_id", notice.ID, "notice_title", notice.Title, "action", action)

		smtpCfg := loadSMTPConfig()
		var page models.StatusPage
		h.DB.First(&page, notice.StatusPageID)
		smtpCfg.applyPageSender(page)

		ctx := noticeDispatchContext{
			notice:      notice,
			action:      action,
			page:        page,
			smtpCfg:     smtpCfg,
			frontendURL: getFrontendURL(),
			// Language of the subscriber, otherwise of the page.
			subTemplate:    h.memoSubscriberTemplate(notice.StatusPageID, resolveNoticeKind(action)),
			escTitle:       html.EscapeString(notice.Title),
			messageHTML:    richtext.ToMailHTML(notice.Message),
			defaultSubject: fmt.Sprintf("[%s] %s", noticeLevelPrefix(notice.Level), notice.Title),
		}

		var subscribers []models.Subscriber
		// only confirmed subscriptions.
		h.DB.Where("status_page_id = ? AND channel = ? AND status = ?", notice.StatusPageID, "email", models.SubscriberStatusActive).FindInBatches(&subscribers, 100, func(tx *gorm.DB, batch int) error {
			for _, sub := range subscribers {
				h.sendNoticeToSubscriber(ctx, sub)
			}
			return nil
		})
	}()
}

// resolveNoticeKind maps the "created"/"updated"/"ended" actions to
// the notice kinds. Empty return = unknown, DEFAULT renderer applies.
func resolveNoticeKind(action string) string {
	switch action {
	case "created":
		return NotificationKindNoticeCreated
	case "updated":
		return NotificationKindNoticeUpdated
	case "ended":
		return NotificationKindNoticeEnded
	}
	return ""
}

func newNoticeTemplateData(notice models.GeneralNotice, messageHTML, action, language, frontendURL, fromAddress string) NotificationTemplateData {
	return NotificationTemplateData{
		IncidentTitle: notice.Title,
		Title:         notice.Title,
		Level:         notice.Level,
		Message:       notice.Message,
		MessageHTML:   htmltemplate.HTML(messageHTML), // #nosec G203 -- Ergebnis von richtext.ToMailHTML
		Action:        action,
		Language:      language,
		FrontendURL:   frontendURL,
		FromAddress:   fromAddress,
	}
}

// noticeLevelPrefix returns the subject prefix per level. "INFO"/"WARN"/
// "CRITICAL" instead of the colored badges from the frontend, because subjects
// are plain text.
func noticeLevelPrefix(level string) string {
	switch level {
	case "warning":
		return "WARN"
	case "critical":
		return "CRITICAL"
	}
	return "INFO"
}

// buildNoticeEmailHTML renders the default body for notice mails when
// no template is stored. Deliberately simpler than
// buildNotificationEmailHTML (no status badge, no component
// meta), because notices are structurally leaner. Layout/colors
// mirror buildNotificationEmailHTML, so that both mail types
// visually belong together.
func buildNoticeEmailHTML(lang, level, escTitle, messageHTML, frontendURL, unsubscribeURL string) string {
	badgeColor, badgeBG := "#4c6ef5", "#eaf2fc"
	// Texts in the dispatch language (de, otherwise en).
	de := lang == "de"
	levelLabel, headLabel, linkLabel, unsubLabel := "Info", "Notice", "View status page", "Unsubscribe"
	if de {
		headLabel, linkLabel, unsubLabel = "Ankündigung", "Statusseite ansehen", "Abbestellen"
	}
	switch level {
	case "warning":
		badgeColor, badgeBG = "#c98a1e", "#fdf3e0"
		levelLabel = "Warning"
		if de {
			levelLabel = "Warnung"
		}
	case "critical":
		badgeColor, badgeBG = "#d64b45", "#fdecea"
		levelLabel = "Critical"
		if de {
			levelLabel = "Kritisch"
		}
	}
	unsubscribeHTML := ""
	if unsubscribeURL != "" {
		unsubscribeHTML = fmt.Sprintf(`<a href="%s" style="color:#8b95a3;font-size:11px;text-decoration:underline;">%s</a>`, unsubscribeURL, unsubLabel)
	}
	// A single Sprintf template string whose line breaks are removed
	// beforehand - same trick as in buildNotificationEmailHTML,
	// so that sendAlertEmail's CR/LF guard can only
	// trigger on genuine user content.
	tmpl := `<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f5f6f8;padding:32px 16px;"><div style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #e6e8ec;border-radius:10px;overflow:hidden;"><div style="background:%s;padding:20px 24px;"><span style="color:#ffffff;font-size:16px;font-weight:600;">%s</span></div><div style="padding:24px;"><span style="display:inline-block;padding:2px 10px;border-radius:12px;background:%s;color:%s;font-size:12px;font-weight:600;margin-bottom:12px;">%s</span><h2 style="margin:0 0 12px;font-size:18px;color:#1a2230;">%s</h2><div style="margin:0 0 20px;color:#1a2230;font-size:14px;line-height:1.6;">%s</div><a href="%s" style="display:inline-block;background:#4c6ef5;color:#ffffff;text-decoration:none;font-size:14px;font-weight:600;padding:10px 20px;border-radius:6px;">%s</a></div><div style="padding:16px 24px;border-top:1px solid #e6e8ec;"><p style="margin:0;color:#8b95a3;font-size:12px;">Statushafen</p>%s</div></div></div>`
	return fmt.Sprintf(tmpl, badgeColor, headLabel, badgeBG, badgeColor, levelLabel, escTitle, messageHTML, frontendURL, linkLabel, unsubscribeHTML)
}
