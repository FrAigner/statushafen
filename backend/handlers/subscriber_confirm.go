package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/idna"
	"golang.org/x/text/unicode/norm"
)

// ─────────────────────────────────────────────────────────────────────────────
// Double opt-in for email subscriptions.
//
// A new email subscription starts as "pending" and becomes active only via the link in
// the confirmation mail. Otherwise anyone could register a foreign address
// and have incident mails sent to its owner unasked.
//
// Operator decision (of 24.10.): whoever is signed in
// and subscribes with their own account address skips the confirmation.
// Account addresses count as verified: they arise via an invitation
// by an admin, via SSO (only verified provider addresses, see
// auth_sso.go) or via the order confirmed by mail (page_order.go).
//
// Webhook channels (google_chat) need a separate decision according to the specification
// and therefore remain active without confirmation for now.
// ─────────────────────────────────────────────────────────────────────────────

// subscriptionConfirmTTL: how long a confirmation link is valid (like orderTokenTTL).
const subscriptionConfirmTTL = 24 * time.Hour

const (
	subscriptionPendingMessage = "Fast geschafft: Bitte bestätige dein Abo über den Link, den wir dir per E-Mail geschickt haben."
	subscriptionActiveMessage  = "Erfolgreich abonniert"
	// Default subject per sending language (default language of the page,
	// subscribers have no stored language).
	subscriptionConfirmSubjectPrefix   = "Bitte bestätige dein Abo für "
	subscriptionConfirmSubjectPrefixEN = "Please confirm your subscription for "
)

func subscriptionConfirmSubjectPrefixFor(lang string) string {
	if lang == "de" {
		return subscriptionConfirmSubjectPrefix
	}
	return subscriptionConfirmSubjectPrefixEN
}

// maxEmailAddressLength: longest permitted address (RFC 5321).
const maxEmailAddressLength = 254

// isPlainEmailAddress accepts only a bare address ("a@b.example"),
// not the display-name form ("Name <a@b.example>"). This makes the stored
// value directly usable as a recipient and unable to contain CR/LF.
func isPlainEmailAddress(s string) bool {
	if len(s) > maxEmailAddressLength {
		return false
	}
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}

// Gmail domains: Gmail delivers googlemail.com like gmail.com and ignores
// dots in the local part. The dot rule applies only to these two domains;
// for all other providers dots are part of the address.
const (
	gmailDomain       = "gmail.com"
	googlemailDomain  = "googlemail.com"
	gmailDomainNoDots = "gmailcom"

	googlemailNoDots = "googlemailcom"
)

// mailboxKey is the key for deduplication and the limit of
// confirmation mails per mailbox: Unicode NFKC, lowercase, domain as
// Punycode and without "+tag" in the local part ("Opfer+news@Example.com" ->
// "opfer@example.com"). For gmail.com and googlemail.com additionally without dots
// in the local part and with googlemail.com mapped to gmail.com. The address as entered
// is still what is stored and mailed; the key serves only
// for comparison, so that spelling variants of the same mailbox do not pass as foreign
// addresses.
func mailboxKey(email string) string {
	e := strings.ToLower(norm.NFKC.String(strings.TrimSpace(email)))
	at := strings.LastIndex(e, "@")
	if at <= 0 {
		return e
	}
	local, domain := e[:at], e[at+1:]
	// Unicode and Punycode spellings of the same domain become equal; whatever
	// cannot be converted stays lowercase.
	if ascii, err := idna.Lookup.ToASCII(domain); err == nil && ascii != "" {
		domain = ascii
	}
	if domain == googlemailDomain {
		domain = gmailDomain
	}
	if plus := strings.Index(local, "+"); plus > 0 {
		local = local[:plus]
	}
	if domain == gmailDomain {
		if undotted := strings.ReplaceAll(local, ".", ""); undotted != "" {
			local = undotted
		}
	}
	return local + "@" + domain
}

