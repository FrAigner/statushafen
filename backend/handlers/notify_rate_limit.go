package handlers

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Anfragen, die Benachrichtigungen an Abonnenten auslösen (neuer Vorfall,
// Wartung, Meldung, Update, geänderte Abo-Adresse), brauchen ein Limit je Seite
// und je Nutzer, nicht nur je IP: Ein angemeldeter Nutzer mit Schreibrecht
// konnte sonst in kurzer Zeit tausende Mails an alle Abonnenten und die
// Mitglieder der Seite auslösen. Die Grenzen sind großzügig und stören den
// normalen Betrieb nicht (auch ein langer Vorfall mit vielen Updates).
const (
	// Neu angelegte Vorfälle, Wartungen und Meldungen je Stunde.
	notifyCreatePerPagePerHour = 30
	notifyCreatePerUserPerHour = 60
	// Änderungen und Updates, die ebenfalls benachrichtigen, je Stunde.
	notifyChangePerPagePerHour = 120
	notifyChangePerUserPerHour = 120

	// notifyLimitSweepEvery: nach so vielen Aufrufen werden abgelaufene
	// Schlüssel aus dem Speicher entfernt, damit die Map nicht wächst.
	notifyLimitSweepEvery = 500

	// Domain-Prüfungen (DNS-Abfrage des Servers) je Minute.
	customDomainVerifyPerMinute = 10
)

// NotificationCreateLimit begrenzt das Anlegen benachrichtigender Datensätze.
func NotificationCreateLimit() gin.HandlerFunc {
	return NewNotificationRateLimiter(notifyCreatePerPagePerHour, notifyCreatePerUserPerHour, time.Hour)
}

// NotificationChangeLimit begrenzt Änderungen und Updates, die benachrichtigen.
func NotificationChangeLimit() gin.HandlerFunc {
	return NewNotificationRateLimiter(notifyChangePerPagePerHour, notifyChangePerUserPerHour, time.Hour)
}

// CustomDomainVerifyLimit begrenzt die Domain-Prüfung je Seite und je Nutzer.
// Jeder Aufruf löst eine DNS-Abfrage des Servers aus; ohne Grenze ließe sich der
// Server als Werkzeug für beliebig viele Abfragen auf selbst gewählte Hostnamen
// nutzen.
func CustomDomainVerifyLimit() gin.HandlerFunc {
	return NewNotificationRateLimiter(customDomainVerifyPerMinute, customDomainVerifyPerMinute, time.Minute)
}

// notifyLimitNow ist die Uhr des Begrenzers; Tests setzen sie.
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

// live liefert die Zeitpunkte des Schlüssels innerhalb des Fensters.
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

// allow zählt einen Aufruf für Seite und Nutzer (leerer Schlüssel: nicht
// begrenzt). Ist eine der Grenzen erreicht, wird nichts gezählt und die
// Wartezeit bis zum nächsten freien Platz geliefert.
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
			// Frei wird ein Platz, wenn der (len-max+1)-älteste Aufruf das Fenster verlässt.
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

// NewNotificationRateLimiter erlaubt je Seite (Pfadparameter page_id) und je
// angemeldetem Nutzer höchstens perPage bzw. perUser Aufrufe im Zeitfenster
// window. Darüber antwortet er mit 429 rate_limited und Retry-After. Er gehört
// hinter die Anmeldung und die Rechteprüfung, damit nur echte Seiten und Nutzer
// als Schlüssel vorkommen.
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
