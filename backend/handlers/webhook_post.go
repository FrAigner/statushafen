package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Zentraler Versand an Google-Chat-Webhooks. Google Chat antwortet
// bei Überlast mit 429 (ggf. Retry-After) bzw. 503 und erlaubt je Space nur
// etwa eine Nachricht pro Sekunde. Alle vier Aufrufer (Test-Endpunkt,
// Subscriber-Webhook, globaler Webhook, externe Vorfälle) laufen deshalb
// durch postWebhook statt je einen eigenen Post auf webhookTestClient zu
// kopieren. Der SSRF-Schutz bleibt unverändert, denn gesendet wird weiterhin
// über webhookTestClient (safeDialContext).

// webhookKind ist die Fehlerklasse eines Webhook-Versands.
type webhookKind int

const (
	// webhookOK: 2xx.
	webhookOK webhookKind = iota
	// webhookRateLimited: HTTP 429 (oder der lokale Limiter ist erschöpft).
	webhookRateLimited
	// webhookTransient: 5xx, Netzfehler, Timeout - ein späterer Versuch kann klappen.
	webhookTransient
	// webhookPermanent: 4xx außer 429 (und 3xx) - URL oder Key sind falsch.
	webhookPermanent
)

// String liefert die Fehlerklasse fürs Log.
func (k webhookKind) String() string {
	switch k {
	case webhookOK:
		return "ok"
	case webhookRateLimited:
		return "rate_limited"
	case webhookTransient:
		return "transient"
	default:
		return "permanent"
	}
}

// webhookMode steuert, ob postWebhook warten und wiederholen darf.
type webhookMode int

const (
	// webhookModeNotify: Benachrichtigung im Hintergrund, darf im Rahmen der
	// Obergrenzen warten und wiederholen.
	webhookModeNotify webhookMode = iota
	// webhookModeProbe: Test-Endpunkt, genau ein Versuch, nie warten.
	webhookModeProbe
)

// WebhookResult ist das typisierte Ergebnis von postWebhook. Es enthält nie
// die URL.
type WebhookResult struct {
	Kind webhookKind
	// Status ist der HTTP-Status des Ziels (0 bei Netzfehler/Timeout/lokaler Drosselung).
	Status int
	// RetryAfter: bei webhookRateLimited die Wartezeit laut Ziel (bzw. lokalem Limiter).
	RetryAfter time.Duration
	// Attempts: Anzahl tatsächlich gesendeter Versuche.
	Attempts int
	// Class: Kurzname der Fehlerursache fürs Log ("timeout", "network", "http_429" ...).
	Class string
}

// OK meldet, ob der Versand erfolgreich war.
func (r WebhookResult) OK() bool { return r.Kind == webhookOK }

// Err liefert einen URL-freien Fehler (für Audit-Texte) oder nil bei Erfolg.
func (r WebhookResult) Err() error {
	if r.OK() {
		return nil
	}
	if r.Status != 0 {
		return fmt.Errorf("webhook status %d", r.Status)
	}
	return fmt.Errorf("webhook %s", r.Class)
}

// RetryAfterSeconds rundet auf ganze Sekunden (mindestens 1).
func (r WebhookResult) RetryAfterSeconds() int {
	s := int(math.Ceil(r.RetryAfter.Seconds()))
	if s < 1 {
		return 1
	}
	return s
}

const (
	webhookMaxAttempts = 3
	// webhookMaxTotalWait: Obergrenze für die Summe aller Wartezeiten (Limiter
	// und Retry) einer Benachrichtigung.
	webhookMaxTotalWait = 30 * time.Second
	webhookBackoffBase  = time.Second
	// Limiter je URL: 1 Nachricht/Sekunde, Burst 2 (Google: ca. 1/s je Space).
	webhookLimiterRate  = 1.0
	webhookLimiterBurst = 2.0
	// Speicherbegrenzung des Limiters.
	webhookLimiterMaxEntries = 1024
	webhookLimiterTTL        = 10 * time.Minute
	webhookMaxBodyDrain      = 1 << 20
)

// Injizierbar für Tests (keine echten Wartezeiten).
var (
	webhookNow   = time.Now
	webhookSleep = func(ctx context.Context, d time.Duration) error {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// webhookJitter streut eine Backoff-Wartezeit um +-25 %.
	webhookJitter = func(d time.Duration) time.Duration {
		return time.Duration(float64(d) * (0.75 + rand.Float64()*0.5))
	}
	webhookLimiter = newWebhookRateLimiter(webhookLimiterRate, webhookLimiterBurst, webhookLimiterMaxEntries, webhookLimiterTTL)
)

// webhookKey ist der Schlüssel des Limiters: ein Hash, damit die URL (mit
// key/token) nicht im Speicher des Limiters liegt.
func webhookKey(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:])
}

