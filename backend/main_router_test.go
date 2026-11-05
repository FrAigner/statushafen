package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/metrics"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
)

func TestInitLogging(t *testing.T) {
	// initLogging sets the global slog default logger and (best-effort) loads
	// a .env file. It must not panic even when no .env file is present.
	t.Run("without .env file", func(t *testing.T) {
		origWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("could not get working directory: %v", err)
		}
		defer func() { _ = os.Chdir(origWd) }()
		if err := os.Chdir(t.TempDir()); err != nil {
			t.Fatalf("could not chdir to temp dir: %v", err)
		}
		initLogging()
	})

	t.Run("with .env file present", func(t *testing.T) {
		origWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("could not get working directory: %v", err)
		}
		tmpDir := t.TempDir()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("could not chdir to temp dir: %v", err)
		}
		defer func() { _ = os.Chdir(origWd) }()
		if err := os.WriteFile(".env", []byte("SOME_TEST_VAR=1\n"), 0o644); err != nil {
			t.Fatalf("could not write .env: %v", err)
		}
		initLogging()
	})
}

func TestRegisterHealthcheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("healthy DB returns 200", func(t *testing.T) {
		db := testutils.SetupTestDB()
		h := handlers.NewHandler(db)

		r := gin.New()
		registerHealthcheck(r, h)

		req := httptest.NewRequest("GET", "/healthz", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 from healthy DB, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("unreachable DB returns 500", func(t *testing.T) {
		db := testutils.SetupTestDB()
		h := handlers.NewHandler(db)
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("could not get sqlDB: %v", err)
		}
		// Close the underlying connection so Ping() fails, exercising the
		// "database ping failed" error branch of registerHealthcheck.
		_ = sqlDB.Close()

		r := gin.New()
		registerHealthcheck(r, h)

		req := httptest.NewRequest("GET", "/healthz", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 from unreachable DB, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestRegisterStaticFrontend_NoStaticDir(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("could not get working directory: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("could not chdir to temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	r := gin.New()
	registerStaticFrontend(r)

	// Without a static/index.html, no catch-all route is registered, so an
	// unknown path should fall through to gin's default 404.
	req := httptest.NewRequest("GET", "/some/unknown/path", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 without static frontend, got %d", w.Code)
	}
}

func TestRegisterStaticFrontend_WithStaticDir(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("could not get working directory: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("could not chdir to temp dir: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.MkdirAll("static/assets", 0o755); err != nil {
		t.Fatalf("could not create static dir: %v", err)
	}
	if err := os.WriteFile("static/index.html", []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("could not write index.html: %v", err)
	}

	r := gin.New()
	registerStaticFrontend(r)

	t.Run("unknown API path returns JSON 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/does-not-exist", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("unknown frontend path serves index.html", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/some/spa/route", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 serving index.html fallback, got %d", w.Code)
		}
	})
}

// TestNewRouter_RegistersExpectedRoutes ist ein Rauch-Test für newRouter()
// sowie die daraus ausgelagerten register*Routes-Funktionen (main.go): stellt
// sicher, dass der komplette Router ohne Panik aufgebaut wird und ein paar
// repräsentative öffentliche wie geschützte Routen tatsächlich registriert
// sind (RouterGroup-Registrierung selbst enthält keine Verzweigungslogik, ein
// einziger Smoke-Test deckt daher praktisch alle Zeilen von
// registerPublicRoutes/registerAuthRoutes ab).
func TestNewRouter_RegistersExpectedRoutes(t *testing.T) {
	_ = os.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)

	r := newRouter(h)

	routes := r.Routes()
	byPath := map[string]bool{}
	for _, route := range routes {
		byPath[route.Method+" "+route.Path] = true
	}

	expected := []string{
		"GET /healthz",
		"GET /api/auth/:provider",
		"GET /api/auth/:provider/callback",
		"POST /api/auth/sso-exchange",
		"GET /api/auth/oidc/login",
		"GET /api/auth/oidc/callback",
		"POST /api/auth/oidc/link",
		// Frontend-Einstieg ins Seiten-SSO
		"GET /api/public/pages/:slug/sso",
		"GET /api/me/sso",
		"DELETE /api/me/sso/:id",
		"POST /api/admin/pages/:page_id/sso/test",
		// SAML je Statusseite
		"GET /api/auth/saml/login",
		"GET /api/auth/saml/:page_id/metadata",
		"POST /api/auth/saml/:page_id/acs",
		"POST /api/auth/saml/link",
		"POST /api/admin/pages/:page_id/sso/saml-test",
		"POST /api/login",
		"POST /api/subscribe",
		"GET /api/public/pages",
		"GET /api/me",
		"DELETE /api/me", // Konto löschen
		"GET /api/pages",
		"POST /api/admin/pages",
		"GET /api/users",
		"GET /api/roles",
		"GET /api/tags",
		"GET /api/admin/overview",
	}
	for _, want := range expected {
		if !byPath[want] {
			t.Errorf("expected route %q to be registered, routes seen: %d", want, len(routes))
		}
	}
}

// Nach dem Router-Aufbau stehen alle Routen (Templates) mit 0
// Requests in der Registry, interne wie /metrics fehlen.
func TestRegisterRoutesInMetrics_ListsAllRoutesWithoutInternal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/pages/:page_id/incidents", func(c *gin.Context) {})
	r.POST("/api/pages/:page_id/incidents", func(c *gin.Context) {})
	r.GET("/metrics", func(c *gin.Context) {})

	reg := metrics.NewRegistry()
	registerRoutesInMetrics(r, reg)
	registerRoutesInMetrics(r, reg) // erneutes Registrieren: keine Duplikate

	snaps := reg.Snapshot()
	if len(snaps) != 2 {
		t.Fatalf("expected 2 routes (without /metrics), got %+v", snaps)
	}
	for _, s := range snaps {
		if s.Route != "/api/pages/:page_id/incidents" || s.RequestCount != 0 {
			t.Errorf("unexpected entry %+v", s)
		}
	}
}

func TestNewRouter_RegistersRealRoutesInDefaultMetrics(t *testing.T) {
	metrics.Default.Reset()
	t.Cleanup(metrics.Default.Reset)
	db := testutils.SetupTestDB()
	r := newRouter(handlers.NewHandler(db))

	got := map[string]bool{}
	for _, s := range metrics.Default.Snapshot() {
		got[s.Method+" "+s.Route] = true
		if s.Route == "/metrics" {
			t.Errorf("/metrics must not be listed")
		}
		if s.RequestCount != 0 {
			t.Errorf("expected 0 requests, got %+v", s)
		}
	}
	for _, ri := range r.Routes() {
		if ri.Path != "/metrics" && !got[ri.Method+" "+ri.Path] {
			t.Errorf("route %s %s missing in registry", ri.Method, ri.Path)
		}
	}
	if !got["GET /api/admin/monitoring"] {
		t.Errorf("GET /api/admin/monitoring missing")
	}
}
