package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/testutils"
)

// White-Box-Test (package handlers statt handlers_test): SSOExchangeHandler
// prüft ausschließlich den unexportierten In-Memory-Code-Store
// (storeSSOCode/consumeSSOCode), auf den ein externes _test-Package keinen
// Zugriff hat.
func TestSSOExchangeHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/auth/sso-exchange", h.SSOExchangeHandler)

	doExchange := func(code string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"code": code})
		req, _ := http.NewRequest("POST", "/api/auth/sso-exchange", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("Gültiger Code liefert das hinterlegte Token", func(t *testing.T) {
		code := storeSSOCode("test-jwt-token-value")

		w := doExchange(code)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Response nicht parsebar: %v", err)
		}
		if resp["token"] != "test-jwt-token-value" {
			t.Fatalf("Expected token 'test-jwt-token-value', got %q", resp["token"])
		}
	})

	t.Run("Code ist Einmalgebrauch - zweiter Austausch schlägt fehl", func(t *testing.T) {
		code := storeSSOCode("single-use-token")

		first := doExchange(code)
		if first.Code != http.StatusOK {
			t.Fatalf("Erster Austausch sollte 200 sein, war %d", first.Code)
		}

		second := doExchange(code)
		if second.Code != http.StatusUnauthorized {
			t.Fatalf("Zweiter Austausch mit demselben Code sollte 401 sein, war %d", second.Code)
		}
	})

	t.Run("Unbekannter Code wird abgelehnt", func(t *testing.T) {
		w := doExchange("never-issued-code")

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401 Unauthorized, got %d", w.Code)
		}
	})

	t.Run("Fehlender Code im Request-Body wird abgelehnt", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/api/auth/sso-exchange", bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400 Bad Request, got %d", w.Code)
		}
	})
}

// isBootstrapAdminEmail ist ebenfalls unexportiert; White-Box-Test für die
// SSO_BOOTSTRAP_ADMIN_EMAILS-Allowlist-Logik (case-insensitive, kommagetrennt).
func TestIsBootstrapAdminEmail(t *testing.T) {
	t.Setenv("SSO_BOOTSTRAP_ADMIN_EMAILS", "Admin@Example.com, second@example.com")

	cases := []struct {
		email string
		want  bool
	}{
		{"admin@example.com", true},
		{"ADMIN@EXAMPLE.COM", true},
		{"second@example.com", true},
		{"third@example.com", false},
		{"", false},
	}

	for _, tc := range cases {
		if got := isBootstrapAdminEmail(tc.email); got != tc.want {
			t.Errorf("isBootstrapAdminEmail(%q) = %v, want %v", tc.email, got, tc.want)
		}
	}

	t.Run("Leere Env-Var erlaubt niemanden", func(t *testing.T) {
		t.Setenv("SSO_BOOTSTRAP_ADMIN_EMAILS", "")
		if isBootstrapAdminEmail("admin@example.com") {
			t.Error("Erwartet false, wenn SSO_BOOTSTRAP_ADMIN_EMAILS leer ist")
		}
	})
}
