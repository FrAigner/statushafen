package handlers_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// confirmedOrderWithFreshUser legt eine bestätigte Bestellung samt frischem Konto
// (ohne Passwort) an, wie ConfirmOrderHandler sie hinterlässt.
func confirmedOrderWithFreshUser(t *testing.T, db *gorm.DB, email, token string) (models.PageOrder, models.User) {
	t.Helper()
	role := models.Role{Name: "Page Admin"}
	require.NoError(t, db.Where("name = ?", role.Name).FirstOrCreate(&role).Error)
	user := models.User{Email: email, RoleID: role.ID}
	require.NoError(t, db.Create(&user).Error)
	expiry := time.Now().Add(time.Hour)
	order := models.PageOrder{CompanyName: "Firma", Email: email, Status: "confirmed",
		SetPasswordTokenHash: models.HashToken(token), SetPasswordTokenExpiry: &expiry, UserID: &user.ID}
	require.NoError(t, db.Create(&order).Error)
	return order, user
}

func setPasswordRequest(r http.Handler, token, password string) (int, []byte) {
	w := doStatusPageRequest(r, "POST", "/api/public/orders/set-password", map[string]any{"token": token, "password": password})
	return w.Code, w.Body.Bytes()
}

// failOnce registriert einen Hook, der genau einmal fn ausführt, sobald eine
// Abfrage (after=query) bzw. ein Update auf die Tabelle läuft. So lässt sich
// die Reihenfolge "ein zweiter Request war schneller" deterministisch nachstellen.
func afterQueryOnce(t *testing.T, db *gorm.DB, table string, fn func(tx *gorm.DB)) {
	t.Helper()
	done := false
	name := "test:after_query_" + table
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if done || tx.Statement.Table != table || tx.Error != nil {
			return
		}
		done = true
		fn(tx.Session(&gorm.Session{NewDB: true}))
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
}

// Hat ein gleichzeitiger Request den Link zwischen Laden und Schreiben verbraucht,
// wird dieser Request abgewiesen und überschreibt das Passwort nicht.
func TestSetOrderPassword_LinkConsumedByConcurrentRequestIsRejected(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupOrderRouter(handlers.NewHandler(db))
	order, user := confirmedOrderWithFreshUser(t, db, "race@example.com", "tok-race-1")

	afterQueryOnce(t, db, "page_orders", func(tx *gorm.DB) {
		require.NoError(t, tx.Exec("UPDATE page_orders SET status = 'active', set_password_token = '' WHERE id = ?", order.ID).Error)
	})
	code, body := setPasswordRequest(r, "tok-race-1", "langes-sicheres-passwort")
	require.Equal(t, http.StatusBadRequest, code, string(body))
	requireErrorCode(t, body, "password_link_invalid")

	var reloaded models.User
	require.NoError(t, db.First(&reloaded, user.ID).Error)
	assert.Empty(t, reloaded.PasswordHash, "der unterlegene Request darf kein Passwort setzen")
}

// Scheitert das Schreiben des Passworts, bleibt der Link benutzbar (Transaktion
// wird zurückgerollt): kein verbrannter Link ohne gesetztes Passwort.
func TestSetOrderPassword_FailedPasswordWriteKeepsLinkUsable(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupOrderRouter(handlers.NewHandler(db))
	order, user := confirmedOrderWithFreshUser(t, db, "fehler@example.com", "tok-fehler-1")

	const hook = "test:fail_user_update"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			_ = tx.AddError(errors.New("simulierter Datenbankfehler"))
		}
	}))
	code, body := setPasswordRequest(r, "tok-fehler-1", "langes-sicheres-passwort")
	require.Equal(t, http.StatusInternalServerError, code, string(body))
	requireErrorCode(t, body, "server_error")

	var reloadedOrder models.PageOrder
	require.NoError(t, db.First(&reloadedOrder, order.ID).Error)
	assert.Equal(t, "confirmed", reloadedOrder.Status, "Bestellung bleibt unverändert")
	assert.Equal(t, models.HashToken("tok-fehler-1"), reloadedOrder.SetPasswordTokenHash)
	assert.NotNil(t, reloadedOrder.SetPasswordTokenExpiry)

	require.NoError(t, db.Callback().Update().Remove(hook))
	code, body = setPasswordRequest(r, "tok-fehler-1", "langes-sicheres-passwort")
	require.Equal(t, http.StatusOK, code, string(body))
	var reloaded models.User
	require.NoError(t, db.First(&reloaded, user.ID).Error)
	assert.True(t, database.CheckPasswordHash("langes-sicheres-passwort", reloaded.PasswordHash))
}

