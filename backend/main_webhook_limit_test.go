package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
)

// ID- und Kennungs-Weg des Monitoring-Webhooks teilen EINEN
// Rate-Limit-Zähler je IP (sonst 40 Token-Versuche pro Minute).
func TestRouter_WebhookRateLimitGemeinsam(t *testing.T) {
	r := newRouter(handlers.NewHandler(testutils.SetupTestDB()))
	paths := []string{"/api/webhooks/components/1/status", "/api/webhooks/component-slugs/x/status"}
	var codes []int
	for i := 0; i < 25; i++ {
		req, _ := http.NewRequest("POST", paths[i%2], strings.NewReader(`{"status":"operational"}`))
		req.RemoteAddr = "203.0.113.9:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		codes = append(codes, w.Code)
	}
	for i, c := range codes {
		if i < 20 {
			assert.NotEqual(t, http.StatusTooManyRequests, c, "Anfrage %d", i+1)
		} else {
			assert.Equal(t, http.StatusTooManyRequests, c, "Anfrage %d", i+1)
		}
	}
}
