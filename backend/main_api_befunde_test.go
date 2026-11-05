package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/testutils"
)

// prüft die Verdrahtung in main.go über den echten
// Router, nicht über nachgebaute Test-Router.

func TestRouter_UnsubscribeGetRedirectsPostDeletes(t *testing.T) {
	t.Setenv("FRONTEND_URL", "https://status.example.test")
	h := handlers.NewHandler(testutils.SetupTestDB())
	r := newRouter(h)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/public/subscribers/unsubscribe?token=abc", nil))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "https://status.example.test/unsubscribe?token=abc" {
		t.Fatalf("GET must redirect to the frontend page, got %d Location=%q", w.Code, w.Header().Get("Location"))
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/public/subscribers/unsubscribe?token=abc", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST with an invalid token must reach UnsubscribeHandler (400), got %d", w.Code)
	}
}
