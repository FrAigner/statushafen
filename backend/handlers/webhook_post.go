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

// Central dispatch to Google Chat webhooks. Google Chat responds
// under overload with 429 (possibly Retry-After) or 503 and allows only
// about one message per second per space. All four callers (test endpoint,
// subscriber webhook, global webhook, external incidents) therefore go
// through postWebhook instead of each copying its own post to webhookTestClient.
// SSRF protection stays unchanged, because sending still happens
// via webhookTestClient (safeDialContext).

// webhookKind is the error class of a webhook delivery.
type webhookKind int

const (
	// webhookOK: 2xx.
	webhookOK webhookKind = iota
	// webhookRateLimited: HTTP 429 (or the local limiter is exhausted).
	webhookRateLimited
	// webhookTransient: 5xx, network error, timeout - a later attempt may succeed.
	webhookTransient
	// webhookPermanent: 4xx except 429 (and 3xx) - the URL or key is wrong.
	webhookPermanent
)

// String returns the error class for the log.
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

// webhookMode controls whether postWebhook may wait and retry.
type webhookMode int

const (
	// webhookModeNotify: background notification, may wait and retry within the
	// upper limits.
	webhookModeNotify webhookMode = iota
	// webhookModeProbe: test endpoint, exactly one attempt, never waits.
	webhookModeProbe
)

// WebhookResult is the typed result of postWebhook. It never contains
// the URL.
type WebhookResult struct {
	Kind webhookKind
	// Status is the HTTP status of the target (0 on network error/timeout/local throttling).
	Status int
	// RetryAfter: for webhookRateLimited, the wait time according to the target (or the local limiter).
	RetryAfter time.Duration
	// Attempts: number of attempts actually sent.
	Attempts int
	// Class: short name of the failure cause for the log ("timeout", "network", "http_429" ...).
	Class string
}

// OK reports whether the delivery succeeded.
func (r WebhookResult) OK() bool { return r.Kind == webhookOK }

// Err returns a URL-free error (for audit texts) or nil on success.
func (r WebhookResult) Err() error {
	if r.OK() {
		return nil
	}
	if r.Status != 0 {
		return fmt.Errorf("webhook status %d", r.Status)
	}
	return fmt.Errorf("webhook %s", r.Class)
}

// RetryAfterSeconds rounds to whole seconds (at least 1).
func (r WebhookResult) RetryAfterSeconds() int {
	s := int(math.Ceil(r.RetryAfter.Seconds()))
	if s < 1 {
		return 1
	}
	return s
}

const (
	webhookMaxAttempts = 3
	// webhookMaxTotalWait: upper limit for the sum of all wait times (limiter
	// and retry) of one notification.
	webhookMaxTotalWait = 30 * time.Second
	webhookBackoffBase  = time.Second
	// Limiter per URL: 1 message/second, burst 2 (Google: approx. 1/s per space).
	webhookLimiterRate  = 1.0
	webhookLimiterBurst = 2.0
	// Memory bound of the limiter.
	webhookLimiterMaxEntries = 1024
	webhookLimiterTTL        = 10 * time.Minute
	webhookMaxBodyDrain      = 1 << 20
)

// Injectable for tests (no real wait times).
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
	// webhookJitter spreads a backoff wait time by +-25 %.
	webhookJitter = func(d time.Duration) time.Duration {
		return time.Duration(float64(d) * (0.75 + rand.Float64()*0.5))
	}
	webhookLimiter = newWebhookRateLimiter(webhookLimiterRate, webhookLimiterBurst, webhookLimiterMaxEntries, webhookLimiterTTL)
)

// webhookKey is the limiter key: a hash, so that the URL (with
// key/token) is not held in the limiter's memory.
func webhookKey(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(sum[:])
}

// webhookURLID is the short identifier for the log (8 characters of SHA-256). Reveals nothing
// that leads back to the URL.
func webhookURLID(rawURL string) string { return webhookKey(rawURL)[:8] }

// webhookRateLimiter is a process-local token bucket per webhook URL with
// bounded memory (TTL + upper limit on entries).
type webhookRateLimiter struct {
	mu         sync.Mutex
	rate       float64 // tokens per second
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

// reserve reserves a token for key. If it would have to wait longer than maxWait,
// nothing is reserved (ok=false, wait = required wait time).
// Otherwise ok=true and wait = time to wait before sending (0 = immediately).
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

// evictLocked cleans up before an entry is created: expired entries (unused for longer
// than ttl) are dropped; if the upper limit is still reached, the
// oldest one.
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

// parseRetryAfter reads the Retry-After header (seconds or HTTP date).
// ok=false if it is missing or unreadable.
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

// classifyWebhookResponse maps status and Retry-After to an error class.
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

// classifyWebhookError classifies a transport error. The error text
// contains the URL and is therefore never passed on.
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
	// Limit the response size, see TestWebhookHandler.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, webhookMaxBodyDrain))
	return classifyWebhookResponse(resp.StatusCode, resp.Header.Get("Retry-After"), webhookNow())
}

// postWebhook sends payload as JSON to rawURL and returns the typed
// result. In webhookModeNotify mode it waits and retries on 429/5xx/
// network errors (max. webhookMaxAttempts attempts, max. webhookMaxTotalWait
// wait time), never on Permanent. In webhookModeProbe mode there is exactly
// one attempt without waiting. Only status code, error class,
// attempt, Retry-After and a short URL identifier are logged - never the URL itself.
func postWebhook(ctx context.Context, rawURL string, payload any, mode webhookMode) WebhookResult {
	body, err := json.Marshal(payload)
	if err != nil {
		return WebhookResult{Kind: webhookPermanent, Class: "invalid_payload"}
	}
	id := webhookURLID(rawURL)
	key := webhookKey(rawURL)
	var waited time.Duration
	var last WebhookResult
	// No stop condition: nextWebhookDelay ends it at the latest on the last attempt.
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
				// After a sent attempt, its result applies, not the local backlog.
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

// nextWebhookDelay decides whether to retry and after what wait time.
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

// reportWebhookProblem creates a message for the admins in the bell
// on rate limiting (429 or local limiter) per page and channel,
// on permanent rejection (wrong URL/key) per subscriber. Only
// channel, seconds and subscriber ID are passed, never the URL. Does not block the caller
// and is a no-op without a database (tests without DB).
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
