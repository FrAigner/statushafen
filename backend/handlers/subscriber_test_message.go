package handlers

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/fraigner/statushafen/models"
)

// subscriberTestMessageTexts returns subject, Google Chat text and mail body
// (single line HTML) of the test message in lang ("de" or "en").
func subscriberTestMessageTexts(lang, pageName string) (subject, chat, mailHTML string) {
	esc := html.EscapeString(pageName)
	if lang == "en" {
		return "Test message from " + pageName,
			"👋 *Test message* from " + pageName + "\nThis channel receives the status notifications of this status page.",
			wrapTestMessageFrame(esc, "Test message",
				"This is a test message from the status page &quot;"+esc+"&quot;. If you can read it, notifications reach this address.",
				"You received this message because an administrator sent a test message to this address. No subscription has been created.")
	}
	return "Testnachricht von " + pageName,
		"👋 *Testnachricht* von " + pageName + "\nDieser Kanal empfängt die Status-Benachrichtigungen dieser Statusseite.",
		wrapTestMessageFrame(esc, "Testnachricht",
			"Dies ist eine Testnachricht der Statusseite &quot;"+esc+"&quot;. Wenn du sie lesen kannst, kommen Benachrichtigungen an dieser Adresse an.",
			"Du erhältst diese Nachricht, weil ein Administrator eine Testnachricht an diese Adresse geschickt hat. Es wurde kein Abonnement angelegt.")
}

// wrapTestMessageFrame uses the same frame as the subscription confirmation
// mail, without line breaks (hasHeaderInjectionChars also checks the body).
func wrapTestMessageFrame(escPageName, title, text, footer string) string {
	return `<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f5f6f8;padding:32px 16px;">` +
		`<div style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #e6e8ec;border-radius:10px;overflow:hidden;">` +
		`<div style="background:#4c6ef5;padding:20px 24px;"><span style="color:#ffffff;font-size:16px;font-weight:600;">` + escPageName + `</span></div>` +
		`<div style="padding:24px;"><h2 style="margin:0 0 12px;font-size:18px;color:#1a2230;">` + title + `</h2>` +
		`<p style="margin:0;color:#57616f;font-size:14px;line-height:1.5;">` + text + `</p>` +
		`<p style="margin:20px 0 0;color:#8b95a3;font-size:12px;">` + footer + `</p></div></div></div>`
}

// subscriberTestMessageRequest is the unsaved content of the admin subscriber
// form. Language is the admin UI language, not the subscriber's mail language.
type subscriberTestMessageRequest struct {
	Channel    string `json:"Channel"`
	Email      string `json:"Email"`
	WebhookURL string `json:"WebhookURL"`
	Language   string `json:"Language"`
}

// validateSubscriberTestTarget checks the target exactly like creating a
// subscriber in the admin does (including the SSRF checks for webhooks).
func validateSubscriberTestTarget(req subscriberTestMessageRequest) error {
	if err := checkSubscriberTexts(req.Email, req.WebhookURL, "", req.Language); err != nil {
		return err
	}
	switch req.Channel {
	case "email":
		if !isPlainEmailAddress(req.Email) {
			return newCodedError("email_invalid", "Gültige E-Mail-Adresse ist erforderlich.")
		}
		return nil
	case "google_chat":
		return validateAdminSubscriberWebhook(req.Channel, req.WebhookURL)
	}
	return newCodedError("channel_invalid", "Unbekannter Kanal")
}

// SendSubscriberTestMessageHandler sends a test message to the target entered
// in the admin subscriber form (before saving) and stores nothing.
func (h *Handler) SendSubscriberTestMessageHandler(c *gin.Context) {
	pageID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": errInvalidPageID, "code": "invalid_id"})
		return
	}
	var req subscriberTestMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Parameter", "code": "invalid_request"})
		return
	}
	if err := validateSubscriberTestTarget(req); err != nil {
		respondCoded(c, http.StatusBadRequest, err)
		return
	}
	var page models.StatusPage
	if err := h.DB.First(&page, pageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Status page not found", "code": "page_not_found"})
		return
	}
	subject, chat, mailHTML := subscriberTestMessageTexts(outageMailLang(req.Language), page.Name)
	if req.Channel == "google_chat" {
		respondWebhookProbe(c, postWebhook(c.Request.Context(), req.WebhookURL, map[string]any{"text": chat}, webhookModeProbe))
		return
	}
	h.sendSubscriberTestMail(c, page, req.Email, subject, mailHTML)
}

// sendSubscriberTestMail sends synchronously: the admin waits for the result
// of the test. The route is authenticated and tightly rate limited, and the
// admin entered the address, so the response time reveals nothing new.
func (h *Handler) sendSubscriberTestMail(c *gin.Context, page models.StatusPage, recipient, subject, body string) {
	cfg := loadSMTPConfig()
	cfg.applyPageSender(page)
	if hasHeaderInjectionChars(subject) || hasHeaderInjectionChars(cfg.From) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error", "code": "server_error"})
		return
	}
	msg := cfg.fromHeaders() +
		"To: " + recipient + "\n" +
		"Subject: " + subject + "\n" +
		"MIME-Version: 1.0\nContent-Type: text/html; charset=UTF-8\n\n" + body
	pageIDStr := strconv.FormatUint(uint64(page.ID), 10)
	sent, err := h.deliverMail(cfg, recipient, []byte(msg), mailAuditTarget{Category: "subscriber", TargetType: "StatusPage", TargetID: pageIDStr, TargetName: page.Name})
	if err != nil {
		slog.Error("Failed to send subscriber test mail", "status_page_id", page.ID, "smtp_addr", cfg.addr(), "error", err)
		h.RecordSystemAudit("subscriber.test_message_failed", "subscriber", "StatusPage", pageIDStr, page.Name,
			fmt.Sprintf("smtp_addr=%s error=%s", cfg.addr(), err.Error()))
		c.JSON(http.StatusBadGateway, gin.H{"error": "Mail konnte nicht gesendet werden", "code": "mail_send_failed"})
		return
	}
	if !sent {
		c.JSON(http.StatusConflict, gin.H{"error": "E-Mail-Versand ist abgeschaltet", "code": "email_disabled"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Test message sent"})
}

// respondWebhookProbe answers a single webhook attempt (test endpoints).
func respondWebhookProbe(c *gin.Context, res WebhookResult) {
	switch res.Kind {
	case webhookOK:
		c.JSON(http.StatusOK, gin.H{"message": "Test erfolgreich gesendet!"})
	case webhookRateLimited:
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Webhook-Ziel drosselt gerade", "code": "webhook_rate_limited", "retry_after": res.RetryAfterSeconds()})
	case webhookPermanent:
		c.JSON(http.StatusBadGateway, gin.H{"error": "Webhook-Ziel hat die Anfrage abgelehnt", "code": "webhook_rejected"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": "Webhook-Ziel nicht erreichbar", "code": "webhook_unreachable"})
	}
}
