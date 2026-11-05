package middleware

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// AccountThrottle bremst Passwort-Raten gegen ein Konto auf zwei Ebenen.
//
// Je Paar aus Konto und Quelle (IPv4-Adresse bzw. IPv6-/64, dieselbe
// Normalisierung wie beim IP-Limit) gilt eine harte Sperre: Ab threshold
// Fehlversuchen ist genau dieses Paar für baseDelay gesperrt, jeder weitere
// Fehlversuch verdoppelt die Sperre bis maxDelay. Wer nur die E-Mail-Adresse
// eines Nutzers kennt, kann also nur seine eigene Quelle sperren, nie den Login
// des Nutzers von einer anderen. Gesperrte Anfragen zählen nicht (die Sperre
// lässt sich nicht endlos verlängern); Fehlversuche verfallen nach window ohne
// neuen Versuch, ein erfolgreicher Login setzt das Paar zurück.
//
// Zusätzlich zählt ein Zähler je Konto über alle Quellen. Er sperrt nie, er
// verzögert nur: Ab accountDelayThreshold Versuchen im Fenster wartet jeder
// weitere Versuch länger (accountDelayStep je Überschreitung, höchstens
// accountDelayMax). So bleibt verteiltes Raten über viele Adressen langsam, ohne
// dass es sich als Aussperr-Waffe nutzen ließe.
//
// Kontoschlüssel sind SHA-256 der normalisierten E-Mail und werden für bekannte
// und unbekannte Adressen gleich geführt, damit nichts über die Existenz eines
// Kontos verrät. Prüfen und Zählen geschehen in Admit in einem Schritt: Jeder
// zugelassene Versuch zählt sofort als Fehlversuch und wird bei Erfolg über
// Admission.Succeeded zurückgenommen, so lassen parallele Versuche höchstens die
// Schwelle durch.
//
// Der Speicher ist in-memory und je Prozess (wie RateLimiter). Beide Tabellen
// sind auf maxEntries Einträge begrenzt; ist eine voll, wird der am längsten
// unberührte Eintrag verdrängt (abgelaufene zuerst), statt neue Schlüssel nicht
// mehr zu zählen. Verdrängen sperrt niemanden.
type AccountThrottle struct {
	mu        sync.Mutex
	pairs     *lruTable[pairEntry]
	accounts  *lruTable[accountEntry]
	threshold int
	baseDelay time.Duration
	maxDelay  time.Duration
	window    time.Duration
	now       func() time.Time

	accountDelayThreshold int
	accountDelayStep      time.Duration
	accountDelayMax       time.Duration
}

type pairEntry struct {
	failures     int
	lastFailure  time.Time
	blockedUntil time.Time
}

type accountEntry struct {
	count       int
	windowStart time.Time
}

// Standardwerte für den Login: 10 Fehlversuche je Paar, dann 30 s Sperre,
// verdoppelt bis höchstens 15 min; Fehlversuche verfallen nach 15 min Ruhe. Der
// Zähler je Konto verzögert ab 100 Versuchen in 15 min um 50 ms je weiteren
// Versuch, höchstens 3 s.
const (
	loginThrottleThreshold     = 10
	loginThrottleBaseDelay     = 30 * time.Second
	loginThrottleMaxDelay      = 15 * time.Minute
	loginThrottleWindow        = 15 * time.Minute
	loginAccountDelayThreshold = 100
	loginAccountDelayStep      = 50 * time.Millisecond
	loginAccountDelayMax       = 3 * time.Second
	throttleDefaultMaxItems    = 100_000
)

// NewLoginThrottle liefert den AccountThrottle mit den Login-Standardwerten.
func NewLoginThrottle() *AccountThrottle {
	return NewAccountThrottle(loginThrottleThreshold, loginThrottleBaseDelay, loginThrottleMaxDelay, loginThrottleWindow)
}

// NewAccountThrottle: siehe AccountThrottle für die Bedeutung der Parameter. Der
// Zähler je Konto startet mit den Login-Standardwerten (WithAccountDelay ändert sie).
func NewAccountThrottle(threshold int, baseDelay, maxDelay, window time.Duration) *AccountThrottle {
	return &AccountThrottle{
		pairs:                 newLRUTable[pairEntry](throttleDefaultMaxItems),
		accounts:              newLRUTable[accountEntry](throttleDefaultMaxItems),
		threshold:             threshold,
		baseDelay:             baseDelay,
		maxDelay:              maxDelay,
		window:                window,
		now:                   time.Now,
		accountDelayThreshold: loginAccountDelayThreshold,
		accountDelayStep:      loginAccountDelayStep,
		accountDelayMax:       loginAccountDelayMax,
	}
}

// WithAccountDelay stellt den Zähler je Konto ein (Schwelle, Schritt je
// Überschreitung, Obergrenze) und gibt den Throttle zurück.
func (t *AccountThrottle) WithAccountDelay(threshold int, step, maxDelay time.Duration) *AccountThrottle {
	t.accountDelayThreshold, t.accountDelayStep, t.accountDelayMax = threshold, step, maxDelay
	return t
}

