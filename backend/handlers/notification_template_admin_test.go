package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// CRUD- und Preview-Tests für die Admin-Endpunkte.
// Der Fokus liegt darauf, dass die Handler-Kette (Route + pages:write-
// Middleware + Handler) die geforderten Wirkungen erzielt:
// Upsert einer Vorlage, Reset per DELETE, Vorschau mit Beispieldaten,
// 403 für User ohne pages:write.

// notifAdminHarness bündelt den vollständigen Aufbau, den jeder
// Admin-Endpunkt-Test braucht: Test-DB + Global-Admin + Statusseite +
// Gin-Router mit derselben AuthGroup-Middleware wie in main.go.
type notifAdminHarness struct {
	db     *gorm.DB
	router *gin.Engine
	page   models.StatusPage
	admin  models.User
}

func newNotifAdminHarness(t *testing.T) *notifAdminHarness {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)

	globalAdminRole := models.Role{Name: "Global Admin (Test)", Permissions: "pages:write"}
	if err := db.Create(&globalAdminRole).Error; err != nil {
		t.Fatalf("create global admin role: %v", err)
	}
	admin := models.User{Email: "notif-admin@example.com", IsGlobalAdmin: true, RoleID: globalAdminRole.ID}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}

	page := models.StatusPage{Name: "Notif Tpl Page", Slug: "notif-tpl-page"}
	if err := db.Create(&page).Error; err != nil {
		t.Fatalf("create page: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/api")
	authed.Use(func(c *gin.Context) {
		c.Set("userID", admin.ID)
		c.Set("email", admin.Email)
		c.Set("isGlobalAdmin", true)
		c.Next()
	})
	notifTplAdmin := authed.Group("/pages/:page_id/notification-templates")
	notifTplAdmin.Use(middleware.RequirePermission("pages:write"))
	notifTplAdmin.GET("", h.GetNotificationTemplatesHandler)
	notifTplAdmin.PUT("/:kind/:audience/:language", h.PutNotificationTemplateHandler)
	notifTplAdmin.DELETE("/:kind/:audience/:language", h.DeleteNotificationTemplateHandler)
	notifTplAdmin.POST("/:kind/:audience/:language/preview", h.PreviewNotificationTemplateHandler)

	return &notifAdminHarness{db: db, router: r, page: page, admin: admin}
}

func (h *notifAdminHarness) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

