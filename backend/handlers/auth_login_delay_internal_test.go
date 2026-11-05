package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func delayTestRouter(t *testing.T, delayThreshold int, step time.Duration) *gin.Engine {
	t.Helper()
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	h.loginThrottle = middleware.NewLoginThrottle().WithAccountDelay(delayThreshold, step, step)
	require.NoError(t, db.Create(&models.User{Email: "ziel@example.com", PasswordHash: testutils.MustHash("RichtigesPasswort1")}).Error)
	r := testutils.SetupRouter()
	r.POST("/api/login", h.LoginHandler)
	return r
}

func loginFrom(r *gin.Engine, ctx context.Context, remoteAddr, password string) (*httptest.ResponseRecorder, time.Duration) {
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/login",
		bytes.NewBufferString(`{"email":"ziel@example.com","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	start := time.Now()
	r.ServeHTTP(w, req)
	return w, time.Since(start)
}

// Viele Quellen gegen dasselbe Konto verzögern den Login, sperren ihn aber nicht:
// der Nutzer meldet sich mit dem richtigen Passwort weiterhin an.
func TestLogin_DistributedGuessingIsOnlyDelayed(t *testing.T) {
	const step = 150 * time.Millisecond
	r := delayTestRouter(t, 3, step)

	for i := 1; i <= 3; i++ {
		w, _ := loginFrom(r, context.Background(), "198.51.100."+string(rune('0'+i))+":1", "falsch-falsch")
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}
	w, d := loginFrom(r, context.Background(), "198.51.100.9:1", "falsch-falsch")
	assert.Equal(t, http.StatusUnauthorized, w.Code, "weiterhin eine normale Antwort, keine Sperre")
	assert.GreaterOrEqual(t, d, step, "ab der Schwelle wartet der Versuch")

	w, d = loginFrom(r, context.Background(), "203.0.113.20:1", "RichtigesPasswort1")
	assert.Equal(t, http.StatusOK, w.Code, "der echte Nutzer kommt trotz verteiltem Raten hinein")
	assert.GreaterOrEqual(t, d, step)
}

// Schließt der Client während der Wartezeit die Verbindung, endet der Handler,
// ohne das Passwort zu prüfen.
func TestLogin_DelayEndsWhenClientGoesAway(t *testing.T) {
	r := delayTestRouter(t, 0, 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w, d := loginFrom(r, ctx, "198.51.100.1:1", "RichtigesPasswort1")
	assert.Less(t, d, time.Second)
	assert.NotContains(t, w.Body.String(), "token")
}

func TestWaitForThrottle(t *testing.T) {
	assert.True(t, waitForThrottle(context.Background(), 0))
	assert.True(t, waitForThrottle(context.Background(), -time.Second))
	assert.True(t, waitForThrottle(context.Background(), time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.False(t, waitForThrottle(ctx, time.Minute))
	// Ohne verlangte Wartezeit zählt ein bereits geschlossener Client nicht: Der Handler läuft normal weiter.
	for i := 0; i < 50; i++ {
		assert.True(t, waitForThrottle(ctx, 0))
		assert.True(t, waitForThrottle(ctx, -time.Second))
	}
}
