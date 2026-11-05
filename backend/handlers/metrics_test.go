package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/metrics"

	"github.com/gin-gonic/gin"
)

func TestRegisterMetricsEndpoint_NotMountedWithoutCredentials(t *testing.T) {
	t.Setenv(handlers.MetricsBasicAuthUserEnv, "")
	t.Setenv(handlers.MetricsBasicAuthPasswordEnv, "")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	reg := metrics.NewRegistry()

	if handlers.RegisterMetricsEndpoint(r, reg) {
		t.Fatal("expected RegisterMetricsEndpoint to report false when no credentials are configured")
	}

	req, _ := http.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected /metrics to be unmounted (404) without credentials, got %d", w.Code)
	}
}

func TestRegisterMetricsEndpoint_RequiresBasicAuth(t *testing.T) {
	t.Setenv(handlers.MetricsBasicAuthUserEnv, "prom")
	t.Setenv(handlers.MetricsBasicAuthPasswordEnv, "test-fixture-not-a-real-password")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	reg := metrics.NewRegistry()

	if !handlers.RegisterMetricsEndpoint(r, reg) {
		t.Fatal("expected RegisterMetricsEndpoint to report true when credentials are configured")
	}

	req, _ := http.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without Basic-Auth credentials, got %d", w.Code)
	}

	req, _ = http.NewRequest("GET", "/metrics", nil)
	req.SetBasicAuth("prom", "wrong-password")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong password, got %d", w.Code)
	}
}

func TestRegisterMetricsEndpoint_ExposesPrometheusFormat(t *testing.T) {
	t.Setenv(handlers.MetricsBasicAuthUserEnv, "prom")
	t.Setenv(handlers.MetricsBasicAuthPasswordEnv, "test-fixture-not-a-real-password")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	reg := metrics.NewRegistry()
	reg.Observe("GET", "/api/pages/:page_id/incidents", 200, 15*time.Millisecond)

	if !handlers.RegisterMetricsEndpoint(r, reg) {
		t.Fatal("expected RegisterMetricsEndpoint to report true when credentials are configured")
	}

	req, _ := http.NewRequest("GET", "/metrics", nil)
	req.SetBasicAuth("prom", "test-fixture-not-a-real-password")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with correct Basic-Auth credentials, got %d, body=%s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, `statuspage_http_requests_total{method="GET",route="/api/pages/:page_id/incidents",status="200"} 1`) {
		t.Errorf("expected exposition body to contain the observed counter sample, got:\n%s", body)
	}
	if !strings.Contains(body, "statuspage_http_request_duration_seconds") {
		t.Errorf("expected exposition body to contain the latency histogram, got:\n%s", body)
	}
}
