package middleware_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/metrics"
	"github.com/fraigner/statushafen/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func methodMetricsRouter(reg *metrics.Registry) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.GoldenSignals(reg))
	r.Any("/probe", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.NoRoute(func(c *gin.Context) { c.Status(http.StatusNotFound) })
	return r
}

// Die HTTP-Methode ist ein freier Token; als Label würde jede erfundene
// Methode eine neue Zeitreihe anlegen (Speicher wächst bis zum Neustart).
func TestGoldenSignals_UnknownMethodsShareOneLabel(t *testing.T) {
	reg := metrics.NewRegistry()
	r := methodMetricsRouter(reg)

	const n = 300
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodGet, "/nirgendwo", nil)
		req.Method = fmt.Sprintf("X%d", i)
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	snap := reg.Snapshot()
	require.Len(t, snap, 1, "alle erfundenen Methoden landen in einem Eintrag")
	assert.Equal(t, "OTHER", snap[0].Method)
	assert.Equal(t, "NOT_FOUND", snap[0].Route)
	assert.EqualValues(t, n, snap[0].RequestCount)
}

func TestGoldenSignals_VeryLongMethodIsNotStored(t *testing.T) {
	reg := metrics.NewRegistry()
	r := methodMetricsRouter(reg)
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Method = strings.Repeat("M", 500_000)
	r.ServeHTTP(httptest.NewRecorder(), req)
	for _, s := range reg.Snapshot() {
		assert.Equal(t, "OTHER", s.Method)
	}
}

// Gegenprobe: die üblichen Methoden behalten ihr Label.
func TestGoldenSignals_KnownMethodsKeepTheirLabel(t *testing.T) {
	reg := metrics.NewRegistry()
	r := methodMetricsRouter(reg)
	known := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions}
	for _, m := range known {
		req := httptest.NewRequest(http.MethodGet, "/probe", nil)
		req.Method = m
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	var got []string
	for _, s := range reg.Snapshot() {
		got = append(got, s.Method)
	}
	assert.ElementsMatch(t, known, got)

	// Kleinschreibung ist kein bekannter Methodenname.
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Method = "get"
	r.ServeHTTP(httptest.NewRecorder(), req)
	var other int
	for _, s := range reg.Snapshot() {
		if s.Method == "OTHER" {
			other++
		}
	}
	assert.Equal(t, 1, other)
}
