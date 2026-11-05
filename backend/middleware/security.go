package middleware

import (
	"crypto/rand"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// SecurityHeaders setzt Standard-Sicherheitsheader (Review-Befund S11).
// In main.go direkt nach r.Use(CORSMiddleware()) einhängen:
//
//	r.Use(middleware.SecurityHeaders())
//
// HSTS nur senden, wenn TLS terminiert wird: am Backend selbst oder von einem
// vertrauten Reverse-Proxy (X-Forwarded-Proto, siehe requestIsHTTPS).
// ─────────────────────────────────────────────────────────────────────────────
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		// Restriktive CSP – bei Bedarf für die konkrete Frontend-Herkunft anpassen.
		// base-uri und form-action fallen nicht auf default-src zurück und stehen
		// deshalb ausdrücklich da. img-src erlaubt https:, weil Seiten-Logos von
		// beliebigen https-Adressen kommen dürfen (validLogoURL); der Preis ist,
		// dass der Logo-Host IP und User-Agent der Besucher sieht.
		// Cloudflares JavaScript Detections (auf dem Free-Plan nicht
		// abschaltbar) spritzen ein Inline-Skript ein. Cloudflare übernimmt den
		// Nonce aus diesem Header für seine Skripte; unsere eigenen Skripte
		// laufen weiter über 'self'. Der Nonce ist je Anfrage neu (crypto/rand,
		// 130 Bit), damit eingeschleuste Inline-Skripte ihn nicht kennen.
		h.Set("Content-Security-Policy",
			"default-src 'self'; base-uri 'self'; form-action 'self'; img-src 'self' data: https:; style-src 'self' 'unsafe-inline'; "+
				"font-src 'self'; script-src 'self' 'nonce-"+rand.Text()+"'; connect-src 'self'; frame-ancestors 'none'")
		if requestIsHTTPS(c) {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		c.Next()
	}
}

// trustProbeAddr ist eine feste Dokumentationsadresse (RFC 5737), die nur als
// Sonde in peerIsTrustedProxy dient.
const trustProbeAddr = "192.0.2.254"

// peerIsTrustedProxy meldet, ob der TCP-Absender nach den Regeln von gin ein
// vertrauter Proxy ist (TRUSTED_PROXIES, Unix-Socket). Gin hält diese Prüfung
// intern; ClientIP() gibt einen Weiterleitungs-Header nur für vertraute Absender
// heraus. Deshalb setzt die Sonde auf einer Kopie der Anfrage einen eigenen
// X-Forwarded-For und prüft, ob er durchkommt. Die echte Anfrage bleibt
// unverändert.
func peerIsTrustedProxy(c *gin.Context) bool {
	orig := c.Request
	probe := *orig
	probe.Header = http.Header{"X-Forwarded-For": {trustProbeAddr}}
	c.Request = &probe
	defer func() { c.Request = orig }()
	return c.ClientIP() == trustProbeAddr
}

// requestIsHTTPS: TLS endet am Backend selbst, oder ein vertrauter Proxy meldet
// per X-Forwarded-Proto, dass er TLS terminiert hat (maßgeblich ist der letzte,
// vom nächsten Proxy angehängte Eintrag). Ohne vertrauten Absender zählt der
// Header nie, sonst könnte jeder Client HSTS erzwingen oder unterdrücken.
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
// RequireGlobalAdmin gated Endpunkte, die keinen page_id-Scope haben
// (Audit-Log, Mandanten-Übersicht). Ersetzt den Missbrauch von
// RequirePermission("…") auf globalen Routen.
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
		// nie aus einer Seiten-SSO-Sitzung.
		if !EffectiveGlobalAdmin(c, user) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied: global admin required", "code": "forbidden"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RateLimiter: einfacher In-Memory Token-Bucket pro Client-IP (Review-Befund S7).
