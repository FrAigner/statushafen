package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func domainCheck(r http.Handler, ip, domain string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/internal/custom-domain-check?domain="+domain, nil)
	req.RemoteAddr = ip + ":4000"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Der interne Domain-Check ist je IP begrenzt (120/min), damit er auch bei
// durchgeschlagenem Netzzugriff oder Fehlkonfiguration (leeres Token in der
// Entwicklung) kein unbegrenztes Orakel für Domain-Status ist. Normale Nutzung
// (Caddy fragt nur für Hosts ohne Zertifikat) bleibt weit darunter.
func TestRouter_InternalDomainCheckRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("APP_ENV", "development")
	t.Setenv("CADDY_INTERNAL_TOKEN", "")
	db := testutils.SetupTestDB()
	require.NoError(t, db.Create(&models.StatusPage{Name: "P", Slug: "p-limit", CustomDomain: "status.example.org", CustomDomainStatus: "active"}).Error)
	r := newRouter(handlers.NewHandler(db))

	const limit = 120
	for i := 0; i < limit; i++ {
		// Bekannte und unbekannte Domains: normale Antworten, kein 429.
		assert.Equal(t, http.StatusOK, domainCheck(r, "198.51.100.30", "status.example.org").Code, "Anfrage %d", i+1)
	}
	assert.Equal(t, http.StatusNotFound, domainCheck(r, "198.51.100.31", "unbekannt.example.org").Code, "andere IP: unbegrenzt unabhängig")

	w := domainCheck(r, "198.51.100.30", "status.example.org")
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"rate_limited"`)
	assert.Equal(t, "60", w.Header().Get("Retry-After"))
	assert.Equal(t, http.StatusNotFound, domainCheck(r, "198.51.100.31", "unbekannt.example.org").Code, "die andere IP bleibt unberührt")
}
