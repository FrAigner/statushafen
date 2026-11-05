package handlers_test

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// setupOrderRouter registriert nur die drei Bestell-Routen, analog zum
// Muster in statuspage_test.go (doStatusPageRequest/requireStatusCode/
// requireBodyContains werden von dort mitbenutzt, gleiches Testpaket).
func setupOrderRouter(h *handlers.Handler) *gin.Engine {
	r := testutils.SetupRouter()
	r.POST("/api/public/orders", h.CreateOrderHandler)
	r.POST("/api/public/orders/confirm", h.ConfirmOrderHandler)
	r.POST("/api/public/orders/set-password", h.SetOrderPasswordHandler)
	return r
}

// seedPageAdminRole legt die "Page Admin"-Rolle an, die ConfirmOrderHandler
// dem Besteller zuweist (in Produktion durch database.SeedData() vorhanden,
// hier für die schlanken Handler-Tests einzeln nachgebildet, ohne die
// restlichen SeedData-Fixtures wie Testnutzer/-seiten mitzuschleppen).
func seedPageAdminRole(t *testing.T, db *gorm.DB) {
	t.Helper()
	role := models.Role{
		Name:        "Page Admin",
		Permissions: "components:read,components:write,incidents:read,incidents:write,notices:read,notices:write,subscribers:read,subscribers:write,admin_dashboard",
	}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("failed to seed Page Admin role: %v", err)
	}
}

// testToken erzeugt einen zufälligen Hex-Token fürs Testsetup, ohne auf das
// unexportierte generateWebhookToken aus dem handlers-Paket zugreifen zu
// müssen (dieses Testpaket ist handlers_test, siehe Package-Deklaration).
func testToken(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("failed to generate test token: %v", err)
	}
	return hex.EncodeToString(buf)
}

// plainTokens merkt sich zu jedem gespeicherten Token-Hash den Klartext, damit
// Tests Zeilen mit Hash anlegen und den Klartext wie ein Client einreichen können.
var plainTokens sync.Map

// hashedToken liefert die gespeicherte Form (Hash) eines Klartext-Tokens.
func hashedToken(plain string) string {
	h := models.HashToken(plain)
	plainTokens.Store(h, plain)
	return h
}

// plainToken liefert den Klartext zu einem mit hashedToken gespeicherten Hash.
func plainToken(hash string) string {
	v, ok := plainTokens.Load(hash)
	if !ok {
		panic("kein Klartext-Token zu diesem Hash registriert")
	}
	return v.(string)
}

// createPendingOrder legt eine PageOrder im Status "pending_confirmation"
// direkt in der DB an (spart den Umweg über CreateOrderHandler in Tests, die
// nur den Confirm-/Set-Password-Schritt prüfen wollen).
func createPendingOrder(t *testing.T, db *gorm.DB, companyName, email, slug string) models.PageOrder {
	t.Helper()
	expiry := time.Now().Add(24 * time.Hour)
	order := models.PageOrder{
		CompanyName:        companyName,
		Email:              email,
		Slug:               slug,
		Status:             "pending_confirmation",
		ConfirmTokenHash:   hashedToken(testToken(t)),
		ConfirmTokenExpiry: &expiry,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("failed to create pending order: %v", err)
	}
	return order
}

func TestCreateOrderHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupOrderRouter(h)

	t.Run("happy path creates a pending order", func(t *testing.T) {
		w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
			"company_name": "Acme Testing GmbH",
			"email":        "bestell1@example.com",
		})
		requireStatusCode(t, w, 201)

		var order models.PageOrder
		if err := db.Where("email = ?", "bestell1@example.com").First(&order).Error; err != nil {
			t.Fatalf("expected PageOrder row to be created, got error: %v", err)
		}
		if order.Status != "pending_confirmation" {
			t.Errorf("expected status pending_confirmation, got %q", order.Status)
		}
		if !models.IsTokenHash(order.ConfirmTokenHash) {
			t.Error("expected ConfirmTokenHash to be set")
		}
		if order.ConfirmTokenExpiry == nil || !order.ConfirmTokenExpiry.After(time.Now()) {
			t.Error("expected ConfirmTokenExpiry to be set in the future")
		}
	})

	t.Run("honeypot filled returns fake success without side effects", func(t *testing.T) {
		var before int64
		db.Model(&models.PageOrder{}).Count(&before)

		w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
			"company_name": "Bot GmbH",
			"email":        "bot@example.com",
			"website":      "http://spam.example",
		})
		requireStatusCode(t, w, 201)

		var after int64
		db.Model(&models.PageOrder{}).Count(&after)
		if after != before {
			t.Errorf("expected no new PageOrder row when honeypot is filled, before=%d after=%d", before, after)
		}
	})

	t.Run("invalid email is rejected", func(t *testing.T) {
		w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
			"company_name": "Acme",
			"email":        "not-an-email",
		})
		requireStatusCode(t, w, 400)
	})

	t.Run("slug colliding with existing StatusPage is rejected", func(t *testing.T) {
		db.Create(&models.StatusPage{Name: "Existing", Slug: "existing-slug"})

		w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
			"company_name": "Acme",
			"email":        "collision1@example.com",
			"slug":         "existing-slug",
		})
		requireStatusCode(t, w, 409)
	})

	t.Run("slug colliding with another pending order is rejected", func(t *testing.T) {
		w1 := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
			"company_name": "First",
			"email":        "first@example.com",
			"slug":         "shared-slug",
		})
		requireStatusCode(t, w1, 201)

		w2 := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
			"company_name": "Second",
			"email":        "second@example.com",
			"slug":         "shared-slug",
		})
		requireStatusCode(t, w2, 409)
	})

	t.Run("pending cap of 3 per email is enforced", func(t *testing.T) {
		email := "cap-test@example.com"
		for i := 0; i < 3; i++ {
			w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
				"company_name": "Cap Test",
				"email":        email,
				"slug":         "cap-test-" + strconv.Itoa(i),
			})
			requireStatusCode(t, w, 201)
		}

		w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
			"company_name": "Cap Test",
			"email":        email,
			"slug":         "cap-test-overflow",
		})
		requireStatusCode(t, w, 429)
	})
}

// TestCreateOrderHandler_SendsConfirmationMail deckt den Regressionsfall ab:
// Der Body der Bestätigungsmail entstand aus einem mehrzeiligen
// Raw-String, wodurch der CR/LF-Guard (hasHeaderInjectionChars) immer anschlug
// und die Mail nie verschickt wurde. Der Test schickt die Bestellung gegen
// einen Fake-SMTP-Server und erwartet dort eine Mail mit Bestätigungslink.
func TestCreateOrderHandler_SendsConfirmationMail(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("FRONTEND_URL", "https://status.example.test")

	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupOrderRouter(h)

	w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
		"company_name": "Mail Testing GmbH",
		"email":        "bestell-mail@example.com",
	})
	requireStatusCode(t, w, 201)

	msg := awaitSMTPMessage(t, srv)
	if !strings.Contains(msg, "https://status.example.test/order/confirm?token=") {
		t.Errorf("expected confirmation mail to contain the confirm link, got:\n%s", msg)
	}
}

