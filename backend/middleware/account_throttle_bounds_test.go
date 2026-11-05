package middleware

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Die Sperre eines Paars wächst durch Verdopplung, bleibt aber unter der
// Obergrenze: Hier würde 30 s -> 60 s -> 120 s die Grenze von 100 s überschreiten.
func TestAccountThrottle_BackoffNeverExceedsMaxDelay(t *testing.T) {
	clock := newClock()
	th := NewAccountThrottle(2, 30*time.Second, 100*time.Second, 15*time.Minute)
	th.now = clock.now
	const acct = "grenze@example.com"

	var waits []time.Duration
	for i := 0; i < 5; i++ {
		// Der erste Versuch jeder Runde ist zugelassen, der zweite löst die Sperre aus.
		for {
			adm := th.Admit(acct, srcAttacker)
			if adm.Blocked {
				waits = append(waits, adm.RetryAfter)
				clock.advance(adm.RetryAfter + time.Second)
				break
			}
		}
	}
	for _, w := range waits {
		assert.LessOrEqual(t, w, 100*time.Second, "Sperre über der Obergrenze: %v", waits)
	}
	assert.Equal(t, 100*time.Second, waits[len(waits)-1], "die Obergrenze wird erreicht: %v", waits)
}

// Ein erfolgreicher Login nimmt seinen Versuch am Zähler des Kontos zurück,
// sonst verzögerte der echte Nutzer sich mit jedem Login selbst.
func TestAccountThrottle_SuccessRevokesOwnAttemptAtAccountCounter(t *testing.T) {
	th := newTestThrottle(newClock())
	th.WithAccountDelay(2, 100*time.Millisecond, 10*time.Second)
	const acct = "konto@example.com"

	for i := 1; i <= 4; i++ {
		th.Admit(acct, fmt.Sprintf("198.51.100.%d", i))
	}
	own := th.Admit(acct, srcOwner) // fünfter Versuch: 3 über der Schwelle
	require.Equal(t, 300*time.Millisecond, own.Delay)
	own.Succeeded()

	// Ohne Rücknahme wäre dies der sechste Versuch (400 ms).
	assert.Equal(t, 300*time.Millisecond, th.Admit(acct, "198.51.100.50").Delay)
}

// Fehlversuche verfallen erst nach dem Fenster, nicht schon an dessen Grenze.
func TestAccountThrottle_FailuresExpireOnlyAfterWindow(t *testing.T) {
	clock := newClock()
	th := newTestThrottle(clock) // Schwelle 3, Fenster 15 min
	const acct = "fenster@example.com"

	failAttempts(th, acct, srcOwner, 2)
	clock.advance(15 * time.Minute) // genau am Rand: die zwei Versuche zählen noch
	assert.False(t, th.Admit(acct, srcOwner).Blocked, "der dritte Versuch ist noch zugelassen")
	assert.True(t, th.Admit(acct, srcOwner).Blocked, "und löst die Sperre aus")
}
