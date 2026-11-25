package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestTrustedProxiesFromEnv: ohne TRUSTED_PROXIES darf gar kein
// Proxy vertraut werden (sicherer Default, schließt das gin-Default-Verhalten
// "vertraue 0.0.0.0/0" - siehe Review-Befund B3), mit gesetztem Wert müssen
// die konfigurierten Adressen 1:1 übernommen werden.
func TestTrustedProxiesFromEnv(t *testing.T) {
	t.Run("ohne TRUSTED_PROXIES: nil (kein Proxy vertraut)", func(t *testing.T) {
		_ = os.Unsetenv("TRUSTED_PROXIES")
		got := trustedProxiesFromEnv()
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("mit TRUSTED_PROXIES: kommagetrennte Liste wird geparst", func(t *testing.T) {
		_ = os.Setenv("TRUSTED_PROXIES", "10.0.0.1, 10.0.0.0/8 ,192.168.1.1")
		defer func() { _ = os.Unsetenv("TRUSTED_PROXIES") }()

		got := trustedProxiesFromEnv()
		want := []string{"10.0.0.1", "10.0.0.0/8", "192.168.1.1"}
		if len(got) != len(want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("index %d: expected %q, got %q", i, want[i], got[i])
			}
		}
	})
}

// TestClientIP_UntrustedProxyCannotSpoofXForwardedFor is the core regression
// guard for the trusted-proxy fix: gin.Default() trusts ALL proxies by
// default (0.0.0.0/0), so ClientIP() blindly returned whatever
// X-Forwarded-For value an attacker sent - a different value per request
// defeats IP-keyed rate limiting entirely. After applying
// trustedProxiesFromEnv() via SetTrustedProxies(), an untrusted caller's
// X-Forwarded-For must be ignored and the real TCP peer address used instead.
func TestClientIP_UntrustedProxyCannotSpoofXForwardedFor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_ = os.Unsetenv("TRUSTED_PROXIES")

	r := gin.New()
	if err := r.SetTrustedProxies(trustedProxiesFromEnv()); err != nil {
		t.Fatalf("SetTrustedProxies failed: %v", err)
	}
	var gotClientIP string
	r.GET("/test", func(c *gin.Context) {
		gotClientIP = c.ClientIP()
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "203.0.113.9:12345"
	// Attacker-controlled header: without a trusted-proxy configuration this
	// must be ignored entirely.
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if gotClientIP != "203.0.113.9" {
		t.Errorf("expected ClientIP() to fall back to the real peer address 203.0.113.9 (ignoring a spoofed X-Forwarded-For), got %q", gotClientIP)
	}
}

// TestCORSMiddleware prüft CORSMiddleware() (main.go) inkl. Default-Origins,
// Wildcard-Subdomain-Matching (Review-Befund S9) und AllowCredentials. Den
// Fatal-Guard für CORS_ORIGINS="*" prüft TestCORSMiddleware_WildcardIsFatal
// (logging.Fatal statt log.Fatalf).
func TestCORSMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newRouter := func() *gin.Engine {
		r := gin.New()
		r.Use(CORSMiddleware())
		r.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})
		return r
	}

	doRequest := func(r *gin.Engine, origin string) *httptest.ResponseRecorder {
		req, _ := http.NewRequest("GET", "/test", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("Default-Origins ohne CORS_ORIGINS env", func(t *testing.T) {
		_ = os.Unsetenv("CORS_ORIGINS")
		r := newRouter()

		wAllowed := doRequest(r, "http://localhost:5173")
		if got := wAllowed.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
			t.Errorf("erlaubter Default-Origin: Access-Control-Allow-Origin = %q, expected %q", got, "http://localhost:5173")
		}

		wDenied := doRequest(r, "http://evil.example.com")
		if got := wDenied.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("fremder Origin sollte abgelehnt werden, Access-Control-Allow-Origin = %q, expected leer", got)
		}
	})

	t.Run("Wildcard-Subdomain wird akzeptiert", func(t *testing.T) {
		_ = os.Unsetenv("CORS_ORIGINS")
		r := newRouter()

		w := doRequest(r, "http://acme.localhost:5173")
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://acme.localhost:5173" {
			t.Errorf("Wildcard-Subdomain-Origin: Access-Control-Allow-Origin = %q, expected %q", got, "http://acme.localhost:5173")
		}
	})

	t.Run("Custom CORS_ORIGINS ersetzt Default", func(t *testing.T) {
		_ = os.Setenv("CORS_ORIGINS", "https://custom.example.com")
		defer func() { _ = os.Unsetenv("CORS_ORIGINS") }()
		r := newRouter()

		wAllowed := doRequest(r, "https://custom.example.com")
		if got := wAllowed.Header().Get("Access-Control-Allow-Origin"); got != "https://custom.example.com" {
			t.Errorf("custom Origin: Access-Control-Allow-Origin = %q, expected %q", got, "https://custom.example.com")
		}

		wDefaultNoLongerAllowed := doRequest(r, "http://localhost:5173")
		if got := wDefaultNoLongerAllowed.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Default-Origin sollte bei gesetztem CORS_ORIGINS nicht mehr erlaubt sein, got %q", got)
		}
	})

	// With SAML removed there is no exception any more: CORS rejects a
	// cross-origin POST to the former ACS path as well.
	t.Run("no CORS exception for the former SAML ACS", func(t *testing.T) {
		_ = os.Unsetenv("CORS_ORIGINS")
		r := gin.New()
		r.Use(CORSMiddleware())
		r.POST("/api/auth/saml/:page_id/acs", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
		req, _ := http.NewRequest("POST", "/api/auth/saml/7/acs", nil)
		req.Header.Set("Origin", "https://idp.example.org")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("cross-origin POST: status %d, expected 403 (CORS applies)", w.Code)
		}
	})

	t.Run("AllowCredentials gesetzt bei erlaubtem Origin", func(t *testing.T) {
		_ = os.Unsetenv("CORS_ORIGINS")
		r := newRouter()

		w := doRequest(r, "http://localhost:5173")
		if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Errorf("Access-Control-Allow-Credentials = %q, expected %q", got, "true")
		}
	})
}
