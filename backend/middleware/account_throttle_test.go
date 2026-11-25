package middleware

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestThrottle(clock *fakeClock) *AccountThrottle {
	th := NewAccountThrottle(3, 30*time.Second, 5*time.Minute, 15*time.Minute)
	th.now = clock.now
	return th
}

const (
	srcAttacker = "198.51.100.7"
	srcOwner    = "203.0.113.20"
)

func newClock() *fakeClock { return &fakeClock{t: time.Unix(1_700_000_000, 0)} }

// failAttempts runs n wrong attempts (without a success report) through the gate
// and returns the last admission.
func failAttempts(th *AccountThrottle, account, source string, n int) Admission {
	var last Admission
	for i := 0; i < n; i++ {
		last = th.Admit(account, source)
	}
	return last
}

func TestAccountThrottle_BlocksPairAfterThresholdWithBackoff(t *testing.T) {
	clock := newClock()
	th := newTestThrottle(clock)
	const acct = "opfer@example.com"

	for i := 0; i < 3; i++ {
		adm := th.Admit(acct, srcAttacker)
		require.False(t, adm.Blocked, "Versuch %d bis zur Schwelle ist zugelassen", i+1)
	}
	adm := th.Admit(acct, srcAttacker)
	require.True(t, adm.Blocked, "der Versuch nach der Schwelle ist gesperrt")
	assert.Equal(t, 30*time.Second, adm.RetryAfter)

	// Blocked requests do not count: the block is not extended by them.
	clock.advance(10 * time.Second)
	adm = th.Admit(acct, srcAttacker)
	require.True(t, adm.Blocked)
	assert.Equal(t, 20*time.Second, adm.RetryAfter)

	// After expiry the next failed attempt keeps counting, the block doubles.
	clock.advance(21 * time.Second)
	adm = th.Admit(acct, srcAttacker)
	require.False(t, adm.Blocked, "dieser Versuch ist wieder zugelassen und zählt")
	adm = th.Admit(acct, srcAttacker)
	require.True(t, adm.Blocked)
	assert.Equal(t, 60*time.Second, adm.RetryAfter)
}

// Someone who only knows the email address must not be able to lock the user out: the
// block applies per pair of account and source.
func TestAccountThrottle_BlockDoesNotAffectOtherSources(t *testing.T) {
	th := newTestThrottle(newClock())
	const acct = "opfer@example.com"
	require.True(t, failAttempts(th, acct, srcAttacker, 10).Blocked)

	adm := th.Admit(acct, srcOwner)
	assert.False(t, adm.Blocked, "der echte Nutzer von anderer Quelle bleibt zugelassen")
	assert.Zero(t, adm.Delay)
	// Control check: the same source stays blocked, a different account does not.
	assert.True(t, th.Admit(acct, srcAttacker).Blocked)
	assert.False(t, th.Admit("anderes@example.com", srcAttacker).Blocked)
}

func TestAccountThrottle_SourceIsIPv4AddressOrIPv6Prefix(t *testing.T) {
	th := newTestThrottle(newClock())
	const acct = "a@example.com"
	// Host addresses changing within an IPv6 /64 share the counter.
	hosts := []string{"2001:db8:1:2::1", "2001:db8:1:2:aa::2", "2001:db8:1:2:bb::3", "2001:db8:1:2:cc::4"}
	var adm Admission
	for _, h := range hosts {
		adm = th.Admit(acct, h)
	}
	assert.True(t, adm.Blocked, "vierter Versuch aus demselben /64 ist gesperrt")
	assert.False(t, th.Admit(acct, "2001:db8:1:3::1").Blocked, "anderes /64 ist unberührt")

	// IPv4 stays per address, IPv4-in-IPv6 counts as the same IPv4 address.
	failAttempts(th, acct, "192.0.2.1", 3)
	assert.True(t, th.Admit(acct, "::ffff:192.0.2.1").Blocked)
	assert.False(t, th.Admit(acct, "192.0.2.2").Blocked)
}

