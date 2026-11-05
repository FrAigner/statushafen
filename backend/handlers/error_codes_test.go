package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Rest: Die Handler liefern bei 4xx einen Fehlercode
// (`code`, übersetzt unter api.errors.<code>) statt roher err.Error()-Texte.
// Mandantentrennung (404), Rechte (403) und Fehleingaben (400) je Handler.

func newCodeEnvRouter(t *testing.T) (*handlers.Handler, http.Handler, string, string, models.StatusPage, models.StatusPage) {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	pub := r.Group("/api")
	pub.POST("/subscribe", middleware.OptionalAuthMiddleware(), h.SubscribeHandler)
	pub.POST("/subscribe/test-webhook", middleware.AuthMiddleware(), h.TestWebhookHandler)
	pub.POST("/webhooks/components/:component_id/status", h.ComponentStatusWebhookHandler)
	pub.POST("/public/orders", h.CreateOrderHandler)
	pub.POST("/public/orders/confirm", h.ConfirmOrderHandler)
	pub.POST("/public/orders/set-password", h.SetOrderPasswordHandler)

	comp := pub.Group("/pages/:page_id/components", middleware.AuthMiddleware(), middleware.RequirePermission("components:write"))
	comp.POST("", h.CreateComponentHandler)
	comp.PUT("/:id", h.UpdateComponentHandler)
	inc := pub.Group("/pages/:page_id/incidents", middleware.AuthMiddleware(), middleware.RequirePermission("incidents:write"))
	inc.POST("", h.CreateIncidentHandler)
	inc.PUT("/:id", h.UpdateIncidentHandler)
	inc.POST("/:id/updates", h.AddIncidentUpdateHandler)
	sub := pub.Group("/pages/:page_id/subscribers", middleware.AuthMiddleware(), middleware.RequirePermission("subscribers:write"))
	sub.POST("", h.CreateSubscriberAdminHandler)
	sub.PUT("/:id", h.UpdateSubscriberHandler)
	tpl := pub.Group("/pages/:page_id/notification-templates", middleware.AuthMiddleware(), middleware.RequirePermission("pages:write"))
	tpl.PUT("/:kind/:audience/:language", h.PutNotificationTemplateHandler)
	tpl.POST("/:kind/:audience/:language/preview", h.PreviewNotificationTemplateHandler)
	users := pub.Group("/users", middleware.AuthMiddleware(), middleware.RequireGlobalAdmin())
	users.POST("", h.CreateUserHandler)
	users.PUT("/:id", h.UpdateUserHandler)

	// Lesende und weitere Routen für die Serverfehler-Tests (error_codes_server_test.go).
	r.GET("/api/noparam/components", h.GetComponentsHandler)
	pub.GET("/pages/:page_id/components", middleware.OptionalAuthMiddleware(), h.GetComponentsHandler)
	pub.GET("/pages/:page_id/incidents", middleware.OptionalAuthMiddleware(), h.GetIncidentsHandler)
	pub.GET("/me/subscription", middleware.AuthMiddleware(), h.GetMySubscriptionHandler)
	pub.PUT("/me/subscription", middleware.AuthMiddleware(), h.UpdateMySubscriptionHandler)
	pub.POST("/pages/:page_id/webhook-token", middleware.AuthMiddleware(), h.GenerateWebhookTokenHandler)
	sub.DELETE("/:id", h.DeleteSubscriberHandler)
	pub.GET("/pages/:page_id/subscribers", middleware.AuthMiddleware(), middleware.RequirePermission("subscribers:read"), h.GetSubscribersHandler)
	tpl.GET("", h.GetNotificationTemplatesHandler)
	tpl.DELETE("/:kind/:audience/:language", h.DeleteNotificationTemplateHandler)
	users.GET("", h.GetUsersHandler)

	page := models.StatusPage{Name: "P", Slug: "p-613"}
	other := models.StatusPage{Name: "O", Slug: "o-613"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&other).Error)
	admin := models.User{Email: "a613@example.com", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&admin).Error)
	adminTok, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)
	viewer := models.User{Email: "v613@example.com"}
	require.NoError(t, db.Create(&viewer).Error)
	viewerTok, _ := middleware.GenerateToken(viewer.ID, viewer.Email, "", "", false)
	return h, r, adminTok, viewerTok, page, other
}

