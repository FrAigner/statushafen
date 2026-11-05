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
// Self-Service-Bestellung neuer Statusseiten über die öffentliche Startseite
// ("Eingangstor"-Homepage). Ablauf:
//
//  1. POST /api/public/orders          -> CreateOrderHandler
//     legt eine PageOrder("pending_confirmation") an und verschickt eine
//     Bestätigungsmail. Es wird bewusst NOCH KEIN User angelegt - sonst wäre
//     das öffentliche Formular selbst schon ein Vektor für beliebig viele
//     leere User-Zeilen.
//  2. POST /api/public/orders/confirm  -> ConfirmOrderHandler
//     legt StatusPage + User (ohne Passwort) + UserStatusPageRole("Page
//     Admin") an und liefert einen frischen Set-Password-Token.
//  3. POST /api/public/orders/set-password -> SetOrderPasswordHandler
//     setzt das Passwort, danach ist ein normaler Login über /api/login
//     möglich (bewusst kein Auto-Login hier, siehe Handler-Kommentar).
//
// Missbrauchsschutz (ohne Drittanbieter-Captcha, siehe main.go-Registrierung
// für die konkreten Rate-Limits): IP-Rate-Limiting + Honeypot-Feld + Pflicht-
// Email-Bestätigung per Einmal-Link + Cap auf gleichzeitig offene
// Bestellungen je Email-Adresse.
// ─────────────────────────────────────────────────────────────────────────────

const (
	// maxPendingOrdersPerEmail begrenzt, wie viele gleichzeitig unbestätigte
	// Bestellungen eine Email-Adresse haben darf - verhindert, dass eine
	// einzelne Adresse mit Bestätigungsmails geflutet oder für beliebig viele
	// Slug-Reservierungen missbraucht wird.
	maxPendingOrdersPerEmail = 3
	// orderTokenTTL gilt sowohl für den Bestätigungs- als auch den separat
	// erzeugten Set-Password-Token.
	orderTokenTTL = 24 * time.Hour

	// orderExpiredRetention: so lange bleibt eine als abgelaufen markierte
	// Bestellung (mit E-Mail-Adresse) noch gespeichert, bevor der
	// Aufräumjob sie endgültig löscht.
	orderExpiredRetention = 30 * 24 * time.Hour

	// orderPendingExpiredSQL: Bedingung (ein Zeitparameter "jetzt"), unter der
	// eine unbestätigte Bestellung als abgelaufen gilt. Ohne Ablaufzeit zählt
	// sie wie in ConfirmOrderHandler als abgelaufen.
	orderPendingExpiredSQL = "status = 'pending_confirmation' AND (confirm_token_expiry IS NULL OR confirm_token_expiry < ?)"

	// Längengrenzen der Eingaben der öffentlichen Bestellung (Zeichen, nicht Byte).
	// Der Name erscheint in der Bestätigungsmail und als Seitenname; der Slug
	// folgt dem dokumentierten Höchstwert von 60 Zeichen.
	maxOrderNameLength = 120
	maxOrderSlugLength = 60

	orderCreatedMessage = "Bitte bestätige deine Email-Adresse über den Link, den wir dir geschickt haben."
)

// errSlugTaken signalisiert innerhalb der ConfirmOrderHandler-Transaktion eine
// Slug-Kollision, die erst dort (nach dem Öffnen der Transaktion) final
// geprüft werden kann - siehe Kommentar in ConfirmOrderHandler.
var errSlugTaken = errors.New("slug bereits vergeben")

// errEmailTaken signalisiert innerhalb derselben Transaktion, dass für
// order.Email bereits ein User-Konto existiert (Sicherheitsprüfung):
// ConfirmOrderHandler darf sich NIE an ein bestehendes Konto hängen - der
// nachfolgende SetOrderPasswordHandler würde sonst dessen PasswordHash
// überschreiben (inkl. Global Admin, ohne jede Owner- oder SSO-Prüfung). Wie
// bei errSlugTaken muss die Prüfung innerhalb der Transaktion laufen (nicht
// vorher), sonst entsteht dieselbe Race-Lücke zwischen zwei nebenläufigen
// Bestätigungen.
var errEmailTaken = errors.New("email bereits vergeben")

// errOrderLinkUsed: Ein gleichzeitiger Request hat den Set-Password-Link
// zwischen Laden und Einlösen bereits verbraucht. errAccountAlreadySecured: Das
// Konto hat zwischen Prüfung und Schreiben ein Passwort oder SSO bekommen.
var (
	errOrderLinkUsed         = errors.New("link bereits verbraucht")
	errAccountAlreadySecured = errors.New("konto bereits eingerichtet")
)

