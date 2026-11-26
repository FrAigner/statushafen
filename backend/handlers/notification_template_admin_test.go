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

// CRUD and preview tests for the admin endpoints.
// The focus is on the handler chain (route + pages:write
// middleware + handler) achieving the required effects:
// upsert of a template, reset via DELETE, preview with sample data,
// 403 for users without pages:write.

// notifAdminHarness bundles the complete setup that every
// admin endpoint test needs: test DB + global admin + status page +
// Gin router with the same AuthGroup middleware as in main.go.
type notifAdminHarness struct {
	db     *gorm.DB
	router *gin.Engine
	page   models.StatusPage
	admin  models.User
}

func newNotifAdminHarness(t *testing.T) *notifAdminHarness {
	t.Helper()
	db := testutils.SetupTestDB()
	if err := testutils.EnableTranslation(db); err != nil {
		t.Fatal(err)
	}
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

// TestNotificationTemplate_Upsert_CreatesThenUpdates checks the core of the
// admin endpoints: the first PUT creates a new entry, the second
// PUT with the same (kind, language) updates it without creating a second
// record.
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

// TestNotificationTemplate_Delete_ResetsToDefault covers the
// required "reset to default" capability: after DELETE no
// custom row exists anymore, and on the next dispatch the status page falls back
// to the hard-coded default (the renderer
// depends on that, so it suffices to prove the plain row deletion).
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

	// A second DELETE returns 404 because there is no entry anymore - documents
	// the (deliberately non-idempotent) error message from the handler comment.
	rec = env.do(t, http.MethodDelete, "/api/pages/1/notification-templates/incident_resolved/subscriber/de", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on second delete, got %d", rec.Code)
	}
}

// TestNotificationTemplate_Preview_UsesSampleData proves that the
// preview endpoint replaces placeholders with the sample values
// provided by the handler - so the admin gets a realistic
// preview, not the raw {{.Placeholder}} strings.
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

// A template that can be parsed but not executed (comparison
// of different types) makes the preview report 400 with the template error
// instead of 500; nothing is stored.
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

// TestNotificationTemplate_Put_RejectsHeaderInjection proves that the
// validation is actually invoked by the save handler -
// a subject with CR/LF must be rejected with 400 instead of silently slipping into the
// DB.
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

// TestNotificationTemplate_Put_RejectsInvalidKindOrLang covers the
// URL parameter validation. Without these guards, arbitrary kind
// values would be accepted (and ignored again at dispatch for lack of a lookup),
// which could silently leave a "broken" template behind.
//
// The test page has (as created in newNotifAdminHarness) no single
// StatusPageLanguage row - isValidNotificationLanguage therefore falls back
// to the old en/de convention for it (see the comment there), "fr"
// thus stays invalid until the page has a real language catalog.
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

// TestNotificationTemplate_Put_AcceptsMaintenanceKinds secures the
// extension: the three maintenance-window kinds must be
// accepted by PUT. If this line breaks, someone has removed kinds from the whitelist
// or constants.
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

// TestNotificationTemplate_Preview_MaintenanceSampleData - the
// preview sample data must provide different examples per kind,
// otherwise an admin sees the same payment-gateway story on every kind.
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

// TestNotificationTemplate_List_ReturnsPageScopedOnly ensures that
// the GET list returns only templates of the requested status page -
// cross-tenant leaks (analogous on other models)
// are secured here by the WHERE, this test guards that.
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