// mailboxMatch returns a WHERE condition with arguments that matches all
// stored spelling variants of the mailbox of email (case,
// "+tag", for Gmail dots and googlemail.com). What is stored
// is the address as entered; equivalent Unicode spellings of other
// inputs cannot be found in the database, only the input itself.
func mailboxMatch(email string) (string, []any) {
	key := mailboxKey(email)
	at := strings.LastIndex(key, "@")
	if at <= 0 {
		return "lower(email) = ?", []any{key}
	}
	local, domain := key[:at], key[at+1:]
	esc := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)
	if domain == gmailDomain && !strings.Contains(local, ".") {
		// Stored are dot variants and both domains: dots are dropped
		// in the comparison, the domain is checked strictly beforehand (otherwise
		// "g.mail.com").
		l := esc.Replace(local)
		return `((lower(email) LIKE ? OR lower(email) LIKE ?) AND ` +
				`(replace(lower(email), '.', '') IN (?, ?) OR replace(lower(email), '.', '') LIKE ? ESCAPE '\' OR replace(lower(email), '.', '') LIKE ? ESCAPE '\'))`,
			[]any{"%@" + gmailDomain, "%@" + googlemailDomain,
				local + "@" + gmailDomainNoDots, local + "@" + googlemailNoDots,
				l + "+%@" + gmailDomainNoDots, l + "+%@" + googlemailNoDots}
	}
	clause := `(lower(email) = ? OR lower(email) LIKE ? ESCAPE '\')`
	args := []any{key, esc.Replace(local) + "+%@" + domain}
	// Non-ASCII inputs are stored raw in the database: their unchanged
	// repetition remains assigned to the limit.
	if raw := strings.ToLower(strings.TrimSpace(email)); raw != key && !isASCII(raw) {
		clause = `(lower(email) = ? OR lower(email) = ? OR lower(email) LIKE ? ESCAPE '\')`
		args = []any{key, raw, args[1]}
	}
	return clause, args
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// maxConfirmationMailsPerMailbox: this many confirmation mails go out at most per mailbox
// within the validity of a link (24 h), across pages.
const maxConfirmationMailsPerMailbox = 3

// confirmationMailBudgetExhausted counts the still-valid pending subscriptions of the
// mailbox across all pages: each stands for a confirmation mail of the last
// 24 hours. A database error counts as exhausted (fail closed: better no
// mail than an unlimited number).
func (h *Handler) confirmationMailBudgetExhausted(email string) bool {
	clause, args := mailboxMatch(email)
	var n int64
	err := h.DB.Model(&models.Subscriber{}).
		Where("channel = 'email' AND status = ? AND confirm_token_expiry > ? AND "+clause, append([]any{models.SubscriberStatusPending, time.Now()}, args...)...).
		Count(&n).Error
	return err != nil || n >= maxConfirmationMailsPerMailbox
}

// isOwnAccountEmail: signed in (userID set) and the subscription address is the
// own account address (case-insensitive).
func isOwnAccountEmail(c *gin.Context, email string) bool {
	return getUserID(c) != 0 && email != "" && strings.EqualFold(getEmail(c), email)
}

// hashSubscriptionToken: only the SHA-256 of the token is stored in the DB. A
// fast hash suffices because the token has 128 bits of randomness (not a password).
func hashSubscriptionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// prepareSubscriptionConfirmation sets status and token fields of a (not yet
// saved) subscription. Returns: the plaintext token for the mail link,
// or "" if no confirmation is needed (webhook channel, own address).
// crypto/rand.Text delivers 128 bits of randomness and (since Go 1.24) cannot
// fail, hence there is no error path here.
func prepareSubscriptionConfirmation(c *gin.Context, sub *models.Subscriber) string {
	if sub.Channel != "email" || isOwnAccountEmail(c, sub.Email) {
		sub.Status = models.SubscriberStatusActive
		sub.ConfirmTokenHash = ""
		sub.ConfirmTokenExpiry = nil
		return ""
	}
	token := rand.Text()
	expiry := time.Now().Add(subscriptionConfirmTTL)
	sub.Status = models.SubscriberStatusPending
	sub.ConfirmTokenHash = hashSubscriptionToken(token)
	sub.ConfirmTokenExpiry = &expiry
	return token
}

// respondSubscribed returns the response for a created or re-requested
// subscription. An already pending subscription deliberately gets the same
// response as a new one, so it is not discernible whether the address has already
// been registered.
func respondSubscribed(c *gin.Context, sub models.Subscriber) {
	msg := subscriptionActiveMessage
	if sub.Status == models.SubscriberStatusPending {
		msg = subscriptionPendingMessage
	}
	c.JSON(http.StatusOK, gin.H{"message": msg, "channel": sub.Channel, "status": sub.Status})
}