type createOrderRequest struct {
	CompanyName string `json:"company_name" binding:"required"`
	Email       string `json:"email" binding:"required"`
	Slug        string `json:"slug"`
	// Website: Honeypot-Feld. Echte Nutzer sehen/befüllen es nie
	// (im Formular per CSS versteckt) - ein befüllter Wert heißt Bot.
	Website string `json:"website"`
}

type confirmOrderRequest struct {
	Token string `json:"token" binding:"required"`
}

type setOrderPasswordRequest struct {
	Token    string `json:"token" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// rejectInvalidOrderFields beantwortet überlange Felder und ungültige Zeichen
// (NUL, ungültiges UTF-8) mit 400 und Fehlercode und meldet true, wenn die
// Anfrage damit beendet ist.
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

// POST /api/public/orders — Formular "Eigene Statusseite bestellen".
func (h *Handler) CreateOrderHandler(c *gin.Context) {
	// Ist die Self-Service-Bestellung global deaktiviert, wird hier
	// vor jeder Verarbeitung abgebrochen (Default "an", nur ein explizites
	// orders.enabled=false sperrt). Bewusst 403 statt der Honeypot-Erfolgsantwort.
	if !h.getSystemSettingBool(settingOrdersEnabled, true) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Die Self-Service-Bestellung ist derzeit nicht verfügbar.", "code": "orders_disabled"})
		return
	}

	var in createOrderRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		respondInvalidInput(c)
		return
	}

	// Honeypot: identische Erfolgsantwort wie der echte Fall, aber ohne jede
	// Nebenwirkung - ein Bot bekommt kein Signal, dass er erkannt wurde.
	if in.Website != "" {
		h.RecordSystemAudit("pageorder.honeypot_triggered", "auth", "PageOrder", "", limitEmail(in.Email), "ip="+c.ClientIP())
		c.JSON(http.StatusCreated, gin.H{"message": orderCreatedMessage})
		return
	}

	// Länge und Zeichen prüfen, bevor etwas geparst, gesucht oder gespeichert wird.
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
		// Eine nie bestätigte Bestellung mit abgelaufenem Link sperrt den Slug
		// nicht mehr, auch wenn der Aufräumjob sie noch nicht erfasst hat.
		if err := h.DB.Where("slug = ? AND status <> ? AND NOT ("+orderPendingExpiredSQL+")", slug, "expired", time.Now()).First(&existingOrder).Error; err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Slug wird bereits in einer anderen Bestellung verwendet", "code": "slug_taken_order"})
			return
		}
	}

	// Das Limit gilt je Postfach (Groß-/Kleinschreibung und "+tag" zählen nicht
	// als andere Adresse), nicht je Zeichenfolge.
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

	// Mailversand-Fehler dürfen die Bestellung selbst nicht scheitern lassen -
	// die Zeile existiert bereits, ein Resend-Endpunkt ist bewusst nicht im
	// Scope (wäre selbst wieder ein neuer Missbrauchsvektor). Der Versand läuft
	// über die begrenzte Hintergrund-Warteschlange (mail_queue.go): Der Endpunkt
	// ist anonym, ein hängender Mailserver bände die Anfrage sonst bis zur
	// SMTP-Gesamtfrist, und die Antwortzeit verriete, ob eine Mail rausgeht.
	smtpCfg := loadSMTPConfig()
	h.queueMail("order_confirmation", func() { h.sendOrderConfirmationEmail(smtpCfg, order, token) }, mailSourceKey(c.ClientIP()))

	h.RecordSystemAudit("pageorder.created", "auth", "PageOrder", strconv.FormatUint(uint64(order.ID), 10), order.Email, "")
	c.JSON(http.StatusCreated, gin.H{"message": orderCreatedMessage})
}

// POST /api/public/orders/confirm — wird vom expliziten "Jetzt bestätigen"-
// Button auf der Frontend-Confirm-Seite ausgelöst (bewusst kein GET-Link
// direkt aus der Mail, damit Mail-Gateways mit Link-Prefetching den
// Einmal-Token nicht schon vor dem eigentlichen Klick verbrauchen).
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
			// Defensiv: erneute Prüfung innerhalb der Transaktion, falls der
			// Slug zwischen Bestellung und Bestätigung anderweitig vergeben
			// wurde (der uniqueIndex auf StatusPage.Slug fängt den Rest ab).
			if err := tx.Where(slugWhereClause, slug).First(&existing).Error; err == nil {
				return errSlugTaken
			}
		}

		page = models.StatusPage{Name: order.CompanyName, Slug: slug}
		if err := tx.Create(&page).Error; err != nil {
			if isUniqueViolation(err) { // Race nach der Prüfung oben
				return errSlugTaken
			}
			return err
		}
		if err := database.SeedDefaultStatusPageLanguages(tx, page.ID); err != nil {
			slog.Error("Failed to seed default languages for self-service status page", "status_page_id", page.ID, "error", err)
		}

		// Sicherheitsprüfung: existiert bereits ein User mit dieser
		// Email, wird die Bestellung abgelehnt statt sich an das bestehende
		// Konto zu hängen - SetOrderPasswordHandler würde sonst dessen
		// Passwort überschreiben können. Ein separater, owner-verifizierter
		// Passwort-Reset-Flow ist ein eigenes Feature und nicht Teil dieses
		// Fixes.
		// Unscoped: auch soft-gelöschte Konten belegen die Email (uniqueIndex),
		// sonst würde Create unten mit 500 scheitern.
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

// POST /api/public/orders/set-password — letzter Schritt: Passwort setzen,
// danach ist ein normaler Login über /api/login möglich. Bewusst kein
// automatisches Einloggen hier (kein JWT-Issuing) - der bereits gehärtete
// Login-Pfad wird stattdessen wiederverwendet, statt an einer neuen,
// ungeprüften Stelle vorbei eine Session zu erzeugen.
func (h *Handler) SetOrderPasswordHandler(c *gin.Context) {
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
	// Orders, die vor dem Fix in ConfirmOrderHandler
	// bestätigt wurden, können noch auf ein bestehendes fremdes Konto zeigen.
	// Überschrieben wird nur ein Konto, das frisch aus dieser Bestellung
	// stammt: ConfirmOrderHandler legt es ohne Hash, ohne SSO und ohne
	// Global-Admin-Recht an. Alles andere: 409, nichts wird verändert.
	if user.PasswordHash != "" || user.SSOProvider != "" || user.IsGlobalAdmin {
		c.JSON(http.StatusConflict, gin.H{"error": "Für dieses Konto ist bereits eine Anmeldung eingerichtet", "code": "password_already_set"})
		return
	}
	// Vor jeder Änderung hashen: scheitert es, bleiben Konto und Bestellung
	// unberührt und der Link weiter benutzbar.
	hash, err := hashPassword(in.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fehler beim Setzen des Passworts", "code": "server_error"})
		return
	}
	// Link verbrauchen und Passwort setzen in einer Transaktion. Der Link wird
	// per bedingtem UPDATE (Token noch gesetzt, Status confirmed) beansprucht und
	// die Zeilenzahl geprüft: Bei gleichzeitigen Einlösungen gewinnt genau eine,
	// die anderen sehen 0 Zeilen. Das Konto wird ebenfalls nur bedingt
	// geschrieben (noch ohne Passwort, ohne SSO, kein Global Admin). Scheitert
	// ein Schritt, wird alles zurückgerollt und der Link bleibt benutzbar.
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

// pageOrderConfirmSubject: fester deutscher Betreff, analog zu den übrigen
// Transaktionsmails (keine Sprachauswahl - die App ist de-first, siehe
// vue-i18n-Default in frontend/src/i18n/index.js).
const pageOrderConfirmSubject = "Bitte bestätige deine Statushafen-Bestellung"

// rawPageOrderConfirmEmailTemplate ist der lesbar formatierte HTML-Body der
// Bestätigungsmail (Platzhalter: Firmenname, Bestätigungslink).
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

// pageOrderConfirmEmailTemplate ist rawPageOrderConfirmEmailTemplate ohne die
// eigenen Formatierungs-Zeilenumbrüche/Tabs. Der CR/LF-Guard
// (hasHeaderInjectionChars) prüft den fertigen Body und schlug bei dem
// mehrzeiligen Raw-String immer an - die Mail wurde nie verschickt. Statt den
// Guard aufzuweichen, entfernen wir die Umbrüche einmalig beim Package-Init
// (gleiches Muster wie notificationEmailTemplate in incident.go); injizierte
// CR/LF aus Nutzereingaben werden weiterhin erkannt.
var pageOrderConfirmEmailTemplate = strings.NewReplacer("\n", "", "\t", "").Replace(rawPageOrderConfirmEmailTemplate)

// sendOrderConfirmationEmail folgt demselben Aufbau wie sendAlertEmail
// (incident.go): mail.ParseAddress, Header-Injection-Guard, rohe
// net/smtp.SendMail-Nachricht über die globale SMTP-Konfiguration. Bewusst
// eine eigenständige Funktion statt eines Aufrufs von sendAlertEmail, da
// dieser fest an Incident-Audit-Felder gekoppelt ist.
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

// pruneExpiredOrders räumt Bestellungen auf: unbestätigte Bestellungen mit
// abgelaufenem Link werden auf "expired" gesetzt (Token entwertet), bereits
// länger abgelaufene endgültig gelöscht. Idempotent; läuft im Wartungs-Tick.
// Ohne den Job liefen unbestätigte Bestellungen nur beim Einlösen ihres Links
// ab und hielten Slug und Bestell-Kontingent einer Adresse beliebig lange.
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
