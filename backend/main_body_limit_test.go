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

// Der Router begrenzt jeden Body global; zu große Anfragen ergeben 413 mit
// Fehlercode, bevor ein Handler sie liest (Login, Bestellung, Abo usw.).
func TestRouter_OversizedBodyIs413(t *testing.T) {
	for _, path := range []string{"/api/login", "/api/public/orders", "/api/subscribe"} {
		w := postBody(t, path, defaultMaxBodyBytes+1)
		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, path)
		assert.Contains(t, w.Body.String(), `"code":"request_too_large"`, path)
	}
}

func TestRouter_BodyAtLimitReachesHandler(t *testing.T) {
	// Gegenprobe: ein Body genau an der Grenze erreicht den Handler (400, kein JSON).
	w := postBody(t, "/api/login", defaultMaxBodyBytes)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"invalid_request"`)
}

// Die SAML-Antwort darf bis 1 MiB groß sein (samlMaxResponseBytes) und kommt
// formularkodiert; der ACS-Pfad hat deshalb eine höhere Grenze.
func TestRouter_SAMLACSAllowsLargerBody(t *testing.T) {
	const acs = "/api/auth/saml/1/acs"
	w := postBody(t, acs, defaultMaxBodyBytes+10)
	assert.NotEqual(t, http.StatusRequestEntityTooLarge, w.Code, "ACS-Body über dem Standard bleibt zulässig")
	w = postBody(t, acs, samlACSMaxBodyBytes+1)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}