// webhookURLID ist die Kurzkennung fürs Log (8 Zeichen SHA-256). Gibt nichts
// preis, das zur URL zurückführt.
func webhookURLID(rawURL string) string { return webhookKey(rawURL)[:8] }

// webhookRateLimiter ist ein prozesslokaler Token Bucket je Webhook-URL mit
// begrenztem Speicher (TTL + Obergrenze der Einträge).
type webhookRateLimiter struct {
	mu         sync.Mutex
	rate       float64 // Tokens pro Sekunde
	burst      float64
	maxEntries int
	ttl        time.Duration
	buckets    map[string]*webhookBucket
}

type webhookBucket struct {
	tokens float64
	last   time.Time
}

func newWebhookRateLimiter(rate, burst float64, maxEntries int, ttl time.Duration) *webhookRateLimiter {
	return &webhookRateLimiter{rate: rate, burst: burst, maxEntries: maxEntries, ttl: ttl, buckets: map[string]*webhookBucket{}}
}

// reserve reserviert einen Token für key. Muss länger als maxWait gewartet
// werden, wird nichts reserviert (ok=false, wait = nötige Wartezeit).
// Sonst ok=true und wait = bis zum Senden zu wartende Zeit (0 = sofort).
func (l *webhookRateLimiter) reserve(key string, now time.Time, maxWait time.Duration) (ok bool, wait time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, found := l.buckets[key]
	if !found {
		l.evictLocked(now)
		b = &webhookBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = math.Min(l.burst, b.tokens+elapsed*l.rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait = time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	if wait > maxWait {
		return false, wait
	}
	b.tokens--
	return true, wait
}

// evictLocked räumt vor dem Anlegen eines Eintrags auf: abgelaufene (länger
// als ttl unbenutzt) fliegen raus; ist die Obergrenze trotzdem erreicht, der
// älteste.
func (l *webhookRateLimiter) evictLocked(now time.Time) {
	if len(l.buckets) < l.maxEntries {
		return
	}
	var oldestKey string
	var oldest time.Time
	for k, b := range l.buckets {
		if now.Sub(b.last) > l.ttl {
			delete(l.buckets, k)
			continue
		}
		if oldestKey == "" || b.last.Before(oldest) {
			oldestKey, oldest = k, b.last
		}
	}
	if len(l.buckets) >= l.maxEntries && oldestKey != "" {
		delete(l.buckets, oldestKey)
	}
}

// parseRetryAfter liest den Retry-After-Header (Sekunden oder HTTP-Datum).
// ok=false, wenn er fehlt oder unlesbar ist.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs < 0 {
			return 0, true
		}
		if secs > 24*3600 {
			secs = 24 * 3600
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}

// classifyWebhookResponse ordnet Status und Retry-After einer Fehlerklasse zu.
func classifyWebhookResponse(status int, retryAfter string, now time.Time) WebhookResult {
	res := WebhookResult{Status: status, Class: "http_" + strconv.Itoa(status)}
	switch {
	case status >= 200 && status < 300:
		res.Kind, res.Class = webhookOK, "ok"
	case status == http.StatusTooManyRequests:
		res.Kind = webhookRateLimited
		res.RetryAfter, _ = parseRetryAfter(retryAfter, now)
	case status >= 500:
		res.Kind = webhookTransient
		res.RetryAfter, _ = parseRetryAfter(retryAfter, now)
	default:
		res.Kind = webhookPermanent
	}
	return res
}

// classifyWebhookError ordnet einen Transportfehler ein. Der Fehlertext
// enthält die URL und wird deshalb nie weitergegeben.
func classifyWebhookError(err error) WebhookResult {
	class := "network"
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		class = "timeout"
	}
	return WebhookResult{Kind: webhookTransient, Class: class}
}

func postWebhookOnce(ctx context.Context, rawURL string, body []byte) WebhookResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return WebhookResult{Kind: webhookPermanent, Class: "invalid_url"}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := webhookTestClient.Do(req)
	if err != nil {
		return classifyWebhookError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Response-Größe begrenzen, siehe TestWebhookHandler.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, webhookMaxBodyDrain))
	return classifyWebhookResponse(resp.StatusCode, resp.Header.Get("Retry-After"), webhookNow())
}

