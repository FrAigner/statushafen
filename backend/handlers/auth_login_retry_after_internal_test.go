package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Retry-After gives whole seconds and rounds up: a remaining lockout of less than
// one second must not appear as "0" (the client would retry immediately
// and run into the lockout again).
func TestLogin_RetryAfterRoundsUpToWholeSeconds(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	h.loginThrottle = middleware.NewAccountThrottle(1, 400*time.Millisecond, time.Minute, time.Minute)
	r := testutils.SetupRouter()
	r.POST("/api/login", h.LoginHandler)

	const source = "198.51.100.7"
	// The very first attempt reaches the threshold and locks the pair for 400 ms.
	h.loginThrottle.Admit("ziel@example.com", source)

	w, _ := loginFrom(r, context.Background(), source+":4000", "falsch-falsch")
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "1", w.Header().Get("Retry-After"))
}