// handleExistingSubscription handles POST /api/subscribe for an address
// that already has a subscription on the page. Active subscriptions: the same response as for
// a new subscription (409 only for the own account address). Pending
// subscriptions: the own account address is activated directly. Otherwise a new
// mail is sent only if the old link has expired. This way nobody can
// send a stranger arbitrarily many mails by subscribing repeatedly.
func (h *Handler) handleExistingSubscription(c *gin.Context, req SubscribeRequest, sub models.Subscriber) {
	if sub.Status != models.SubscriberStatusPending {
		// Only someone signed in with their own account address learns that the
		// subscription exists (it is their address, no oracle). Everyone else gets
		// the same response as for a new subscription, without mail and without change.
		if req.Channel == "email" && isOwnAccountEmail(c, req.Email) {
			c.JSON(http.StatusConflict, gin.H{"error": subscriberConflictMessage(req.Channel), "code": subscriberConflictCode(req.Channel)})
			return
		}
		if req.Channel == "email" {
			respondSubscribed(c, models.Subscriber{Channel: "email", Status: models.SubscriberStatusPending})
			return
		}
		respondSubscribed(c, sub)
		return
	}
	own := isOwnAccountEmail(c, sub.Email)
	if !own && sub.ConfirmTokenExpiry != nil && sub.ConfirmTokenExpiry.After(time.Now()) {
		respondSubscribed(c, sub)
		return
	}
	if !own && h.confirmationMailBudgetExhausted(sub.Email) {
		respondSubscribed(c, sub)
		return
	}
	token := prepareSubscriptionConfirmation(c, &sub)
	if own {
		uid := getUserID(c)
		sub.CreatedByUserID = &uid
	}
	if err := h.DB.Save(&sub).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Abonnement fehlgeschlagen", "code": "server_error"})
		return
	}
	if own {
		h.RecordAudit(c, "subscriber.confirmed", "subscriber", "Subscriber", strconv.FormatUint(uint64(sub.ID), 10), sub.Email, "own_account_email")
	}
	h.queueSubscriptionConfirmation(c, sub, token)
	respondSubscribed(c, sub)
}

type confirmSubscriptionRequest struct {
	Token string `json:"token" binding:"required"`
}

// ConfirmSubscriptionHandler (POST /api/public/subscribers/confirm) confirms
// a pending subscription by token. Deliberately POST from a button on the frontend
// page rather than GET directly from the mail: mail gateways with link prefetching
// would otherwise consume the one-time token before the click (as with
// ConfirmOrderHandler). Expired token: 410, the unconfirmed subscription is
// removed in the process (data minimization, the address was never confirmed).
func (h *Handler) ConfirmSubscriptionHandler(c *gin.Context) {
	var in confirmSubscriptionRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Bestätigungstoken fehlt", "code": "confirm_token_missing"})
		return
	}

	var sub models.Subscriber
	if err := h.DB.Where("confirm_token_hash = ? AND status = ?", hashSubscriptionToken(in.Token), models.SubscriberStatusPending).First(&sub).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültiger oder bereits verwendeter Bestätigungslink", "code": "confirm_link_invalid"})
		return
	}
	if sub.ConfirmTokenExpiry == nil || sub.ConfirmTokenExpiry.Before(time.Now()) {
		h.DB.Delete(&sub) //nolint:errcheck // Aufräumen ist Best Effort, die Antwort bleibt 410.
		c.JSON(http.StatusGone, gin.H{"error": "Der Bestätigungslink ist abgelaufen. Bitte abonniere erneut.", "code": "subscription_link_expired"})
		return
	}

	sub.Status = models.SubscriberStatusActive
	sub.ConfirmTokenHash = ""
	sub.ConfirmTokenExpiry = nil
	if err := h.DB.Save(&sub).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Bestätigung fehlgeschlagen", "code": "server_error"})
		return
	}
	h.RecordAudit(c, "subscriber.confirmed", "subscriber", "Subscriber", strconv.FormatUint(uint64(sub.ID), 10), sub.Email, "")

	var page models.StatusPage
	h.DB.First(&page, sub.StatusPageID)
	c.JSON(http.StatusOK, gin.H{"message": "Abonnement bestätigt", "slug": page.Slug})
}

