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

// Retry-After nennt ganze Sekunden und rundet auf: Eine Restsperre von unter
// einer Sekunde darf nicht als "0" erscheinen (der Client versuchte es sofort
// wieder und liefe erneut in die Sperre).
func TestLogin_RetryAfterRoundsUpToWholeSeconds(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	h.loginThrottle = middleware.NewAccountThrottle(1, 400*time.Millisecond, time.Minute, time.Minute)
	r := testutils.SetupRouter()
	r.POST("/api/login", h.LoginHandler)

	const source = "198.51.100.7"
	// Schon der erste Versuch erreicht die Schwelle und sperrt das Paar für 400 ms.
	h.loginThrottle.Admit("ziel@example.com", source)

	w, _ := loginFrom(r, context.Background(), source+":4000", "falsch-falsch")
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "1", w.Header().Get("Retry-After"))
}
