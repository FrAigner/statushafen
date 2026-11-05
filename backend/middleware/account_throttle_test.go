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

// failAttempts lässt n falsche Versuche (ohne Erfolgsmeldung) durch das Tor laufen
// und liefert die letzte Zulassung.
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

	// Gesperrte Anfragen zählen nicht: die Sperre verlängert sich dadurch nicht.
	clock.advance(10 * time.Second)
	adm = th.Admit(acct, srcAttacker)
	require.True(t, adm.Blocked)
	assert.Equal(t, 20*time.Second, adm.RetryAfter)

	// Nach Ablauf zählt der nächste Fehlversuch weiter, die Sperre verdoppelt sich.
	clock.advance(21 * time.Second)
	adm = th.Admit(acct, srcAttacker)
	require.False(t, adm.Blocked, "dieser Versuch ist wieder zugelassen und zählt")
	adm = th.Admit(acct, srcAttacker)
	require.True(t, adm.Blocked)
	assert.Equal(t, 60*time.Second, adm.RetryAfter)
}

// Wer nur die E-Mail-Adresse kennt, darf den Nutzer nicht aussperren: Die
// Sperre gilt je Paar aus Konto und Quelle.
func TestAccountThrottle_BlockDoesNotAffectOtherSources(t *testing.T) {
	th := newTestThrottle(newClock())
	const acct = "opfer@example.com"
	require.True(t, failAttempts(th, acct, srcAttacker, 10).Blocked)

	adm := th.Admit(acct, srcOwner)
	assert.False(t, adm.Blocked, "der echte Nutzer von anderer Quelle bleibt zugelassen")
	assert.Zero(t, adm.Delay)
	// Gegenprobe: dieselbe Quelle bleibt gesperrt, ein anderes Konto nicht.
	assert.True(t, th.Admit(acct, srcAttacker).Blocked)
	assert.False(t, th.Admit("anderes@example.com", srcAttacker).Blocked)
}

func TestAccountThrottle_SourceIsIPv4AddressOrIPv6Prefix(t *testing.T) {
	th := newTestThrottle(newClock())
	const acct = "a@example.com"
	// Innerhalb eines IPv6-/64 wechselnde Hostadressen teilen sich den Zähler.
	hosts := []string{"2001:db8:1:2::1", "2001:db8:1:2:aa::2", "2001:db8:1:2:bb::3", "2001:db8:1:2:cc::4"}
	var adm Admission
	for _, h := range hosts {
		adm = th.Admit(acct, h)
	}
	assert.True(t, adm.Blocked, "vierter Versuch aus demselben /64 ist gesperrt")
	assert.False(t, th.Admit(acct, "2001:db8:1:3::1").Blocked, "anderes /64 ist unberührt")

	// IPv4 bleibt je Adresse, IPv4-in-IPv6 zählt als dieselbe IPv4-Adresse.
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

	// Auch eine am Schwellenversuch ausgelöste Sperre verschwindet mit dem Erfolg.
	th2 := newTestThrottle(newClock())
	th2.Admit(acct, srcOwner)
	th2.Admit(acct, srcOwner)
	th2.Admit(acct, srcOwner).Succeeded()
	assert.False(t, th2.Admit(acct, srcOwner).Blocked)

	// Fehlversuche verfallen nach dem Zeitfenster ohne weiteren Versuch.
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

// Zähler je Konto über alle Quellen: bremst verteiltes Raten, sperrt aber nie.
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

	// Der echte Nutzer ist nur verzögert, nicht gesperrt.
	adm := th.Admit(acct, srcOwner)
	assert.False(t, adm.Blocked)
	assert.Equal(t, 400*time.Millisecond, adm.Delay)

	// Ein Erfolg nimmt nur den eigenen Versuch zurück, nicht die der Angreifer.
	adm.Succeeded()
	assert.Equal(t, 400*time.Millisecond, th.Admit(acct, srcOwner).Delay)

	// Nach dem Zeitfenster ist das Konto wieder frei.
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

// Prüfen und Zählen sind ein Schritt: 100 gleichzeitige Fehlversuche lassen
// höchstens die Schwelle durch (kein Prüfen-dann-Zählen mehr).
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

// Volle Tabellen verdrängen den ältesten Eintrag, statt neue Schlüssel nicht
// mehr zu zählen: Auch das 20. neue Konto bekommt seine Sperre.
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

	// Der älteste Eintrag ist verdrängt, ohne dass jemand dadurch gesperrt wird.
	assert.False(t, th.Admit("k0@example.com", srcAttacker).Blocked)
}

// Verdrängt wird der am längsten unberührte Eintrag; ein aktiv genutzter bleibt.
func TestAccountThrottle_EvictionKeepsRecentlyUsedEntries(t *testing.T) {
	th := newTestThrottle(newClock())
	th.pairs.max, th.accounts.max = 3, 3
	failAttempts(th, "aktiv@example.com", srcAttacker, 3)
	for i := 0; i < 6; i++ {
		th.Admit(fmt.Sprintf("fremd%d@example.com", i), srcOwner)
		th.Admit("aktiv@example.com", srcAttacker) // gesperrt, aber zuletzt benutzt
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

// Die Login-Standardwerte: bis zu 10 Fehlversuche je Paar bleiben folgenlos, der
// elfte Versuch ist 30 Sekunden gesperrt; Fehlversuche verfallen nach 15 Minuten.
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

	// Zähler je Konto: ab 100 Fehlversuchen in 15 Minuten gibt es Verzögerung, nie eine Sperre.
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
