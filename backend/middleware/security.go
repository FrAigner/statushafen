package middleware

import (
	"crypto/rand"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// SecurityHeaders sets standard security headers (review finding S11).
// Mount in main.go directly after r.Use(CORSMiddleware()):
//
//	r.Use(middleware.SecurityHeaders())
//
// Send HSTS only when TLS is terminated: at the backend itself or by a
// trusted reverse proxy (X-Forwarded-Proto, see requestIsHTTPS).
// ─────────────────────────────────────────────────────────────────────────────
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		// Restrictive CSP – adjust for the concrete frontend origin if needed.
		// base-uri and form-action do not fall back to default-src and are therefore
		// listed explicitly. img-src allows https: because page logos may come from
		// arbitrary https addresses (validLogoURL); the price is
		// that the logo host sees visitors' IP and User-Agent.
		// Cloudflare's JavaScript Detections (cannot be disabled on the Free
		// plan) inject an inline script. Cloudflare takes the
		// nonce from this header for its scripts; our own scripts
		// keep running via 'self'. The nonce is new per request (crypto/rand,
		// 130 bits), so that injected inline scripts do not know it.
		h.Set("Content-Security-Policy",
			"default-src 'self'; base-uri 'self'; form-action 'self'; img-src 'self' data: https:; style-src 'self' 'unsafe-inline'; "+
				"font-src 'self'; script-src 'self' 'nonce-"+rand.Text()+"'; connect-src 'self'; frame-ancestors 'none'")
		if requestIsHTTPS(c) {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		c.Next()
	}
}

// trustProbeAddr is a fixed documentation address (RFC 5737) that serves only as a
// probe in peerIsTrustedProxy.
const trustProbeAddr = "192.0.2.254"

// peerIsTrustedProxy reports whether the TCP sender is a trusted proxy according to gin's
// rules (TRUSTED_PROXIES, Unix socket). Gin keeps this check
// internal; ClientIP() only hands out a forwarding header for trusted senders.
// The probe therefore sets its own X-Forwarded-For on a copy of the request
// and checks whether it comes through. The real request remains
// unchanged.
func peerIsTrustedProxy(c *gin.Context) bool {
	orig := c.Request
	probe := *orig
	probe.Header = http.Header{"X-Forwarded-For": {trustProbeAddr}}
	c.Request = &probe
	defer func() { c.Request = orig }()
	return c.ClientIP() == trustProbeAddr
}

// requestIsHTTPS: TLS ends at the backend itself, or a trusted proxy reports
// via X-Forwarded-Proto that it terminated TLS (the last entry, appended
// by the nearest proxy, is authoritative). Without a trusted sender the
// header never counts, otherwise any client could force or suppress HSTS.
func requestIsHTTPS(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	proto := c.GetHeader("X-Forwarded-Proto")
	if proto == "" {
		return false
	}
	parts := strings.Split(proto, ",")
	if !strings.EqualFold(strings.TrimSpace(parts[len(parts)-1]), "https") {
		return false
	}
	return peerIsTrustedProxy(c)
}

// ─────────────────────────────────────────────────────────────────────────────
// RequireGlobalAdmin gates endpoints that have no page_id scope
// (audit log, tenant overview). Replaces the misuse of
// RequirePermission("…") on global routes.
// ─────────────────────────────────────────────────────────────────────────────
func RequireGlobalAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDVal, exists := c.Get("userID")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized", "code": "unauthorized"})
			c.Abort()
			return
		}
		userID, ok := userIDVal.(uint)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user session", "code": "server_error"})
			c.Abort()
			return
		}
		var user models.User
		if err := database.DB.First(&user, userID).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found", "code": "unauthorized"})
			c.Abort()
			return
		}
		// never from a page SSO session.
		if !EffectiveGlobalAdmin(c, user) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied: global admin required", "code": "forbidden"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RateLimiter: simple in-memory token bucket per client IP (review finding S7).
// For horizontal scaling, replace later with a Redis-based limiter.
//
// Use on the sensitive public endpoints in main.go, e.g.:
//
//	loginLimiter := middleware.NewRateLimiter(5, time.Minute)   // 5 attempts / min.
//	r.POST("/api/login", loginLimiter, h.LoginHandler)
//	r.POST("/api/subscribe", middleware.NewRateLimiter(10, time.Minute), h.SubscribeHandler)
//
// ─────────────────────────────────────────────────────────────────────────────
type visitor struct {
	tokens   float64
	lastSeen time.Time
}

