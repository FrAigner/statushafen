package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestOAuthStateUse_ConsumeIsSingleUseAndExpires(t *testing.T) {
	var u oauthStateUse
	now := time.Date(2031, 1, 1, 12, 0, 0, 0, time.UTC)
	assert.True(t, u.consume("a", now))
	assert.False(t, u.consume("a", now.Add(time.Second)), "zweites Einlösen")
	assert.True(t, u.consume("b", now.Add(time.Second)))

	// Nach der Lebensdauer sind „a“ und „b“ aus dem Speicher; dasselbe Cookie
	// wäre dann ohnehin abgelaufen und scheitert an der Altersprüfung.
	later := now.Add(oauthStateLifetime + time.Minute)
	assert.True(t, u.consume("c", later))
	assert.Len(t, u.used, 1)
}

func TestOAuthStateUse_TableIsBoundedAndDropsOldestFirst(t *testing.T) {
	require.LessOrEqual(t, oauthStateMaxUsed, 100_000, "die Tabelle der eingelösten States bleibt klein")
	var u oauthStateUse
	now := time.Date(2031, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < oauthStateMaxUsed+50; i++ {
		require.True(t, u.consume(fmt.Sprintf("s%d", i), now))
	}
	assert.LessOrEqual(t, len(u.used), oauthStateMaxUsed)
	assert.Len(t, u.order, len(u.used))
	_, oldestStillThere := u.used["s0"]
	assert.False(t, oldestStillThere, "der älteste Eintrag wurde verdrängt")
	assert.False(t, u.consume(fmt.Sprintf("s%d", oauthStateMaxUsed+49), now), "der neueste ist noch gemerkt")
}

func TestVerifyOAuthState_RejectsMalformedAndOutOfWindowValues(t *testing.T) {
	t.Setenv("JWT_SECRET", "unit-test-secret-for-oauth-state-hmac")
	now := time.Date(2031, 1, 1, 12, 0, 0, 0, time.UTC)
	good := signOAuthState("google", "st", "ver", now)

	state, verifier, ok := verifyOAuthState("google", good, now.Add(time.Minute))
	require.True(t, ok)
	assert.Equal(t, "st", state)
	assert.Equal(t, "ver", verifier)

	_, _, ok = verifyOAuthState("github", good, now)
	assert.False(t, ok, "an den Anbieter gebunden")
	_, _, ok = verifyOAuthState("google", good, now.Add(oauthStateLifetime))
	assert.True(t, ok, "genau fünf Minuten alt: noch gültig")
	_, _, ok = verifyOAuthState("google", good, now.Add(oauthStateLifetime+time.Second))
	assert.False(t, ok, "abgelaufen")
	_, _, ok = verifyOAuthState("google", good, now.Add(-time.Minute))
	assert.False(t, ok, "aus der Zukunft")

	// Gültige Signatur über einen unlesbaren Zeitstempel.
	payload := "st.ver.keinezahl"
	_, _, ok = verifyOAuthState("google", payload+"."+oauthStateMAC("google", payload), now)
	assert.False(t, ok)
	// Ein angehängter Teil macht ein sonst gültiges Cookie ungültig.
	_, _, ok = verifyOAuthState("google", good+".zusatz", now)
	assert.False(t, ok, "zu viele Teile")
	_, _, ok = verifyOAuthState("google", good[:strings.LastIndex(good, ".")], now)
	assert.False(t, ok, "zu wenige Teile")

	// Gültig signiert, aber ohne State oder ohne Verifier: nie zulässig, auch wenn
	// die Signatur stimmt (ein leerer State stimmte mit einem leeren Parameter überein).
	issued := fmt.Sprint(now.Unix())
	for name, p := range map[string]string{"State leer": "." + "ver." + issued, "Verifier leer": "st.." + issued} {
		_, _, ok = verifyOAuthState("google", p+"."+oauthStateMAC("google", p), now)
		assert.False(t, ok, name)
	}
	for _, v := range []string{"", "a.b", ".b.1." + "x", "a..1.x", strings.Repeat("a.", 6)} {
		_, _, ok = verifyOAuthState("google", v, now)
		assert.False(t, ok, v)
	}

	// Anderes Geheimnis, andere Signatur.
	t.Setenv("JWT_SECRET", "ein-ganz-anderes-geheimnis-fuer-den-test")
	_, _, ok = verifyOAuthState("google", good, now)
	assert.False(t, ok)
}

func TestOAuthContext_UsesTimeoutClientByDefaultAndConfiguredClientInTests(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	h := &Handler{}
	def, ok := h.oauthContext(c).Value(oauth2.HTTPClient).(*http.Client)
	require.True(t, ok)
	assert.Equal(t, oauthTimeout, def.Timeout, "der Standardclient hat eine Zeitgrenze")
	// Die Grenze selbst ist festgeschrieben: Ein hängender Anbieter hält die
	// Anfrage höchstens so lange offen (unabhängig vom Wert der Konstante).
	assert.Equal(t, 15*time.Second, def.Timeout)

	custom := &http.Client{Timeout: time.Second}
	h.OAuthClient = custom
	assert.Same(t, custom, h.oauthContext(c).Value(oauth2.HTTPClient))
}
