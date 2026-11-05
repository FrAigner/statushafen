package middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Die Sperre eines Paars endet genau zum Zeitpunkt blockedUntil: In dieser
// Sekunde ist der Versuch wieder zugelassen, eine Nanosekunde davor nicht.
func TestAccountThrottle_BlockEndsExactlyAtItsDeadline(t *testing.T) {
	clock := newClock()
	th := newTestThrottle(clock) // Schwelle 3, Sperre 30 s
	const acct = "ende@example.com"

	last := failAttempts(th, acct, srcAttacker, 3)
	require.False(t, last.Blocked)
	adm := th.Admit(acct, srcAttacker)
	require.True(t, adm.Blocked)
	require.Equal(t, 30*time.Second, adm.RetryAfter)

	clock.advance(30*time.Second - time.Nanosecond)
	early := th.Admit(acct, srcAttacker)
	assert.True(t, early.Blocked, "eine Nanosekunde vor dem Ende ist die Sperre aktiv")
	assert.Equal(t, time.Nanosecond, early.RetryAfter)

	clock.advance(time.Nanosecond)
	assert.False(t, th.Admit(acct, srcAttacker).Blocked, "zum Zeitpunkt blockedUntil ist der Versuch zugelassen")
}

// Das Fenster des Zählers je Konto beginnt neu erst nach Ablauf eines vollen
// Fensters; genau am Rand zählt der Versuch noch zum alten Fenster.
func TestAccountThrottle_AccountWindowRestartsOnlyAfterFullWindow(t *testing.T) {
	clock := newClock()
	th := newTestThrottle(clock) // Fenster 15 min
	th.WithAccountDelay(2, 100*time.Millisecond, 10*time.Second)
	const acct = "konto-fenster@example.com"

	th.Admit(acct, "198.51.100.1")
	th.Admit(acct, "198.51.100.2")
	clock.advance(15 * time.Minute) // genau am Rand
	assert.Equal(t, 100*time.Millisecond, th.Admit(acct, "198.51.100.3").Delay, "dritter Versuch im selben Fenster: eine Stufe über der Schwelle")

	clock.advance(15*time.Minute + time.Nanosecond)
	assert.Equal(t, time.Duration(0), th.Admit(acct, "198.51.100.4").Delay, "neues Fenster: wieder unter der Schwelle")
}
