package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Rest: Fehlerantworten tragen einen Fehlercode, den das
// Frontend unter api.errors.<code> übersetzt. Dieser Test geht über den echten
// Router (newRouter) und prüft je Handler-Familie 400/401/403/404/409/410 samt
// Mandantentrennung. Die statische Ratsche (handlers/error_codes_static_test.go)
// sichert zusätzlich, dass keine Klartext-Fehler ohne Code hinzukommen.

type restCodeCase struct {
	name, method, url, token string
	body                     any
	status                   int
	code                     string
}

func restCall(t *testing.T, r http.Handler, tc restCodeCase) {
	t.Helper()
	raw, _ := json.Marshal(tc.body)
	req := httptest.NewRequest(tc.method, tc.url, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if tc.token != "" {
		req.Header.Set("Authorization", "Bearer "+tc.token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	assert.Equal(t, tc.status, w.Code, "%s: %s", tc.name, w.Body.String())
	assert.Equal(t, tc.code, out["code"], "%s: %s", tc.name, w.Body.String())
	assert.NotEmpty(t, out["error"], "%s: Rückfalltext fehlt", tc.name)
}

func hashTok(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestRouter_Fehlercodes_RestDerHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	r := newRouter(handlers.NewHandler(db))

	page := models.StatusPage{Name: "A", Slug: "ec-a"}
	other := models.StatusPage{Name: "B", Slug: "ec-b"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&other).Error)
	pid, oid := strconv.Itoa(int(page.ID)), strconv.Itoa(int(other.ID))

	mkUser := func(email string, global bool, perms string, on *models.StatusPage) string {
		u := models.User{Email: email, IsGlobalAdmin: global}
		require.NoError(t, db.Create(&u).Error)
		if on != nil {
			role := models.Role{Name: "R-" + email, Permissions: perms}
			require.NoError(t, db.Create(&role).Error)
			require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: on.ID, RoleID: role.ID}).Error)
		}
		tok, _ := middleware.GenerateToken(u.ID, u.Email, "", perms, global)
		return tok
	}
	admin := mkUser("ga634@example.com", true, "", nil)
	reader := mkUser("rd634@example.com", false, "components:read", &page)
	pageAdminOfA := mkUser("pa634@example.com", false, "pages:write,notices:write", &page)
	noRole := mkUser("nr634@example.com", false, "", nil)

	// Seed für Ablauf-/Konfliktfälle.
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&models.PageOrder{CompanyName: "X", Email: "x1@example.com", Status: "pending_confirmation", ConfirmTokenHash: models.HashToken("tok-exp"), ConfirmTokenExpiry: &past}).Error)
	require.NoError(t, db.Create(&models.PageOrder{CompanyName: "X", Email: "x2@example.com", Status: "confirmed", SetPasswordTokenHash: models.HashToken("pw-exp"), SetPasswordTokenExpiry: &past}).Error)
	hash, _ := bcrypt.GenerateFromPassword([]byte("vorhanden-123"), bcrypt.MinCost)
	owner := models.User{Email: "owner634@example.com", PasswordHash: string(hash)}
	require.NoError(t, db.Create(&owner).Error)
	require.NoError(t, db.Create(&models.PageOrder{CompanyName: "X", Email: "x3@example.com", Status: "confirmed", SetPasswordTokenHash: models.HashToken("pw-set"), SetPasswordTokenExpiry: &future, UserID: &owner.ID}).Error)
	require.NoError(t, db.Create(&models.PageOrder{CompanyName: "Y", Email: "y@example.com", Slug: "order-slug", Status: "pending_confirmation", ConfirmTokenHash: models.HashToken("tok-y"), ConfirmTokenExpiry: &future}).Error)
	for _, mail := range []string{"p1", "p1", "p1"} {
		require.NoError(t, db.Create(&models.PageOrder{CompanyName: "Z", Email: mail + "@cap.example", Status: "pending_confirmation", ConfirmTokenHash: models.HashToken("cap-" + strconv.Itoa(int(time.Now().UnixNano()))), ConfirmTokenExpiry: &future}).Error)
		time.Sleep(time.Millisecond)
	}
	require.NoError(t, db.Create(&models.Subscriber{StatusPageID: page.ID, Email: "dup@example.com", Channel: "email", SubscribedComponentIDs: "all", Status: "active"}).Error)
	require.NoError(t, db.Create(&models.Subscriber{StatusPageID: page.ID, Email: "late@example.com", Channel: "email", SubscribedComponentIDs: "all", Status: "pending", ConfirmTokenHash: hashTok("sub-exp"), ConfirmTokenExpiry: &past}).Error)
	meTok := mkUser("dup@example.com", false, "", nil)

	require.NoError(t, db.Create(&models.Language{Code: "xx", Name: "Xx"}).Error)

	cases := []restCodeCase{
		// page_order.go (öffentlich, 5 Anfragen/min Limit auf POST /orders)
		{"Bestätigung: unbekannter Link", "POST", "/api/public/orders/confirm", "", map[string]any{"token": "gibt-es-nicht"}, 400, "confirm_link_invalid"},
		{"Bestätigung: abgelaufen", "POST", "/api/public/orders/confirm", "", map[string]any{"token": "tok-exp"}, 410, "order_link_expired"},
		{"Passwort-Link unbekannt", "POST", "/api/public/orders/set-password", "", map[string]any{"token": "nope", "password": "langgenug-1"}, 400, "password_link_invalid"},
		{"Passwort-Link abgelaufen", "POST", "/api/public/orders/set-password", "", map[string]any{"token": "pw-exp", "password": "langgenug-1"}, 410, "password_link_expired"},
		{"Passwort-Link: Konto hat schon Anmeldung", "POST", "/api/public/orders/set-password", "", map[string]any{"token": "pw-set", "password": "langgenug-1"}, 409, "password_already_set"},
		{"Bestellung: Slug ungültig", "POST", "/api/public/orders", "", map[string]any{"company_name": "N", "email": "n1@example.com", "slug": "Ungültig!"}, 400, "slug_invalid"},
		{"Bestellung: Slug von Statusseite belegt", "POST", "/api/public/orders", "", map[string]any{"company_name": "N", "email": "n2@example.com", "slug": "ec-a"}, 409, "slug_taken"},
		{"Bestellung: Slug von anderer Bestellung belegt", "POST", "/api/public/orders", "", map[string]any{"company_name": "N", "email": "n3@example.com", "slug": "order-slug"}, 409, "slug_taken_order"},
		{"Bestellung: zu viele offene Bestellungen", "POST", "/api/public/orders", "", map[string]any{"company_name": "N", "email": "p1@cap.example"}, 429, "too_many_orders"},
		// subscriber.go / subscriber_confirm.go / subscriber_token.go
		{"Abo ohne Anmeldung", "GET", "/api/me/subscription?slug=ec-a", "", nil, 401, "unauthorized"},
		{"Abo ohne Slug", "GET", "/api/me/subscription", meTok, nil, 400, "slug_required"},
		{"Abo: Seite unbekannt", "GET", "/api/me/subscription?slug=gibt-es-nicht", meTok, nil, 404, "page_not_found"},
		{"Abo: keins vorhanden", "GET", "/api/me/subscription?slug=ec-b", meTok, nil, 404, "subscriber_not_found"},
		{"Abo doppelt (E-Mail, eigene Konto-Adresse)", "POST", "/api/subscribe", meTok, map[string]any{"channel": "email", "email": "dup@example.com", "component_ids": "all", "slug": "ec-a"}, 409, "subscription_exists_email"},
		{"Bestätigung ohne Token", "POST", "/api/public/subscribers/confirm", "", map[string]any{}, 400, "confirm_token_missing"},
		{"Bestätigung: unbekannter Link", "POST", "/api/public/subscribers/confirm", "", map[string]any{"token": "nope"}, 400, "confirm_link_invalid"},
		{"Bestätigung: abgelaufen", "POST", "/api/public/subscribers/confirm", "", map[string]any{"token": "sub-exp"}, 410, "subscription_link_expired"},
		{"Abbestellen: ungültiger Link", "POST", "/api/public/subscribers/unsubscribe", "", map[string]any{"token": "nope"}, 400, "unsubscribe_link_invalid"},
		// statuspage.go: 400 / 403 / 404 und Mandantentrennung
		{"Seite ändern: ID keine Zahl", "PUT", "/api/admin/pages/abc", admin, map[string]any{"name": "N"}, 404, "page_not_found"},
		{"Seite ändern: unbekannt", "PUT", "/api/admin/pages/99999", admin, map[string]any{"name": "N"}, 404, "page_not_found"},
		{"Seite ändern: ohne Rolle", "PUT", "/api/admin/pages/" + pid, noRole, map[string]any{"name": "N"}, 403, "forbidden"},
		{"Seite ändern: Rolle ohne pages:write", "PUT", "/api/admin/pages/" + pid, reader, map[string]any{"name": "N"}, 403, "forbidden"},
		{"Seite ändern: Mandantentrennung (Rolle nur auf anderer Seite)", "PUT", "/api/admin/pages/" + oid, pageAdminOfA, map[string]any{"name": "N"}, 403, "forbidden"},
		{"Seite löschen: unbekannt", "DELETE", "/api/admin/pages/99999", admin, nil, 404, "page_not_found"},
		{"Seite per Slug: unbekannt", "GET", "/api/pages/slug/gibt-es-nicht", "", nil, 404, "page_not_found"},
		{"Seite anlegen: Slug ungültig", "POST", "/api/admin/pages", admin, map[string]any{"name": "N", "slug": "Ungültig!"}, 400, "slug_invalid"},
		{"Seite anlegen: Slug belegt", "POST", "/api/admin/pages", admin, map[string]any{"name": "N", "slug": "ec-a"}, 409, "slug_taken"},
		{"Verfügbarkeit: Seite unbekannt", "GET", "/api/public/pages/gibt-es-nicht/availability", "", nil, 404, "page_not_found"},
		// Auth / Middleware
		{"Login falsch", "POST", "/api/login", "", map[string]any{"email": "ga634@example.com", "password": "falsch-falsch"}, 401, "invalid_credentials"},
		{"Ohne Token", "GET", "/api/me", "", nil, 401, "unauthorized"},
		{"Nicht Global Admin", "GET", "/api/users", reader, nil, 403, "forbidden"},
		{"Profil: Sprache unbekannt", "PATCH", "/api/me", reader, map[string]any{"preferred_language": "xx"}, 400, "invalid_request"},
		// Ankündigungen, Rollen, Vorfälle, Übersetzungen, externe Quellen
		{"Ankündigung ändern: unbekannt", "PUT", "/api/pages/" + pid + "/notices/99999", pageAdminOfA, map[string]any{"title": "T", "message": "m", "level": "info"}, 404, "notice_not_found"},
		{"Ankündigung: Level ungültig", "POST", "/api/pages/" + pid + "/notices", pageAdminOfA, map[string]any{"title": "T", "message": "m", "level": "boese"}, 400, "notice_level_invalid"},
		{"Ankündigung: Mandantentrennung", "POST", "/api/pages/" + oid + "/notices", pageAdminOfA, map[string]any{"title": "T", "message": "m", "level": "info"}, 403, "forbidden"},
		{"Rolle löschen: unbekannt", "DELETE", "/api/roles/99999", admin, nil, 404, "role_not_found"},
		{"Rolle ändern: ungültige ID", "PUT", "/api/roles/abc", admin, map[string]any{"name": "N"}, 400, "invalid_id"},
		{"Vorfall-Übersetzung: Vorfall unbekannt", "GET", "/api/incidents/99999/translations", "", nil, 404, "incident_not_found"},
		{"Vorfall-Übersetzung: ungültige ID", "GET", "/api/incidents/abc/translations", "", nil, 400, "invalid_id"},
		{"Komponenten-Übersetzung: unbekannt", "GET", "/api/components/99999/translations", "", nil, 404, "component_not_found"},
		{"Externe Quelle: unbekannt", "GET", "/api/pages/" + pid + "/external-sources/99999", admin, nil, 404, "external_source_not_found"},
		{"Externer Vorfall: ungültige ID", "GET", "/api/pages/" + pid + "/external-incidents/abc", admin, nil, 400, "invalid_id"},
		{"Externer Vorfall: unbekannt", "GET", "/api/pages/" + pid + "/external-incidents/99999", admin, nil, 404, "external_incident_not_found"},
		{"Externe Vorfälle: Filter ungültig", "GET", "/api/pages/" + pid + "/external-incidents?kind=x", admin, nil, 400, "invalid_request"},
		{"Externe Quellen: Mandantentrennung", "GET", "/api/pages/" + oid + "/external-sources", reader, nil, 403, "forbidden"},
		{"Eigene Domain: ungültig", "PUT", "/api/admin/pages/" + pid + "/custom-domain", admin, map[string]any{"domain": "kein domain!"}, 400, "domain_invalid"},
		{"Eigene Domain entfernen: keine eingetragen", "POST", "/api/admin/pages/" + pid + "/custom-domain/verify", admin, nil, 400, "custom_domain_missing"},
		{"Sprache: doppelter Code", "POST", "/api/languages", admin, map[string]any{"code": "xx", "name": "Xx", "native_name": "Xx"}, 409, "language_exists"},
		{"Einstellungen: nichts angegeben", "PUT", "/api/settings", admin, map[string]any{}, 400, "settings_field_required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { restCall(t, r, tc) })
	}

	// Self-Service-Bestellung abgeschaltet -> 403 orders_disabled (zuletzt, wirkt global).
	require.NoError(t, db.Create(&models.SystemSetting{Key: "orders.enabled", Value: "false"}).Error)
	t.Run("Bestellung abgeschaltet", func(t *testing.T) {
		restCall(t, r, restCodeCase{"", "POST", "/api/public/orders", "", map[string]any{"company_name": "N", "email": "n9@example.com"}, 403, "orders_disabled"})
	})
}

