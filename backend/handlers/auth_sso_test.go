package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/testutils"
)

func TestAuthSSOHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/auth/:provider", h.OAuthBeginHandler)
	r.GET("/api/auth/:provider/callback", h.OAuthCallbackHandler)

	t.Run("OAuth Begin - GitHub", func(t *testing.T) {
		_ = os.Setenv("GITHUB_CLIENT_ID", "testclientid")
		defer func() { _ = os.Unsetenv("GITHUB_CLIENT_ID") }()

		req, _ := http.NewRequest("GET", "/api/auth/github", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusTemporaryRedirect {
			t.Fatalf("Expected 307 Temporary Redirect, got %d", w.Code)
		}
	})

	t.Run("OAuth Begin - GitHub ohne konfigurierte ClientID", func(t *testing.T) {
		// GITHUB_CLIENT_ID bewusst nicht gesetzt (Vorfall 2026-07-28: leere
		// ClientID fuehrte sonst zu einem Redirect auf eine verwirrende
		// 404-Seite bei GitHub statt einer klaren Fehlermeldung).
		req, _ := http.NewRequest("GET", "/api/auth/github", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("Expected 503 Service Unavailable, got %d", w.Code)
		}
	})

	t.Run("OAuth Begin - Google ohne konfigurierte ClientID", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/auth/google", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("Expected 503 Service Unavailable, got %d", w.Code)
		}
	})

	t.Run("OAuth Begin - Unsupported Provider", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/auth/invalid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("OAuth Callback - Unsupported Provider", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/auth/invalid/callback?code=123", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("OAuth Callback - Missing Code", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/auth/github/callback", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400 Bad Request, got %d", w.Code)
		}
	})
}
