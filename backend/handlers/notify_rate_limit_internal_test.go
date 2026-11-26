package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// notifyLimitRouter attaches the limiter to a route with :page_id; the
// user comes from the header X-Test-User (as authentication would set it).
func notifyLimitRouter(perPage, perUser int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if id, err := strconv.Atoi(c.GetHeader("X-Test-User")); err == nil {
			c.Set("userID", uint(id))
		}
		c.Next()
	})
	r.POST("/pages/:page_id/x", NewNotificationRateLimiter(perPage, perUser, time.Hour), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.POST("/ohne-seite", NewNotificationRateLimiter(perPage, perUser, time.Hour), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return r
}

// notifyLimitRouterWith attaches a finished limiter to the route.
func notifyLimitRouterWith(limit gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if id, err := strconv.Atoi(c.GetHeader("X-Test-User")); err == nil {
			c.Set("userID", uint(id))
		}
		c.Next()
	})
	r.POST("/pages/:page_id/x", limit, func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return r
}

func callNotifyLimit(r http.Handler, path string, user int) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("X-Test-User", strconv.Itoa(user))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func fakeNotifyClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	prev := notifyLimitNow
	notifyLimitNow = func() time.Time { return now }
	t.Cleanup(func() { notifyLimitNow = prev })
	return &now
}

func TestNotificationRateLimiter_LimitJeSeite(t *testing.T) {
	now := fakeNotifyClock(t)
	r := notifyLimitRouter(3, 100)
	// Different users, same page: the limit applies per page.
	for i := 1; i <= 3; i++ {
		require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/7/x", i).Code, "Aufruf %d", i)
	}
	w := callNotifyLimit(r, "/pages/7/x", 4)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.JSONEq(t, `{"error":"Zu viele Anfragen. Bitte später erneut versuchen.","code":"rate_limited"}`, w.Body.String())
	assert.Equal(t, "3600", w.Header().Get("Retry-After"))

	// Control check: a different page is not affected.
	assert.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/8/x", 4).Code)

	// After the window expires it works again; Retry-After counts down.
	*now = now.Add(40 * time.Minute)
	w = callNotifyLimit(r, "/pages/7/x", 4)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "1200", w.Header().Get("Retry-After"))
	*now = now.Add(20*time.Minute + time.Second)
	assert.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/7/x", 4).Code)
}

func TestNotificationRateLimiter_LimitJeNutzerSeitenuebergreifend(t *testing.T) {
	fakeNotifyClock(t)
	r := notifyLimitRouter(100, 2)
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 5).Code)
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/2/x", 5).Code)
	assert.Equal(t, http.StatusTooManyRequests, callNotifyLimit(r, "/pages/3/x", 5).Code, "dritter Aufruf desselben Nutzers auf anderer Seite")
	// Control check: a different user is independent.
	assert.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/3/x", 6).Code)
}

func TestNotificationRateLimiter_AbgewieseneAufrufeZaehlenNichtWeiter(t *testing.T) {
	now := fakeNotifyClock(t)
	r := notifyLimitRouter(1, 100)
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 1).Code)
	for i := 0; i < 50; i++ {
		require.Equal(t, http.StatusTooManyRequests, callNotifyLimit(r, "/pages/1/x", 1).Code)
	}
	// The window ends one hour after the one counted call, not after the rejected ones.
	*now = now.Add(time.Hour + time.Second)
	assert.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 1).Code)
}

func TestNotificationRateLimiter_RouteOhneSeiteUndOhneNutzer(t *testing.T) {
	fakeNotifyClock(t)
	r := notifyLimitRouter(1, 1)
	// without :page_id only the user counts
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/ohne-seite", 9).Code)
	assert.Equal(t, http.StatusTooManyRequests, callNotifyLimit(r, "/ohne-seite", 9).Code)
	// without a user (invalid header) only the page counts
	req := httptest.NewRequest(http.MethodPost, "/pages/55/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/pages/55/x", nil))
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestNotificationRateLimiter_RaeumtAbgelaufeneEintraegeAuf(t *testing.T) {
	now := fakeNotifyClock(t)
	rl := newNotifyLimiter(5, 5, time.Hour)
	for i := 0; i < notifyLimitSweepEvery+1; i++ {
		rl.allow("page-"+strconv.Itoa(i), "")
	}
	require.Greater(t, len(rl.hits), notifyLimitSweepEvery)
	*now = now.Add(2 * time.Hour)
	for i := 0; i < notifyLimitSweepEvery; i++ {
		rl.allow("neu", "")
	}
	assert.LessOrEqual(t, len(rl.hits), 2, "abgelaufene Schlüssel werden entfernt")
}

// Only some of the calls have fallen out of the window: the rest still count.
func TestNotificationRateLimiter_TeilweiseAbgelaufenesFenster(t *testing.T) {
	now := fakeNotifyClock(t)
	r := notifyLimitRouter(2, 100)
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 1).Code) // t0
	*now = now.Add(40 * time.Minute)
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 1).Code) // t0+40
	require.Equal(t, http.StatusTooManyRequests, callNotifyLimit(r, "/pages/1/x", 1).Code)

	*now = now.Add(30 * time.Minute) // t0+70: the first call is out, the second still counts
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 1).Code)
	w := callNotifyLimit(r, "/pages/1/x", 1)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "1800", w.Header().Get("Retry-After"), "der Aufruf bei t0+40 verlässt das Fenster bei t0+100, jetzt ist t0+70")
}