// TestNotificationTemplate_Upsert_CreatesThenUpdates prüft den Kern der
// Admin-Endpunkte: der erste PUT legt einen neuen Eintrag an, der zweite
// PUT mit derselben (Kind, Language) aktualisiert ihn, ohne einen zweiten
// Datensatz zu erzeugen.
func TestNotificationTemplate_Upsert_CreatesThenUpdates(t *testing.T) {
	env := newNotifAdminHarness(t)

	path := "/api/pages/1/notification-templates/incident_created/staff/en"
	rec := env.do(t, http.MethodPut, path, map[string]string{
		"Subject":  "Initial subject {{.IncidentTitle}}",
		"BodyHTML": "<p>Initial body {{.Message}}</p>",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("first PUT: expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = env.do(t, http.MethodPut, path, map[string]string{
		"Subject":  "Updated subject",
		"BodyHTML": "<p>Updated body</p>",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("second PUT: expected 200, got %d", rec.Code)
	}

	var count int64
	env.db.Model(&models.NotificationTemplate{}).Where("status_page_id = ? AND kind = ? AND language = ?", env.page.ID, handlers.NotificationKindIncidentCreated, "en").Count(&count)
	if count != 1 {
		t.Fatalf("expected exactly one row for (page, kind, language), got %d", count)
	}
	var tmpl models.NotificationTemplate
	env.db.First(&tmpl, "status_page_id = ? AND kind = ? AND language = ?", env.page.ID, handlers.NotificationKindIncidentCreated, "en")
	if tmpl.Subject != "Updated subject" {
		t.Fatalf("expected updated subject, got %q", tmpl.Subject)
	}
}

// TestNotificationTemplate_Delete_ResetsToDefault deckt die
// geforderte "Reset auf Default"-Möglichkeit ab: nach DELETE existiert
// keine Custom-Zeile mehr, die Statusseite fällt beim nächsten Versand
// auf den hartkodierten Default zurück (davon macht der Renderer
// sich abhängig, es reicht also die reine Row-Löschung nachzuweisen).
func TestNotificationTemplate_Delete_ResetsToDefault(t *testing.T) {
	env := newNotifAdminHarness(t)
	env.db.Create(&models.NotificationTemplate{
		StatusPageID: env.page.ID,
		Kind:         handlers.NotificationKindIncidentResolved,
		Audience:     handlers.NotificationAudienceSubscriber,
		Language:     "de",
		Subject:      "existing",
		BodyHTML:     "<p>existing</p>",
	})

	rec := env.do(t, http.MethodDelete, "/api/pages/1/notification-templates/incident_resolved/subscriber/de", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var count int64
	env.db.Model(&models.NotificationTemplate{}).Where("status_page_id = ?", env.page.ID).Count(&count)
	if count != 0 {
		t.Fatalf("expected the row to be gone, got %d rows", count)
	}

	// Zweites DELETE liefert 404, weil es keinen Eintrag mehr gibt - dokumentiert
	// die (bewusst nicht-idempotente) Fehlermeldung aus dem Handler-Kommentar.
	rec = env.do(t, http.MethodDelete, "/api/pages/1/notification-templates/incident_resolved/subscriber/de", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on second delete, got %d", rec.Code)
	}
}

// TestNotificationTemplate_Preview_UsesSampleData weist nach, dass der
// Preview-Endpunkt Platzhalter durch die vom Handler bereitgestellten
// Beispielwerte ersetzt - der Admin bekommt also eine realistische
// Vorschau, nicht die rohen {{.Placeholder}}-Zeichenfolgen.
func TestNotificationTemplate_Preview_UsesSampleData(t *testing.T) {
	env := newNotifAdminHarness(t)

	rec := env.do(t, http.MethodPost, "/api/pages/1/notification-templates/incident_created/staff/en/preview", map[string]string{
		"Subject":  "[{{.Status}}] {{.IncidentTitle}}",
		"BodyHTML": `<p>Hi, {{.ComponentNames}} is affected. Details: {{.Message}}</p>`,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !strings.Contains(resp["subject"], "Payment gateway degraded") {
		t.Fatalf("expected sample title in preview subject, got: %s", resp["subject"])
	}
	if !strings.Contains(resp["bodyHTML"], "Payment API, Billing UI") {
		t.Fatalf("expected sample component names in preview body, got: %s", resp["bodyHTML"])
	}
}

// Eine Vorlage, die sich parsen, aber nicht ausführen lässt (Vergleich
// unterschiedlicher Typen), meldet die Vorschau als 400 mit dem Vorlagenfehler
// statt als 500; gespeichert wird nichts.
func TestNotificationTemplate_Preview_ExecutionErrorReturns400(t *testing.T) {
	env := newNotifAdminHarness(t)

	rec := env.do(t, http.MethodPost, "/api/pages/1/notification-templates/incident_created/staff/en/preview", map[string]string{
		"Subject":  "S",
		"BodyHTML": `{{if eq .IncidentTitle 1}}x{{end}}`,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"template_invalid"`) {
		t.Fatalf("expected template_invalid, got: %s", rec.Body.String())
	}
	var n int64
	env.db.Model(&models.NotificationTemplate{}).Count(&n)
	if n != 0 {
		t.Fatalf("preview must not persist anything, found %d templates", n)
	}
}

// TestNotificationTemplate_Put_RejectsHeaderInjection weist nach, dass die
// Validierung durch den Save-Handler tatsächlich aufgerufen wird -
// ein Subject mit CR/LF muss mit 400 abgelehnt werden statt still in die
// DB durchzurutschen.
func TestNotificationTemplate_Put_RejectsHeaderInjection(t *testing.T) {
	env := newNotifAdminHarness(t)

	rec := env.do(t, http.MethodPut, "/api/pages/1/notification-templates/incident_created/staff/en", map[string]string{
		"Subject":  "Hello\r\nBcc: attacker@example.com",
		"BodyHTML": "<p>Body</p>",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for header-injection subject, got %d", rec.Code)
	}
	var count int64
	env.db.Model(&models.NotificationTemplate{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected no row to be created after validation failure, got %d", count)
	}
}

// TestNotificationTemplate_Put_RejectsInvalidKindOrLang deckt die
// URL-Parameter-Validierung ab. Ohne diese Guards würden beliebige Kind-
// Werte akzeptiert (und beim Versand mangels Lookup wieder ignoriert),
// was still ein "kaputtes" Template hinterlassen könnte.
//
// Die Testseite hat (wie in newNotifAdminHarness angelegt) keine einzige
// StatusPageLanguage-Zeile - isValidNotificationLanguage fällt für sie
// deshalb auf die alte en/de-Konvention zurück (siehe Kommentar dort), "fr"
// bleibt also ungültig, bis die Seite einen echten Sprachkatalog hat.
func TestNotificationTemplate_Put_RejectsInvalidKindOrLang(t *testing.T) {
	env := newNotifAdminHarness(t)

	payload := map[string]string{"Subject": "Subject", "BodyHTML": "<p>Body</p>"}
	rec := env.do(t, http.MethodPut, "/api/pages/1/notification-templates/incident_unknown/staff/en", payload)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown kind, got %d", rec.Code)
	}
	rec = env.do(t, http.MethodPut, "/api/pages/1/notification-templates/incident_created/staff/fr", payload)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown language, got %d", rec.Code)
	}
	rec = env.do(t, http.MethodPut, "/api/pages/1/notification-templates/incident_created/bogus-audience/en", payload)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown audience, got %d", rec.Code)
	}
}

// TestNotificationTemplate_Put_AcceptsMaintenanceKinds sichert die
// Erweiterung: die drei Wartungsfenster-Kinds müssen von PUT akzeptiert
// werden. Wenn diese Zeile bricht, hat jemand Kinds aus der Whitelist
// oder Konstanten entfernt.
func TestNotificationTemplate_Put_AcceptsMaintenanceKinds(t *testing.T) {
	env := newNotifAdminHarness(t)
	payload := map[string]string{"Subject": "Wartung", "BodyHTML": "<p>Body</p>"}
	for _, kind := range []string{handlers.NotificationKindMaintenanceReminder, handlers.NotificationKindMaintenanceStarted, handlers.NotificationKindMaintenanceCompleted} {
		rec := env.do(t, http.MethodPut, "/api/pages/1/notification-templates/"+kind+"/subscriber/de", payload)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s: expected 200, got %d, body=%s", kind, rec.Code, rec.Body.String())
		}
	}
	var count int64
	env.db.Model(&models.NotificationTemplate{}).Where("status_page_id = ?", env.page.ID).Count(&count)
	if count != 3 {
		t.Fatalf("expected three maintenance templates persisted, got %d", count)
	}
}

// TestNotificationTemplate_Preview_MaintenanceSampleData - die
// Vorschau-Sample-Data muss pro Kind unterschiedliche Beispiele liefern,
// sonst sieht ein Admin auf jedem Kind dieselbe Zahlungs-Gateway-Story.
func TestNotificationTemplate_Preview_MaintenanceSampleData(t *testing.T) {
	env := newNotifAdminHarness(t)
	rec := env.do(t, http.MethodPost, "/api/pages/1/notification-templates/maintenance_started/staff/en/preview", map[string]string{
		"Subject":  "{{.IncidentTitle}}",
		"BodyHTML": "<p>{{.Message}}</p>",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp["subject"], "DB maintenance") {
		t.Fatalf("expected maintenance-specific sample title, got: %s", resp["subject"])
	}
}

// TestNotificationTemplate_List_ReturnsPageScopedOnly stellt sicher, dass
// die GET-Liste nur Templates der angefragten Statusseite zurückgibt -
// mandantenübergreifende Leaks (analog auf anderen Modellen)
// sind hier durch das WHERE gesichert, dieser Test wacht darüber.
func TestNotificationTemplate_List_ReturnsPageScopedOnly(t *testing.T) {
	env := newNotifAdminHarness(t)
	other := models.StatusPage{Name: "Other", Slug: "other"}
	if err := env.db.Create(&other).Error; err != nil {
		t.Fatalf("create other page: %v", err)
	}

	env.db.Create(&models.NotificationTemplate{StatusPageID: env.page.ID, Kind: handlers.NotificationKindIncidentCreated, Audience: handlers.NotificationAudienceStaff, Language: "en", Subject: "mine", BodyHTML: "<p>mine</p>"})
	env.db.Create(&models.NotificationTemplate{StatusPageID: other.ID, Kind: handlers.NotificationKindIncidentCreated, Audience: handlers.NotificationAudienceStaff, Language: "en", Subject: "other", BodyHTML: "<p>other</p>"})

	rec := env.do(t, http.MethodGet, "/api/pages/1/notification-templates", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var list []models.NotificationTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list) != 1 || list[0].Subject != "mine" {
		t.Fatalf("expected only 'mine' in list, got %+v", list)
	}
}
