package middleware_test

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/middleware"

	"github.com/gin-gonic/gin"
)

// requireHeaderEquals bricht den (Sub-)Test ab, wenn der angegebene
// Response-Header nicht exakt dem erwarteten Wert entspricht.
func requireHeaderEquals(t *testing.T, w *httptest.ResponseRecorder, name, want string) {
	t.Helper()
	if got := w.Header().Get(name); got != want {
		t.Errorf("%s = %q, expected %q", name, got, want)
	}
}

// requireHeaderContains bricht den (Sub-)Test ab, wenn der angegebene
// Response-Header den erwarteten Substring nicht enthält.
func requireHeaderContains(t *testing.T, w *httptest.ResponseRecorder, name, wantSubstr string) {
	t.Helper()
	if got := w.Header().Get(name); !strings.Contains(got, wantSubstr) {
		t.Errorf("%s = %q, expected to contain %q", name, got, wantSubstr)
	}
}

// requireHSTSHeader prüft den HSTS-Sonderfall: der Header darf nur bei
// tatsächlich terminiertem TLS gesetzt werden.
func requireHSTSHeader(t *testing.T, w *httptest.ResponseRecorder, expectHSTS bool) {
	t.Helper()
	hsts := w.Header().Get("Strict-Transport-Security")
	if !expectHSTS {
		if hsts != "" {
			t.Errorf("Strict-Transport-Security should not be set without TLS, got %q", hsts)
		}
		return
	}
	if hsts != "max-age=63072000; includeSubDomains" {
		t.Errorf("Strict-Transport-Security = %q, expected %q", hsts, "max-age=63072000; includeSubDomains")
	}
}

// TestSecurityHeaders prüft die Standard-Sicherheitsheader aus SecurityHeaders()
// (Review-Befund S11), inklusive des HSTS-Sonderfalls: der Header darf nur bei
// tatsächlich terminiertem TLS gesetzt werden.
func TestSecurityHeaders(t *testing.T) {
	tests := []struct {
		name       string
		withTLS    bool
		expectHSTS bool
	}{
		{
			name:       "Ohne TLS (Standard bei httptest) - kein HSTS",
			withTLS:    false,
			expectHSTS: false,
		},
		{
			name:       "Mit TLS - HSTS gesetzt",
			withTLS:    true,
			expectHSTS: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			r.Use(middleware.SecurityHeaders())
			r.GET("/test", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			req, _ := http.NewRequest("GET", "/test", nil)
			if tc.withTLS {
				req.TLS = &tls.ConnectionState{}
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			requireHeaderEquals(t, w, "X-Content-Type-Options", "nosniff")
			requireHeaderEquals(t, w, "X-Frame-Options", "DENY")
			requireHeaderEquals(t, w, "Referrer-Policy", "strict-origin-when-cross-origin")
			requireHeaderEquals(t, w, "Permissions-Policy", "geolocation=(), microphone=(), camera=()")
			requireHeaderContains(t, w, "Content-Security-Policy", "default-src 'self'")
			requireHSTSHeader(t, w, tc.expectHSTS)
		})
	}
}

// TestRateLimiter prüft den In-Memory Token-Bucket-Limiter pro Client-IP
// (Review-Befund S7): Burst-Kontingent, 429 mit Retry-After bei Überschreitung,
// unabhängige Kontingente pro IP, und Token-Refill nach Ablauf des Zeitfensters.
func TestRateLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const burst = 2
	const window = 100 * time.Millisecond

	r := gin.New()
	r.Use(middleware.NewRateLimiter(burst, window))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	doRequest := func(remoteAddr string) *httptest.ResponseRecorder {
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = remoteAddr
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// Requests 1-2 von IP 1.2.3.4 verbrauchen das Burst-Kontingent.
	for i := 1; i <= burst; i++ {
		w := doRequest("1.2.3.4:1")
		if w.Code != http.StatusOK {
			t.Fatalf("Request %d von IP 1.2.3.4 erwartet 200, got %d", i, w.Code)
		}
	}

	// Request 3 sofort danach ist über dem Kontingent -> 429 + Retry-After.
	w := doRequest("1.2.3.4:1")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("Erwartet 429 nach Ausschöpfen des Kontingents, got %d", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, expected %q", got, "60")
	}

	// Eine andere IP hat ihr eigenes, unabhängiges Kontingent.
	wOther := doRequest("5.6.7.8:1")
	if wOther.Code != http.StatusOK {
		t.Fatalf("Andere IP sollte eigenes Kontingent haben, erwartet 200, got %d", wOther.Code)
	}

	// Nach Ablauf des Refill-Fensters ist wieder mindestens 1 Token verfügbar.
	time.Sleep(window + 50*time.Millisecond)
	wRefilled := doRequest("1.2.3.4:1")
	if wRefilled.Code != http.StatusOK {
		t.Fatalf("Nach Refill-Fenster erwartet 200, got %d", wRefilled.Code)
	}
}

// TestSecurityHeaders_CSPOhneFremdeSchriften prüft DSGVO: Schriften
// werden selbst gehostet, die CSP darf daher keine Google-Fonts-Hosts mehr
// freigeben und style-src/font-src bleiben auf 'self' beschränkt
// ('unsafe-inline' bei style-src bleibt, weil Vue Inline-Styles setzt).
func TestSecurityHeaders_CSPOhneFremdeSchriften(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.SecurityHeaders())
	r.GET("/test", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	csp := w.Header().Get("Content-Security-Policy")
	for _, host := range []string{"fonts.googleapis.com", "fonts.gstatic.com"} {
		if strings.Contains(csp, host) {
			t.Errorf("CSP %q darf %s nicht enthalten", csp, host)
		}
	}
	for _, want := range []string{"style-src 'self' 'unsafe-inline';", "font-src 'self';"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q muss %q enthalten", csp, want)
		}
	}
}