// buildSubscriptionConfirmEmailHTML: default body of the confirmation mail,
// visually like the order confirmation (page_order.go). escPageName is
// already HTML-escaped.
func buildSubscriptionConfirmEmailHTML(lang, escPageName, confirmURL string) string {
	// Sending language de, otherwise en.
	if lang != "de" {
		return fmt.Sprintf(subscriptionConfirmEmailTemplateEN, escPageName, escPageName, confirmURL)
	}
	return fmt.Sprintf(subscriptionConfirmEmailTemplate, escPageName, escPageName, confirmURL)
}

// subscriptionConfirmEmailTemplateEN is the English default text,
// built from the German template with the same placeholders in the same order.
var subscriptionConfirmEmailTemplateEN = strings.NewReplacer(
	"Bitte bestätige dein Abo", "Please confirm your subscription",
	"Jemand hat diese E-Mail-Adresse für Status-Benachrichtigungen von &quot;%s&quot; eingetragen. Benachrichtigungen erhältst du erst, wenn du das Abonnement bestätigst.",
	"Someone entered this email address for status notifications from &quot;%s&quot;. You will only receive notifications once you confirm the subscription.",
	"Abonnement bestätigen", "Confirm subscription",
	"Dieser Link ist 24 Stunden gültig. Falls du das nicht warst, ignoriere diese E-Mail einfach. Ohne Bestätigung bekommst du keine weiteren Nachrichten.",
	"This link is valid for 24 hours. If this was not you, simply ignore this email. Without confirmation you will not receive any further messages.",
).Replace(subscriptionConfirmEmailTemplate)

// subscriptionConfirmEmailTemplate without line breaks (same pattern as sendAlertEmail):
// the CR/LF guard (hasHeaderInjectionChars) also checks the body.
var subscriptionConfirmEmailTemplate = strings.NewReplacer("\n", "").Replace(`<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f5f6f8;padding:32px 16px;">
  <div style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #e6e8ec;border-radius:10px;overflow:hidden;">
    <div style="background:#4c6ef5;padding:20px 24px;">
      <span style="color:#ffffff;font-size:16px;font-weight:600;">%s</span>
    </div>
    <div style="padding:24px;">
      <h2 style="margin:0 0 12px;font-size:18px;color:#1a2230;">Bitte bestätige dein Abo</h2>
      <p style="margin:0 0 16px;color:#57616f;font-size:14px;line-height:1.5;">Jemand hat diese E-Mail-Adresse für Status-Benachrichtigungen von &quot;%s&quot; eingetragen. Benachrichtigungen erhältst du erst, wenn du das Abonnement bestätigst.</p>
      <a href="%s" style="display:inline-block;background:#4c6ef5;color:#ffffff;text-decoration:none;font-size:14px;font-weight:600;padding:10px 20px;border-radius:6px;">Abonnement bestätigen</a>
      <p style="margin:20px 0 0;color:#8b95a3;font-size:12px;">Dieser Link ist 24 Stunden gültig. Falls du das nicht warst, ignoriere diese E-Mail einfach. Ohne Bestätigung bekommst du keine weiteren Nachrichten.</p>
    </div>
  </div>
</div>`)

// renderSubscriptionConfirmTemplate renders a custom confirmation template in
// a fixed frame (page name, notice for recipients who did not
// sign up). A stored template without an unconditional confirmation link
// or with a render error is not used; the default text applies.
func (h *Handler) renderSubscriptionConfirmTemplate(tmpl *models.NotificationTemplate, data NotificationTemplateData, defSubject, defBody string) (string, string) {
	if _, used, err := parseAllowedTemplateFields("body", tmpl.BodyHTML, MaxTemplateBodyLen); err != nil || !used["ConfirmURL"] {
		slog.Error("Subscription confirmation template unusable, falling back to default", "template_id", tmpl.ID, "status_page_id", tmpl.StatusPageID)
		return defSubject, defBody
	}
	subject, inner := applyNotificationTemplate(tmpl, data, "", "")
	if inner == "" {
		return defSubject, defBody
	}
	return subject, wrapSubscriptionConfirmFrame(data.Language, html.EscapeString(data.Title), inner)
}

