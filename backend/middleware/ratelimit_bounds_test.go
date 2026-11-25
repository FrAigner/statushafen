package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The IP limiter's table has a hard upper bound. An attacker with
// many source addresses fills it, but cannot make the memory grow
// beyond that.
func TestRateLimiter_TableNeverExceedsMaxEntries(t *testing.T) {
	const maxEntries = 100
	rl := newRateLimiter(5, time.Hour, maxEntries)

	for i := 0; i < 10*maxEntries; i++ {
		rl.allow(fmt.Sprintf("198.51.100.%d-%d", i/250, i%250))
		require.LessOrEqual(t, rl.size(), maxEntries, "nach %d Schlüsseln", i+1)
	}
	assert.Equal(t, maxEntries, rl.size())
}

// When the table is full, a new key still counts (it evicts the entry untouched
// for the longest time) and is throttled according to its quota. Full
// must never mean "let everything through".
func TestRateLimiter_NewKeyIsStillLimitedWhenTableIsFull(t *testing.T) {
	const maxEntries = 10
	rl := newRateLimiter(3, time.Hour, maxEntries)
	for i := 0; i < maxEntries; i++ {
		rl.allow(fmt.Sprintf("fuller-%d", i))
	}

	var allowed int
	for i := 0; i < 20; i++ {
		if rl.allow("neu") {
			allowed++
		}
	}
	assert.Equal(t, 3, allowed, "der neue Schlüssel bekommt genau sein Kontingent")
	assert.LessOrEqual(t, rl.size(), maxEntries)
}

// The entry untouched for the longest time is evicted; an active key
// keeps its counter and cannot be reset by foreign traffic,
// as long as it is among the most recently used.
func TestRateLimiter_EvictsLeastRecentlyUsedAndKeepsActiveKeyCounted(t *testing.T) {
	const maxEntries = 10
	rl := newRateLimiter(2, time.Hour, maxEntries)

	require.True(t, rl.allow("aktiv"))
	require.True(t, rl.allow("aktiv"))
	require.False(t, rl.allow("aktiv"), "Kontingent aufgebraucht")

	// Foreign keys almost fill the table; "aktiv" is used in between.
	for i := 0; i < maxEntries-2; i++ {
		rl.allow(fmt.Sprintf("fremd-%d", i))
	}
	assert.False(t, rl.allow("aktiv"), "aktiver Schlüssel bleibt gezählt")
	rl.allow("noch-einer")
	rl.allow("und-noch-einer")
	assert.False(t, rl.allow("aktiv"), "auch nach weiterem Fremdverkehr zuletzt benutzt, also nicht verdrängt")
	assert.LessOrEqual(t, rl.size(), maxEntries)
}

// Expired entries (bucket long since full again) make room first.
func TestRateLimiter_ExpiredEntriesAreDroppedFirst(t *testing.T) {
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	rl := newRateLimiter(2, time.Minute, 4)
	rl.now = func() time.Time { return now }

	for i := 0; i < 4; i++ {
		rl.allow(fmt.Sprintf("alt-%d", i))
	}
	now = now.Add(2 * time.Minute) // all buckets would be full again
	rl.allow("frisch")
	assert.Equal(t, 1, rl.size(), "abgelaufene Einträge sind entfernt")
}

// The default bound of the public constructors is 100,000 entries.
func TestRateLimiter_DefaultCapIs100k(t *testing.T) {
	assert.Equal(t, 100_000, rateLimiterMaxEntries)
}

// Control check: normal usage below the bound behaves as before.
func TestRateLimiter_NormalUseUnchanged(t *testing.T) {
	rl := newRateLimiter(3, time.Hour, 100)
	assert.True(t, rl.allow("a"))
	assert.True(t, rl.allow("a"))
	assert.True(t, rl.allow("a"))
	assert.False(t, rl.allow("a"))
	assert.True(t, rl.allow("b"), "anderer Schlüssel hat sein eigenes Kontingent")
	assert.Equal(t, 2, rl.size())
}

// The cleanup loop removes expired entries on its own, even if the
// table never becomes full.
func TestRateLimiter_CleanupLoopDropsExpiredEntries(t *testing.T) {
	rl := newRateLimiter(2, 20*time.Millisecond, 100)
	rl.allow("selten")
	require.Equal(t, 1, rl.size())

	go rl.cleanupStaleVisitors(5 * time.Millisecond)
	assert.Eventually(t, func() bool { return rl.size() == 0 }, 2*time.Second, 5*time.Millisecond)
}

// An entry counts as expired only after a full time window. If it is
// removed earlier, an exhausted key starts with a full quota and
// bypasses the limit as soon as another key creates an entry.
func TestRateLimiter_EntryIsKeptUntilAFullWindowHasPassed(t *testing.T) {
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	rl := newRateLimiter(2, time.Minute, 100)
	rl.now = func() time.Time { return now }

	require.True(t, rl.allow("a"))
	require.True(t, rl.allow("a"))
	require.False(t, rl.allow("a"), "Kontingent aufgebraucht")

	now = now.Add(40 * time.Second) // two thirds of the window: only 1.33 of 2 slots refilled so far
	rl.allow("b")                   // creates an entry and removes expired ones in the process
	assert.Equal(t, 2, rl.size(), "a ist noch nicht abgelaufen")
	assert.True(t, rl.allow("a"), "ein Platz ist wieder frei")
	assert.False(t, rl.allow("a"), "mehr nicht: kein Zurücksetzen auf das volle Kontingent")
}

// The public middleware uses the upper bound: after 100,000 further
// addresses the oldest entry is evicted, so the table does not grow
// any further. (Evicted means: at most a full quota, never locked out.)
func TestNewRateLimiter_MiddlewareTableIsBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", NewRateLimiter(1, time.Hour), func(c *gin.Context) { c.Status(http.StatusOK) })
	get := func(ip string) int {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.RemoteAddr = ip + ":4000"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	require.Equal(t, http.StatusOK, get("203.0.113.1"))
	require.Equal(t, http.StatusTooManyRequests, get("203.0.113.1"), "zweite Anfrage derselben Adresse")
	for i := 0; i < rateLimiterMaxEntries; i++ {
		get(fmt.Sprintf("10.%d.%d.%d", i>>16&0xff, i>>8&0xff, i&0xff))
	}
	assert.Equal(t, http.StatusOK, get("203.0.113.1"), "der älteste Eintrag wurde verdrängt, die Tabelle ist begrenzt")
}
