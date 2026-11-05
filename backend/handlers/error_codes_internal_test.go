package handlers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Hilfsfunktionen für Fehlercodes.

func codeTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	return c, w
}

func TestCodedError(t *testing.T) {
	err := newCodedError("parent_cycle", "Text")
	assert.EqualError(t, err, "Text")

	c, w := codeTestContext()
	respondCoded(c, http.StatusBadRequest, err)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"error":"Text","code":"parent_cycle"}`, w.Body.String())
}

// Ein Fehler ohne Code (z. B. aus der Datenbank) wird nie im Wortlaut
// ausgeliefert, sondern als 500 server_error.
func TestRespondCoded_UnbekannterFehlerLiefertKeinenFehlertext(t *testing.T) {
	c, w := codeTestContext()
	respondCoded(c, http.StatusBadRequest, errors.New("pq: relation \"users\" does not exist"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.JSONEq(t, `{"error":"Internal server error","code":"server_error"}`, w.Body.String())
}

func TestRespondIncidentValidation_UnbekannterTextFaelltAufInvalidRequest(t *testing.T) {
	c, w := codeTestContext()
	respondIncidentValidation(c, "ein ganz neuer Text")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"error":"ein ganz neuer Text","code":"invalid_request"}`, w.Body.String())

	c, w = codeTestContext()
	respondIncidentValidation(c, errInvalidMaintenanceStatus)
	assert.Contains(t, w.Body.String(), `"code":"invalid_maintenance_status"`)
}

func TestValidateWebhookURL_HostNichtAufloesbar(t *testing.T) {
	orig := webhookLookupIP
	t.Cleanup(func() { webhookLookupIP = orig })
	webhookLookupIP = func(context.Context, string) ([]net.IP, error) { return nil, errors.New("NXDOMAIN") }

	err := validateWebhookURL("https://gibtesnicht.example/hook")
	require.Error(t, err)
	var ce *codedError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, "webhook_host_unresolvable", ce.code)
}
