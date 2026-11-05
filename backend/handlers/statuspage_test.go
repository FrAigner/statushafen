package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// doStatusPageRequest führt einen Request gegen den Test-Router aus und
// JSON-kodiert payload (falls gesetzt) als Body; reduziert die sonst pro
// Subtest wiederholte Request/Serve-Boilerplate.
func doStatusPageRequest(r http.Handler, method, path string, payload interface{}) *httptest.ResponseRecorder {
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = bytes.NewBuffer(b)
	}
	req, _ := http.NewRequest(method, path, body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// requireStatusCode bricht den (Sub-)Test ab, wenn der Response-Code nicht
// wie erwartet ist - reduziert die sonst pro Testfall wiederholte
// if/Fatalf-Boilerplate der Slug-Kollisions-Tests.
func requireStatusCode(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if w.Code != expected {
		t.Fatalf("Expected %d, got %d: %s", expected, w.Code, w.Body.String())
	}
}

// requireBodyContains bricht den (Sub-)Test ab, wenn der Response-Body die
// angegebene Substring nicht enthält.
func requireBodyContains(t *testing.T, w *httptest.ResponseRecorder, substr string) {
	t.Helper()
	if !strings.Contains(w.Body.String(), substr) {
		t.Fatalf("Expected body to contain %q, got: %s", substr, w.Body.String())
	}
}

// requireBodyNotContains bricht den (Sub-)Test ab, wenn der Response-Body die
// angegebene Substring enthält (z.B. für Datenleck-Regressionstests).
func requireBodyNotContains(t *testing.T, w *httptest.ResponseRecorder, substr string) {
	t.Helper()
	if strings.Contains(w.Body.String(), substr) {
		t.Fatalf("Expected body to NOT contain %q, got: %s", substr, w.Body.String())
	}
}

// mockUserIDMiddleware maps the X-Mock-User-ID test header onto the "userID"
// gin context key, standing in for the real AuthMiddleware in handler tests
// that don't need full JWT verification.
func mockUserIDMiddleware(c *gin.Context) {
	userIDStr := c.GetHeader("X-Mock-User-ID")
	if userIDStr != "" {
		uid, _ := strconv.Atoi(userIDStr)
		c.Set("userID", uint(uid))
	}
	c.Next()
}

func TestStatusPageHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.Use(mockUserIDMiddleware)

	r.GET("/api/public/pages", h.GetPublicPagesHandler)
	r.GET("/api/public/pages/:slug/dashboard", h.GetPublicDashboardHandler)
	r.POST("/api/admin/pages", h.CreateStatusPageHandler)
	r.PUT("/api/admin/pages/:page_id", h.UpdateStatusPageHandler)
	r.DELETE("/api/admin/pages/:page_id", h.DeleteStatusPageHandler)

	t.Run("Create StatusPage", func(t *testing.T) {
		w := doStatusPageRequest(r, "POST", "/api/admin/pages", map[string]interface{}{
			"Name":            "New Tech Page",
			"Slug":            "new-tech",
			"Description":     "Test Desc",
			"IsPublicIndexed": true,
		})

		requireStatusCode(t, w, http.StatusCreated)

		var page models.StatusPage
		db.Where("slug = ?", "new-tech").First(&page)
		if page.Name != "New Tech Page" {
			t.Errorf("Page not created correctly in DB")
		}
	})

	t.Run("Update StatusPage", func(t *testing.T) {
		var page models.StatusPage
		db.Where("slug = ?", "new-tech").First(&page)

		w := doStatusPageRequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID)), map[string]interface{}{
			"Name":            "Updated Tech Page",
			"Slug":            "new-tech",
			"Description":     "Test Desc",
			"IsPublicIndexed": false,
		})

		requireStatusCode(t, w, http.StatusOK)

		db.First(&page, page.ID)
		if page.Name != "Updated Tech Page" || page.IsPublicIndexed != false {
			t.Errorf("Page not updated correctly in DB")
		}
	})

	t.Run("Get Public Dashboard", func(t *testing.T) {
		w := doStatusPageRequest(r, "GET", "/api/public/pages/new-tech/dashboard", nil)

		requireStatusCode(t, w, http.StatusOK)

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)

		pageData := resp["page"].(map[string]interface{})
		if pageData["Name"] != "Updated Tech Page" {
			t.Errorf("Expected Updated Tech Page, got %v", pageData["Name"])
		}
	})

	t.Run("Delete StatusPage", func(t *testing.T) {
		var page models.StatusPage
		db.Where("slug = ?", "new-tech").First(&page)

		// Create a dummy user role linked to this page to verify cascade/cleanup
		db.Create(&models.UserStatusPageRole{UserID: 999, StatusPageID: page.ID, RoleID: 1})

		w := doStatusPageRequest(r, "DELETE", "/api/admin/pages/"+strconv.Itoa(int(page.ID)), nil)

		requireStatusCode(t, w, http.StatusOK)

		var count int64
		db.Model(&models.StatusPage{}).Where("id = ?", page.ID).Count(&count)
		if count != 0 {
			t.Errorf("Page was not deleted")
		}

		// Due to GORM soft deletes, the UserStatusPageRole might still exist or be soft-deleted.
		// If we wanted strict DB foreign keys it would fail or cascade. For now we just test the handler responds 200.
	})

	t.Run("Delete nonexistent StatusPage returns 404", func(t *testing.T) {
		w := doStatusPageRequest(r, "DELETE", "/api/admin/pages/999999", nil)
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Delete StatusPage with invalid id returns 400", func(t *testing.T) {
		w := doStatusPageRequest(r, "DELETE", "/api/admin/pages/not-a-number", nil)
		requireStatusCode(t, w, http.StatusBadRequest)
	})
}