// postWebhook sendet payload als JSON an rawURL und liefert das typisierte
// Ergebnis. Im Modus webhookModeNotify wartet und wiederholt es bei 429/5xx/
// Netzfehlern (max. webhookMaxAttempts Versuche, max. webhookMaxTotalWait
// Wartezeit), bei Permanent nie. Im Modus webhookModeProbe gibt es genau
// einen Versuch ohne Warten. Geloggt werden nur Statuscode, Fehlerklasse,
// Versuch, Retry-After und eine Kurzkennung der URL - nie die URL selbst.
func postWebhook(ctx context.Context, rawURL string, payload any, mode webhookMode) WebhookResult {
	body, err := json.Marshal(payload)
	if err != nil {
		return WebhookResult{Kind: webhookPermanent, Class: "invalid_payload"}
	}
	id := webhookURLID(rawURL)
	key := webhookKey(rawURL)
	var waited time.Duration
	var last WebhookResult
	// Ohne Abbruchbedingung: nextWebhookDelay beendet spätestens beim letzten Versuch.
	for attempt := 1; ; attempt++ {
		maxWait := webhookMaxTotalWait - waited
		if mode == webhookModeProbe {
			maxWait = 0
		}
		ok, wait := webhookLimiter.reserve(key, webhookNow(), maxWait)
		if !ok {
			slog.Warn(fmt.Sprintf("webhook rate limited, retry in %ds", int(math.Ceil(wait.Seconds()))),
				"source", "local_limiter", "attempt", attempt, "url_id", id)
			if attempt > 1 {
				// Nach einem gesendeten Versuch gilt dessen Ergebnis, nicht der lokale Stau.
				return last
			}
			return WebhookResult{Kind: webhookRateLimited, RetryAfter: wait, Class: "local_limit"}
		}
		if wait > 0 {
			if webhookSleep(ctx, wait) != nil {
				return abortedWebhook(last, attempt-1)
			}
			waited += wait
		}
		last = postWebhookOnce(ctx, rawURL, body)
		last.Attempts = attempt
		if last.Kind == webhookOK {
			return last
		}
		delay, retry := nextWebhookDelay(last, attempt, mode, waited)
		logWebhookFailure(last, id, attempt, delay, retry)
		if !retry {
			return last
		}
		if webhookSleep(ctx, delay) != nil {
			return last
		}
		waited += delay
	}
}

func abortedWebhook(last WebhookResult, attempts int) WebhookResult {
	if attempts == 0 {
		return WebhookResult{Kind: webhookTransient, Class: "canceled"}
	}
	return last
}

// nextWebhookDelay entscheidet, ob und nach welcher Wartezeit wiederholt wird.
func nextWebhookDelay(res WebhookResult, attempt int, mode webhookMode, waited time.Duration) (time.Duration, bool) {
	if mode == webhookModeProbe || res.Kind == webhookPermanent || attempt >= webhookMaxAttempts {
		return 0, false
	}
	delay := res.RetryAfter
	if delay <= 0 {
		delay = webhookJitter(webhookBackoffBase << (attempt - 1))
	}
	if waited+delay > webhookMaxTotalWait {
		return delay, false
	}
	return delay, true
}

func logWebhookFailure(res WebhookResult, id string, attempt int, delay time.Duration, retry bool) {
	attrs := []any{"status", res.Status, "class", res.Class, "attempt", attempt, "url_id", id}
	if res.Kind == webhookRateLimited {
		if !retry {
			attrs = append(attrs, "retry_after_s", int(math.Ceil(res.RetryAfter.Seconds())))
			slog.Warn("webhook rate limited, giving up", attrs...)
			return
		}
		slog.Warn(fmt.Sprintf("webhook rate limited, retry in %ds", int(math.Ceil(delay.Seconds()))), attrs...)
		return
	}
	if retry {
		slog.Warn(fmt.Sprintf("webhook %s, retry in %ds", res.Kind, int(math.Ceil(delay.Seconds()))), attrs...)
		return
	}
	slog.Warn("webhook failed", attrs...)
}

// reportWebhookProblem legt für die Admins eine Nachricht in der Glocke an
// bei Ratenlimit (429 bzw. lokaler Limiter) je Seite und Kanal,
// bei dauerhafter Ablehnung (URL/Key falsch) je Abonnent. Übergeben werden nur
// Kanal, Sekunden und Abonnenten-ID, nie die URL. Blockiert den Aufrufer nicht
// und ist ohne Datenbank (Tests ohne DB) ein No-op.
func (h *Handler) reportWebhookProblem(pageID, subscriberID uint, channel string, res WebhookResult) {
	if h == nil || h.DB == nil || res.OK() || pageID == 0 {
		return
	}
	switch res.Kind {
	case webhookRateLimited:
		h.NotifyWebhookRateLimited(pageID, channel, res.RetryAfterSeconds())
	case webhookPermanent:
		h.NotifyWebhookRejected(pageID, channel, subscriberID)
	}
}
