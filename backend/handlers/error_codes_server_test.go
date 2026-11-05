package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Auch die 500-Antworten und die übrigen Bind-/ID-Fehler der
// Handler tragen einen Fehlercode (server_error, invalid_request, invalid_id).

// failDB lässt jede Operation op (create, update, delete, query) auf table mit
// einem Fehler scheitern; skip überspringt die ersten Treffer. Der Hook wird
// nach dem Test wieder entfernt.
func failDB(t *testing.T, db *gorm.DB, op, table string, skip int) {
	t.Helper()
	name := "test:fail613:" + op + ":" + table
	seen := 0
	hook := func(tx *gorm.DB) {
		if tx.Statement.Table != table {
			return
		}
		seen++
		if seen > skip {
			_ = tx.AddError(errors.New("injizierter Fehler"))
		}
	}
	var err error
	switch op {
	case "create":
		err = db.Callback().Create().Before("gorm:create").Register(name, hook)
	case "update":
		err = db.Callback().Update().Before("gorm:update").Register(name, hook)
	case "delete":
		err = db.Callback().Delete().Before("gorm:delete").Register(name, hook)
	default:
		err = db.Callback().Query().Before("gorm:query").Register(name, hook)
	}
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Callback().Create().Remove(name)
		_ = db.Callback().Update().Remove(name)
		_ = db.Callback().Delete().Remove(name)
		_ = db.Callback().Query().Remove(name)
	})
}

// dbCase: Anfrage, bei der vorher eine DB-Operation scheitert.
type dbCase struct {
	name        string
	op, table   string
	skip        int
	method, url string
	token       string
	body        any
	wantStatus  int
	wantCode    string
	prepare     func()
}

func runDBCases(t *testing.T, h *handlers.Handler, r http.Handler, cases []dbCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.prepare != nil {
				tc.prepare()
			}
			if tc.op != "" {
				failDB(t, h.DB, tc.op, tc.table, tc.skip)
			}
			status, out := doJSON(t, r, tc.method, tc.url, tc.token, tc.body)
			assert.Equal(t, tc.wantStatus, status, "%v", out)
			assert.Equal(t, tc.wantCode, out["code"], "%v", out)
			msg, _ := out["error"].(string)
			assert.NotContains(t, msg, "injizierter Fehler", "kein roher Fehlertext in der Antwort")
		})
	}
}

func TestFehlercodes_Serverfehler_KomponentenVorfaelleBenutzer(t *testing.T) {
	h, r, adminTok, _, page, _ := newCodeEnvRouter(t)
	db := h.DB
	pid := strconv.Itoa(int(page.ID))
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	inc := models.Incident{StatusPageID: page.ID, Title: "T", Message: "m", Status: "investigating"}
	require.NoError(t, db.Create(&inc).Error)
	require.NoError(t, db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID}).Error)
	tag := models.Tag{Name: "global613"}
	require.NoError(t, db.Create(&tag).Error)
	target := models.User{Email: "ziel613s@example.com"}
	require.NoError(t, db.Create(&target).Error)
	iid, tid := strconv.Itoa(int(inc.ID)), strconv.Itoa(int(target.ID))
	incBase := "/api/pages/" + pid + "/incidents"
	update := map[string]any{"message": "m", "status": "investigating", "ComponentIDs": []uint{comp.ID}}

	runDBCases(t, h, r, []dbCase{
		{name: "Komponenten ohne page_id", method: "GET", url: "/api/noparam/components", wantStatus: 400, wantCode: "invalid_request"},
		{name: "Komponenten mit ungültiger page_id", method: "GET", url: "/api/pages/abc/components", wantStatus: 400, wantCode: "invalid_id"},
		{name: "Komponente anlegen: Speichern scheitert", op: "create", table: "components", method: "POST", url: "/api/pages/" + pid + "/components", token: adminTok, body: map[string]any{"name": "Neu"}, wantStatus: 500, wantCode: "server_error"},
		{name: "Vorfälle lesen scheitert", op: "query", table: "incidents", method: "GET", url: incBase, token: adminTok, wantStatus: 500, wantCode: "server_error"},
		{name: "Vorfall anlegen: Speichern scheitert", op: "create", table: "incidents", method: "POST", url: incBase, token: adminTok,
			body: map[string]any{"Title": "T", "Message": "m", "Status": "investigating", "ComponentIDs": []uint{comp.ID}}, wantStatus: 500, wantCode: "server_error"},
		{name: "Vorfall ändern: ungültige ID", method: "PUT", url: incBase + "/abc", token: adminTok, body: update, wantStatus: 400, wantCode: "invalid_id"},
		{name: "Vorfall ändern: Komponentenzuordnung scheitert", op: "delete", table: "incident_components", method: "PUT", url: incBase + "/" + iid, token: adminTok, body: update, wantStatus: 500, wantCode: "server_error"},
		{name: "Vorfall ändern: Speichern scheitert", op: "update", table: "incidents", method: "PUT", url: incBase + "/" + iid, token: adminTok, body: update, wantStatus: 500, wantCode: "server_error"},
		{name: "Vorfall ändern: Tags speichern scheitert", op: "create", table: "incident_tags", method: "PUT", url: incBase + "/" + iid, token: adminTok,
			body: map[string]any{"message": "m", "status": "investigating", "TagIDs": []uint{tag.ID}}, wantStatus: 500, wantCode: "server_error"},
		{name: "Benutzer lesen scheitert", op: "query", table: "users", skip: 2, method: "GET", url: "/api/users", token: adminTok, wantStatus: 500, wantCode: "server_error"},
		{name: "Benutzer anlegen: Speichern scheitert", op: "create", table: "users", method: "POST", url: "/api/users", token: adminTok, body: map[string]any{"email": "neu613s@example.com", "password": "geheim1234"}, wantStatus: 500, wantCode: "server_error"},
		{name: "Benutzer ändern: kaputter Body", method: "PUT", url: "/api/users/" + tid, token: adminTok, body: "{", wantStatus: 400, wantCode: "invalid_request"},
		{name: "Benutzer ändern: Speichern scheitert", op: "update", table: "users", method: "PUT", url: "/api/users/" + tid, token: adminTok, body: map[string]any{"email": "ziel613s@example.com", "name": "Neu"}, wantStatus: 500, wantCode: "server_error"},
	})
}

