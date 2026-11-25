package middleware

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A pair's block grows by doubling but stays under the
// upper bound: here 30 s -> 60 s -> 120 s would exceed the 100 s limit.
func TestAccountThrottle_BackoffNeverExceedsMaxDelay(t *testing.T) {
	clock := newClock()
	th := NewAccountThrottle(2, 30*time.Second, 100*time.Second, 15*time.Minute)
	th.now = clock.now
	const acct = "grenze@example.com"

	var waits []time.Duration
	for i := 0; i < 5; i++ {
		// The first attempt of each round is admitted, the second triggers the block.
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

// A successful login takes its attempt back from the account's counter,
// otherwise the real user would delay themselves with every login.
func TestAccountThrottle_SuccessRevokesOwnAttemptAtAccountCounter(t *testing.T) {
	th := newTestThrottle(newClock())
	th.WithAccountDelay(2, 100*time.Millisecond, 10*time.Second)
	const acct = "konto@example.com"

	for i := 1; i <= 4; i++ {
		th.Admit(acct, fmt.Sprintf("198.51.100.%d", i))
	}
	own := th.Admit(acct, srcOwner) // fifth attempt: 3 over the threshold
	require.Equal(t, 300*time.Millisecond, own.Delay)
	own.Succeeded()

	// Without the rollback this would be the sixth attempt (400 ms).
	assert.Equal(t, 300*time.Millisecond, th.Admit(acct, "198.51.100.50").Delay)
}

// Failed attempts expire only after the window, not already at its boundary.
func TestAccountThrottle_FailuresExpireOnlyAfterWindow(t *testing.T) {
	clock := newClock()
	th := newTestThrottle(clock) // threshold 3, window 15 min
	const acct = "fenster@example.com"

	failAttempts(th, acct, srcOwner, 2)
	clock.advance(15 * time.Minute) // exactly at the edge: the two attempts still count
	assert.False(t, th.Admit(acct, srcOwner).Blocked, "der dritte Versuch ist noch zugelassen")
	assert.True(t, th.Admit(acct, srcOwner).Blocked, "und löst die Sperre aus")
}