// Regressionstest: GET /api/pages liefert ALLE Statusseiten ungefiltert (auch
// private/unlisted) und muss deshalb Global Admins vorbehalten sein. War zuvor
// komplett öffentlich erreichbar und leakte Namen/Slugs aller Mandanten (u.a.
// sichtbar über AdminOverview.vues Degraded-Mode-Fallback für Nicht-Admins).
func TestGetStatusPagesRequiresGlobalAdmin(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())
	authGroup.GET("/pages", middleware.RequireGlobalAdmin(), h.GetStatusPagesHandler)

	db.Create(&models.StatusPage{Name: "Secret Tenant", Slug: "secret-tenant"})

	adminUser := models.User{Email: "pages-admin@example.com", IsGlobalAdmin: true}
	db.Create(&adminUser)
	adminToken, _ := middleware.GenerateToken(adminUser.ID, adminUser.Email, "", "", true)

	regularUser := models.User{Email: "pages-regular@example.com"}
	db.Create(&regularUser)
	regularToken, _ := middleware.GenerateToken(regularUser.ID, regularUser.Email, "", "", false)

	doPagesGet := func(token string) *httptest.ResponseRecorder {
		req, _ := http.NewRequest("GET", "/api/pages", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("No auth -> 401", func(t *testing.T) {
		w := doPagesGet("")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401, got %d", w.Code)
		}
	})

	t.Run("Non-admin -> 403, keine Seiten-Liste im Body", func(t *testing.T) {
		w := doPagesGet(regularToken)
		if w.Code != http.StatusForbidden {
			t.Fatalf("Expected 403, got %d", w.Code)
		}
		if strings.Contains(w.Body.String(), "Secret Tenant") {
			t.Fatalf("Response body must not leak the page list on 403, got: %s", w.Body.String())
		}
	})

	t.Run("Global admin -> 200 mit Seiten-Liste", func(t *testing.T) {
		w := doPagesGet(adminToken)
		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Secret Tenant") {
			t.Errorf("Expected page list to contain 'Secret Tenant', got: %s", w.Body.String())
		}
	})
}