// TestCreateOrderHandler_CRLFInCompanyNameBlocksMail sichert ab, dass der Fix
// den CR/LF-Guard nicht aufweicht: Umbrüche aus Nutzereingaben
// landen weiterhin im Body und verhindern den Versand.
func TestCreateOrderHandler_CRLFInCompanyNameBlocksMail(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()

	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupOrderRouter(h)

	w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
		"company_name": "Evil GmbH\r\nBcc: victim@example.com",
		"email":        "bestell-crlf@example.com",
	})
	requireStatusCode(t, w, 201)

	select {
	case msg := <-srv.dataCh:
		t.Fatalf("expected no mail for a company name with CR/LF, got:\n%s", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestConfirmOrderHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupOrderRouter(h)
	seedPageAdminRole(t, db)

	t.Run("happy path provisions StatusPage, User and Page Admin role", func(t *testing.T) {
		order := createPendingOrder(t, db, "Confirm Happy Path GmbH", "confirm-happy@example.com", "")

		w := doStatusPageRequest(r, "POST", "/api/public/orders/confirm", map[string]interface{}{
			"token": plainToken(order.ConfirmTokenHash),
		})
		requireStatusCode(t, w, 200)
		requireBodyContains(t, w, "set_password_token")

		var updated models.PageOrder
		if err := db.First(&updated, order.ID).Error; err != nil {
			t.Fatalf("could not reload order: %v", err)
		}
		if updated.Status != "confirmed" {
			t.Errorf("expected status confirmed, got %q", updated.Status)
		}
		if updated.ConfirmTokenHash != "" {
			t.Error("expected ConfirmTokenHash to be burned (empty) after confirm")
		}
		if !models.IsTokenHash(updated.SetPasswordTokenHash) {
			t.Error("expected SetPasswordTokenHash to be generated")
		}
		if updated.StatusPageID == nil {
			t.Fatal("expected StatusPageID to be set")
		}
		if updated.UserID == nil {
			t.Fatal("expected UserID to be set")
		}

		var page models.StatusPage
		if err := db.First(&page, *updated.StatusPageID).Error; err != nil {
			t.Fatalf("expected StatusPage to be created: %v", err)
		}
		if page.Name != "Confirm Happy Path GmbH" {
			t.Errorf("expected page name to match company name, got %q", page.Name)
		}

		var user models.User
		if err := db.First(&user, *updated.UserID).Error; err != nil {
			t.Fatalf("expected User to be created: %v", err)
		}
		if user.Email != "confirm-happy@example.com" {
			t.Errorf("expected user email to match order email, got %q", user.Email)
		}
		if user.PasswordHash != "" {
			t.Error("expected PasswordHash to be empty until set-password step")
		}

		var role models.UserStatusPageRole
		if err := db.Where("user_id = ? AND status_page_id = ?", user.ID, page.ID).First(&role).Error; err != nil {
			t.Fatalf("expected UserStatusPageRole to be created: %v", err)
		}
		var roleModel models.Role
		db.First(&roleModel, role.RoleID)
		if roleModel.Name != "Page Admin" {
			t.Errorf("expected assigned role to be Page Admin, got %q", roleModel.Name)
		}
	})

	t.Run("unknown token is rejected", func(t *testing.T) {
		w := doStatusPageRequest(r, "POST", "/api/public/orders/confirm", map[string]interface{}{
			"token": "does-not-exist",
		})
		requireStatusCode(t, w, 400)
	})

	t.Run("already-confirmed token cannot be reused", func(t *testing.T) {
		order := createPendingOrder(t, db, "Reuse GmbH", "reuse@example.com", "")
		token := plainToken(order.ConfirmTokenHash)

		w1 := doStatusPageRequest(r, "POST", "/api/public/orders/confirm", map[string]interface{}{"token": token})
		requireStatusCode(t, w1, 200)

		w2 := doStatusPageRequest(r, "POST", "/api/public/orders/confirm", map[string]interface{}{"token": token})
		requireStatusCode(t, w2, 400)
	})

	t.Run("expired token is rejected with 410", func(t *testing.T) {
		order := createPendingOrder(t, db, "Expired GmbH", "expired@example.com", "")
		past := time.Now().Add(-1 * time.Hour)
		order.ConfirmTokenExpiry = &past
		db.Save(&order)

		w := doStatusPageRequest(r, "POST", "/api/public/orders/confirm", map[string]interface{}{
			"token": plainToken(order.ConfirmTokenHash),
		})
		requireStatusCode(t, w, 410)
	})

	// Sicherheitsprüfung: ConfirmOrderHandler hängte sich bei
	// Email-Übereinstimmung bisher an ein BESTEHENDES User-Konto -
	// SetOrderPasswordHandler hätte danach dessen PasswordHash überschrieben
	// (inkl. Global Admin, ohne Owner-Prüfung). Eine Order mit der Email eines
	// bestehenden Kontos muss abgelehnt werden, nicht wiederverwendet.
	t.Run("email colliding with an existing User account is rejected, that account is untouched", func(t *testing.T) {
		existing := models.User{Email: "victim@example.com", PasswordHash: testutils.MustHash("original-password")}
		if err := db.Create(&existing).Error; err != nil {
			t.Fatalf("failed to seed existing user: %v", err)
		}
		hashBefore := existing.PasswordHash

		order := createPendingOrder(t, db, "Hijack Attempt GmbH", "victim@example.com", "")

		w := doStatusPageRequest(r, "POST", "/api/public/orders/confirm", map[string]interface{}{
			"token": plainToken(order.ConfirmTokenHash),
		})
		requireStatusCode(t, w, 409)

		// Explizit vorher/nachher verglichen, nicht nur der Status-Code: das
		// bestehende Konto darf durch den abgelehnten Versuch in keiner Weise
		// verändert worden sein.
		var reloadedUser models.User
		if err := db.First(&reloadedUser, existing.ID).Error; err != nil {
			t.Fatalf("could not reload existing user: %v", err)
		}
		if reloadedUser.PasswordHash != hashBefore {
			t.Error("expected existing account's PasswordHash to remain unchanged after a rejected order confirmation")
		}

		var userCount int64
		db.Model(&models.User{}).Where("email = ?", "victim@example.com").Count(&userCount)
		if userCount != 1 {
			t.Errorf("expected exactly 1 User row for victim@example.com, got %d", userCount)
		}

		// Kein set-password-Token darf entstanden sein - sonst könnte der
		// letzte Schritt trotz 409 auf der Confirm-Antwort noch missbraucht
		// werden.
		var reloadedOrder models.PageOrder
		if err := db.First(&reloadedOrder, order.ID).Error; err != nil {
			t.Fatalf("could not reload order: %v", err)
		}
		if reloadedOrder.Status == "confirmed" {
			t.Error("expected order to NOT be confirmed after an email collision rejection")
		}
		if reloadedOrder.SetPasswordTokenHash != "" {
			t.Error("expected no SetPasswordToken to be issued after an email collision rejection")
		}

		// Die gesamte Transaktion muss zurückgerollt sein - auch keine
		// verwaiste StatusPage.
		var pageCount int64
		db.Model(&models.StatusPage{}).Where("name = ?", "Hijack Attempt GmbH").Count(&pageCount)
		if pageCount != 0 {
			t.Errorf("expected no StatusPage to be created for a rejected order, got %d", pageCount)
		}
	})

	// Ein soft-gelöschter User mit
	// derselben Email findet First() nicht, der uniqueIndex auf email schlägt
	// dann beim Create zu. Erwartet ist dieselbe 409 wie beim aktiven Konto,
	// keine 500 und keine verwaiste StatusPage.
	t.Run("email of a soft-deleted User account is rejected with 409", func(t *testing.T) {
		deleted := models.User{Email: "deleted-user@example.com", PasswordHash: testutils.MustHash("old-password")}
		if err := db.Create(&deleted).Error; err != nil {
			t.Fatalf("failed to seed user: %v", err)
		}
		if err := db.Delete(&deleted).Error; err != nil {
			t.Fatalf("failed to soft-delete user: %v", err)
		}

		order := createPendingOrder(t, db, "Soft Deleted GmbH", "deleted-user@example.com", "")
		w := doStatusPageRequest(r, "POST", "/api/public/orders/confirm", map[string]interface{}{
			"token": plainToken(order.ConfirmTokenHash),
		})
		requireStatusCode(t, w, 409)

		var pageCount int64
		db.Model(&models.StatusPage{}).Where("name = ?", "Soft Deleted GmbH").Count(&pageCount)
		if pageCount != 0 {
			t.Errorf("expected no StatusPage for a rejected order, got %d", pageCount)
		}
	})

	// Schlägt das Anlegen des Users fehl (DB-Fehler),
	// muss die ganze Transaktion zurückgerollt werden und die Antwort 500 sein.
	t.Run("DB error while creating the User rolls back everything with 500", func(t *testing.T) {
		const cb = "test:fail_user_create"
		if err := db.Callback().Create().Before("gorm:create").Register(cb, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "users" {
				_ = tx.AddError(errors.New("simulierter DB-Fehler"))
			}
		}); err != nil {
			t.Fatalf("failed to register callback: %v", err)
		}
		t.Cleanup(func() { _ = db.Callback().Create().Remove(cb) })

		order := createPendingOrder(t, db, "DB Error GmbH", "db-error@example.com", "")
		w := doStatusPageRequest(r, "POST", "/api/public/orders/confirm", map[string]interface{}{
			"token": plainToken(order.ConfirmTokenHash),
		})
		requireStatusCode(t, w, 500)

		var pageCount int64
		db.Model(&models.StatusPage{}).Where("name = ?", "DB Error GmbH").Count(&pageCount)
		if pageCount != 0 {
			t.Errorf("expected no StatusPage after rollback, got %d", pageCount)
		}
		var userCount int64
		db.Model(&models.User{}).Where("email = ?", "db-error@example.com").Count(&userCount)
		if userCount != 0 {
			t.Errorf("expected no User after rollback, got %d", userCount)
		}
		var reloaded models.PageOrder
		if err := db.First(&reloaded, order.ID).Error; err != nil {
			t.Fatalf("could not reload order: %v", err)
		}
		if reloaded.Status != "pending_confirmation" {
			t.Errorf("expected order to stay pending_confirmation, got %q", reloaded.Status)
		}
		if reloaded.SetPasswordTokenHash != "" {
			t.Error("expected no SetPasswordToken after rollback")
		}
	})

	// Gegenprobe: eine komplett neue Email-Adresse (kein bestehender User)
	// muss weiterhin ganz normal bestätigt werden - sonst wäre der Bestellflow
	// insgesamt kaputt statt nur die Verwechslungslücke geschlossen.
	t.Run("Gegenprobe: a brand-new email with no existing account still confirms normally", func(t *testing.T) {
		order := createPendingOrder(t, db, "Legit New GmbH", "brand-new-user@example.com", "")

		w := doStatusPageRequest(r, "POST", "/api/public/orders/confirm", map[string]interface{}{
			"token": plainToken(order.ConfirmTokenHash),
		})
		requireStatusCode(t, w, 200)
		requireBodyContains(t, w, "set_password_token")

		var user models.User
		if err := db.Where("email = ?", "brand-new-user@example.com").First(&user).Error; err != nil {
			t.Fatalf("expected a new User row for a genuinely new email, got error: %v", err)
		}
	})
}

func TestSetOrderPasswordHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupOrderRouter(h)
	seedPageAdminRole(t, db)

	confirmOrder := func(t *testing.T, companyName, email string) models.PageOrder {
		t.Helper()
		order := createPendingOrder(t, db, companyName, email, "")
		w := doStatusPageRequest(r, "POST", "/api/public/orders/confirm", map[string]interface{}{
			"token": plainToken(order.ConfirmTokenHash),
		})
		requireStatusCode(t, w, 200)
		// Die Antwort trägt den Klartext; der Hash dazu steht in der Datenbank.
		pwToken, _ := responseField(t, w, "set_password_token").(string)
		hashedToken(pwToken)
		var confirmed models.PageOrder
		if err := db.First(&confirmed, order.ID).Error; err != nil {
			t.Fatalf("could not reload confirmed order: %v", err)
		}
		return confirmed
	}

	t.Run("happy path sets password and activates order", func(t *testing.T) {
		order := confirmOrder(t, "Set Password GmbH", "setpw@example.com")

		w := doStatusPageRequest(r, "POST", "/api/public/orders/set-password", map[string]interface{}{
			"token":    plainToken(order.SetPasswordTokenHash),
			"password": "correct-horse-battery",
		})
		requireStatusCode(t, w, 200)

		var updated models.PageOrder
		db.First(&updated, order.ID)
		if updated.Status != "active" {
			t.Errorf("expected status active, got %q", updated.Status)
		}
		if updated.SetPasswordTokenHash != "" {
			t.Error("expected SetPasswordToken to be burned (empty)")
		}

		var user models.User
		db.First(&user, *updated.UserID)
		if !database.CheckPasswordHash("correct-horse-battery", user.PasswordHash) {
			t.Error("expected the new password to be usable for login")
		}
	})

	// Orders, die VOR dem Fix bestätigt wurden,
	// können noch auf ein bestehendes fremdes Konto zeigen. Der Schritt, der
	// tatsächlich überschreibt, muss daher selbst prüfen, dass das Konto
	// frisch aus dieser Bestellung stammt (kein Hash, kein SSO, kein Global
	// Admin), sonst 409 ohne jede Änderung.
	legacyConfirmedOrder := func(t *testing.T, victim models.User) models.PageOrder {
		t.Helper()
		if err := db.Create(&victim).Error; err != nil {
			t.Fatalf("failed to seed victim: %v", err)
		}
		expiry := time.Now().Add(24 * time.Hour)
		order := models.PageOrder{
			CompanyName:            "Legacy " + victim.Email,
			Email:                  victim.Email,
			Status:                 "confirmed",
			UserID:                 &victim.ID,
			SetPasswordTokenHash:   hashedToken(testToken(t)),
			SetPasswordTokenExpiry: &expiry,
		}
		if err := db.Create(&order).Error; err != nil {
			t.Fatalf("failed to create legacy order: %v", err)
		}
		return order
	}

	for name, victim := range map[string]models.User{
		"existing account with password": {Email: "legacy-victim@example.com", PasswordHash: testutils.MustHash("original-password")},
		"global admin without password":  {Email: "legacy-admin@example.com", IsGlobalAdmin: true},
		"SSO account without password":   {Email: "legacy-sso@example.com", SSOProvider: "oidc", SSOSubject: "sub-1"},
	} {
		t.Run("legacy confirmed order pointing to "+name+" is rejected with 409", func(t *testing.T) {
			order := legacyConfirmedOrder(t, victim)
			var before models.User
			db.First(&before, *order.UserID)

			w := doStatusPageRequest(r, "POST", "/api/public/orders/set-password", map[string]interface{}{
				"token":    plainToken(order.SetPasswordTokenHash),
				"password": "attacker-password",
			})
			requireStatusCode(t, w, 409)

			var after models.User
			db.First(&after, *order.UserID)
			if after.PasswordHash != before.PasswordHash {
				t.Error("expected victim's PasswordHash to remain unchanged")
			}
			var reloaded models.PageOrder
			db.First(&reloaded, order.ID)
			if reloaded.Status != "confirmed" {
				t.Errorf("expected order to stay confirmed, got %q", reloaded.Status)
			}
		})
	}

	t.Run("password too short is rejected", func(t *testing.T) {
		order := confirmOrder(t, "Short PW GmbH", "shortpw@example.com")

		w := doStatusPageRequest(r, "POST", "/api/public/orders/set-password", map[string]interface{}{
			"token":    plainToken(order.SetPasswordTokenHash),
			"password": "short",
		})
		requireStatusCode(t, w, 400)
	})

	t.Run("unknown token is rejected", func(t *testing.T) {
		w := doStatusPageRequest(r, "POST", "/api/public/orders/set-password", map[string]interface{}{
			"token":    "does-not-exist",
			"password": "correct-horse-battery",
		})
		requireStatusCode(t, w, 400)
	})

	t.Run("expired token is rejected with 410", func(t *testing.T) {
		order := confirmOrder(t, "Expired PW GmbH", "expiredpw@example.com")
		past := time.Now().Add(-1 * time.Hour)
		order.SetPasswordTokenExpiry = &past
		db.Save(&order)

		w := doStatusPageRequest(r, "POST", "/api/public/orders/set-password", map[string]interface{}{
			"token":    plainToken(order.SetPasswordTokenHash),
			"password": "correct-horse-battery",
		})
		requireStatusCode(t, w, 410)
	})
}