// Ein Nutzer mit Tag-Einschränkung darf Vorfälle nicht auf fremde Tags verschieben (403 forbidden).
func TestFehlercodes_Vorfall_TagEinschraenkung(t *testing.T) {
	h, r, _, _, page, _ := newCodeEnvRouter(t)
	db := h.DB
	role := models.Role{Name: "Inc613", Permissions: "incidents:write"}
	require.NoError(t, db.Create(&role).Error)
	user := models.User{Email: "tags613@example.com"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: user.ID, StatusPageID: page.ID, RoleID: role.ID}).Error)
	mine := models.Tag{Name: "meins613"}
	foreign := models.Tag{Name: "fremd613t"}
	require.NoError(t, db.Create(&mine).Error)
	require.NoError(t, db.Create(&foreign).Error)
	require.NoError(t, db.Create(&models.UserTag{UserID: user.ID, TagID: mine.ID}).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	inc := models.Incident{StatusPageID: page.ID, Title: "T", Message: "m", Status: "investigating"}
	require.NoError(t, db.Create(&inc).Error)
	require.NoError(t, db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID}).Error)
	require.NoError(t, db.Create(&models.IncidentTag{IncidentID: inc.ID, TagID: mine.ID}).Error)
	tok, _ := middleware.GenerateToken(user.ID, user.Email, "", "", false)

	status, out := doJSON(t, r, "PUT", "/api/pages/"+strconv.Itoa(int(page.ID))+"/incidents/"+strconv.Itoa(int(inc.ID)), tok,
		map[string]any{"message": "m", "status": "investigating", "TagIDs": []uint{foreign.ID}})
	assert.Equal(t, http.StatusForbidden, status, "%v", out)
	assert.Equal(t, "forbidden", out["code"])
}