// Branding-Felder (LogoURL, PrimaryColor, SecondaryColor,
// HeaderStyle) müssen über Create/Update persistiert und in der Response
// zurückgegeben werden. Zusätzlich wird geprüft, dass ungültige Werte
// (kein Hex-Farbwert / unbekannter HeaderStyle) mit 400 abgelehnt werden und
// dass eine bestehende Seite ohne gesetzte Farben (leerer String) weiterhin
// so ausgeliefert wird - der Fallback auf die globalen CSS-Defaults passiert
// bewusst erst im Frontend, nicht im Backend.
func TestStatusPageBrandingFields(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.POST("/api/admin/pages", h.CreateStatusPageHandler)
	r.PUT("/api/admin/pages/:page_id", h.UpdateStatusPageHandler)
	r.GET("/api/pages/slug/:slug", h.GetStatusPageBySlugHandler)

	t.Run("Create with branding fields persists and returns them", func(t *testing.T) {
		w := doStatusPageRequest(r, "POST", "/api/admin/pages", map[string]interface{}{
			"Name":           "Branded Page",
			"Slug":           "branded-page",
			"LogoURL":        "https://example.com/logo.png",
			"PrimaryColor":   "#007bff",
			"SecondaryColor": "#ff8800",
			"HeaderStyle":    "centered",
		})

		requireStatusCode(t, w, http.StatusCreated)

		var page models.StatusPage
		db.Where("slug = ?", "branded-page").First(&page)
		if page.LogoURL != "https://example.com/logo.png" {
			t.Errorf("LogoURL not persisted, got %q", page.LogoURL)
		}
		if page.PrimaryColor != "#007bff" {
			t.Errorf("PrimaryColor not persisted, got %q", page.PrimaryColor)
		}
		if page.SecondaryColor != "#ff8800" {
			t.Errorf("SecondaryColor not persisted, got %q", page.SecondaryColor)
		}
		if page.HeaderStyle != "centered" {
			t.Errorf("HeaderStyle not persisted, got %q", page.HeaderStyle)
		}

		// Auch über die öffentliche GET-Route sichtbar (Response, nicht nur DB).
		w2 := doStatusPageRequest(r, "GET", "/api/pages/slug/branded-page", nil)
		var resp map[string]interface{}
		_ = json.Unmarshal(w2.Body.Bytes(), &resp)
		if resp["PrimaryColor"] != "#007bff" {
			t.Errorf("Expected PrimaryColor #007bff in response, got %v", resp["PrimaryColor"])
		}
	})

	t.Run("Create without branding fields falls back to migration defaults", func(t *testing.T) {
		w := doStatusPageRequest(r, "POST", "/api/admin/pages", map[string]interface{}{
			"Name": "Unbranded Page",
			"Slug": "unbranded-page",
		})

		requireStatusCode(t, w, http.StatusCreated)

		var page models.StatusPage
		db.Where("slug = ?", "unbranded-page").First(&page)
		if page.PrimaryColor != "#4c6ef5" {
			t.Errorf("Expected default PrimaryColor #4c6ef5 (matches global CSS --primary), got %q", page.PrimaryColor)
		}
		if page.SecondaryColor != "#5e6c84" {
			t.Errorf("Expected default SecondaryColor #5e6c84 (matches global CSS --text-secondary), got %q", page.SecondaryColor)
		}
		if page.HeaderStyle != "classic" {
			t.Errorf("Expected default HeaderStyle 'classic', got %q", page.HeaderStyle)
		}
	})

	t.Run("Update persists new branding values", func(t *testing.T) {
		var page models.StatusPage
		db.Where("slug = ?", "unbranded-page").First(&page)

		w := doStatusPageRequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID)), map[string]interface{}{
			"Name":           page.Name,
			"Slug":           page.Slug,
			"PrimaryColor":   "#123456",
			"SecondaryColor": "#abcdef",
			"HeaderStyle":    "minimal",
		})

		requireStatusCode(t, w, http.StatusOK)

		var updated models.StatusPage
		db.First(&updated, page.ID)
		if updated.PrimaryColor != "#123456" || updated.SecondaryColor != "#abcdef" || updated.HeaderStyle != "minimal" {
			t.Errorf("Branding fields not updated correctly: %+v", updated)
		}
	})

	t.Run("Invalid PrimaryColor is rejected with 400", func(t *testing.T) {
		w := doStatusPageRequest(r, "POST", "/api/admin/pages", map[string]interface{}{
			"Name":         "Invalid Color Page",
			"Slug":         "invalid-color-page",
			"PrimaryColor": "not-a-color",
		})

		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Invalid HeaderStyle is rejected with 400", func(t *testing.T) {
		w := doStatusPageRequest(r, "POST", "/api/admin/pages", map[string]interface{}{
			"Name":        "Invalid Header Page",
			"Slug":        "invalid-header-page",
			"HeaderStyle": "fancy-does-not-exist",
		})

		requireStatusCode(t, w, http.StatusBadRequest)
	})
}