// Admission ist das Ergebnis von Admit.
type Admission struct {
	// Blocked: Das Paar aus Konto und Quelle ist gesperrt; RetryAfter sagt, wie
	// lange noch. Der Versuch wurde nicht gezählt.
	Blocked    bool
	RetryAfter time.Duration
	// Delay ist die Wartezeit vor der Passwortprüfung, wenn das Konto aus vielen
	// Quellen angegriffen wird (nie eine Sperre).
	Delay time.Duration

	t         *AccountThrottle
	pairKey   string
	accountID string
}

func throttleKey(account string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(account))))
	return hex.EncodeToString(sum[:])
}

// Admit prüft und zählt in einem Schritt (unter einem Schloss): Ist das Paar
// gesperrt, kommt Blocked zurück und nichts wird gezählt. Sonst gilt der Versuch
// als Fehlversuch, bis Succeeded ihn zurücknimmt. Ein nil-Throttle lässt alles zu.
func (t *AccountThrottle) Admit(account, source string) Admission {
	if t == nil {
		return Admission{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	acct := throttleKey(account)
	pairKey := acct + "|" + limiterKey(source)

	pair, ok := t.pairs.get(pairKey)
	if ok && now.Sub(pair.lastFailure) > t.window && !pair.blockedUntil.After(now) {
		pair.failures = 0 // verfallen
	}
	if ok {
		if wait := pair.blockedUntil.Sub(now); wait > 0 {
			return Admission{Blocked: true, RetryAfter: wait}
		}
	} else {
		pair = t.pairs.add(pairKey, &pairEntry{}, now, t.pairExpired)
	}
	pair.failures++
	pair.lastFailure = now
	if pair.failures >= t.threshold {
		delay := t.baseDelay
		for i := t.threshold; i < pair.failures && delay < t.maxDelay; i++ {
			delay *= 2
		}
		pair.blockedUntil = now.Add(min(delay, t.maxDelay))
	}

	return Admission{t: t, pairKey: pairKey, accountID: acct, Delay: t.countAccountLocked(acct, now)}
}

// countAccountLocked zählt den Versuch am Konto und liefert die Verzögerung.
func (t *AccountThrottle) countAccountLocked(acct string, now time.Time) time.Duration {
	e, ok := t.accounts.get(acct)
	switch {
	case !ok:
		e = t.accounts.add(acct, &accountEntry{windowStart: now}, now, t.accountExpired)
	case now.Sub(e.windowStart) > t.window:
		*e = accountEntry{windowStart: now}
	}
	e.count++
	over := e.count - t.accountDelayThreshold
	if over <= 0 {
		return 0
	}
	return min(time.Duration(over)*t.accountDelayStep, t.accountDelayMax)
}

// Succeeded nimmt den Versuch nach einem erfolgreichen Login zurück: Das Paar
// wird zurückgesetzt, am Konto zählt der Versuch nicht mit.
func (a Admission) Succeeded() {
	if a.t == nil || a.Blocked {
		return
	}
	a.t.mu.Lock()
	defer a.t.mu.Unlock()
	a.t.pairs.remove(a.pairKey)
	if e, ok := a.t.accounts.get(a.accountID); ok && e.count > 0 {
		e.count--
	}
}

func (t *AccountThrottle) pairExpired(e *pairEntry, now time.Time) bool {
	return now.Sub(e.lastFailure) > t.window && !e.blockedUntil.After(now)
}

func (t *AccountThrottle) accountExpired(e *accountEntry, now time.Time) bool {
	return now.Sub(e.windowStart) > t.window
}

// lruTable ist eine größenbegrenzte Tabelle: vorne steht der am längsten
// unberührte Eintrag. Einfügen verwirft zuerst abgelaufene Einträge von vorn und
// dann bei Bedarf den ältesten; beides in O(1) je Eintrag, es gibt keinen Scan
// über die ganze Tabelle.
type lruTable[V any] struct {
	items map[string]*list.Element
	order *list.List
	max   int
}

type lruItem[V any] struct {
	key string
	val *V
}

func newLRUTable[V any](maxEntries int) *lruTable[V] {
	return &lruTable[V]{items: make(map[string]*list.Element), order: list.New(), max: maxEntries}
}

func (l *lruTable[V]) len() int { return len(l.items) }

// get liefert den Eintrag und markiert ihn als zuletzt benutzt.
func (l *lruTable[V]) get(key string) (*V, bool) {
	el, ok := l.items[key]
	if !ok {
		return nil, false
	}
	l.order.MoveToBack(el)
	return el.Value.(*lruItem[V]).val, true
}

func (l *lruTable[V]) remove(key string) {
	if el, ok := l.items[key]; ok {
		l.order.Remove(el)
		delete(l.items, key)
	}
}

// add fügt einen neuen Eintrag ein und hält die Grenze ein.
func (l *lruTable[V]) add(key string, val *V, now time.Time, expired func(*V, time.Time) bool) *V {
	for front := l.order.Front(); front != nil && expired(front.Value.(*lruItem[V]).val, now); front = l.order.Front() {
		l.remove(front.Value.(*lruItem[V]).key)
	}
	for len(l.items) >= l.max {
		l.remove(l.order.Front().Value.(*lruItem[V]).key)
	}
	l.items[key] = l.order.PushBack(&lruItem[V]{key: key, val: val})
	return val
}