// wrapSubscriptionConfirmFrame puts the fixed frame around the text of a
// custom confirmation template. Header and footer cannot be changed via the
// template.
func wrapSubscriptionConfirmFrame(lang, escPageName, inner string) string {
	footer := `Falls du das nicht warst, ignoriere diese E-Mail einfach. Ohne Bestätigung bekommst du keine weiteren Nachrichten.`
	if lang != "de" {
		footer = `If this was not you, simply ignore this email. Without confirmation you will not receive any further messages.`
	}
	return `<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f5f6f8;padding:32px 16px;">` +
		`<div style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #e6e8ec;border-radius:10px;overflow:hidden;">` +
		`<div style="background:#4c6ef5;padding:20px 24px;"><span style="color:#ffffff;font-size:16px;font-weight:600;">` + escPageName + `</span></div>` +
		`<div style="padding:24px;">` + inner +
		`<p style="margin:20px 0 0;color:#8b95a3;font-size:12px;">` + footer + `</p></div></div></div>`
}

// queueSubscriptionConfirmation triggers sending the confirmation mail in the
// background (see mail_queue.go), so that the response to the caller depends neither
// on the mail server nor on whether a mail goes out for the address.
// Without a token (no confirmation needed) nothing happens.
//
// The queue limits the share per page and per source IP group, so that
// a single source cannot fill it alone.
func (h *Handler) queueSubscriptionConfirmation(c *gin.Context, sub models.Subscriber, token string) {
	if token == "" {
		return
	}
	h.queueMail("subscription_confirmation", func() { h.sendSubscriptionConfirmation(sub, token) },
		mailPageKey(sub.StatusPageID), mailSourceKey(c.ClientIP()))
}

// sendSubscriptionConfirmation sends the confirmation mail for a
// pending subscription (token != ""). Subject/body come from the template system
// (kind subscription_confirm, audience subscriber, language de as with the
// other subscriber mails), otherwise from the default. A send error leaves
// the subscription in place and lands in the audit log (pattern: sendOrderConfirmationEmail).
// The address has been checked beforehand via isPlainEmailAddress.
func (h *Handler) sendSubscriptionConfirmation(sub models.Subscriber, token string) {
	if token == "" {
		return
	}
	var page models.StatusPage
	h.DB.First(&page, sub.StatusPageID)
	smtpCfg := loadSMTPConfig()
	smtpCfg.applyPageSender(page)
	frontendURL := getFrontendURL()
	confirmURL := frontendURL + "/subscribe/confirm?token=" + url.QueryEscape(token)

	// Template/text in the subscriber's language, otherwise the page's.
	tmpl, lang := h.subscriberMailTemplate(page.ID, NotificationKindSubscriptionConfirm, sub.Language)
	data := NotificationTemplateData{
		Title:       page.Name,
		Language:    lang,
		FrontendURL: frontendURL + "/status/" + page.Slug,
		ConfirmURL:  confirmURL,
		FromAddress: smtpCfg.From,
	}
	subject, body := subscriptionConfirmSubjectPrefixFor(lang)+page.Name,
		buildSubscriptionConfirmEmailHTML(lang, html.EscapeString(page.Name), confirmURL)
	if tmpl != nil {
		subject, body = h.renderSubscriptionConfirmTemplate(tmpl, data, subject, body)
	}

	if hasHeaderInjectionChars(subject) || hasHeaderInjectionChars(body) || hasHeaderInjectionChars(smtpCfg.From) {
		slog.Error("Skipping subscription confirmation mail: subject/body/sender contain invalid CR/LF characters", "subscriber_id", sub.ID)
		return
	}
	msg := smtpCfg.fromHeaders() +
		"To: " + sub.Email + "\n" +
		"Subject: " + subject + "\n" +
		"MIME-Version: 1.0\nContent-Type: text/html; charset=UTF-8\n\n" + body

	subID := strconv.FormatUint(uint64(sub.ID), 10)
	sent, err := h.deliverMail(smtpCfg, sub.Email, []byte(msg), mailAuditTarget{Category: "subscriber", TargetType: "Subscriber", TargetID: subID, TargetName: sub.Email})
	if err != nil {
		slog.Error("Failed to send subscription confirmation mail", "subscriber_id", sub.ID, "smtp_addr", smtpCfg.addr(), "error", err)
		h.RecordSystemAudit("subscriber.confirmation_email_failed", "subscriber", "Subscriber", subID, sub.Email,
			fmt.Sprintf("smtp_addr=%s error=%s", smtpCfg.addr(), err.Error()))
		return
	}
	if !sent {
		return
	}
	slog.Info("Subscription confirmation mail sent", "subscriber_id", sub.ID, "smtp_addr", smtpCfg.addr())
}