// Für horizontale Skalierung später durch einen Redis-basierten Limiter ersetzen.
//
// Einsatz an den sensiblen öffentlichen Endpunkten in main.go, z.B.:
//
//	loginLimiter := middleware.NewRateLimiter(5, time.Minute)   // 5 Versuche / Min.
//	r.POST("/api/login", loginLimiter, h.LoginHandler)
//	r.POST("/api/subscribe", middleware.NewRateLimiter(10, time.Minute), h.SubscribeHandler)
//
// ─────────────────────────────────────────────────────────────────────────────
type visitor struct {
	tokens   float64
	lastSeen time.Time
}

// rateLimiterMaxEntries ist die harte Obergrenze der Einträge je Limiter. Eine
// Instanz belegt bei voller Tabelle wenige Megabyte; wer mit sehr vielen
// Quelladressen anfragt, verdrängt nur die am längsten unberührten Einträge.
const rateLimiterMaxEntries = 100_000

type rateLimiter struct {
	mu       sync.Mutex
	visitors *lruTable[visitor]
	max      float64
	refill   float64 // Tokens pro Sekunde
	window   time.Duration
	now      func() time.Time
}

func newRateLimiter(max int, per time.Duration, maxEntries int) *rateLimiter {
	return &rateLimiter{
		visitors: newLRUTable[visitor](maxEntries),
		max:      float64(max),
		refill:   float64(max) / per.Seconds(),
		window:   per,
		now:      time.Now,
	}
}

// size liefert die Zahl der gemerkten Schlüssel.
func (rl *rateLimiter) size() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.visitors.len()
}

// visitorExpired: Nach einem vollen Zeitfenster ohne Anfrage wäre der Bucket
// wieder voll, der Eintrag also gleichwertig zu einem neuen und entbehrlich.
func (rl *rateLimiter) visitorExpired(v *visitor, now time.Time) bool {
	return now.Sub(v.lastSeen) >= rl.window
}

// dropExpired entfernt die abgelaufenen Einträge vom älteren Ende der Tabelle.
func (rl *rateLimiter) dropExpired() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()
	for front := rl.visitors.order.Front(); front != nil && rl.visitorExpired(front.Value.(*lruItem[visitor]).val, now); front = rl.visitors.order.Front() {
		rl.visitors.remove(front.Value.(*lruItem[visitor]).key)
	}
}

// cleanupStaleVisitors räumt regelmäßig ab, damit die Tabelle auch bei seltenem
// Verkehr klein bleibt. Die Obergrenze hält sie unabhängig davon in jedem Fall ein.
func (rl *rateLimiter) cleanupStaleVisitors(every time.Duration) {
	for {
		time.Sleep(every)
		rl.dropExpired()
	}
}

// allow prüft und verbraucht (Token-Bucket) ein Kontingent für die gegebene
// Client-IP. Ist die Tabelle voll, verdrängt ein neuer Schlüssel den am längsten
// unberührten Eintrag: Er wird weiter gezählt und gebremst, und niemand wird
// ausgesperrt (ein verdrängter Schlüssel startet höchstens mit vollem Kontingent).
func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	v, ok := rl.visitors.get(ip)
	if !ok {
		v = rl.visitors.add(ip, &visitor{tokens: rl.max, lastSeen: now}, now, rl.visitorExpired)
	} else {
		// Bucket auffüllen
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

// limiterKey bildet eine Client-IP auf den Schlüssel des Buckets ab. Ein
// IPv6-Anschluss bekommt üblicherweise ein ganzes /64 und kann darin beliebig
// die Hostadresse wechseln; deshalb zählt bei IPv6 das /64-Präfix. IPv4 und
// IPv4-in-IPv6 bleiben je Adresse.
func limiterKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	// Prefix(64) schlägt für eine gültige IPv6-Adresse nie fehl.
	prefix, _ := addr.Prefix(64)
	return prefix.String()
}

// NewRateLimiter erlaubt `max` Anfragen pro `per`-Zeitfenster (Burst = max).
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