func TestAccountThrottle_NormalisesAccountAndKeepsAccountsApart(t *testing.T) {
	th := newTestThrottle(newClock())
	for _, variant := range []string{"Opfer@example.com", " opfer@EXAMPLE.com ", "OPFER@example.com"} {
		th.Admit(variant, srcAttacker)
	}
	assert.True(t, th.Admit("opfer@example.com", srcAttacker).Blocked)
	assert.False(t, th.Admit("anderes@example.com", srcAttacker).Blocked)
}

func TestAccountThrottle_SuccessResetsPairAndDecays(t *testing.T) {
	clock := newClock()
	th := newTestThrottle(clock)
	const acct = "a@example.com"

	th.Admit(acct, srcOwner)
	th.Admit(acct, srcOwner)
	th.Admit(acct, srcOwner).Succeeded()
	adm := failAttempts(th, acct, srcOwner, 3)
	assert.False(t, adm.Blocked, "Erfolg setzt den Zähler des Paars zurück")

	// A block triggered at the threshold attempt also disappears on success.
	th2 := newTestThrottle(newClock())
	th2.Admit(acct, srcOwner)
	th2.Admit(acct, srcOwner)
	th2.Admit(acct, srcOwner).Succeeded()
	assert.False(t, th2.Admit(acct, srcOwner).Blocked)

	// Failed attempts expire after the time window without further attempts.
	th3 := newTestThrottle(clock)
	failAttempts(th3, acct, srcOwner, 2)
	clock.advance(16 * time.Minute)
	assert.False(t, failAttempts(th3, acct, srcOwner, 3).Blocked, "alte Fehlversuche zählen nicht mit")
	assert.True(t, th3.Admit(acct, srcOwner).Blocked)
}

func TestAccountThrottle_NilIsNoOp(t *testing.T) {
	var th *AccountThrottle
	adm := th.Admit("a@example.com", srcOwner)
	assert.False(t, adm.Blocked)
	assert.Zero(t, adm.Delay)
	assert.NotPanics(t, adm.Succeeded)
}

// Per-account counter across all sources: slows down distributed guessing but never blocks.
func TestAccountThrottle_AccountWideCounterOnlyDelaysAndNeverBlocks(t *testing.T) {
	clock := newClock()
	th := newTestThrottle(clock)
	th.WithAccountDelay(5, 100*time.Millisecond, 400*time.Millisecond)
	const acct = "opfer@example.com"

	source := func(i int) string { return fmt.Sprintf("198.51.100.%d", i) }
	for i := 1; i <= 5; i++ {
		adm := th.Admit(acct, source(i))
		require.False(t, adm.Blocked)
		require.Zero(t, adm.Delay, "bis zur Schwelle keine Verzögerung (Versuch %d)", i)
	}
	var delays []time.Duration
	for i := 6; i <= 12; i++ {
		adm := th.Admit(acct, source(i))
		require.False(t, adm.Blocked, "viele Quellen führen nie zur Sperre")
		delays = append(delays, adm.Delay)
	}
	assert.Equal(t, []time.Duration{
		100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond, 400 * time.Millisecond,
		400 * time.Millisecond, 400 * time.Millisecond, 400 * time.Millisecond,
	}, delays, "steigende Wartezeit mit Obergrenze")

	// The real user is only delayed, not blocked.
	adm := th.Admit(acct, srcOwner)
	assert.False(t, adm.Blocked)
	assert.Equal(t, 400*time.Millisecond, adm.Delay)

	// A success only takes back its own attempt, not the attacker's.
	adm.Succeeded()
	assert.Equal(t, 400*time.Millisecond, th.Admit(acct, srcOwner).Delay)

	// After the time window the account is free again.
	clock.advance(16 * time.Minute)
	assert.Zero(t, th.Admit(acct, srcOwner).Delay)
}

func TestAccountThrottle_AccountWideCounterIsPerAccount(t *testing.T) {
	th := newTestThrottle(newClock())
	th.WithAccountDelay(2, 10*time.Millisecond, time.Second)
	for i := 0; i < 6; i++ {
		th.Admit("a@example.com", fmt.Sprintf("198.51.100.%d", i))
	}
	assert.NotZero(t, th.Admit("a@example.com", srcOwner).Delay)
	assert.Zero(t, th.Admit("b@example.com", srcOwner).Delay)
}

