package handlers

import (
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ─────────────────────────────────────────────────────────────────────────────
// Self-service ordering of new status pages via the public landing page
// ("gateway" homepage). Flow:
//
//  1. POST /api/public/orders          -> CreateOrderHandler
//     creates a PageOrder("pending_confirmation") and sends a
//     confirmation email. Deliberately NO user is created yet - otherwise
//     the public form itself would already be a vector for arbitrarily many
//     empty user rows.
//  2. POST /api/public/orders/confirm  -> ConfirmOrderHandler
//     creates StatusPage + User (without password) + UserStatusPageRole("Page
//     Admin") and returns a fresh set-password token.
//  3. POST /api/public/orders/set-password -> SetOrderPasswordHandler
//     sets the password, after which a normal login via /api/login is
//     possible (deliberately no auto-login here, see handler comment).
//
// Abuse protection (without a third-party captcha, see the main.go registration
// for the concrete rate limits): IP rate limiting + honeypot field + mandatory
// email confirmation via one-time link + cap on simultaneously open
// orders per email address.
// ─────────────────────────────────────────────────────────────────────────────

const (
	// maxPendingOrdersPerEmail limits how many simultaneously unconfirmed
	// orders an email address may have - prevents a
	// single address from being flooded with confirmation emails or abused for
	// arbitrarily many slug reservations.
	maxPendingOrdersPerEmail = 3
	// orderTokenTTL applies to both the confirmation token and the separate
	// generated set-password token.
	orderTokenTTL = 24 * time.Hour

	// orderExpiredRetention: how long an order marked as expired
	// (with its email address) is still kept before the
	// cleanup job deletes it permanently.
	orderExpiredRetention = 30 * 24 * time.Hour

	// orderPendingExpiredSQL: condition (a time parameter "now") under which
	// an unconfirmed order counts as expired. Without an expiry time it
	// counts as expired, as in ConfirmOrderHandler.
	orderPendingExpiredSQL = "status = 'pending_confirmation' AND (confirm_token_expiry IS NULL OR confirm_token_expiry < ?)"

	// Length limits for the inputs of the public order (characters, not bytes).
	// The name appears in the confirmation email and as the page name; the slug
	// follows the documented maximum of 60 characters.
	maxOrderNameLength = 120
	maxOrderSlugLength = 60

	orderCreatedMessage = "Bitte bestätige deine Email-Adresse über den Link, den wir dir geschickt haben."
)

// errSlugTaken signals, within the ConfirmOrderHandler transaction, a
// slug collision that can only be finally checked there (after the transaction
// is opened) - see comment in ConfirmOrderHandler.
var errSlugTaken = errors.New("slug bereits vergeben")

// errEmailTaken signals, within the same transaction, that a user account
// already exists for order.Email (security check):
// ConfirmOrderHandler must NEVER attach itself to an existing account - the
// subsequent SetOrderPasswordHandler would otherwise overwrite its PasswordHash
// (including a Global Admin, without any owner or SSO check). As
// with errSlugTaken, the check must run inside the transaction (not
// before), otherwise the same race gap arises between two concurrent
// confirmations.
var errEmailTaken = errors.New("email bereits vergeben")

// errOrderLinkUsed: A concurrent request has already consumed the set-password link
// between loading and redeeming. errAccountAlreadySecured: The
// account received a password or SSO between the check and the write.
var (
	errOrderLinkUsed         = errors.New("link bereits verbraucht")
	errAccountAlreadySecured = errors.New("konto bereits eingerichtet")
)

type createOrderRequest struct {
	CompanyName string `json:"company_name" binding:"required"`
	Email       string `json:"email" binding:"required"`
	Slug        string `json:"slug"`
	// Website: honeypot field. Real users never see or fill it
	// (hidden via CSS in the form) - a filled value means a bot.
	Website string `json:"website"`
}

type confirmOrderRequest struct {
	Token string `json:"token" binding:"required"`
}

type setOrderPasswordRequest struct {
	Token    string `json:"token" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// rejectInvalidOrderFields answers overlong fields and invalid characters
// (NUL, invalid UTF-8) with 400 and an error code and returns true if the
// request is thereby finished.
func rejectInvalidOrderFields(c *gin.Context, in createOrderRequest) bool {
	name, slug, email := strings.TrimSpace(in.CompanyName), strings.TrimSpace(in.Slug), strings.TrimSpace(in.Email)
	switch {
	case utf8.RuneCountInString(email) > maxEmailLength:
		abortInvalidInput(c, codeEmailTooLong, fmt.Sprintf("Email-Adresse darf höchstens %d Zeichen lang sein", maxEmailLength))
	case utf8.RuneCountInString(name) > maxOrderNameLength:
		abortInvalidInput(c, codeOrderNameTooLong, fmt.Sprintf("Firmenname/Seitenname darf höchstens %d Zeichen lang sein", maxOrderNameLength))
	case utf8.RuneCountInString(slug) > maxOrderSlugLength:
		abortInvalidInput(c, codeSlugTooLong, fmt.Sprintf("Slug darf höchstens %d Zeichen lang sein", maxOrderSlugLength))
	case containsInvalidText(name, slug, email):
		abortInvalidInput(c, codeInvalidCharacters, "Eingabe enthält ungültige Zeichen")
	default:
		return false
	}
	return true
}

// POST /api/public/orders — form "Order your own status page".
func (h *Handler) CreateOrderHandler(c *gin.Context) {
	// If self-service ordering is globally disabled, processing is aborted here
	// before anything else (default "on", only an explicit
	// orders.enabled=false blocks). Deliberately 403 instead of the honeypot success response.
	if !h.getSystemSettingBool(settingOrdersEnabled, true) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Die Self-Service-Bestellung ist derzeit nicht verfügbar.", "code": "orders_disabled"})
		return
	}

	var in createOrderRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		respondInvalidInput(c)
		return
	}

	// Honeypot: identical success response as the real case, but without any
	// side effect - a bot gets no signal that it was detected.
	if in.Website != "" {
		h.RecordSystemAudit("pageorder.honeypot_triggered", "auth", "PageOrder", "", limitEmail(in.Email), "ip="+c.ClientIP())
		c.JSON(http.StatusCreated, gin.H{"message": orderCreatedMessage})
		return
	}

	// Check length and characters before anything is parsed, searched or stored.
	if rejectInvalidOrderFields(c, in) {
		return
	}

	addr, err := mail.ParseAddress(in.Email)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige Email-Adresse", "code": "email_invalid"})
		return
	}
	email := addr.Address

	companyName := strings.TrimSpace(in.CompanyName)
	if companyName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Firmenname/Seitenname darf nicht leer sein", "code": "name_required"})
		return
	}

	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if slug != "" {
		if !validSlugRegex.MatchString(slug) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Slug darf nur Kleinbuchstaben, Ziffern und einzelne Bindestriche enthalten", "code": "slug_invalid"})
			return
		}
		if isReservedSlug(slug) {
			abortInvalidInput(c, codeSlugReserved, slugReservedMessage)
			return
		}
		var existingPage models.StatusPage
		if err := h.DB.Where(slugWhereClause, slug).First(&existingPage).Error; err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Slug wird bereits von einer anderen Statusseite verwendet", "code": "slug_taken"})
			return
		}
		var existingOrder models.PageOrder
		// A never-confirmed order with an expired link no longer blocks the slug,
		// even if the cleanup job has not picked it up yet.
		if err := h.DB.Where("slug = ? AND status <> ? AND NOT ("+orderPendingExpiredSQL+")", slug, "expired", time.Now()).First(&existingOrder).Error; err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Slug wird bereits in einer anderen Bestellung verwendet", "code": "slug_taken_order"})
			return
		}
	}

	// The limit applies per mailbox (case and "+tag" do not count
	// as a different address), not per character string.
	var pendingCount int64
	mailboxCond, mailboxArgs := mailboxMatch(email)
	h.DB.Model(&models.PageOrder{}).
		Where(mailboxCond+" AND status = ? AND NOT ("+orderPendingExpiredSQL+")", append(mailboxArgs, "pending_confirmation", time.Now())...).
		Count(&pendingCount)
	if pendingCount >= maxPendingOrdersPerEmail {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": "Zu viele offene Bestellungen für diese Email-Adresse. Bitte bestätige eine bestehende Bestellung oder warte, bis sie abläuft.",
			"code":  "too_many_orders",
		})
		return
	}

	token, err := generateWebhookToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Erzeugen des Bestätigungslinks"})
		return
	}
	expiry := time.Now().Add(orderTokenTTL)

	order := models.PageOrder{
		CompanyName:        companyName,
		Email:              email,
		Slug:               slug,
		Status:             "pending_confirmation",
		ConfirmTokenHash:   models.HashToken(token),
		ConfirmTokenExpiry: &expiry,
		RequestIP:          c.ClientIP(),
	}
	if err := h.DB.Create(&order).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Anlegen der Bestellung", "code": "server_error"})
		return
	}

	// Mail sending errors must not make the order itself fail -
	// the row already exists, a resend endpoint is deliberately out of
	// scope (it would itself be a new abuse vector). Sending runs
	// via the bounded background queue (mail_queue.go): the endpoint
	// is anonymous, a hanging mail server would otherwise tie up the request until the
	// overall SMTP deadline, and the response time would reveal whether a mail goes out.
	smtpCfg := loadSMTPConfig()
	h.queueMail("order_confirmation", func() { h.sendOrderConfirmationEmail(smtpCfg, order, token) }, mailSourceKey(c.ClientIP()))

	h.RecordSystemAudit("pageorder.created", "auth", "PageOrder", strconv.FormatUint(uint64(order.ID), 10), order.Email, "")
	c.JSON(http.StatusCreated, gin.H{"message": orderCreatedMessage})
}

// POST /api/public/orders/confirm — triggered by the explicit "Jetzt bestätigen"
// button on the frontend confirm page (deliberately not a GET link
// directly from the mail, so that mail gateways with link prefetching do not
// consume the one-time token before the actual click).
func (h *Handler) ConfirmOrderHandler(c *gin.Context) {
	var in confirmOrderRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		respondInvalidInput(c)
		return
	}

	var order models.PageOrder
	if err := h.DB.Where("confirm_token = ? AND confirm_token <> ''", models.HashToken(in.Token)).First(&order).Error; err != nil ||
		order.Status != "pending_confirmation" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültiger oder bereits verwendeter Bestätigungslink", "code": "confirm_link_invalid"})
		return
	}
	if order.ConfirmTokenExpiry == nil || order.ConfirmTokenExpiry.Before(time.Now()) {
		order.Status = "expired"
		h.DB.Save(&order) //nolint:errcheck
		c.JSON(http.StatusGone, gin.H{"error": "Der Bestätigungslink ist abgelaufen. Bitte bestelle die Statusseite erneut.", "code": "order_link_expired"})
		return
	}

	var pageAdminRole models.Role
	if err := h.DB.Where("name = ?", "Page Admin").First(&pageAdminRole).Error; err != nil {
		slog.Error("Page Admin role missing - SeedData() nicht gelaufen?", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Anlegen der Statusseite", "code": "server_error"})
		return
	}

	setPasswordToken, err := generateWebhookToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Anlegen der Statusseite"})
		return
	}
	setPasswordExpiry := time.Now().Add(orderTokenTTL)

	var page models.StatusPage
	var user models.User

	txErr := h.DB.Transaction(func(tx *gorm.DB) error {
		slug := order.Slug
		if slug == "" {
			slug = h.uniqueSlug(slugify(order.CompanyName))
		} else {
			var existing models.StatusPage
			// Defensive: check again inside the transaction in case the
			// slug was assigned elsewhere between order and confirmation
			// (the uniqueIndex on StatusPage.Slug catches the rest).
			if err := tx.Where(slugWhereClause, slug).First(&existing).Error; err == nil {
				return errSlugTaken
			}
		}

		page = models.StatusPage{Name: order.CompanyName, Slug: slug}
		if err := tx.Create(&page).Error; err != nil {
			if isUniqueViolation(err) { // Race after the check above
				return errSlugTaken
			}
			return err
		}
		if err := database.SeedDefaultStatusPageLanguages(tx, page.ID); err != nil {
			slog.Error("Failed to seed default languages for self-service status page", "status_page_id", page.ID, "error", err)
		}

		// Security check: if a user with this
		// email already exists, the order is rejected instead of attaching to the existing
		// account - SetOrderPasswordHandler could otherwise overwrite its
		// password. A separate, owner-verified
		// password reset flow is a feature of its own and not part of
		// this fix.
		// Unscoped: soft-deleted accounts also occupy the email (uniqueIndex),
		// otherwise Create below would fail with 500.
		if err := tx.Unscoped().Where("email = ?", order.Email).First(&user).Error; err == nil {
			return errEmailTaken
		}
		user = models.User{Email: order.Email, RoleID: pageAdminRole.ID}
		if err := tx.Create(&user).Error; err != nil {
			if isUniqueViolation(err) {
				return errEmailTaken
			}
			return err
		}

		userRole := models.UserStatusPageRole{UserID: user.ID, StatusPageID: page.ID, RoleID: pageAdminRole.ID}
		if err := tx.Create(&userRole).Error; err != nil {
			return err
		}

		order.Status = "confirmed"
		order.StatusPageID = &page.ID
		order.UserID = &user.ID
		order.ConfirmTokenHash = ""
		order.ConfirmTokenExpiry = nil
		order.SetPasswordTokenHash = models.HashToken(setPasswordToken)
		order.SetPasswordTokenExpiry = &setPasswordExpiry
		// Without password login there is no set-password step: the account
		// stays without a password and its owner signs in through a global
		// OIDC provider with the ordered (and now confirmed) e-mail address.
		if !passwordLoginEnabled() {
			order.Status = "active"
			order.SetPasswordTokenHash = ""
			order.SetPasswordTokenExpiry = nil
			setPasswordToken = ""
		}
		return tx.Save(&order).Error
	})

	if txErr != nil {
		if errors.Is(txErr, errSlugTaken) {
			c.JSON(http.StatusConflict, gin.H{"error": "Slug wird bereits von einer anderen Statusseite verwendet", "code": "slug_taken"})
			return
		}
		if errors.Is(txErr, errEmailTaken) {
			c.JSON(http.StatusConflict, gin.H{"error": "Für diese Email-Adresse existiert bereits ein Konto", "code": "email_taken"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Anlegen der Statusseite", "code": "server_error"})
		return
	}

	h.RecordSystemAudit("pageorder.confirmed", "config", "StatusPage",
		strconv.FormatUint(uint64(page.ID), 10), page.Name, "order_id="+strconv.FormatUint(uint64(order.ID), 10))

	c.JSON(http.StatusOK, gin.H{"set_password_token": setPasswordToken, "slug": page.Slug})
}

// POST /api/public/orders/set-password — last step: set the password,
// after which a normal login via /api/login is possible. Deliberately no
// automatic login here (no JWT issuing) - the already hardened
// login path is reused instead of creating a session
// at a new, unchecked place.
func (h *Handler) SetOrderPasswordHandler(c *gin.Context) {
	if rejectDisabledPasswordLogin(c) {
		return
	}
	var in setOrderPasswordRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		respondInvalidInput(c)
		return
	}
	if rejectInvalidPassword(c, in.Password) {
		return
	}

	var order models.PageOrder
	if err := h.DB.Where("set_password_token = ? AND set_password_token <> '' AND status = ?", models.HashToken(in.Token), "confirmed").
		First(&order).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültiger oder bereits verwendeter Link", "code": "password_link_invalid"})
		return
	}
	if order.SetPasswordTokenExpiry == nil || order.SetPasswordTokenExpiry.Before(time.Now()) {
		c.JSON(http.StatusGone, gin.H{"error": "Der Link zum Setzen des Passworts ist abgelaufen.", "code": "password_link_expired"})
		return
	}
	if order.UserID == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Setzen des Passworts", "code": "server_error"})
		return
	}

	var user models.User
	if err := h.DB.First(&user, *order.UserID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Setzen des Passworts", "code": "server_error"})
		return
	}
	// Orders confirmed before the fix in ConfirmOrderHandler
	// may still point to an existing account belonging to someone else.
	// Only an account that freshly originates from this order is overwritten:
	// ConfirmOrderHandler creates it without hash, without SSO and without
	// Global Admin right. Anything else: 409, nothing is changed.
	if user.PasswordHash != "" || user.SSOProvider != "" || user.IsGlobalAdmin {
		c.JSON(http.StatusConflict, gin.H{"error": "Für dieses Konto ist bereits eine Anmeldung eingerichtet", "code": "password_already_set"})
		return
	}
	// Hash before any change: if it fails, account and order remain
	// untouched and the link stays usable.
	hash, err := hashPassword(in.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Setzen des Passworts", "code": "server_error"})
		return
	}
	// Consume the link and set the password in one transaction. The link is
	// claimed via a conditional UPDATE (token still set, status confirmed) and
	// the row count is checked: with concurrent redemptions exactly one wins,
	// the others see 0 rows. The account is likewise written only conditionally
	// (still without password, without SSO, not Global Admin). If
	// a step fails, everything is rolled back and the link stays usable.
	txErr := h.DB.Transaction(func(tx *gorm.DB) error {
		claim := tx.Model(&models.PageOrder{}).
			Where("id = ? AND set_password_token = ? AND set_password_token <> '' AND status = ?", order.ID, models.HashToken(in.Token), "confirmed").
			Updates(map[string]any{"status": "active", "set_password_token": "", "set_password_token_expiry": nil})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return errOrderLinkUsed
		}
		write := tx.Model(&models.User{}).
			Where("id = ? AND (password_hash IS NULL OR password_hash = '') AND (sso_provider IS NULL OR sso_provider = '') AND is_global_admin = ?", user.ID, false).
			Update("password_hash", hash)
		if write.Error != nil {
			return write.Error
		}
		if write.RowsAffected != 1 {
			return errAccountAlreadySecured
		}
		return nil
	})
	switch {
	case errors.Is(txErr, errOrderLinkUsed):
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültiger oder bereits verwendeter Link", "code": "password_link_invalid"})
		return
	case errors.Is(txErr, errAccountAlreadySecured):
		c.JSON(http.StatusConflict, gin.H{"error": "Für dieses Konto ist bereits eine Anmeldung eingerichtet", "code": "password_already_set"})
		return
	case txErr != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Setzen des Passworts", "code": "server_error"})
		return
	}

	slug := ""
	if order.StatusPageID != nil {
		var page models.StatusPage
		if err := h.DB.First(&page, *order.StatusPageID).Error; err == nil {
			slug = page.Slug
		}
	}

	h.RecordSystemAudit("pageorder.activated", "auth", "User", strconv.FormatUint(uint64(user.ID), 10), user.Email, "")
	c.JSON(http.StatusOK, gin.H{"slug": slug})
}

// pageOrderConfirmSubject: fixed German subject, analogous to the other
// transactional emails (no language selection - the app is de-first, see
// i18n default in frontend/src/i18n/index.ts).
const pageOrderConfirmSubject = "Bitte bestätige deine Statushafen-Bestellung"

// rawPageOrderConfirmEmailTemplate is the readably formatted HTML body of the
// confirmation email (placeholders: company name, confirmation link).
const rawPageOrderConfirmEmailTemplate = `<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f5f6f8;padding:32px 16px;">
  <div style="max-width:520px;margin:0 auto;background:#ffffff;border:1px solid #e6e8ec;border-radius:10px;overflow:hidden;">
    <div style="background:#4c6ef5;padding:20px 24px;">
      <span style="color:#ffffff;font-size:16px;font-weight:600;">Statushafen</span>
    </div>
    <div style="padding:24px;">
      <h2 style="margin:0 0 12px;font-size:18px;color:#1a2230;">Willkommen bei Statushafen!</h2>
      <p style="margin:0 0 16px;color:#57616f;font-size:14px;line-height:1.5;">Bitte bestätige deine Bestellung für die Statusseite &quot;%s&quot;, damit wir sie automatisch für dich anlegen können.</p>
      <a href="%s" style="display:inline-block;background:#4c6ef5;color:#ffffff;text-decoration:none;font-size:14px;font-weight:600;padding:10px 20px;border-radius:6px;">Jetzt bestätigen</a>
      <p style="margin:20px 0 0;color:#8b95a3;font-size:12px;">Dieser Link ist 24 Stunden gültig. Falls du diese Bestellung nicht ausgelöst hast, ignoriere diese Email einfach.</p>
    </div>
  </div>
</div>`

// pageOrderConfirmEmailTemplate is rawPageOrderConfirmEmailTemplate without the
// formatting line breaks/tabs. The CR/LF guard
// (hasHeaderInjectionChars) checks the finished body and always triggered on the
// multi-line raw string - the mail was never sent. Instead of
// weakening the guard, we strip the line breaks once at package init
// (same pattern as notificationEmailTemplate in incident.go); CR/LF
// injected via user input is still detected.
var pageOrderConfirmEmailTemplate = strings.NewReplacer("\n", "", "\t", "").Replace(rawPageOrderConfirmEmailTemplate)

// sendOrderConfirmationEmail follows the same structure as sendAlertEmail
// (incident.go): mail.ParseAddress, header injection guard, raw
// net/smtp.SendMail message via the global SMTP configuration. Deliberately
// a standalone function instead of a call to sendAlertEmail, since
// that one is tightly coupled to incident audit fields.
func (h *Handler) sendOrderConfirmationEmail(smtpCfg smtpConfig, order models.PageOrder, token string) {
	addr, err := mail.ParseAddress(order.Email)
	if err != nil {
		slog.Error("Skipping order confirmation mail: invalid recipient address", "email", order.Email, "error", err)
		return
	}
	recipient := addr.Address

	confirmLink := getFrontendURL() + "/order/confirm?token=" + token
	bodyHTML := fmt.Sprintf(pageOrderConfirmEmailTemplate, html.EscapeString(order.CompanyName), confirmLink)

	if hasHeaderInjectionChars(pageOrderConfirmSubject) || hasHeaderInjectionChars(bodyHTML) || hasHeaderInjectionChars(smtpCfg.From) {
		slog.Error("Skipping order confirmation mail: subject/body/sender contain invalid CR/LF characters", "email", recipient)
		return
	}

	subject := "Subject: " + pageOrderConfirmSubject + "\n"
	mime := "MIME-Version: 1.0\nContent-Type: text/html; charset=UTF-8\n\n"
	msg := "From: " + smtpCfg.From + "\n" +
		"To: " + recipient + "\n" +
		subject + mime + bodyHTML

	orderID := strconv.FormatUint(uint64(order.ID), 10)
	sent, err := h.deliverMail(smtpCfg, recipient, []byte(msg), mailAuditTarget{Category: "auth", TargetType: "PageOrder", TargetID: orderID, TargetName: order.Email})
	if err != nil {
		slog.Error("Failed to send order confirmation mail", "email", recipient, "smtp_addr", smtpCfg.addr(), "error", err)
		h.RecordSystemAudit("pageorder.email_failed", "auth", "PageOrder", orderID, order.Email,
			fmt.Sprintf("recipient=%s smtp_addr=%s error=%s", recipient, smtpCfg.addr(), err.Error()))
		return
	}
	if !sent {
		return
	}
	slog.Info("Order confirmation email sent via SMTP", "email", recipient, "smtp_addr", smtpCfg.addr())
}

// pruneExpiredOrders cleans up orders: unconfirmed orders with an
// expired link are set to "expired" (token invalidated), those expired for
// longer are deleted permanently. Idempotent; runs in the maintenance tick.
// Without the job, unconfirmed orders would only expire when their link is redeemed
// and would hold an address's slug and order quota indefinitely.
func pruneExpiredOrders(db *gorm.DB, now time.Time) (marked, deleted int64, err error) {
	res := db.Model(&models.PageOrder{}).Where(orderPendingExpiredSQL, now).
		Updates(map[string]any{"status": "expired", "confirm_token": ""})
	if res.Error != nil {
		return 0, 0, res.Error
	}
	marked = res.RowsAffected
	del := db.Unscoped().Where("status = ? AND updated_at < ?", "expired", now.Add(-orderExpiredRetention)).Delete(&models.PageOrder{})
	return marked, del.RowsAffected, del.Error
}
