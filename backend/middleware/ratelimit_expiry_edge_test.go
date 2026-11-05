package middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Ein Eintrag ist genau nach einem vollen Zeitfenster abgelaufen (der Bucket
// wäre dann wieder voll), nicht erst danach und nicht früher.
func TestRateLimiter_EntryExpiresExactlyAfterOneWindow(t *testing.T) {
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	rl := newRateLimiter(2, time.Minute, 100)
	rl.now = func() time.Time { return now }
	rl.allow("a")

	now = now.Add(time.Minute - time.Nanosecond)
	rl.dropExpired()
	assert.Equal(t, 1, rl.size(), "knapp vor Ablauf des Fensters bleibt der Eintrag")

	now = now.Add(time.Nanosecond)
	rl.dropExpired()
	assert.Equal(t, 0, rl.size(), "nach genau einem Fenster ist er entfernt")
}