// failTable lässt Abfragen/Schreibzugriffe auf table scheitern (500-Pfade).
func failTable(t *testing.T, db *gorm.DB, table string, ops ...string) {
	t.Helper()
	hook := func(tx *gorm.DB) {
		if tx.Statement.Table == table {
			_ = tx.AddError(assert.AnError)
		}
	}
	only := len(ops) > 0
	want := func(op string) bool { return !only || ops[0] == op }
	if want("q") {
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register("t634:q:"+table, hook))
	}
	if want("c") {
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("t634:c:"+table, hook))
	}
	if want("u") {
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("t634:u:"+table, hook))
	}
	if want("d") {
		require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("t634:d:"+table, hook))
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove("t634:q:" + table)
		_ = db.Callback().Create().Remove("t634:c:" + table)
		_ = db.Callback().Update().Remove("t634:u:" + table)
		_ = db.Callback().Delete().Remove("t634:d:" + table)
	})
}

func TestRouter_Fehlercodes_Serverfehler500(t *testing.T) {
	db := testutils.SetupTestDB()
	r := newRouter(handlers.NewHandler(db))
	admin := models.User{Email: "ga634-500@example.com", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&admin).Error)
	tok, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)
	page := models.StatusPage{Name: "S", Slug: "ec-500"}
	require.NoError(t, db.Create(&page).Error)
	pid := strconv.Itoa(int(page.ID))

	t.Run("Seite löschen: Speichern scheitert", func(t *testing.T) {
		failTable(t, db, "status_pages", "d")
		restCall(t, r, restCodeCase{"", "DELETE", "/api/admin/pages/" + pid, tok, nil, 500, "server_error"})
	})
	t.Run("Seitenliste: Lesen scheitert", func(t *testing.T) {
		failTable(t, db, "status_pages")
		restCall(t, r, restCodeCase{"", "GET", "/api/pages", tok, nil, 500, "server_error"})
	})
	t.Run("Rollenliste: Lesen scheitert", func(t *testing.T) {
		failTable(t, db, "roles")
		restCall(t, r, restCodeCase{"", "GET", "/api/roles", tok, nil, 500, "server_error"})
	})
	t.Run("Abonnentenliste: Lesen scheitert", func(t *testing.T) {
		failTable(t, db, "subscribers")
		restCall(t, r, restCodeCase{"", "GET", "/api/pages/" + pid + "/subscribers", tok, nil, 500, "server_error"})
	})
}
