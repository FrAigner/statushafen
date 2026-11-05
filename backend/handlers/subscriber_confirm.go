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
// Double-Opt-in für E-Mail-Abos.
//
// Ein neues E-Mail-Abo startet als "pending" und wird erst über den Link in
// der Bestätigungsmail aktiv. Sonst könnte jeder eine fremde Adresse
// eintragen und dem Inhaber ungefragt Vorfall-Mails schicken lassen.
//
// Entscheidung des Betreibers (vom 24.10.): Wer angemeldet
// ist und mit der eigenen Konto-Adresse abonniert, überspringt die Bestätigung.
// Konto-Adressen gelten als verifiziert: Sie entstehen über eine Einladung
// durch einen Admin, über SSO (nur verifizierte Provider-Adressen, siehe
// auth_sso.go) oder über die per Mail bestätigte Bestellung (page_order.go).
//
// Webhook-Kanäle (google_chat) brauchen laut Vorgabe eine eigene Entscheidung
// und bleiben deshalb vorerst ohne Bestätigung aktiv.
// ─────────────────────────────────────────────────────────────────────────────

// subscriptionConfirmTTL: so lange gilt ein Bestätigungslink (wie orderTokenTTL).
const subscriptionConfirmTTL = 24 * time.Hour

const (
	subscriptionPendingMessage = "Fast geschafft: Bitte bestätige dein Abo über den Link, den wir dir per E-Mail geschickt haben."
	subscriptionActiveMessage  = "Erfolgreich abonniert"
	// Default-Betreff je Versandsprache (Standardsprache der Seite,
	// Subscriber haben keine gespeicherte Sprache).
	subscriptionConfirmSubjectPrefix   = "Bitte bestätige dein Abo für "
	subscriptionConfirmSubjectPrefixEN = "Please confirm your subscription for "
)

func subscriptionConfirmSubjectPrefixFor(lang string) string {
	if lang == "de" {
		return subscriptionConfirmSubjectPrefix
	}
	return subscriptionConfirmSubjectPrefixEN
}

// maxEmailAddressLength: längste zulässige Adresse (RFC 5321).
const maxEmailAddressLength = 254

// isPlainEmailAddress akzeptiert nur eine nackte Adresse ("a@b.example"),
// keine Anzeigenamen-Form ("Name <a@b.example>"). Damit ist der gespeicherte
// Wert direkt als Empfänger nutzbar und kann keine CR/LF enthalten.
func isPlainEmailAddress(s string) bool {
	if len(s) > maxEmailAddressLength {
		return false
	}
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}

// Gmail-Domains: Gmail liefert googlemail.com wie gmail.com aus und ignoriert
// Punkte im lokalen Teil. Nur für diese beiden Domains gilt die Punkt-Regel;
// bei allen anderen Anbietern sind Punkte Teil der Adresse.
const (
	gmailDomain       = "gmail.com"
	googlemailDomain  = "googlemail.com"
	gmailDomainNoDots = "gmailcom"

	googlemailNoDots = "googlemailcom"
)