// Checking and counting are one step: 100 concurrent failed attempts let
// at most the threshold through (no more check-then-count).
func TestAccountThrottle_ParallelAttemptsAdmitAtMostThreshold(t *testing.T) {
	th := NewLoginThrottle()
	var admitted atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if !th.Admit("opfer@example.com", srcAttacker).Blocked {
				admitted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	assert.EqualValues(t, loginThrottleThreshold, admitted.Load())
}

// Full tables evict the oldest entry instead of no longer counting new
// keys: even the 20th new account gets its block.
func TestAccountThrottle_FullTableEvictsOldestInsteadOfIgnoringNewKeys(t *testing.T) {
	th := newTestThrottle(newClock())
	th.pairs.max, th.accounts.max = 5, 5
	for i := 0; i < 20; i++ {
		th.Admit(fmt.Sprintf("k%d@example.com", i), srcAttacker)
	}
	assert.LessOrEqual(t, th.pairs.len(), 5)
	assert.LessOrEqual(t, th.accounts.len(), 5)

	const neu = "neu@example.com"
	require.True(t, failAttempts(th, neu, srcAttacker, 4).Blocked, "neuer Schlüssel wird trotz voller Tabelle gezählt und gesperrt")
	assert.LessOrEqual(t, th.pairs.len(), 5)

	// The oldest entry is evicted without anyone being blocked as a result.
	assert.False(t, th.Admit("k0@example.com", srcAttacker).Blocked)
}

// The entry untouched for the longest time is evicted; an actively used one stays.
func TestAccountThrottle_EvictionKeepsRecentlyUsedEntries(t *testing.T) {
	th := newTestThrottle(newClock())
	th.pairs.max, th.accounts.max = 3, 3
	failAttempts(th, "aktiv@example.com", srcAttacker, 3)
	for i := 0; i < 6; i++ {
		th.Admit(fmt.Sprintf("fremd%d@example.com", i), srcOwner)
		th.Admit("aktiv@example.com", srcAttacker) // blocked, but used most recently
	}
	assert.True(t, th.Admit("aktiv@example.com", srcAttacker).Blocked)
}

func TestAccountThrottle_ExpiredEntriesAreDroppedBeforeNewOnes(t *testing.T) {
	clock := newClock()
	th := newTestThrottle(clock)
	th.pairs.max, th.accounts.max = 5, 5
	for i := 0; i < 5; i++ {
		th.Admit(fmt.Sprintf("k%d@example.com", i), srcAttacker)
	}
	clock.advance(time.Hour)
	th.Admit("neu@example.com", srcAttacker)
	assert.Equal(t, 1, th.pairs.len())
	assert.Equal(t, 1, th.accounts.len())
}

// The login defaults: up to 10 failed attempts per pair have no consequences, the
// eleventh attempt is blocked for 30 seconds; failed attempts expire after 15 minutes.
func TestNewLoginThrottle_UsesLoginDefaults(t *testing.T) {
	clock := newClock()
	th := NewLoginThrottle()
	th.now = clock.now
	const acct = "konto@example.com"

	for i := 0; i < loginThrottleThreshold; i++ {
		require.False(t, th.Admit(acct, srcAttacker).Blocked, "Versuch %d bis zur Schwelle", i+1)
	}
	adm := th.Admit(acct, srcAttacker)
	require.True(t, adm.Blocked)
	assert.Equal(t, loginThrottleBaseDelay, adm.RetryAfter)

	clock.advance(loginThrottleWindow + loginThrottleBaseDelay)
	assert.False(t, th.Admit(acct, srcAttacker).Blocked, "nach Ablauf von Sperre und Fenster wieder frei")

	// Per-account counter: from 100 failed attempts in 15 minutes there is a delay, never a block.
	th2 := NewLoginThrottle()
	th2.now = clock.now
	for i := 0; i < loginAccountDelayThreshold; i++ {
		require.Zero(t, th2.Admit(acct, fmt.Sprintf("10.%d.%d.1", i/200, i%200)).Delay)
	}
	adm = th2.Admit(acct, srcOwner)
	assert.False(t, adm.Blocked)
	assert.Equal(t, loginAccountDelayStep, adm.Delay)
	assert.LessOrEqual(t, loginAccountDelayMax, 5*time.Second, "höchstens wenige Sekunden je Versuch")
}