// rateLimiterMaxEntries is the hard upper bound on entries per limiter. An
// instance with a full table occupies a few megabytes; whoever requests from very many
// source addresses only evicts the entries untouched for the longest time.
const rateLimiterMaxEntries = 100_000

type rateLimiter struct {
	mu       sync.Mutex
	visitors *lruTable[visitor]
	max      float64
	refill   float64 // tokens per second
	window   time.Duration
	now      func() time.Time
}

// rateLimiterClock is the clock of all rate limiters created by NewRateLimiter (tests replace it).
// It is atomic because the cleanup goroutines of long-lived limiters read it concurrently.
var rateLimiterClock atomic.Pointer[func() time.Time]

func init() {
	now := time.Now
	rateLimiterClock.Store(&now)
}

func rateLimiterNow() time.Time { return (*rateLimiterClock.Load())() }

// SetRateLimiterClockForTests replaces the rate limiter clock and registers the reset via
// cleanup (like the other *ForTests helpers). It also affects limiters created earlier.
func SetRateLimiterClockForTests(cleanup func(func()), now func() time.Time) {
	prev := rateLimiterClock.Swap(&now)
	cleanup(func() { rateLimiterClock.Store(prev) })
}

func newRateLimiter(max int, per time.Duration, maxEntries int) *rateLimiter {
	return &rateLimiter{
		visitors: newLRUTable[visitor](maxEntries),
		max:      float64(max),
		refill:   float64(max) / per.Seconds(),
		window:   per,
		// Indirection so that SetRateLimiterClockForTests also reaches limiters built before it.
		now: rateLimiterNow,
	}
}

// size returns the number of remembered keys.
func (rl *rateLimiter) size() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.visitors.len()
}

// visitorExpired: after a full time window without a request the bucket would
// be full again, so the entry is equivalent to a new one and dispensable.
func (rl *rateLimiter) visitorExpired(v *visitor, now time.Time) bool {
	return now.Sub(v.lastSeen) >= rl.window
}

// dropExpired removes the expired entries from the older end of the table.
func (rl *rateLimiter) dropExpired() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()
	for front := rl.visitors.order.Front(); front != nil && rl.visitorExpired(front.Value.(*lruItem[visitor]).val, now); front = rl.visitors.order.Front() {
		rl.visitors.remove(front.Value.(*lruItem[visitor]).key)
	}
}

// cleanupStaleVisitors cleans up regularly so that the table stays small even with infrequent
// traffic. The upper bound enforces itself in every case regardless.
func (rl *rateLimiter) cleanupStaleVisitors(every time.Duration) {
	for {
		time.Sleep(every)
		rl.dropExpired()
	}
}

// allow checks and consumes (token bucket) a quota for the given
// client IP. If the table is full, a new key evicts the entry untouched for the
// longest time: it is still counted and throttled, and nobody is
// locked out (an evicted key starts at most with a full quota).
func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	v, ok := rl.visitors.get(ip)
	if !ok {
		v = rl.visitors.add(ip, &visitor{tokens: rl.max, lastSeen: now}, now, rl.visitorExpired)
	} else {
		// refill bucket
		v.tokens += now.Sub(v.lastSeen).Seconds() * rl.refill
		if v.tokens > rl.max {
			v.tokens = rl.max
		}
		v.lastSeen = now
	}

	if v.tokens < 1 {
		return false
	}
	v.tokens--
	return true
}

// limiterKey maps a client IP to the bucket key. An
// IPv6 connection usually gets an entire /64 and can switch the host address
// within it at will; therefore for IPv6 the /64 prefix counts. IPv4 and
// IPv4-in-IPv6 stay per address.
func limiterKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	// Prefix(64) never fails for a valid IPv6 address.
	prefix, _ := addr.Prefix(64)
	return prefix.String()
}

// NewRateLimiter allows `max` requests per `per` time window (burst = max).
func NewRateLimiter(max int, per time.Duration) gin.HandlerFunc {
	rl := newRateLimiter(max, per, rateLimiterMaxEntries)
	go rl.cleanupStaleVisitors(10 * time.Minute)

	return func(c *gin.Context) {
		if !rl.allow(limiterKey(c.ClientIP())) {
			c.Header("Retry-After", "60")
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Zu viele Anfragen. Bitte später erneut versuchen.", "code": "rate_limited"})
			c.Abort()
			return
		}
		c.Next()
	}
}
