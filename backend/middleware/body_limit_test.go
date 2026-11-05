package middleware_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bodyProbe struct {
	reached bool
	read    int
	readErr error
}

func bodyLimitRouter(probe *bodyProbe, limit int64, overrides map[string]int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.BodyLimit(limit, overrides))
	handler := func(c *gin.Context) {
		probe.reached = true
		b, err := io.ReadAll(c.Request.Body)
		probe.read, probe.readErr = len(b), err
		c.Status(http.StatusOK)
	}
	r.POST("/small", handler)
	r.POST("/big", handler)
	r.GET("/get", handler)
	return r
}

func TestBodyLimit_RejectsDeclaredOversizeWith413(t *testing.T) {
	probe := &bodyProbe{}
	r := bodyLimitRouter(probe, 100, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/small", strings.NewReader(strings.Repeat("a", 101))))
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"request_too_large"`)
	assert.False(t, probe.reached, "Handler läuft nicht")
}

func TestBodyLimit_AllowsBodiesUpToLimit(t *testing.T) {
	probe := &bodyProbe{}
	r := bodyLimitRouter(probe, 100, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/small", strings.NewReader(strings.Repeat("a", 100))))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 100, probe.read)
	assert.NoError(t, probe.readErr)

	// Anfragen ohne Body (GET) laufen unberührt durch.
	probe = &bodyProbe{}
	r = bodyLimitRouter(probe, 100, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/get", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, probe.reached)
}

// Ohne Content-Length (chunked) greift die Lesegrenze: Der Handler bekommt
// höchstens limit Byte und einen MaxBytesError, nie den ganzen Body.
func TestBodyLimit_CapsBodiesWithUnknownLength(t *testing.T) {
	probe := &bodyProbe{}
	r := bodyLimitRouter(probe, 100, nil)
	req := httptest.NewRequest(http.MethodPost, "/small", io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("a"), 10_000))))
	req.ContentLength = -1
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.True(t, probe.reached)
	var tooLarge *http.MaxBytesError
	assert.True(t, errors.As(probe.readErr, &tooLarge), "Lesefehler: %v", probe.readErr)
	assert.LessOrEqual(t, probe.read, 100)
}

func TestBodyLimit_RouteOverrideRaisesLimit(t *testing.T) {
	probe := &bodyProbe{}
	r := bodyLimitRouter(probe, 100, map[string]int64{"/big": 1000})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/big", strings.NewReader(strings.Repeat("a", 900))))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 900, probe.read)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/big", strings.NewReader(strings.Repeat("a", 1001))))
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}
