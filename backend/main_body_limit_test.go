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

func postBody(t *testing.T, path string, size int) *httptest.ResponseRecorder {
	t.Helper()
	r := newRouter(handlers.NewHandler(testutils.SetupTestDB()))
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(strings.Repeat("a", size)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.50:1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// The router limits every body globally; oversized requests yield 413 with an
// error code before a handler reads them (login, order, subscription, etc.).
func TestRouter_OversizedBodyIs413(t *testing.T) {
	for _, path := range []string{"/api/login", "/api/public/orders", "/api/subscribe"} {
		w := postBody(t, path, defaultMaxBodyBytes+1)
		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, path)
		assert.Contains(t, w.Body.String(), `"code":"request_too_large"`, path)
	}
}

func TestRouter_BodyAtLimitReachesHandler(t *testing.T) {
	// Control check: a body exactly at the limit reaches the handler (400, not JSON).
	w := postBody(t, "/api/login", defaultMaxBodyBytes)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"invalid_request"`)
}