func doJSON(t *testing.T, r http.Handler, method, url, token string, payload any) (int, map[string]any) {
	t.Helper()
	var body []byte
	if s, ok := payload.(string); ok {
		body = []byte(s)
	} else {
		body, _ = json.Marshal(payload)
	}
	req, _ := http.NewRequest(method, url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

type codeCase struct {
	name   string
	method string
	url    string
	token  string
	body   any
	status int
	code   string
}

func runCodeCases(t *testing.T, r http.Handler, cases []codeCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, out := doJSON(t, r, tc.method, tc.url, tc.token, tc.body)
			assert.Equal(t, tc.status, status, "%v", out)
			if tc.code != "" { // leer: Antwort der Rechte-Middleware, hat keinen Code
				assert.Equal(t, tc.code, out["code"], "%v", out)
			}
			if msg, ok := out["error"].(string); ok {
				assert.NotContains(t, msg, "Key: '", "kein roher Validator-Text")
			}
		})
	}
}

func TestFehlercodes_KomponentenUndWebhook(t *testing.T) {
	h, r, adminTok, viewerTok, page, other := newCodeEnvRouter(t)
	db := h.DB
	pid, oid := strconv.Itoa(int(page.ID)), strconv.Itoa(int(other.ID))
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	group := models.Component{StatusPageID: page.ID, Name: "Gruppe", Status: "group"}
	otherComp := models.Component{StatusPageID: other.ID, Name: "Fremd", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	require.NoError(t, db.Create(&group).Error)
	child := models.Component{StatusPageID: page.ID, Name: "Kind", Status: "operational", ParentID: &comp.ID}
	require.NoError(t, db.Create(&child).Error)
	require.NoError(t, db.Create(&otherComp).Error)
	foreignTag := models.Tag{Name: "fremd613", StatusPageID: &other.ID}
	require.NoError(t, db.Create(&foreignTag).Error)
	cid := strconv.Itoa(int(comp.ID))
	base := "/api/pages/" + pid + "/components"

	runCodeCases(t, r, []codeCase{
		{"Anlegen: kaputter Body", "POST", base, adminTok, "{", 400, "invalid_request"},
		{"Anlegen: ungültiger Status", "POST", base, adminTok, map[string]any{"name": "X", "status": "kaputt"}, 400, "invalid_component_status"},
		{"Anlegen: Gruppe mit Status", "POST", base, adminTok, map[string]any{"name": "G", "is_group": true, "status": "operational"}, 400, "group_has_no_status"},
		{"Anlegen: Elternknoten unbekannt", "POST", base, adminTok, map[string]any{"name": "X", "parent_id": 99999}, 400, "parent_not_found"},
		{"Anlegen: Elternknoten einer fremden Seite", "POST", base, adminTok, map[string]any{"name": "X", "parent_id": otherComp.ID}, 400, "parent_not_found"},
		{"Anlegen: unbekannter Tag", "POST", base, adminTok, map[string]any{"name": "X", "tag_ids": []uint{99999}}, 400, "tag_not_found"},
		{"Anlegen: Tag einer fremden Seite", "POST", base, adminTok, map[string]any{"name": "X", "tag_ids": []uint{foreignTag.ID}}, 400, "tag_not_found"},
		{"Ändern: Elternknoten ist sie selbst", "PUT", base + "/" + cid, adminTok, map[string]any{"status": "operational", "parent_id": comp.ID}, 400, "parent_is_self"},
		{"Ändern: Zyklus", "PUT", base + "/" + cid, adminTok, map[string]any{"status": "operational", "parent_id": child.ID}, 400, "parent_cycle"},
		{"Ändern: ungültiger Status", "PUT", base + "/" + cid, adminTok, map[string]any{"status": "kaputt"}, 400, "invalid_component_status"},
		{"Ändern: Tag einer fremden Seite", "PUT", base + "/" + cid, adminTok, map[string]any{"status": "operational", "tag_ids": []uint{foreignTag.ID}}, 400, "tag_not_found"},
		{"Ändern: Komponente einer fremden Seite -> 404", "PUT", "/api/pages/" + oid + "/components/" + cid, adminTok, map[string]any{"status": "operational"}, 404, "component_not_found"},
		{"Ändern: ungültige ID", "PUT", base + "/abc", adminTok, map[string]any{"status": "operational"}, 400, "invalid_id"},
		{"Ändern ohne Recht -> 403", "PUT", base + "/" + cid, viewerTok, map[string]any{"status": "operational"}, 403, ""},
		{"Webhook: unbekannte Komponente", "POST", "/api/webhooks/components/99999/status", "", map[string]any{"status": "operational"}, 401, "webhook_token_invalid"},
		{"Webhook: ohne Token", "POST", "/api/webhooks/components/" + cid + "/status", "", map[string]any{"status": "operational"}, 401, "webhook_token_invalid"},
		{"Webhook: ungültige ID", "POST", "/api/webhooks/components/abc/status", "", map[string]any{"status": "operational"}, 400, "invalid_id"},
	})

	t.Run("Webhook mit Token: ungültiger Status und Gruppe", func(t *testing.T) {
		token := testToken(t)
		require.NoError(t, db.Model(&models.StatusPage{}).Where("id = ?", page.ID).Update("webhook_token", models.HashToken(token)).Error)
		post := func(id uint, status string) (int, map[string]any) {
			req, _ := http.NewRequest("POST", "/api/webhooks/components/"+strconv.Itoa(int(id))+"/status", strings.NewReader(`{"status":"`+status+`"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Webhook-Token", token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			var out map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &out)
			return w.Code, out
		}
		status, out := post(comp.ID, "kaputt")
		assert.Equal(t, 400, status)
		assert.Equal(t, "invalid_component_status", out["code"])
		status, out = post(group.ID, "operational")
		assert.Equal(t, 409, status)
		assert.Equal(t, "component_is_group", out["code"])
	})
}

func TestFehlercodes_Vorfaelle(t *testing.T) {
	h, r, adminTok, viewerTok, page, other := newCodeEnvRouter(t)
	db := h.DB
	pid, oid := strconv.Itoa(int(page.ID)), strconv.Itoa(int(other.ID))
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	inc := models.Incident{StatusPageID: page.ID, Title: "T", Message: "m", Status: "investigating"}
	require.NoError(t, db.Create(&inc).Error)
	require.NoError(t, db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID}).Error)
	foreignTag := models.Tag{Name: "fremd613i", StatusPageID: &other.ID}
	require.NoError(t, db.Create(&foreignTag).Error)
	iid := strconv.Itoa(int(inc.ID))
	base := "/api/pages/" + pid + "/incidents"
	ok := map[string]any{"Title": "T", "Message": "m", "Status": "investigating", "ComponentIDs": []uint{comp.ID}}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for kk, vv := range ok {
			m[kk] = vv
		}
		m[k] = v
		return m
	}

	runCodeCases(t, r, []codeCase{
		{"Anlegen: kaputter Body", "POST", base, adminTok, "{", 400, "invalid_request"},
		{"Anlegen: ungültiger Status", "POST", base, adminTok, with("Status", "kaputt"), 400, "invalid_incident_status"},
		{"Anlegen: ungültiger Schweregrad", "POST", base, adminTok, with("Severity", "kaputt"), 400, "invalid_incident_severity"},
		{"Ändern: ungültiger Schweregrad", "PUT", base + "/" + iid, adminTok, map[string]any{"message": "m", "status": "investigating", "Severity": "kaputt", "ComponentIDs": []uint{comp.ID}}, 400, "invalid_incident_severity"},
		{"Anlegen: Status fehlt", "POST", base, adminTok, with("Status", ""), 400, "status_required"},
		{"Anlegen: Wartung ohne Zeiten", "POST", base, adminTok, with("IsMaintenance", true), 400, "maintenance_times_required"},
		{"Anlegen: unbekannter Tag", "POST", base, adminTok, with("TagIDs", []uint{99999}), 400, "tag_not_found"},
		{"Anlegen: Tag einer fremden Seite", "POST", base, adminTok, with("TagIDs", []uint{foreignTag.ID}), 400, "tag_not_found"},
		{"Anlegen: Komponente einer fremden Seite", "POST", "/api/pages/" + oid + "/incidents", adminTok, ok, 404, "components_not_found"},
		{"Ändern: ungültiger Status", "PUT", base + "/" + iid, adminTok, map[string]any{"message": "m", "status": "kaputt", "ComponentIDs": []uint{comp.ID}}, 400, "invalid_incident_status"},
		{"Ändern: Ende bei offenem Vorfall", "PUT", base + "/" + iid, adminTok, map[string]any{"message": "m", "status": "investigating", "ResolvedAt": "2026-01-01T10:00:00Z", "ComponentIDs": []uint{comp.ID}}, 400, "resolved_at_needs_resolved"},
		{"Ändern: Tag einer fremden Seite", "PUT", base + "/" + iid, adminTok, map[string]any{"message": "m", "status": "investigating", "TagIDs": []uint{foreignTag.ID}, "ComponentIDs": []uint{comp.ID}}, 400, "tag_not_found"},
		{"Ändern: Vorfall einer fremden Seite -> 404", "PUT", "/api/pages/" + oid + "/incidents/" + iid, adminTok, map[string]any{"message": "m", "status": "investigating"}, 404, "incident_not_found"},
		{"Update: ungültiger Status", "POST", base + "/" + iid + "/updates", adminTok, map[string]any{"status": "kaputt", "message": "x"}, 400, "invalid_incident_status"},
		{"Ändern ohne Recht -> 403", "PUT", base + "/" + iid, viewerTok, map[string]any{"message": "m", "status": "investigating"}, 403, ""},
	})
}

func TestFehlercodes_Benutzer(t *testing.T) {
	h, r, adminTok, viewerTok, page, _ := newCodeEnvRouter(t)
	db := h.DB
	pageTag := models.Tag{Name: "seite613", StatusPageID: &page.ID}
	require.NoError(t, db.Create(&pageTag).Error)
	target := models.User{Email: "ziel613@example.com"}
	require.NoError(t, db.Create(&target).Error)
	tid := strconv.Itoa(int(target.ID))

	runCodeCases(t, r, []codeCase{
		{"Anlegen: kaputter Body", "POST", "/api/users", adminTok, "{", 400, "invalid_request"},
		{"Anlegen: E-Mail existiert", "POST", "/api/users", adminTok, map[string]any{"email": "ziel613@example.com", "password": "geheim1234"}, 400, "email_taken"},
		{"Anlegen: unbekannter Tag", "POST", "/api/users", adminTok, map[string]any{"email": "neu613@example.com", "password": "geheim1234", "tag_ids": []uint{99999}}, 400, "tag_not_found"},
		{"Anlegen: seitengebundener Tag", "POST", "/api/users", adminTok, map[string]any{"email": "neu613@example.com", "password": "geheim1234", "tag_ids": []uint{pageTag.ID}}, 400, "tag_page_bound"},
		{"Ändern: seitengebundener Tag", "PUT", "/api/users/" + tid, adminTok, map[string]any{"email": "ziel613@example.com", "tag_ids": []uint{pageTag.ID}}, 400, "tag_page_bound"},
		{"Ändern: unbekannter Benutzer", "PUT", "/api/users/99999", adminTok, map[string]any{"email": "x@example.com"}, 404, "user_not_found"},
		{"Ändern: ungültige ID", "PUT", "/api/users/abc", adminTok, map[string]any{"email": "x@example.com"}, 400, "invalid_id"},
		{"Anlegen ohne Recht -> 403", "POST", "/api/users", viewerTok, map[string]any{"email": "x@example.com"}, 403, ""},
	})
}

func TestFehlercodes_AbonnentenUndWebhookUrl(t *testing.T) {
	h, r, adminTok, viewerTok, page, other := newCodeEnvRouter(t)
	db := h.DB
	pid, oid := strconv.Itoa(int(page.ID)), strconv.Itoa(int(other.ID))
	sub := models.Subscriber{StatusPageID: page.ID, Email: "s613@example.com", Channel: "email", SubscribedComponentIDs: "all", Status: models.SubscriberStatusActive}
	require.NoError(t, db.Create(&sub).Error)
	sid := strconv.Itoa(int(sub.ID))
	base := "/api/pages/" + pid + "/subscribers"

	runCodeCases(t, r, []codeCase{
		{"Abo: kaputter Body", "POST", "/api/subscribe", "", "{", 400, "invalid_request"},
		{"Abo: ungültige E-Mail", "POST", "/api/subscribe", "", map[string]any{"channel": "email", "email": "kaputt", "component_ids": "all", "slug": "p-613"}, 400, "email_invalid"},
		{"Abo: Google Chat ohne URL", "POST", "/api/subscribe", "", map[string]any{"channel": "google_chat", "component_ids": "all", "slug": "p-613"}, 400, "webhook_url_required"},
		{"Abo: Google Chat per http", "POST", "/api/subscribe", "", map[string]any{"channel": "google_chat", "webhook_url": "http://chat.example.com/x", "component_ids": "all", "slug": "p-613"}, 400, "webhook_url_invalid"},
		{"Abo: Google Chat auf interne Adresse", "POST", "/api/subscribe", "", map[string]any{"channel": "google_chat", "webhook_url": "https://127.0.0.1/x", "component_ids": "all", "slug": "p-613"}, 400, "webhook_url_invalid"},
		{"Abo: Slug fehlt", "POST", "/api/subscribe", "", map[string]any{"channel": "email", "email": "ok613@example.com", "component_ids": "all"}, 400, "slug_required"},
		{"Webhook testen: URL fehlt", "POST", "/api/subscribe/test-webhook", adminTok, map[string]any{}, 400, "webhook_url_required"},
		{"Webhook testen: http", "POST", "/api/subscribe/test-webhook", adminTok, map[string]any{"webhook_url": "http://chat.example.com/x"}, 400, "webhook_https_only"},
		{"Admin anlegen: Google Chat ohne URL", "POST", base, adminTok, map[string]any{"Channel": "google_chat", "SubscribedComponentIDs": "all"}, 400, "webhook_url_required"},
		{"Admin anlegen: Google Chat intern", "POST", base, adminTok, map[string]any{"Channel": "google_chat", "WebhookURL": "https://127.0.0.1/x", "SubscribedComponentIDs": "all"}, 400, "webhook_internal_address"},
		{"Admin ändern: Kanalwechsel auf http", "PUT", base + "/" + sid, adminTok, map[string]any{"Channel": "google_chat", "WebhookURL": "http://chat.example.com/x", "SubscribedComponentIDs": "all"}, 400, "webhook_https_only"},
		{"Admin ändern: Abo einer fremden Seite -> 404", "PUT", "/api/pages/" + oid + "/subscribers/" + sid, adminTok, map[string]any{"Channel": "email", "Email": "x@example.com", "SubscribedComponentIDs": "all"}, 404, "subscriber_not_found"},
		{"Admin ändern: ungültige ID", "PUT", base + "/abc", adminTok, map[string]any{"channel": "email", "email": "x@example.com"}, 400, "invalid_id"},
		{"Admin ändern ohne Recht -> 403", "PUT", base + "/" + sid, viewerTok, map[string]any{"channel": "email", "email": "x@example.com"}, 403, ""},
	})
}

func TestFehlercodes_Vorlagen(t *testing.T) {
	h, r, adminTok, viewerTok, page, other := newCodeEnvRouter(t)
	_ = h
	pid, oid := strconv.Itoa(int(page.ID)), strconv.Itoa(int(other.ID))
	url := "/api/pages/" + pid + "/notification-templates/incident_created/subscriber/de"

	runCodeCases(t, r, []codeCase{
		{"Speichern: kaputter Body", "PUT", url, adminTok, "{", 400, "invalid_request"},
		{"Speichern: Betreff leer", "PUT", url, adminTok, map[string]any{"Subject": "   ", "BodyHTML": "<p>x</p>"}, 400, "template_subject_empty"},
		{"Speichern: Inhalt leer", "PUT", url, adminTok, map[string]any{"Subject": "S", "BodyHTML": "   "}, 400, "template_body_empty"},
		{"Speichern: Zeilenumbruch im Betreff", "PUT", url, adminTok, map[string]any{"Subject": "A\r\nBcc: x", "BodyHTML": "<p>x</p>"}, 400, "template_subject_crlf"},
		{"Speichern: Betreff-Vorlage defekt", "PUT", url, adminTok, map[string]any{"Subject": "{{.Kaputt", "BodyHTML": "<p>x</p>"}, 400, "template_invalid"},
		{"Speichern: Inhalt-Vorlage defekt", "PUT", url, adminTok, map[string]any{"Subject": "S", "BodyHTML": "{{.Kaputt"}, 400, "template_invalid"},
		{"Speichern: unbekannte Art", "PUT", "/api/pages/" + pid + "/notification-templates/gibtesnicht/subscriber/de", adminTok, map[string]any{"Subject": "S", "BodyHTML": "x"}, 400, "invalid_request"},
		{"Vorschau: kaputter Body", "POST", url + "/preview", adminTok, "{", 400, "invalid_request"},
		{"Vorschau: Vorlage defekt", "POST", url + "/preview", adminTok, map[string]any{"Subject": "{{.Kaputt", "BodyHTML": "<p>x</p>"}, 400, "template_invalid"},
		{"Vorschau: Ausführungsfehler", "POST", url + "/preview", adminTok, map[string]any{"Subject": "{{.GibtEsNicht}}", "BodyHTML": "<p>x</p>"}, 400, "template_invalid"},
		{"Speichern ohne Recht -> 403", "PUT", url, viewerTok, map[string]any{"Subject": "S", "BodyHTML": "x"}, 403, ""},
		{"Speichern auf fremder Seite ohne Recht -> 403", "PUT", "/api/pages/" + oid + "/notification-templates/incident_created/subscriber/de", viewerTok, map[string]any{"Subject": "S", "BodyHTML": "x"}, 403, ""},
	})
}

func TestFehlercodes_Bestellung(t *testing.T) {
	_, r, _, _, _, _ := newCodeEnvRouter(t)
	for _, tc := range []struct{ name, url string }{
		{"Bestellung anlegen", "/api/public/orders"},
		{"Bestellung bestätigen", "/api/public/orders/confirm"},
		{"Passwort setzen", "/api/public/orders/set-password"},
	} {
		t.Run(tc.name+": Typfehler im Body liefert kein Go-Fehlerwerk", func(t *testing.T) {
			status, out := doJSON(t, r, "POST", tc.url, "", `{"email": 5, "token": 7, "password": 1}`)
			assert.Equal(t, http.StatusBadRequest, status)
			assert.Equal(t, "invalid_request", out["code"])
			msg, _ := out["error"].(string)
			assert.NotContains(t, msg, "json: cannot unmarshal")
			assert.NotContains(t, msg, "Go struct field")
		})
		t.Run(tc.name+": Pflichtfelder fehlen", func(t *testing.T) {
			status, out := doJSON(t, r, "POST", tc.url, "", map[string]any{})
			assert.Equal(t, http.StatusBadRequest, status)
			assert.Equal(t, "invalid_request", out["code"])
			msg, _ := out["error"].(string)
			assert.NotContains(t, msg, "Key: '")
		})
	}
}

func TestFehlercodes_ExterneUebernahme(t *testing.T) {
	f := newExtFixture(t)
	src := f.createSource(f.page, f.adminTok, "https://status.example.com")
	a := f.extComp(src, "a", "A", "operational")
	foreignTag := models.Tag{Name: "fremd613e", StatusPageID: &f.other.ID}
	require.NoError(t, f.db.Create(&foreignTag).Error)
	path := f.importPath(f.page, src)

	runCodeCases(t, f.r, []codeCase{
		{"Ziel keine Gruppe", "POST", path, f.pageAdminTok, map[string]any{"external_component_ids": []uint{a.ID}, "parent_id": f.comp.ID}, 400, "parent_not_group"},
		{"Ziel unbekannt", "POST", path, f.pageAdminTok, map[string]any{"external_component_ids": []uint{a.ID}, "parent_id": 99999}, 400, "parent_not_found"},
		{"Ziel auf fremder Seite", "POST", path, f.pageAdminTok, map[string]any{"external_component_ids": []uint{a.ID}, "parent_id": f.otherComp.ID}, 400, "parent_not_found"},
		{"Tag einer fremden Seite", "POST", path, f.pageAdminTok, map[string]any{"external_component_ids": []uint{a.ID}, "tag_ids": []uint{foreignTag.ID}}, 400, "tag_not_found"},
		{"unbekannter Tag", "POST", path, f.pageAdminTok, map[string]any{"external_component_ids": []uint{a.ID}, "tag_ids": []uint{99999}}, 400, "tag_not_found"},
		{"leere Auswahl", "POST", path, f.pageAdminTok, map[string]any{"external_component_ids": []uint{}}, 400, "invalid_request"},
		{"unbekannter Baustein", "POST", path, f.pageAdminTok, map[string]any{"external_component_ids": []uint{99999}}, 404, "external_component_not_found"},
	})
}
