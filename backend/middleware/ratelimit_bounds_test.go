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

// Die Tabelle des IP-Limiters hat eine harte Obergrenze. Ein Angreifer mit
// vielen Quelladressen füllt sie, kann den Speicher aber nicht darüber hinaus
// wachsen lassen.
func TestRateLimiter_TableNeverExceedsMaxEntries(t *testing.T) {
	const maxEntries = 100
	rl := newRateLimiter(5, time.Hour, maxEntries)

	for i := 0; i < 10*maxEntries; i++ {
		rl.allow(fmt.Sprintf("198.51.100.%d-%d", i/250, i%250))
		require.LessOrEqual(t, rl.size(), maxEntries, "nach %d Schlüsseln", i+1)
	}
	assert.Equal(t, maxEntries, rl.size())
}

// Ist die Tabelle voll, zählt ein neuer Schlüssel trotzdem (er verdrängt den am
// längsten unberührten Eintrag) und wird nach seinem Kontingent gebremst. Voll
// darf nie heißen „alles durchlassen“.
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

// Verdrängt wird der am längsten unberührte Eintrag; ein aktiver Schlüssel
// behält seinen Zähler und lässt sich nicht durch Fremdverkehr zurücksetzen,
// solange er zu den zuletzt benutzten gehört.
func TestRateLimiter_EvictsLeastRecentlyUsedAndKeepsActiveKeyCounted(t *testing.T) {
	const maxEntries = 10
	rl := newRateLimiter(2, time.Hour, maxEntries)

	require.True(t, rl.allow("aktiv"))
	require.True(t, rl.allow("aktiv"))
	require.False(t, rl.allow("aktiv"), "Kontingent aufgebraucht")

	// Fremde Schlüssel füllen die Tabelle fast; „aktiv“ wird zwischendurch benutzt.
	for i := 0; i < maxEntries-2; i++ {
		rl.allow(fmt.Sprintf("fremd-%d", i))
	}
	assert.False(t, rl.allow("aktiv"), "aktiver Schlüssel bleibt gezählt")
	rl.allow("noch-einer")
	rl.allow("und-noch-einer")
	assert.False(t, rl.allow("aktiv"), "auch nach weiterem Fremdverkehr zuletzt benutzt, also nicht verdrängt")
	assert.LessOrEqual(t, rl.size(), maxEntries)
}

// Abgelaufene Einträge (Bucket längst wieder voll) machen zuerst Platz.
func TestRateLimiter_ExpiredEntriesAreDroppedFirst(t *testing.T) {
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	rl := newRateLimiter(2, time.Minute, 4)
	rl.now = func() time.Time { return now }

	for i := 0; i < 4; i++ {
		rl.allow(fmt.Sprintf("alt-%d", i))
	}
	now = now.Add(2 * time.Minute) // alle Buckets wären wieder voll
	rl.allow("frisch")
	assert.Equal(t, 1, rl.size(), "abgelaufene Einträge sind entfernt")
}

// Die Standardgrenze der öffentlichen Konstruktoren ist 100.000 Einträge.
func TestRateLimiter_DefaultCapIs100k(t *testing.T) {
	assert.Equal(t, 100_000, rateLimiterMaxEntries)
}

// Gegenprobe: normale Nutzung unter der Grenze verhält sich wie zuvor.
func TestRateLimiter_NormalUseUnchanged(t *testing.T) {
	rl := newRateLimiter(3, time.Hour, 100)
	assert.True(t, rl.allow("a"))
	assert.True(t, rl.allow("a"))
	assert.True(t, rl.allow("a"))
	assert.False(t, rl.allow("a"))
	assert.True(t, rl.allow("b"), "anderer Schlüssel hat sein eigenes Kontingent")
	assert.Equal(t, 2, rl.size())
}

// Die Aufräum-Schleife entfernt abgelaufene Einträge von allein, auch wenn die
// Tabelle nie voll wird.
func TestRateLimiter_CleanupLoopDropsExpiredEntries(t *testing.T) {
	rl := newRateLimiter(2, 20*time.Millisecond, 100)
	rl.allow("selten")
	require.Equal(t, 1, rl.size())

	go rl.cleanupStaleVisitors(5 * time.Millisecond)
	assert.Eventually(t, func() bool { return rl.size() == 0 }, 2*time.Second, 5*time.Millisecond)
}

// Ein Eintrag gilt erst nach einem vollen Zeitfenster als abgelaufen. Wird er
// früher entfernt, startet ein erschöpfter Schlüssel mit vollem Kontingent und
// umgeht das Limit, sobald ein anderer Schlüssel einen Eintrag anlegt.
func TestRateLimiter_EntryIsKeptUntilAFullWindowHasPassed(t *testing.T) {
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	rl := newRateLimiter(2, time.Minute, 100)
	rl.now = func() time.Time { return now }

	require.True(t, rl.allow("a"))
	require.True(t, rl.allow("a"))
	require.False(t, rl.allow("a"), "Kontingent aufgebraucht")

	now = now.Add(40 * time.Second) // zwei Drittel des Fensters: erst 1,33 von 2 Plätzen aufgefüllt
	rl.allow("b")                   // legt einen Eintrag an und räumt dabei abgelaufene weg
	assert.Equal(t, 2, rl.size(), "a ist noch nicht abgelaufen")
	assert.True(t, rl.allow("a"), "ein Platz ist wieder frei")
	assert.False(t, rl.allow("a"), "mehr nicht: kein Zurücksetzen auf das volle Kontingent")
}

// Die öffentliche Middleware nutzt die Obergrenze: Nach 100.000 weiteren
// Adressen ist der älteste Eintrag verdrängt, die Tabelle wächst also nicht
// weiter. (Verdrängt heißt: höchstens volles Kontingent, nie ausgesperrt.)
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