// Bekommt das Konto zwischen Prüfung und Schreiben anderweitig ein Passwort,
// wird es nicht überschrieben (409), und der Link bleibt unverbraucht.
func TestSetOrderPassword_AccountSecuredInBetweenIsNotOverwritten(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupOrderRouter(handlers.NewHandler(db))
	order, user := confirmedOrderWithFreshUser(t, db, "dazwischen@example.com", "tok-dazwischen-1")
	existing := testutils.MustHash("bestehendes-Passwort-1")

	afterQueryOnce(t, db, "users", func(tx *gorm.DB) {
		require.NoError(t, tx.Exec("UPDATE users SET password_hash = ? WHERE id = ?", existing, user.ID).Error)
	})
	code, body := setPasswordRequest(r, "tok-dazwischen-1", "neues-Passwort-uberschreibt")
	require.Equal(t, http.StatusConflict, code, string(body))
	requireErrorCode(t, body, "password_already_set")

	var reloaded models.User
	require.NoError(t, db.First(&reloaded, user.ID).Error)
	assert.Equal(t, existing, reloaded.PasswordHash)
	var reloadedOrder models.PageOrder
	require.NoError(t, db.First(&reloadedOrder, order.ID).Error)
	assert.Equal(t, "confirmed", reloadedOrder.Status)
	assert.Equal(t, models.HashToken("tok-dazwischen-1"), reloadedOrder.SetPasswordTokenHash)
}

// Ändert sich die Bestellung zwischen Laden und Einlösen (anderer Token, anderer
// Status), gilt der Link nicht mehr: Das bedingte Update trifft keine Zeile.
func TestSetOrderPassword_OrderChangedInBetweenIsRejected(t *testing.T) {
	for name, set := range map[string]string{
		"anderer Token":  "set_password_token = 'tok-neu-und-anders'",
		"anderer Status": "status = 'cancelled'",
	} {
		t.Run(name, func(t *testing.T) {
			db := testutils.SetupTestDB()
			r := setupOrderRouter(handlers.NewHandler(db))
			order, user := confirmedOrderWithFreshUser(t, db, "geaendert-"+strings.ReplaceAll(name, " ", "-")+"@example.com", "tok-geaendert-1")

			afterQueryOnce(t, db, "page_orders", func(tx *gorm.DB) {
				require.NoError(t, tx.Exec("UPDATE page_orders SET "+set+" WHERE id = ?", order.ID).Error)
			})
			code, body := setPasswordRequest(r, "tok-geaendert-1", "langes-sicheres-passwort")
			require.Equal(t, http.StatusBadRequest, code, string(body))
			requireErrorCode(t, body, "password_link_invalid")

			var reloaded models.User
			require.NoError(t, db.First(&reloaded, user.ID).Error)
			assert.Empty(t, reloaded.PasswordHash)
		})
	}
}

// Wird das Konto zwischen Laden und Schreiben an SSO gebunden oder zum Global
// Admin, setzt der Link kein Passwort (409): Er darf kein bestehendes oder
// privilegiertes Konto übernehmen.
func TestSetOrderPassword_AccountBecameSSOOrAdminInBetweenIsNotTouched(t *testing.T) {
	for name, set := range map[string]string{
		"SSO-Konto":    "sso_provider = 'oidc'",
		"Global Admin": "is_global_admin = true",
	} {
		t.Run(name, func(t *testing.T) {
			db := testutils.SetupTestDB()
			r := setupOrderRouter(handlers.NewHandler(db))
			order, user := confirmedOrderWithFreshUser(t, db, "konto-"+strings.ReplaceAll(name, " ", "-")+"@example.com", "tok-konto-1")

			afterQueryOnce(t, db, "users", func(tx *gorm.DB) {
				require.NoError(t, tx.Exec("UPDATE users SET "+set+" WHERE id = ?", user.ID).Error)
			})
			code, body := setPasswordRequest(r, "tok-konto-1", "langes-sicheres-passwort")
			require.Equal(t, http.StatusConflict, code, string(body))
			requireErrorCode(t, body, "password_already_set")

			var reloaded models.User
			require.NoError(t, db.First(&reloaded, user.ID).Error)
			assert.Empty(t, reloaded.PasswordHash)
			var reloadedOrder models.PageOrder
			require.NoError(t, db.First(&reloadedOrder, order.ID).Error)
			assert.Equal(t, "confirmed", reloadedOrder.Status, "Link bleibt unverbraucht")
		})
	}
}

// Ein Datenbankfehler beim Einlösen des Links ist ein 500, kein "Link ungültig":
// Der Nutzer soll es erneut versuchen können, und der Fehler fällt im Betrieb auf.
func TestSetOrderPassword_ClaimDatabaseErrorIsServerError(t *testing.T) {
	db := testutils.SetupTestDB()
	r := setupOrderRouter(handlers.NewHandler(db))
	order, user := confirmedOrderWithFreshUser(t, db, "claim-fehler@example.com", "tok-claim-1")

	const hook = "test:fail_order_update"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table == "page_orders" {
			_ = tx.AddError(errors.New("simulierter Datenbankfehler"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(hook) })

	code, body := setPasswordRequest(r, "tok-claim-1", "langes-sicheres-passwort")
	require.Equal(t, http.StatusInternalServerError, code, string(body))
	requireErrorCode(t, body, "server_error")

	var reloaded models.User
	require.NoError(t, db.First(&reloaded, user.ID).Error)
	assert.Empty(t, reloaded.PasswordHash)
	var reloadedOrder models.PageOrder
	require.NoError(t, db.First(&reloadedOrder, order.ID).Error)
	assert.Equal(t, "confirmed", reloadedOrder.Status)
}
