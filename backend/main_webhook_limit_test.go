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

// The ID and identifier paths of the monitoring webhook share ONE
// rate-limit counter per IP (otherwise 40 token attempts per minute).
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
