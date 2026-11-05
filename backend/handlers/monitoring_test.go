package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/metrics"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
)

func doMonitoringRequest(r http.Handler, token string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest("GET", "/api/admin/monitoring", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMonitoringHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())
	authGroup.GET("/admin/monitoring", middleware.RequireGlobalAdmin(), h.GetMonitoringHandler)

	adminUser := models.User{Email: "monitoring-admin@example.com", IsGlobalAdmin: true}
	db.Create(&adminUser)
	adminToken, _ := middleware.GenerateToken(adminUser.ID, adminUser.Email, "", "", true)

	regularUser := models.User{Email: "monitoring-regular@example.com"}
	db.Create(&regularUser)
	regularToken, _ := middleware.GenerateToken(regularUser.ID, regularUser.Email, "", "", false)

	// Registry isoliert füllen statt über echte Requests, um die Zahlen im
	// Response deterministisch prüfen zu können (Registry.Observe ist die
	// gleiche Einstiegsstelle, die middleware.GoldenSignals in main.go nutzt).
	metrics.Default.Reset()
	metrics.Default.Observe("GET", "/api/monitoring-test-route", 200, 10*time.Millisecond)
	metrics.Default.Observe("GET", "/api/monitoring-test-route", 200, 20*time.Millisecond)
	metrics.Default.Observe("GET", "/api/monitoring-test-route", 500, 30*time.Millisecond)
	defer metrics.Default.Reset()

	t.Run("Non-admin -> 403", func(t *testing.T) {
		w := doMonitoringRequest(r, regularToken)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("Global admin sees aggregated monitoring data", func(t *testing.T) {
		w := doMonitoringRequest(r, adminToken)
		assert.Equal(t, http.StatusOK, w.Code)

		body := w.Body.String()
		assert.Contains(t, body, `"route":"/api/monitoring-test-route"`)
		assert.Contains(t, body, `"method":"GET"`)
		assert.Contains(t, body, `"request_count":3`)
		assert.Contains(t, body, `"error_count":1`)
		assert.Contains(t, body, `"uptime_seconds"`)
		assert.Contains(t, body, `"p50_latency_ms"`)
		assert.Contains(t, body, `"p95_latency_ms"`)
		assert.Contains(t, body, `"requests_per_minute"`)
		assert.Contains(t, body, `"totals"`)
		assert.Contains(t, body, `"request_count":3`) // totals.request_count (nur eine Route beobachtet)

		// Saturation (Golden Signals Schritt 4/4): goroutine/memory
		// stats are always present, DB pool stats reflect
		// DBStatsAvailable=false since no DBStatsProvider is registered in
		// this test setup.
		assert.Contains(t, body, `"saturation"`)
		assert.Contains(t, body, `"goroutine_count"`)
		assert.Contains(t, body, `"heap_alloc_bytes"`)
		assert.Contains(t, body, `"db_stats_available":false`)
	})
}

func TestMonitoringHandler_SaturationReflectsDBStatsProvider(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())
	authGroup.GET("/admin/monitoring", middleware.RequireGlobalAdmin(), h.GetMonitoringHandler)

	adminUser := models.User{Email: "monitoring-admin-2@example.com", IsGlobalAdmin: true}
	db.Create(&adminUser)
	adminToken, _ := middleware.GenerateToken(adminUser.ID, adminUser.Email, "", "", true)

	sqlDB, err := db.DB()
	assert.NoError(t, err)
	sqlDB.SetMaxOpenConns(17)

	metrics.Default.SetDBStatsProvider(sqlDB.Stats)
	defer metrics.Default.SetDBStatsProvider(nil)

	w := doMonitoringRequest(r, adminToken)
	assert.Equal(t, http.StatusOK, w.Code)

	body := w.Body.String()
	assert.Contains(t, body, `"db_stats_available":true`)
	assert.Contains(t, body, `"db_max_open_connections":17`)
}

// Der Grafana-Link kommt aus GRAFANA_DASHBOARD_URL (vorher im Frontend
// fest auf staging verdrahtet). Nur http(s)-URLs werden durchgereicht.
func TestMonitoringHandler_GrafanaDashboardURL(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())
	authGroup.GET("/admin/monitoring", middleware.RequireGlobalAdmin(), h.GetMonitoringHandler)
	admin := models.User{Email: "monitoring-grafana@example.com", IsGlobalAdmin: true}
	db.Create(&admin)
	token, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)

	cases := map[string]string{
		"":                                   "",
		"   ":                                "",
		"javascript:alert(1)":                "",
		"ftp://example.com/d":                "",
		"https://":                           "",
		"nicht-absolut":                      "",
		"https://g.example/d/x?var-env=prod": "https://g.example/d/x?var-env=prod",
		"http://g.example/d/x":               "http://g.example/d/x",
	}
	for env, want := range cases {
		t.Run("env="+env, func(t *testing.T) {
			t.Setenv("GRAFANA_DASHBOARD_URL", env)
			w := doMonitoringRequest(r, token)
			assert.Equal(t, http.StatusOK, w.Code)
			var resp struct {
				URL string `json:"grafana_dashboard_url"`
			}
			assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, want, resp.URL)
		})
	}
}