// TestSecurityHeaders_CSPNonceJeAnfrage: Cloudflares
// JavaScript Detections (auf dem Free-Plan nicht abschaltbar) spritzen ein
// Inline-Skript ein, das script-src 'self' blockiert. Cloudflare übernimmt
// laut Doku einen Nonce aus dem CSP-Header für seine Skripte. Der Nonce muss
// je Anfrage neu und zufällig sein (sonst könnte eingeschleustes Inline-
// Skript ihn mitbenutzen), 'self' bleibt, 'unsafe-inline' kommt nicht dazu.
func TestSecurityHeaders_CSPNonceJeAnfrage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.SecurityHeaders())
	r.GET("/test", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	nonceRe := regexp.MustCompile(`script-src 'self' 'nonce-([A-Za-z0-9+/_-]{22,})={0,2}';`)
	nonces := map[string]bool{}
	for i := 0; i < 20; i++ {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		r.ServeHTTP(w, req)
		csp := w.Header().Get("Content-Security-Policy")
		m := nonceRe.FindStringSubmatch(csp)
		if m == nil {
			t.Fatalf("CSP %q enthält keinen Nonce in script-src", csp)
		}
		if strings.Contains(csp, "unsafe-inline';") && strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
			t.Fatalf("CSP %q darf script-src nicht mit 'unsafe-inline' aufweichen", csp)
		}
		nonces[m[1]] = true
	}
	if len(nonces) != 20 {
		t.Errorf("Nonce muss je Anfrage neu sein, %d verschiedene bei 20 Anfragen", len(nonces))
	}
}

// base-uri und form-action fallen nicht auf default-src zurück. Ohne sie könnte
// eingeschleustes HTML mit <base href> alle relativen Skript- und Link-Pfade auf
// einen fremden Host umbiegen oder ein Formular (etwa ein nachgebautes
// Login) an einen fremden Host schicken. img-src erlaubt weiter https:, weil
// Seiten-Logos von beliebigen https-Adressen kommen dürfen (validLogoURL);
// connect-src bleibt auf 'self' und begrenzt damit den Abfluss per Skript.
func TestSecurityHeaders_CSPBaseUriFormActionUndBildquellen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.SecurityHeaders())
	r.GET("/test", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"base-uri 'self';", "form-action 'self';", "img-src 'self' data: https:;", "connect-src 'self';", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q muss %q enthalten", csp, want)
		}
	}
	// Gegenprobe: keine Aufweichung von base-uri und form-action.
	for _, bad := range []string{"base-uri *", "form-action *", "form-action https:"} {
		if strings.Contains(csp, bad) {
			t.Errorf("CSP %q darf %q nicht enthalten", csp, bad)
		}
	}
}
