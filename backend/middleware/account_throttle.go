package middleware

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// AccountThrottle slows down password guessing against an account on two levels.
//
// For each pair of account and source (IPv4 address or IPv6 /64, the same
// normalization as for the IP limit) a hard block applies: from threshold
// failed attempts on, exactly this pair is blocked for baseDelay, and every further
// failed attempt doubles the block up to maxDelay. Someone who knows only a user's
// email address can therefore only block their own source, never the user's login
// from another one. Blocked requests do not count (the block
// cannot be extended endlessly); failed attempts expire after window without a
// new attempt, a successful login resets the pair.
//
// In addition, a per-account counter across all sources counts. It never blocks, it
// only delays: from accountDelayThreshold attempts in the window on, every
// further attempt waits longer (accountDelayStep per excess attempt, at most
// accountDelayMax). Distributed guessing across many addresses thus stays slow, without
// it being usable as a lockout weapon.
//
// Account keys are the SHA-256 of the normalized email and are handled identically for known
// and unknown addresses, so that nothing reveals the existence of an
// account. Checking and counting happen in Admit in one step: every
// admitted attempt immediately counts as a failed attempt and is taken back on success via
// Admission.Succeeded, so concurrent attempts let at most the
// threshold through.
//
// The storage is in-memory and per process (like RateLimiter). Both tables
// are limited to maxEntries entries; when one is full, the entry untouched for the
// longest time is evicted (expired ones first), instead of no longer counting new keys.
// Eviction blocks nobody.
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

// Defaults for login: 10 failed attempts per pair, then a 30 s block,
// doubled up to at most 15 min; failed attempts expire after 15 min of quiet. The
// per-account counter delays from 100 attempts in 15 min on by 50 ms per further
// attempt, at most 3 s.
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

// NewLoginThrottle returns the AccountThrottle with the login defaults.
func NewLoginThrottle() *AccountThrottle {
	return NewAccountThrottle(loginThrottleThreshold, loginThrottleBaseDelay, loginThrottleMaxDelay, loginThrottleWindow)
}

// NewAccountThrottle: see AccountThrottle for the meaning of the parameters. The
// per-account counter starts with the login defaults (WithAccountDelay changes them).
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

// WithAccountDelay configures the per-account counter (threshold, step per
// excess attempt, upper bound) and returns the throttle.
func (t *AccountThrottle) WithAccountDelay(threshold int, step, maxDelay time.Duration) *AccountThrottle {
	t.accountDelayThreshold, t.accountDelayStep, t.accountDelayMax = threshold, step, maxDelay
	return t
}

// Admission is the result of Admit.
type Admission struct {
	// Blocked: the pair of account and source is blocked; RetryAfter says how
	// long remains. The attempt was not counted.
	Blocked    bool
	RetryAfter time.Duration
	// Delay is the wait before the password check when the account is attacked from many
	// sources (never a block).
	Delay time.Duration

	t         *AccountThrottle
	pairKey   string
	accountID string
}

func throttleKey(account string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(account))))
	return hex.EncodeToString(sum[:])
}

// Admit checks and counts in one step (under one lock): if the pair is
// blocked, Blocked is returned and nothing is counted. Otherwise the attempt counts
// as a failed attempt until Succeeded takes it back. A nil throttle allows everything.
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
		pair.failures = 0 // expired
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

// countAccountLocked counts the attempt against the account and returns the delay.
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

// Succeeded takes the attempt back after a successful login: the pair
// is reset, the attempt does not count toward the account.
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

// lruTable is a size-bounded table: the entry untouched for the longest
// time is at the front. Inserting first discards expired entries from the front and
// then, if needed, the oldest; both in O(1) per entry, there is no scan
// over the whole table.
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

// get returns the entry and marks it as most recently used.
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

// add inserts a new entry and enforces the bound.
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