func TestFehlercodes_Serverfehler_Vorlagen(t *testing.T) {
	h, r, adminTok, _, page, _ := newCodeEnvRouter(t)
	pid := strconv.Itoa(int(page.ID))
	url := "/api/pages/" + pid + "/notification-templates/incident_created/subscriber/de"
	valid := map[string]any{"Subject": "Betreff", "BodyHTML": "<p>Inhalt</p>"}
	seedTemplate := func() {
		require.NoError(t, h.DB.Create(&models.NotificationTemplate{StatusPageID: page.ID, Kind: "incident_created", Audience: "subscriber", Language: "de", Subject: "S", BodyHTML: "<p>x</p>"}).Error)
	}

	runDBCases(t, h, r, []dbCase{
		{name: "Liste lesen scheitert", op: "query", table: "notification_templates", method: "GET", url: "/api/pages/" + pid + "/notification-templates", token: adminTok, wantStatus: 500, wantCode: "server_error"},
		{name: "Speichern: Laden scheitert", op: "query", table: "notification_templates", method: "PUT", url: url, token: adminTok, body: valid, wantStatus: 500, wantCode: "server_error"},
		{name: "Speichern: Schreiben scheitert", op: "create", table: "notification_templates", method: "PUT", url: url, token: adminTok, body: valid, wantStatus: 500, wantCode: "server_error"},
		{name: "Löschen: Laden scheitert", op: "query", table: "notification_templates", method: "DELETE", url: url, token: adminTok, wantStatus: 500, wantCode: "server_error"},
		{name: "Löschen: Schreiben scheitert", op: "delete", table: "notification_templates", method: "DELETE", url: url, token: adminTok, prepare: seedTemplate, wantStatus: 500, wantCode: "server_error"},
	})
}

func TestFehlercodes_Serverfehler_Bestellung(t *testing.T) {
	h, r, _, _, _, _ := newCodeEnvRouter(t)
	db := h.DB
	valid := map[string]any{"company_name": "Acme", "email": "x613@example.com", "slug": "acme-613"}

	runDBCases(t, h, r, []dbCase{
		{name: "Firmenname leer", method: "POST", url: "/api/public/orders", body: map[string]any{"company_name": "   ", "email": "x613@example.com"}, wantStatus: 400, wantCode: "name_required"},
		{name: "Bestellung speichern scheitert", op: "create", table: "page_orders", method: "POST", url: "/api/public/orders", body: valid, wantStatus: 500, wantCode: "server_error"},
	})

	t.Run("Bestätigen ohne Rolle Page Admin", func(t *testing.T) {
		order := createPendingOrder(t, db, "Acme", "rolle613@example.com", "rolle-613")
		status, out := doJSON(t, r, "POST", "/api/public/orders/confirm", "", map[string]any{"token": plainToken(order.ConfirmTokenHash)})
		assert.Equal(t, http.StatusInternalServerError, status)
		assert.Equal(t, "server_error", out["code"])
	})

	confirmed := func(userID *uint) string {
		token := testToken(t)
		expiry := time.Now().Add(time.Hour)
		order := models.PageOrder{CompanyName: "Acme", Email: "pw" + token[:6] + "@example.com", Slug: "pw-" + token[:6], Status: "confirmed",
			SetPasswordTokenHash: models.HashToken(token), SetPasswordTokenExpiry: &expiry, UserID: userID}
		require.NoError(t, db.Create(&order).Error)
		return token
	}
	setPw := func(token string) map[string]any { return map[string]any{"token": token, "password": "geheim1234"} }
	user := models.User{Email: "frisch613@example.com"}
	require.NoError(t, db.Create(&user).Error)
	missing := uint(99999)

	runDBCases(t, h, r, []dbCase{
		{name: "Passwort: Bestellung ohne Konto", method: "POST", url: "/api/public/orders/set-password", body: setPw(confirmed(nil)), wantStatus: 500, wantCode: "server_error"},
		{name: "Passwort: Konto fehlt", method: "POST", url: "/api/public/orders/set-password", body: setPw(confirmed(&missing)), wantStatus: 500, wantCode: "server_error"},
		{name: "Passwort: Konto speichern scheitert", op: "update", table: "users", method: "POST", url: "/api/public/orders/set-password", body: setPw(confirmed(&user.ID)), wantStatus: 500, wantCode: "server_error"},
		{name: "Passwort: Bestellung speichern scheitert", op: "update", table: "page_orders", method: "POST", url: "/api/public/orders/set-password", body: setPw(confirmed(&user.ID)), wantStatus: 500, wantCode: "server_error"},
	})
}

