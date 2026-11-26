package handlers

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// A response that cannot be encoded as JSON (NaN) must
// not go into the 60 s cache. Old failure mode: gin sets status 200 in c.JSON
// before encoding, with NaN the client got 200 with an empty body (the error
// was only in c.Errors) and the broken response stayed in the cache for 60 s.
func TestRespondAvailability_KaputteAntwortWirdNichtGecacht(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}
	key := availabilityCacheKey{pageID: 1}
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	nan := math.NaN()
	bad := availabilityResponse{UptimePercent: &nan}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	h.respondAvailability(c, key, now, bad)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	_, cached := h.availCache.get(key, now)
	assert.False(t, cached, "kaputte Antwort im Cache")

	ok := 99.5
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	h.respondAvailability(c, key, now, availabilityResponse{UptimePercent: &ok})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), "99.5")
	_, cached = h.availCache.get(key, now)
	assert.True(t, cached)
}