// Regression: 4xx (404 ohne Abo, 409, 422, 429) duerfen die Fehlerquote nicht
// anheben, nur 5xx zaehlen (Betreiber-Entscheidung 1.11.2026).
func TestMonitoringHandler_ErrorRateCountsOnly5xx(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())
	authGroup.GET("/admin/monitoring", middleware.RequireGlobalAdmin(), h.GetMonitoringHandler)
	admin := models.User{Email: "monitoring-5xx@example.com", IsGlobalAdmin: true}
	db.Create(&admin)
	token, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)

	metrics.Default.Reset()
	defer metrics.Default.Reset()
	for _, code := range []int{200, 200, 404, 409, 422, 429, 500, 200, 200, 200} {
		metrics.Default.Observe("GET", "/api/rate-test", code, time.Millisecond)
	}

	w := doMonitoringRequest(r, token)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Totals struct {
			ErrorCount uint64  `json:"error_count"`
			ErrorRate  float64 `json:"error_rate"`
		} `json:"totals"`
		Routes []struct {
			ErrorCount uint64  `json:"error_count"`
			ErrorRate  float64 `json:"error_rate"`
		} `json:"routes"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, uint64(1), resp.Totals.ErrorCount)
	assert.InDelta(t, 0.1, resp.Totals.ErrorRate, 1e-9)
	assert.Len(t, resp.Routes, 1)
	assert.Equal(t, uint64(1), resp.Routes[0].ErrorCount)
	assert.InDelta(t, 0.1, resp.Routes[0].ErrorRate, 1e-9)
}

// Registrierte, nie aufgerufene Routen erscheinen mit 0 Requests,
// ohne Fehlerquote/Latenz (keine NaN/Inf im JSON) und hinter den genutzten.
func TestMonitoringHandler_ListsUnusedRoutesWithZeros(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())
	authGroup.GET("/admin/monitoring", middleware.RequireGlobalAdmin(), h.GetMonitoringHandler)

	adminUser := models.User{Email: "monitoring-unused@example.com", IsGlobalAdmin: true}
	db.Create(&adminUser)
	token, _ := middleware.GenerateToken(adminUser.ID, adminUser.Email, "", "", true)

	metrics.Default.Reset()
	defer metrics.Default.Reset()
	metrics.Default.RegisterRoute("GET", "/api/a-unused")
	metrics.Default.RegisterRoute("GET", "/api/b-used")
	metrics.Default.Observe("GET", "/api/b-used", 200, 10*time.Millisecond)

	w := doMonitoringRequest(r, token)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Routes []struct {
			Route             string  `json:"route"`
			RequestCount      uint64  `json:"request_count"`
			ErrorRate         float64 `json:"error_rate"`
			RequestsPerMinute float64 `json:"requests_per_minute"`
			P95LatencyMs      float64 `json:"p95_latency_ms"`
		} `json:"routes"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	if assert.Len(t, resp.Routes, 2) {
		assert.Equal(t, "/api/b-used", resp.Routes[0].Route)
		assert.Equal(t, "/api/a-unused", resp.Routes[1].Route)
		assert.Zero(t, resp.Routes[1].RequestCount)
		assert.Zero(t, resp.Routes[1].ErrorRate)
		assert.Zero(t, resp.Routes[1].RequestsPerMinute)
		assert.Zero(t, resp.Routes[1].P95LatencyMs)
	}
}
