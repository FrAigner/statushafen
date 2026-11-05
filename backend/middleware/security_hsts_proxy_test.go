package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Hinter einem TLS-terminierenden Proxy sieht das Backend nur HTTP. HSTS muss
// trotzdem kommen, aber nur, wenn ein vertrauter Proxy "https" meldet: Ohne
// Vertrauen kann jeder Client den Header setzen (oder weglassen).
func TestSecurityHeaders_HSTSHinterVertrautemProxy(t *testing.T) {
	cases := []struct {
		name       string
		trusted    []string
		remoteAddr string
		proto      string
		wantHSTS   bool
	}{
		{"vertrauter Proxy meldet https", []string{"10.0.0.0/8"}, "10.1.2.3:4444", "https", true},
		{"vertrauter Proxy, Großschreibung", []string{"10.0.0.0/8"}, "10.1.2.3:4444", "HTTPS", true},
		{"vertrauter Proxy, Kette endet auf https", []string{"10.0.0.0/8"}, "10.1.2.3:4444", "http, https", true},
		{"vertrauter Proxy, Kette endet auf http", []string{"10.0.0.0/8"}, "10.1.2.3:4444", "https, http", false},
		{"vertrauter Proxy meldet http", []string{"10.0.0.0/8"}, "10.1.2.3:4444", "http", false},
		{"vertrauter Proxy ohne Header", []string{"10.0.0.0/8"}, "10.1.2.3:4444", "", false},
		{"vertrauter Proxy, unsinniger Wert", []string{"10.0.0.0/8"}, "10.1.2.3:4444", "gopher", false},
		{"Absender nicht vertraut", []string{"10.0.0.0/8"}, "203.0.113.9:4444", "https", false},
		{"keine Proxys konfiguriert", nil, "10.1.2.3:4444", "https", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(tc.trusted))
			r.Use(middleware.SecurityHeaders())
			r.GET("/t", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

			req := httptest.NewRequest(http.MethodGet, "/t", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.proto != "" {
				req.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			requireHSTSHeader(t, w, tc.wantHSTS)
		})
	}
}

// Die Prüfung darf die Anfrage nicht verändern: Handler sehen weiter die
// echten Header und die echte Client-Adresse.
func TestSecurityHeaders_HSTSPruefungLaesstAnfrageUnveraendert(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.NoError(t, r.SetTrustedProxies([]string{"10.0.0.0/8"}))
	r.Use(middleware.SecurityHeaders())
	var seenXFF, seenIP string
	r.GET("/t", func(c *gin.Context) {
		seenXFF = c.GetHeader("X-Forwarded-For")
		seenIP = c.ClientIP()
		c.String(http.StatusOK, "ok")
	})
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.RemoteAddr = "10.1.2.3:4444"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	req.Header.Set("X-Forwarded-Proto", "https")
	r.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, "198.51.100.7", seenXFF)
	assert.Equal(t, "198.51.100.7", seenIP)
}