// The hard-wired limits: create 30 per page and hour, change 120.
func TestNotificationLimits_FesteGrenzen(t *testing.T) {
	fakeNotifyClock(t)
	gin.SetMode(gin.TestMode)
	for name, tc := range map[string]struct {
		limit gin.HandlerFunc
		max   int
	}{
		"Anlegen": {NotificationCreateLimit(), notifyCreatePerPagePerHour},
		"Ändern":  {NotificationChangeLimit(), notifyChangePerPagePerHour},
	} {
		t.Run(name, func(t *testing.T) {
			r := gin.New()
			r.POST("/pages/:page_id/x", tc.limit, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			for i := 0; i < tc.max; i++ {
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/pages/3/x", nil))
				require.Equal(t, http.StatusNoContent, w.Code, "Aufruf %d", i+1)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/pages/3/x", nil))
			assert.Equal(t, http.StatusTooManyRequests, w.Code)
		})
	}
	assert.Equal(t, 30, notifyCreatePerPagePerHour)
	assert.Equal(t, 120, notifyChangePerPagePerHour)
}

// The per-user limits apply across pages: someone with
// write permission on several pages cannot get around the per-page limit by spreading calls.
func TestNotificationLimits_FesteGrenzenJeNutzer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, tc := range map[string]struct {
		limit gin.HandlerFunc
		max   int // calls per user and hour
	}{
		"Anlegen": {NotificationCreateLimit(), 60},
		"Ändern":  {NotificationChangeLimit(), 120},
	} {
		t.Run(name, func(t *testing.T) {
			now := fakeNotifyClock(t)
			r := notifyLimitRouterWith(tc.limit)
			// Pages 1..n each take at most 30 calls (create) or 60 (change),
			// so that only the per-user limit applies.
			perPage := 20
			for i := 0; i < tc.max; i++ {
				path := "/pages/" + strconv.Itoa(i/perPage+1) + "/x"
				require.Equal(t, http.StatusNoContent, callNotifyLimit(r, path, 7).Code, "Aufruf %d", i+1)
			}
			assert.Equal(t, http.StatusTooManyRequests, callNotifyLimit(r, "/pages/99/x", 7).Code, "Aufruf über der Grenze je Nutzer")
			assert.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/99/x", 8).Code, "ein anderer Nutzer ist frei")

			// The window is one hour: before that the block remains, afterwards it is gone.
			*now = now.Add(time.Hour - time.Minute)
			assert.Equal(t, http.StatusTooManyRequests, callNotifyLimit(r, "/pages/99/x", 7).Code)
			*now = now.Add(2 * time.Minute)
			assert.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/99/x", 7).Code)
		})
	}
}

// A window of one hour also applies per page (create as well as change).
func TestNotificationLimits_FensterJeSeiteEineStunde(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, tc := range map[string]struct {
		limit gin.HandlerFunc
		max   int
	}{
		"Anlegen": {NotificationCreateLimit(), notifyCreatePerPagePerHour},
		"Ändern":  {NotificationChangeLimit(), notifyChangePerPagePerHour},
	} {
		t.Run(name, func(t *testing.T) {
			now := fakeNotifyClock(t)
			r := notifyLimitRouterWith(tc.limit)
			for i := 0; i < tc.max; i++ { // each call from a different user: only the page counts
				require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/5/x", 1000+i).Code)
			}
			require.Equal(t, http.StatusTooManyRequests, callNotifyLimit(r, "/pages/5/x", 5000).Code)
			*now = now.Add(time.Hour - time.Minute)
			require.Equal(t, http.StatusTooManyRequests, callNotifyLimit(r, "/pages/5/x", 5000).Code)
			*now = now.Add(2 * time.Minute)
			assert.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/5/x", 5000).Code)
		})
	}
}

// Throttled requests do not reach the handler: it triggers the mails.
func TestNotificationRateLimiter_GedrosselterAufrufErreichtHandlerNicht(t *testing.T) {
	fakeNotifyClock(t)
	gin.SetMode(gin.TestMode)
	calls := 0
	r := gin.New()
	r.POST("/pages/:page_id/x", NewNotificationRateLimiter(1, 100, time.Hour), func(c *gin.Context) {
		calls++
		c.Status(http.StatusNoContent)
	})
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/pages/1/x", nil))
	}
	assert.Equal(t, 1, calls, "der Handler läuft nur für den zugelassenen Aufruf")
}

// If page and user are both full, the longer wait time applies.
func TestNotificationRateLimiter_LaengsteWartezeitGewinnt(t *testing.T) {
	now := fakeNotifyClock(t)
	r := notifyLimitRouter(1, 1)
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 1).Code) // t0: page 1 and user 1
	*now = now.Add(30 * time.Minute)
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/2/x", 2).Code) // t0+30: page 2 and user 2
	*now = now.Add(10 * time.Minute)
	// t0+40: user 1 frees up at t0+60 (20 min), page 2 at t0+90 (50 min).
	w := callNotifyLimit(r, "/pages/2/x", 1)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "3000", w.Header().Get("Retry-After"))
	// Conversely: page 1 frees up at t0+60, user 2 at t0+90.
	w = callNotifyLimit(r, "/pages/1/x", 2)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "3000", w.Header().Get("Retry-After"))
}

// Domain verification is limited to ten calls per minute, page and user.
func TestCustomDomainVerifyLimit_TenPerMinutePerPageAndUser(t *testing.T) {
	clock := fakeNotifyClock(t)
	r := notifyLimitRouterWith(CustomDomainVerifyLimit())

	for i := 1; i <= customDomainVerifyPerMinute; i++ {
		require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 7).Code, "Aufruf %d", i)
	}
	w := callNotifyLimit(r, "/pages/1/x", 7)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"rate_limited"`)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))

	*clock = clock.Add(time.Minute + time.Second)
	assert.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 7).Code, "nach der Minute wieder frei")
}
