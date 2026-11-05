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

// notifyLimitRouter hängt den Begrenzer an eine Route mit :page_id; der
// Nutzer kommt aus dem Header X-Test-User (wie ihn die Anmeldung setzen würde).
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

// notifyLimitRouterWith hängt einen fertigen Begrenzer an die Route.
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
	// Verschiedene Nutzer, dieselbe Seite: Die Grenze gilt je Seite.
	for i := 1; i <= 3; i++ {
		require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/7/x", i).Code, "Aufruf %d", i)
	}
	w := callNotifyLimit(r, "/pages/7/x", 4)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.JSONEq(t, `{"error":"Zu viele Anfragen. Bitte später erneut versuchen.","code":"rate_limited"}`, w.Body.String())
	assert.Equal(t, "3600", w.Header().Get("Retry-After"))

	// Gegenprobe: eine andere Seite ist nicht betroffen.
	assert.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/8/x", 4).Code)

	// Nach Ablauf des Fensters geht es wieder; Retry-After zählt herunter.
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
	// Gegenprobe: ein anderer Nutzer ist unabhängig.
	assert.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/3/x", 6).Code)
}

func TestNotificationRateLimiter_AbgewieseneAufrufeZaehlenNichtWeiter(t *testing.T) {
	now := fakeNotifyClock(t)
	r := notifyLimitRouter(1, 100)
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 1).Code)
	for i := 0; i < 50; i++ {
		require.Equal(t, http.StatusTooManyRequests, callNotifyLimit(r, "/pages/1/x", 1).Code)
	}
	// Das Fenster endet eine Stunde nach dem einen gezählten Aufruf, nicht nach den abgewiesenen.
	*now = now.Add(time.Hour + time.Second)
	assert.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 1).Code)
}

func TestNotificationRateLimiter_RouteOhneSeiteUndOhneNutzer(t *testing.T) {
	fakeNotifyClock(t)
	r := notifyLimitRouter(1, 1)
	// ohne :page_id zählt nur der Nutzer
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/ohne-seite", 9).Code)
	assert.Equal(t, http.StatusTooManyRequests, callNotifyLimit(r, "/ohne-seite", 9).Code)
	// ohne Nutzer (Header ungültig) zählt nur die Seite
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

// Nur ein Teil der Aufrufe ist aus dem Fenster gefallen: Die übrigen zählen weiter.
func TestNotificationRateLimiter_TeilweiseAbgelaufenesFenster(t *testing.T) {
	now := fakeNotifyClock(t)
	r := notifyLimitRouter(2, 100)
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 1).Code) // t0
	*now = now.Add(40 * time.Minute)
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 1).Code) // t0+40
	require.Equal(t, http.StatusTooManyRequests, callNotifyLimit(r, "/pages/1/x", 1).Code)

	*now = now.Add(30 * time.Minute) // t0+70: der erste Aufruf ist raus, der zweite zählt noch
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 1).Code)
	w := callNotifyLimit(r, "/pages/1/x", 1)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "1800", w.Header().Get("Retry-After"), "der Aufruf bei t0+40 verlässt das Fenster bei t0+100, jetzt ist t0+70")
}

// Die fest verdrahteten Grenzen: Anlegen 30 je Seite und Stunde, Ändern 120.
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

// Die Grenzen je Nutzer gelten seitenübergreifend: Wer auf mehreren Seiten
// Schreibrecht hat, kann die Grenze je Seite nicht durch Verteilen umgehen.
func TestNotificationLimits_FesteGrenzenJeNutzer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, tc := range map[string]struct {
		limit gin.HandlerFunc
		max   int // Aufrufe je Nutzer und Stunde
	}{
		"Anlegen": {NotificationCreateLimit(), 60},
		"Ändern":  {NotificationChangeLimit(), 120},
	} {
		t.Run(name, func(t *testing.T) {
			now := fakeNotifyClock(t)
			r := notifyLimitRouterWith(tc.limit)
			// Seiten 1..n nehmen je höchstens 30 Aufrufe (Anlegen) bzw. 60 (Ändern) auf,
			// damit nur die Grenze je Nutzer greift.
			perPage := 20
			for i := 0; i < tc.max; i++ {
				path := "/pages/" + strconv.Itoa(i/perPage+1) + "/x"
				require.Equal(t, http.StatusNoContent, callNotifyLimit(r, path, 7).Code, "Aufruf %d", i+1)
			}
			assert.Equal(t, http.StatusTooManyRequests, callNotifyLimit(r, "/pages/99/x", 7).Code, "Aufruf über der Grenze je Nutzer")
			assert.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/99/x", 8).Code, "ein anderer Nutzer ist frei")

			// Das Fenster ist eine Stunde: davor bleibt die Sperre, danach ist sie weg.
			*now = now.Add(time.Hour - time.Minute)
			assert.Equal(t, http.StatusTooManyRequests, callNotifyLimit(r, "/pages/99/x", 7).Code)
			*now = now.Add(2 * time.Minute)
			assert.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/99/x", 7).Code)
		})
	}
}

// Je Seite gilt ebenfalls ein Fenster von einer Stunde (Anlegen wie Ändern).
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
			for i := 0; i < tc.max; i++ { // jeder Aufruf von einem anderen Nutzer: nur die Seite zählt
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

// Gedrosselte Anfragen erreichen den Handler nicht: Er löst die Mails aus.
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

// Sind Seite und Nutzer voll, gilt die längere Wartezeit.
func TestNotificationRateLimiter_LaengsteWartezeitGewinnt(t *testing.T) {
	now := fakeNotifyClock(t)
	r := notifyLimitRouter(1, 1)
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/1/x", 1).Code) // t0: Seite 1 und Nutzer 1
	*now = now.Add(30 * time.Minute)
	require.Equal(t, http.StatusNoContent, callNotifyLimit(r, "/pages/2/x", 2).Code) // t0+30: Seite 2 und Nutzer 2
	*now = now.Add(10 * time.Minute)
	// t0+40: Nutzer 1 wird bei t0+60 frei (20 min), Seite 2 bei t0+90 (50 min).
	w := callNotifyLimit(r, "/pages/2/x", 1)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "3000", w.Header().Get("Retry-After"))
	// Umgekehrt: Seite 1 wird bei t0+60 frei, Nutzer 2 bei t0+90.
	w = callNotifyLimit(r, "/pages/1/x", 2)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "3000", w.Header().Get("Retry-After"))
}

// Die Domain-Prüfung ist auf zehn Aufrufe je Minute, Seite und Nutzer begrenzt.
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
