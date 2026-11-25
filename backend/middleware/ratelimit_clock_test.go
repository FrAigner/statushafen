package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fraigner/statushafen/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// Rate limit tests count requests against a token bucket that refills with wall-clock time.
// Under -race the requests are slow enough for the bucket to refill mid-test, so the request
// over the limit slipped through. Tests therefore need to control the limiter's clock.
func TestSetRateLimiterClockForTests_ControlsRefill(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC)
	middleware.SetRateLimiterClockForTests(t.Cleanup, func() time.Time { return now })

	r := gin.New()
	r.GET("/x", middleware.NewRateLimiter(1, time.Minute), func(c *gin.Context) { c.Status(http.StatusOK) })
	get := func() int {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = "198.51.100.77:5000"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	assert.Equal(t, http.StatusOK, get())
	assert.Equal(t, http.StatusTooManyRequests, get(), "the frozen clock must not refill the bucket")

	now = now.Add(time.Minute)
	assert.Equal(t, http.StatusOK, get(), "advancing the test clock refills the bucket")
}
