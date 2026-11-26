package handlers

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Requests that trigger notifications to subscribers (new incident,
// maintenance, notice, update, changed subscription address) need a limit per page
// and per user, not just per IP: a logged-in user with write permission
// could otherwise trigger thousands of mails to all subscribers and the
// page's members in a short time. The limits are generous and do not disturb
// normal operation (even a long incident with many updates).
const (
	// Newly created incidents, maintenances and notices per hour.
	notifyCreatePerPagePerHour = 30
	notifyCreatePerUserPerHour = 60
	// Changes and updates that also notify, per hour.
	notifyChangePerPagePerHour = 120
	notifyChangePerUserPerHour = 120

	// notifyLimitSweepEvery: after this many calls, expired
	// keys are removed from memory so the map does not grow.
	notifyLimitSweepEvery = 500

	// Domain checks (server-side DNS lookup) per minute.
	customDomainVerifyPerMinute = 10
)

// NotificationCreateLimit limits the creation of notifying records.
func NotificationCreateLimit() gin.HandlerFunc {
	return NewNotificationRateLimiter(notifyCreatePerPagePerHour, notifyCreatePerUserPerHour, time.Hour)
}

// NotificationChangeLimit limits changes and updates that notify.
func NotificationChangeLimit() gin.HandlerFunc {
	return NewNotificationRateLimiter(notifyChangePerPagePerHour, notifyChangePerUserPerHour, time.Hour)
}

// CustomDomainVerifyLimit limits domain verification per page and per user.
// Every call triggers a DNS lookup by the server; without a limit the
// server could be exploited as a tool for any number of lookups
// on self-chosen hostnames.
func CustomDomainVerifyLimit() gin.HandlerFunc {
	return NewNotificationRateLimiter(customDomainVerifyPerMinute, customDomainVerifyPerMinute, time.Minute)
}

// notifyLimitNow is the limiter's clock; tests set it.
var notifyLimitNow = time.Now

type notifyLimiter struct {
	mu      sync.Mutex
	perPage int
	perUser int
	window  time.Duration
	hits    map[string][]time.Time
	calls   int
}

func newNotifyLimiter(perPage, perUser int, window time.Duration) *notifyLimiter {
	return &notifyLimiter{perPage: perPage, perUser: perUser, window: window, hits: map[string][]time.Time{}}
}

// live returns the key's timestamps within the window.
func (l *notifyLimiter) live(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	hits := l.hits[key]
	i := 0
	for i < len(hits) && !hits[i].After(cutoff) {
		i++
	}
	if i > 0 {
		hits = hits[i:]
		if len(hits) == 0 {
			delete(l.hits, key)
		} else {
			l.hits[key] = hits
		}
	}
	return hits
}

// allow counts a call for page and user (empty key: not
// limited). If one of the limits is reached, nothing is counted and the
// wait time until the next free slot is returned.
func (l *notifyLimiter) allow(pageKey, userKey string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := notifyLimitNow()
	l.calls++
	if l.calls%notifyLimitSweepEvery == 0 {
		for k := range l.hits {
			l.live(k, now)
		}
	}

	type bucket struct {
		key string
		max int
	}
	var buckets []bucket
	if pageKey != "" {
		buckets = append(buckets, bucket{"p:" + pageKey, l.perPage})
	}
	if userKey != "" {
		buckets = append(buckets, bucket{"u:" + userKey, l.perUser})
	}
	var wait time.Duration
	for _, b := range buckets {
		hits := l.live(b.key, now)
		if len(hits) >= b.max {
			// A slot becomes free when the (len-max+1)-oldest call leaves the window.
			free := hits[len(hits)-b.max].Add(l.window).Sub(now)
			wait = max(wait, free)
		}
	}
	if wait > 0 {
		return false, wait
	}
	for _, b := range buckets {
		l.hits[b.key] = append(l.hits[b.key], now)
	}
	return true, 0
}

// NewNotificationRateLimiter allows each page (path parameter page_id) and each
// logged-in user at most perPage and perUser calls, respectively, within the time window
// window. Beyond that it answers with 429 rate_limited and Retry-After. It belongs
// behind authentication and the permission check, so that only real pages and users
// occur as keys.
func NewNotificationRateLimiter(perPage, perUser int, window time.Duration) gin.HandlerFunc {
	l := newNotifyLimiter(perPage, perUser, window)
	return func(c *gin.Context) {
		userKey := ""
		if uid := getUserID(c); uid != 0 {
			userKey = strconv.FormatUint(uint64(uid), 10)
		}
		ok, wait := l.allow(c.Param("page_id"), userKey)
		if ok {
			c.Next()
			return
		}
		secs := int((wait + time.Second - 1) / time.Second)
		c.Header("Retry-After", strconv.Itoa(max(secs, 1)))
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Zu viele Anfragen. Bitte später erneut versuchen.", "code": "rate_limited"})
		c.Abort()
	}
}