// mailboxKey ist der Schlüssel für Deduplizierung und das Limit der
// Bestätigungsmails je Postfach: Unicode-NFKC, Kleinschreibung, Domain als
// Punycode und ohne "+tag" im lokalen Teil ("Opfer+news@Example.com" ->
// "opfer@example.com"). Bei gmail.com und googlemail.com zusätzlich ohne Punkte
// im lokalen Teil und mit googlemail.com als gmail.com. Gespeichert und
// angeschrieben wird weiter die eingegebene Adresse; der Schlüssel dient nur
// dem Vergleich, damit Schreibvarianten derselben Mailbox nicht als fremde
// Adressen durchgehen.
func mailboxKey(email string) string {
	e := strings.ToLower(norm.NFKC.String(strings.TrimSpace(email)))
	at := strings.LastIndex(e, "@")
	if at <= 0 {
		return e
	}
	local, domain := e[:at], e[at+1:]
	// Unicode- und Punycode-Schreibweise derselben Domain werden gleich; was
	// sich nicht wandeln lässt, bleibt in Kleinschreibung.
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

// mailboxMatch liefert eine WHERE-Bedingung samt Argumenten, die alle
// gespeicherten Schreibvarianten des Postfachs von email trifft (Groß-/
// Kleinschreibung, "+tag", bei Gmail Punkte und googlemail.com). Gespeichert
// ist die eingegebene Adresse; gleichwertige Unicode-Schreibweisen anderer
// Eingaben sind in der Datenbank nicht auffindbar, nur die Eingabe selbst.
func mailboxMatch(email string) (string, []any) {
	key := mailboxKey(email)
	at := strings.LastIndex(key, "@")
	if at <= 0 {
		return "lower(email) = ?", []any{key}
	}
	local, domain := key[:at], key[at+1:]
	esc := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)
	if domain == gmailDomain && !strings.Contains(local, ".") {
		// Gespeichert sind Punkt-Varianten und beide Domains: Punkte entfallen
		// im Vergleich, die Domain wird vorher fest geprüft (sonst träfe auch
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
	// Nicht-ASCII-Eingaben stehen roh in der Datenbank: ihre unveränderte
	// Wiederholung bleibt dem Limit zugeordnet.
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

// maxConfirmationMailsPerMailbox: So viele Bestätigungsmails gehen je Postfach
// innerhalb der Gültigkeit eines Links (24 h) höchstens raus, seitenübergreifend.
const maxConfirmationMailsPerMailbox = 3

// confirmationMailBudgetExhausted zählt die noch gültigen wartenden Abos des
// Postfachs über alle Seiten: Jedes steht für eine Bestätigungsmail der letzten
// 24 Stunden. Ein Datenbankfehler gilt als erschöpft (fail closed: lieber keine
// Mail als unbegrenzt viele).
func (h *Handler) confirmationMailBudgetExhausted(email string) bool {
	clause, args := mailboxMatch(email)
	var n int64
	err := h.DB.Model(&models.Subscriber{}).
		Where("channel = 'email' AND status = ? AND confirm_token_expiry > ? AND "+clause, append([]any{models.SubscriberStatusPending, time.Now()}, args...)...).
		Count(&n).Error
	return err != nil || n >= maxConfirmationMailsPerMailbox
}

// isOwnAccountEmail: angemeldet (userID gesetzt) und die Abo-Adresse ist die
// eigene Konto-Adresse (Groß-/Kleinschreibung egal).
func isOwnAccountEmail(c *gin.Context, email string) bool {
	return getUserID(c) != 0 && email != "" && strings.EqualFold(getEmail(c), email)
}

// hashSubscriptionToken: In der DB steht nur der SHA-256 des Tokens. Ein
// schneller Hash genügt, da der Token 128 Bit Zufall hat (kein Passwort).
func hashSubscriptionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// prepareSubscriptionConfirmation setzt Status und Token-Felder eines (noch
// nicht gespeicherten) Abos. Rückgabe: der Klartext-Token für den Mail-Link,
// oder "", wenn keine Bestätigung nötig ist (Webhook-Kanal, eigene Adresse).
// crypto/rand.Text liefert 128 Bit Zufall und kann (seit Go 1.24) nicht
// fehlschlagen, deshalb gibt es hier keinen Fehlerpfad.
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

// respondSubscribed liefert die Antwort für ein angelegtes bzw. erneut
// angefragtes Abo. Ein bereits wartendes Abo bekommt bewusst dieselbe
// Antwort wie ein neues, damit nicht erkennbar ist, ob die Adresse schon
// eingetragen wurde.
func respondSubscribed(c *gin.Context, sub models.Subscriber) {
	msg := subscriptionActiveMessage
	if sub.Status == models.SubscriberStatusPending {
		msg = subscriptionPendingMessage
	}
	c.JSON(http.StatusOK, gin.H{"message": msg, "channel": sub.Channel, "status": sub.Status})
}

// handleExistingSubscription behandelt POST /api/subscribe für eine Adresse,
// die auf der Seite schon ein Abo hat. Aktive Abos: dieselbe Antwort wie für
// ein neues Abo (409 nur für die eigene Konto-Adresse). Wartende
// Abos: Die eigene Konto-Adresse wird direkt aktiviert. Sonst gibt es nur
// dann eine neue Mail, wenn der alte Link abgelaufen ist. So kann niemand
// einem Fremden über wiederholtes Abonnieren beliebig viele Mails schicken.
func (h *Handler) handleExistingSubscription(c *gin.Context, req SubscribeRequest, sub models.Subscriber) {
	if sub.Status != models.SubscriberStatusPending {
		// Nur wer mit der eigenen Konto-Adresse angemeldet ist, erfährt, dass das
		// Abo besteht (es ist seine Adresse, kein Orakel). Alle anderen bekommen
		// dieselbe Antwort wie für ein neues Abo, ohne Mail und ohne Änderung.
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

// ConfirmSubscriptionHandler (POST /api/public/subscribers/confirm) bestätigt
// ein wartendes Abo per Token. Bewusst POST aus einem Button der Frontend-
// Seite statt GET direkt aus der Mail: Mail-Gateways mit Link-Prefetching
// würden den Einmal-Token sonst vor dem Klick verbrauchen (wie bei
// ConfirmOrderHandler). Abgelaufener Token: 410, das unbestätigte Abo wird
// dabei entfernt (Datensparsamkeit, die Adresse wurde nie bestätigt).
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

// buildSubscriptionConfirmEmailHTML: Default-Body der Bestätigungsmail,
// optisch wie die Bestellbestätigung (page_order.go). escPageName ist
// bereits HTML-escaped.
func buildSubscriptionConfirmEmailHTML(lang, escPageName, confirmURL string) string {
	// Versandsprache de, sonst en.
	if lang != "de" {
		return fmt.Sprintf(subscriptionConfirmEmailTemplateEN, escPageName, escPageName, confirmURL)
	}
	return fmt.Sprintf(subscriptionConfirmEmailTemplate, escPageName, escPageName, confirmURL)
}

// subscriptionConfirmEmailTemplateEN ist der englische Standardtext,
// gebaut aus der deutschen Vorlage mit denselben Platzhaltern in derselben Reihenfolge.
var subscriptionConfirmEmailTemplateEN = strings.NewReplacer(
	"Bitte bestätige dein Abo", "Please confirm your subscription",
	"Jemand hat diese E-Mail-Adresse für Status-Benachrichtigungen von &quot;%s&quot; eingetragen. Benachrichtigungen erhältst du erst, wenn du das Abonnement bestätigst.",
	"Someone entered this email address for status notifications from &quot;%s&quot;. You will only receive notifications once you confirm the subscription.",
	"Abonnement bestätigen", "Confirm subscription",
	"Dieser Link ist 24 Stunden gültig. Falls du das nicht warst, ignoriere diese E-Mail einfach. Ohne Bestätigung bekommst du keine weiteren Nachrichten.",
	"This link is valid for 24 hours. If this was not you, simply ignore this email. Without confirmation you will not receive any further messages.",
).Replace(subscriptionConfirmEmailTemplate)

// subscriptionConfirmEmailTemplate ohne Zeilenumbrüche: sendAlertEmail-
// Muster, der CR/LF-Guard (hasHeaderInjectionChars) prüft auch den Body.
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

// renderSubscriptionConfirmTemplate rendert eine eigene Bestätigungsvorlage in
// einem festen Rahmen (Seitenname, Hinweis für Empfänger, die sich nicht
// angemeldet haben). Eine gespeicherte Vorlage ohne unbedingten Bestätigungslink
// oder mit Renderfehler wird nicht verwendet; es gilt der Standardtext.
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

// wrapSubscriptionConfirmFrame legt den festen Rahmen um den Text einer
// eigenen Bestätigungsvorlage. Kopf und Fußzeile lassen sich nicht über die
// Vorlage ändern.
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

// queueSubscriptionConfirmation stößt den Versand der Bestätigungsmail im
// Hintergrund an (siehe mail_queue.go), damit die Antwort an den Aufrufer weder
// vom Mailserver noch davon abhängt, ob für die Adresse eine Mail rausgeht.
// Ohne Token (keine Bestätigung nötig) passiert nichts.
//
// Die Warteschlange begrenzt den Anteil je Seite und je Quell-IP-Gruppe, damit
// eine einzelne Quelle sie nicht allein füllt.
func (h *Handler) queueSubscriptionConfirmation(c *gin.Context, sub models.Subscriber, token string) {
	if token == "" {
		return
	}
	h.queueMail("subscription_confirmation", func() { h.sendSubscriptionConfirmation(sub, token) },
		mailPageKey(sub.StatusPageID), mailSourceKey(c.ClientIP()))
}

// sendSubscriptionConfirmation verschickt die Bestätigungsmail für ein
// wartendes Abo (token != ""). Subject/Body kommen aus dem Template-System
// (Kind subscription_confirm, Audience subscriber, Sprache de wie bei den
// übrigen Subscriber-Mails), sonst aus dem Default. Ein Versandfehler lässt
// das Abo bestehen und landet im Audit-Log (Muster: sendOrderConfirmationEmail).
// Die Adresse ist vorher per isPlainEmailAddress geprüft.
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

	// Vorlage/Text in der Sprache des Abonnenten, sonst der Seite.
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