// PageLayout ("standard" | "liste") ist ein eigenes, additives
// Feld neben HeaderStyle. Fehlt es im Body, bleibt der Wert bzw. der Default
// "standard" erhalten; ein explizit gesendeter unbekannter oder leerer Wert
// wird mit 400 abgelehnt, ohne den gespeicherten Wert zu verändern.
func TestStatusPagePageLayout(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.POST("/api/admin/pages", h.CreateStatusPageHandler)
	r.PUT("/api/admin/pages/:page_id", h.UpdateStatusPageHandler)
	r.GET("/api/pages/slug/:slug", h.GetStatusPageBySlugHandler)

	create := func(slug string, extra map[string]interface{}) *httptest.ResponseRecorder {
		body := map[string]interface{}{"Name": slug, "Slug": slug}
		for k, v := range extra {
			body[k] = v
		}
		return doStatusPageRequest(r, "POST", "/api/admin/pages", body)
	}
	update := func(page models.StatusPage, extra map[string]interface{}) *httptest.ResponseRecorder {
		body := map[string]interface{}{"Name": page.Name, "Slug": page.Slug}
		for k, v := range extra {
			body[k] = v
		}
		return doStatusPageRequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID)), body)
	}
	load := func(slug string) models.StatusPage {
		var page models.StatusPage
		db.Where("slug = ?", slug).First(&page)
		return page
	}

	t.Run("Create without PageLayout defaults to standard", func(t *testing.T) {
		requireStatusCode(t, create("layout-default", nil), http.StatusCreated)
		if got := load("layout-default").PageLayout; got != "standard" {
			t.Errorf("Expected default PageLayout 'standard', got %q", got)
		}
	})

	t.Run("Create with liste persists it", func(t *testing.T) {
		requireStatusCode(t, create("layout-liste", map[string]interface{}{"PageLayout": "liste"}), http.StatusCreated)
		if got := load("layout-liste").PageLayout; got != "liste" {
			t.Errorf("Expected PageLayout 'liste', got %q", got)
		}
	})

	t.Run("Update switches between liste and standard", func(t *testing.T) {
		page := load("layout-default")
		requireStatusCode(t, update(page, map[string]interface{}{"PageLayout": "liste"}), http.StatusOK)
		if got := load("layout-default").PageLayout; got != "liste" {
			t.Errorf("Expected PageLayout 'liste', got %q", got)
		}
		requireStatusCode(t, update(page, map[string]interface{}{"PageLayout": "standard"}), http.StatusOK)
		if got := load("layout-default").PageLayout; got != "standard" {
			t.Errorf("Expected PageLayout 'standard', got %q", got)
		}
	})

	t.Run("Update without PageLayout keeps the stored value", func(t *testing.T) {
		page := load("layout-liste")
		requireStatusCode(t, update(page, nil), http.StatusOK)
		if got := load("layout-liste").PageLayout; got != "liste" {
			t.Errorf("Expected PageLayout to stay 'liste', got %q", got)
		}
	})

	t.Run("Unknown or empty value is rejected and leaves the stored value", func(t *testing.T) {
		page := load("layout-liste")
		for _, bad := range []string{"fancy", "", "Liste", " liste"} {
			requireStatusCode(t, update(page, map[string]interface{}{"PageLayout": bad}), http.StatusBadRequest)
			if got := load("layout-liste").PageLayout; got != "liste" {
				t.Errorf("PageLayout changed to %q after rejected value %q", got, bad)
			}
			requireStatusCode(t, create("layout-bad", map[string]interface{}{"PageLayout": bad}), http.StatusBadRequest)
		}
	})

	t.Run("Public slug endpoint returns PageLayout", func(t *testing.T) {
		w := doStatusPageRequest(r, "GET", "/api/pages/slug/layout-liste", nil)
		requireStatusCode(t, w, http.StatusOK)
		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["PageLayout"] != "liste" {
			t.Errorf("Expected PageLayout 'liste' in response, got %v", resp["PageLayout"])
		}
	})
}

// TestUpdateStatusPageHandler_IgnoresIDMassAssignment: a
// PUT request that includes an "ID" field in the body must never redirect
// the write to a different Status Page than the one addressed by :page_id in
// the URL. Before the fix, ShouldBindJSON bound directly into the loaded
// models.StatusPage (which embeds gorm.Model without json:"-" on ID), so a
// body like {"ID": <other page>, ...} silently overwrote page.ID and Save()
// updated the wrong tenant's page.
func TestUpdateStatusPageHandler_IgnoresIDMassAssignment(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.PUT("/api/admin/pages/:page_id", h.UpdateStatusPageHandler)

	pageA := models.StatusPage{Name: "Page A", Slug: "page-a-mass-assignment"}
	db.Create(&pageA)
	pageB := models.StatusPage{Name: "Page B", Slug: "page-b-mass-assignment"}
	db.Create(&pageB)

	// Attacker-controlled body targets :page_id=pageA.ID in the URL, but
	// smuggles pageB.ID in the JSON body.
	w := doStatusPageRequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(pageA.ID)), map[string]interface{}{
		"ID":   pageB.ID,
		"Name": "Hijacked Name",
		"Slug": "page-a-mass-assignment",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var reloadedA, reloadedB models.StatusPage
	db.First(&reloadedA, pageA.ID)
	db.First(&reloadedB, pageB.ID)

	if reloadedA.Name != "Hijacked Name" {
		t.Errorf("Expected Page A (addressed by URL) to be updated, got Name=%q", reloadedA.Name)
	}
	if reloadedB.Name != "Page B" {
		t.Errorf("Page B must not be affected by an update addressed at Page A, but Name changed to %q", reloadedB.Name)
	}
}