func TestFehlercodes_Serverfehler_AbonnentenUndWebhook(t *testing.T) {
	h, r, adminTok, _, page, _ := newCodeEnvRouter(t)
	db := h.DB
	handlers.SetWebhookLoopbackAllowedForTests(t.Cleanup)
	pid := strconv.Itoa(int(page.ID))
	sub := models.Subscriber{StatusPageID: page.ID, Email: "a613@example.com", Channel: "email", SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive}
	require.NoError(t, db.Create(&sub).Error)
	sid := strconv.Itoa(int(sub.ID))
	base := "/api/pages/" + pid + "/subscribers"
	adminUpdate := map[string]any{"Channel": "email", "Email": "a613@example.com", "SubscribedComponentIDs": "all"}

	runDBCases(t, h, r, []dbCase{
		{name: "Abo: unbekannte Statusseite", method: "POST", url: "/api/subscribe", body: map[string]any{"channel": "email", "email": "ok613@example.com", "component_ids": "all", "slug": "gibtesnicht"}, wantStatus: 404, wantCode: "page_not_found"},
		{name: "Abonnenten lesen scheitert", op: "query", table: "subscribers", method: "GET", url: base, token: adminTok, wantStatus: 500, wantCode: "server_error"},
		{name: "Eigenes Abo: unbekannte Statusseite", method: "PUT", url: "/api/me/subscription?slug=gibtesnicht", token: adminTok, body: map[string]any{"component_ids": "all"}, wantStatus: 404, wantCode: "page_not_found"},
		{name: "Eigenes Abo ändern: kaputter Body", method: "PUT", url: "/api/me/subscription?slug=p-613", token: adminTok, body: "{", wantStatus: 400, wantCode: "invalid_request"},
		{name: "Eigenes Abo ändern: Speichern scheitert", op: "update", table: "subscribers", method: "PUT", url: "/api/me/subscription?slug=p-613", token: adminTok, body: map[string]any{"component_ids": "all"}, wantStatus: 500, wantCode: "server_error"},
		{name: "Admin ändern: kaputter Body", method: "PUT", url: base + "/" + sid, token: adminTok, body: "{", wantStatus: 400, wantCode: "invalid_request"},
		{name: "Admin ändern: Speichern scheitert", op: "update", table: "subscribers", method: "PUT", url: base + "/" + sid, token: adminTok, body: adminUpdate, wantStatus: 500, wantCode: "server_error"},
		{name: "Admin löschen: ungültige ID", method: "DELETE", url: base + "/abc", token: adminTok, wantStatus: 400, wantCode: "invalid_id"},
		{name: "Admin löschen: Löschen scheitert", op: "delete", table: "subscribers", method: "DELETE", url: base + "/" + sid, token: adminTok, wantStatus: 500, wantCode: "server_error"},
		{name: "Admin anlegen: Speichern scheitert", op: "create", table: "subscribers", method: "POST", url: base, token: adminTok, body: map[string]any{"Channel": "email", "Email": "neu613@example.com", "SubscribedComponentIDs": "all"}, wantStatus: 500, wantCode: "server_error"},
		{name: "Webhook testen: Senden scheitert", method: "POST", url: "/api/subscribe/test-webhook", token: adminTok, body: map[string]any{"webhook_url": "https://127.0.0.1:1/hook"}, wantStatus: 502, wantCode: "webhook_unreachable"},
	})
}

func TestFehlercodes_Webhook_SeiteFehltUndBody(t *testing.T) {
	h, r, _, _, page, _ := newCodeEnvRouter(t)
	db := h.DB

	t.Run("Statusseite der Komponente fehlt", func(t *testing.T) {
		gone := models.StatusPage{Name: "Weg", Slug: "weg-613"}
		require.NoError(t, db.Create(&gone).Error)
		comp := models.Component{StatusPageID: gone.ID, Name: "X", Status: "operational"}
		require.NoError(t, db.Create(&comp).Error)
		require.NoError(t, db.Unscoped().Delete(&gone).Error)
		status, out := doJSON(t, r, "POST", "/api/webhooks/components/"+strconv.Itoa(int(comp.ID))+"/status", "", map[string]any{"status": "operational"})
		assert.Equal(t, http.StatusUnauthorized, status, "Waise darf sich nicht von unbekannter ID unterscheiden")
		assert.Equal(t, "webhook_token_invalid", out["code"])
	})

	t.Run("Body kaputt trotz gültigem Token", func(t *testing.T) {
		token := testToken(t)
		require.NoError(t, db.Model(&models.StatusPage{}).Where("id = ?", page.ID).Update("webhook_token", models.HashToken(token)).Error)
		comp := models.Component{StatusPageID: page.ID, Name: "Y", Status: "operational"}
		require.NoError(t, db.Create(&comp).Error)
		req, _ := http.NewRequest("POST", "/api/webhooks/components/"+strconv.Itoa(int(comp.ID))+"/status", strings.NewReader("{"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Token", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), `"code":"invalid_request"`)
	})
}