// TestUpdateStatusPageHandler_SaveErrorReturns500:
// h.DB.Save's error was silently ignored, so a failed update (e.g. the DB
// rejecting the write) still answered 200 OK with the in-memory (unsaved)
// data, hiding data loss from the client. The DB file is made read-only
// after seeding to force a real write failure at Save time, since the
// earlier First() (a read) must still succeed.
func TestUpdateStatusPageHandler_SaveErrorReturns500(t *testing.T) {
	if os.Getenv("JWT_SECRET") == "" {
		_ = os.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	}

	dbPath := filepath.Join(t.TempDir(), "readonly.db")
	writeDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open write DB: %v", err)
	}
	if err := writeDB.AutoMigrate(&models.StatusPage{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	page := models.StatusPage{Name: "Readonly Page", Slug: "readonly-page"}
	if err := writeDB.Create(&page).Error; err != nil {
		t.Fatalf("failed to seed page: %v", err)
	}
	sqlWriteDB, _ := writeDB.DB()
	_ = sqlWriteDB.Close()

	if err := os.Chmod(dbPath, 0o444); err != nil {
		t.Fatalf("failed to chmod db file read-only: %v", err)
	}
	defer func() {
		_ = os.Chmod(dbPath, 0o644)
		_ = os.Remove(dbPath)
	}()

	roDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open read-only DB: %v", err)
	}

	h := handlers.NewHandler(roDB)
	r := testutils.SetupRouter()
	r.PUT("/api/admin/pages/:page_id", h.UpdateStatusPageHandler)

	w := doStatusPageRequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID)), map[string]interface{}{
		"Name": "Should Not Persist",
		"Slug": "readonly-page",
	})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500 when Save fails, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetStatusPageBySlugHandler_PrivatePageRequiresAccess:
// GetStatusPageBySlugHandler is reachable via the public route
// GET /api/pages/slug/:slug (no AuthMiddleware) and previously returned the
// full StatusPage (Name, Description, CustomDomain, LogoURL) for ANY slug,
// including private ones, without checking IsPrivate at all.
func TestGetStatusPageBySlugHandler_PrivatePageRequiresAccess(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/slug/:slug", middleware.OptionalAuthMiddleware(), h.GetStatusPageBySlugHandler)

	role := models.Role{Name: "slug-test-role", Permissions: "incidents:read"}
	db.Create(&role)

	privatePage := models.StatusPage{Name: "Secret Page", Slug: "secret-slug-page", IsPrivate: true, CustomDomain: "secret.example.com"}
	db.Create(&privatePage)

	publicPage := models.StatusPage{Name: "Public Page", Slug: "public-slug-page", IsPrivate: false}
	db.Create(&publicPage)

	memberUser := models.User{Email: "slug-member@example.com"}
	db.Create(&memberUser)
	db.Create(&models.UserStatusPageRole{UserID: memberUser.ID, StatusPageID: privatePage.ID, RoleID: role.ID})
	memberToken, _ := middleware.GenerateToken(memberUser.ID, memberUser.Email, "", "", false)

	outsiderUser := models.User{Email: "slug-outsider@example.com"}
	db.Create(&outsiderUser)
	outsiderToken, _ := middleware.GenerateToken(outsiderUser.ID, outsiderUser.Email, "", "", false)

	doGet := func(slug, token string) *httptest.ResponseRecorder {
		req, _ := http.NewRequest("GET", "/api/pages/slug/"+slug, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("Anonymer Zugriff auf private Seite -> 404, kein Datenleck", func(t *testing.T) {
		w := doGet("secret-slug-page", "")
		requireStatusCode(t, w, http.StatusNotFound)
		requireBodyNotContains(t, w, "secret.example.com")
		requireBodyNotContains(t, w, "Secret Page")
	})

	t.Run("Eingeloggter User ohne Rolle auf private Seite -> 404", func(t *testing.T) {
		w := doGet("secret-slug-page", outsiderToken)
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("User mit Rolle auf private Seite -> 200 mit Daten", func(t *testing.T) {
		w := doGet("secret-slug-page", memberToken)
		requireStatusCode(t, w, http.StatusOK)
		requireBodyContains(t, w, "Secret Page")
	})

	t.Run("Anonymer Zugriff auf öffentliche Seite bleibt erlaubt", func(t *testing.T) {
		w := doGet("public-slug-page", "")
		requireStatusCode(t, w, http.StatusOK)
	})

	t.Run("Unbekannter Slug bleibt 404 (nicht unterscheidbar von privat)", func(t *testing.T) {
		w := doGet("does-not-exist-slug", "")
		requireStatusCode(t, w, http.StatusNotFound)
	})
}

// TestCreateStatusPageHandler_SlugAutoGeneration: eine neue
// Statusseite ist automatisch unter <slug>.<Basisdomain> erreichbar (Caddy
// routet die Wildcard-Domain bereits vollständig),
// der Nutzer muss dafür aber keinen Slug mehr manuell im Formular vergeben.
func TestCreateStatusPageHandler_SlugAutoGeneration(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/admin/pages", h.CreateStatusPageHandler)

	db.Create(&models.StatusPage{Name: "Acme", Slug: "acme"})
	db.Create(&models.StatusPage{Name: "Beta", Slug: "beta-manual"})

	// Tabellengetrieben statt separater Subtest-Bodies: die
	// Request/Assert-Logik ist strukturell für alle vier Fälle identisch und
	// wurde von SonarCloud als Duplikat markiert, als sie noch pro Subtest
	// dupliziert war.
	cases := []struct {
		name       string
		payload    map[string]interface{}
		wantStatus int
		wantSlug   string // leer = nicht geprüft
	}{
		{
			name:       "Leerer Slug wird aus dem Namen generiert (inkl. Umlaut-Transliteration)",
			payload:    map[string]interface{}{"Name": "Müller GmbH Status"},
			wantStatus: http.StatusCreated,
			wantSlug:   "mueller-gmbh-status",
		},
		{
			name:       "Kollidierender Auto-Slug wird durchnummeriert statt abgelehnt",
			payload:    map[string]interface{}{"Name": "Acme"},
			wantStatus: http.StatusCreated,
			wantSlug:   "acme-2",
		},
		{
			name:       "Manuell vergebener, bereits belegter Slug liefert 409 statt 500",
			payload:    map[string]interface{}{"Name": "Beta Zwei", "Slug": "beta-manual"},
			wantStatus: http.StatusConflict,
		},
		{
			name:       "Manuell vergebener Slug mit ungültigen Zeichen liefert 400",
			payload:    map[string]interface{}{"Name": "Invalid Slug Page", "Slug": "Not A Valid Slug!"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doStatusPageRequest(r, "POST", "/api/admin/pages", tc.payload)
			requireStatusCode(t, w, tc.wantStatus)
			if tc.wantSlug == "" {
				return
			}
			var page models.StatusPage
			_ = json.Unmarshal(w.Body.Bytes(), &page)
			if page.Slug != tc.wantSlug {
				t.Errorf("Expected slug %q, got %q", tc.wantSlug, page.Slug)
			}
		})
	}
}

// TestUpdateStatusPageHandler_SlugCollision deckt denselben Kollisions-Schutz
// beim nachträglichen Ändern des Slugs (und damit der Subdomain) einer
// bestehenden Statusseite ab.
func TestUpdateStatusPageHandler_SlugCollision(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.PUT("/api/admin/pages/:page_id", h.UpdateStatusPageHandler)

	pageA := models.StatusPage{Name: "Page A", Slug: "update-collision-a"}
	db.Create(&pageA)
	pageB := models.StatusPage{Name: "Page B", Slug: "update-collision-b"}
	db.Create(&pageB)

	cases := []struct {
		name       string
		payload    map[string]interface{}
		wantStatus int
	}{
		{
			name:       "Slug einer anderen Seite übernehmen liefert 409",
			payload:    map[string]interface{}{"Name": "Page B", "Slug": "update-collision-a"},
			wantStatus: http.StatusConflict,
		},
		{
			name:       "Eigenen Slug unverändert lassen bleibt erlaubt",
			payload:    map[string]interface{}{"Name": "Page B Renamed", "Slug": "update-collision-b"},
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doStatusPageRequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(pageB.ID)), tc.payload)
			requireStatusCode(t, w, tc.wantStatus)
		})
	}
}
