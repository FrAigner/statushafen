package main

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Structure guard: every GET route readable without login has a limit per
// client address. Which routes are reachable anonymously is determined by the test on the
// router itself: a route that returns neither 401 nor 403 without a token is
// anonymous. A new anonymous read route without a limit is thus noticed.
//
// The list bekannteUnbegrenzteLeseRouten is a ratchet: it may only
// get shorter. If one of these routes gets a limit, it must be removed here
// (the test reports this); new entries are not intended.
//
// /healthz deliberately stays without a limit: orchestrators and load balancers query the
// route every second and often share one address; a limit
// would make the check fail and restart the service unnecessarily. The route
// reads nothing from the database except a ping and returns 15 bytes.
var bekannteUnbegrenzteLeseRouten = map[string]bool{
	"/healthz": true,
}

func TestRouter_EveryAnonymousReadRouteIsRateLimited(t *testing.T) {
	r := newRouter(handlers.NewHandler(testutils.SetupTestDB()))
	const maxRequests = 400 // above every limit of the read routes (at most 120 per minute)

	var anonymous, unlimited []string
	for i, ri := range r.Routes() {
		if ri.Method != http.MethodGet {
			continue
		}
		parts := strings.Split(ri.Path, "/")
		for j, p := range parts {
			if strings.HasPrefix(p, ":") {
				parts[j] = "1"
			}
		}
		path := strings.Join(parts, "/")
		addr := "203.0.113." + strconv.Itoa(i%250+1) + ":4000" // own address per route
		get := func() int {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = addr
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			return w.Code
		}
		if first := get(); first == http.StatusUnauthorized || first == http.StatusForbidden {
			continue // readable only when logged in
		}
		anonymous = append(anonymous, ri.Path)
		limited := false
		for n := 0; n < maxRequests && !limited; n++ {
			limited = get() == http.StatusTooManyRequests
		}
		if !limited {
			unlimited = append(unlimited, ri.Path)
		}
	}
	sort.Strings(anonymous)
	require.GreaterOrEqual(t, len(anonymous), 15, "anonyme Leseroutes nicht gefunden: %v", anonymous)

	for _, p := range unlimited {
		assert.True(t, bekannteUnbegrenzteLeseRouten[p], "anonyme Leseroute ohne Limit: %s (Limit je Adresse ergänzen)", p)
	}
	stillUnlimited := map[string]bool{}
	for _, p := range unlimited {
		stillUnlimited[p] = true
	}
	for p := range bekannteUnbegrenzteLeseRouten {
		assert.True(t, stillUnlimited[p], "%s ist jetzt begrenzt oder nicht mehr anonym: aus bekannteUnbegrenzteLeseRouten streichen", p)
	}
}

// The limits of the previously unlimited routes (120 per minute, the OAuth routes 20):
// the 121st or 21st request is rejected with 429, normal use is not.
func TestRouter_FormerlyUnlimitedReadRoutesHaveTheirLimit(t *testing.T) {
	// Freeze the limiter clock: under -race the requests are slow enough for the token bucket
	// to refill mid-loop, which let the request over the limit through (404 instead of 429).
	frozen := time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC)
	middleware.SetRateLimiterClockForTests(t.Cleanup, func() time.Time { return frozen })
	r := newRouter(handlers.NewHandler(testutils.SetupTestDB()))
	cases := []struct {
		path  string
		limit int
	}{
		{"/api/pages/slug/x", 120},
		{"/api/pages/1/notices", 120},
		{"/api/pages/1/components", 120},
		{"/api/public/pages", 120},
		{"/api/public/ui-overrides", 120},
		{"/api/incidents/1/translations", 120},
		{"/api/components/1/translations", 120},
		{"/api/notices/1/translations", 120},
		{"/api/auth/provider/corp/login", 20},
		{"/api/auth/provider/corp/callback", 20},
		{"/api/public/auth-config", 60},
	}
	for i, tc := range cases {
		addr := "198.51.100." + strconv.Itoa(i+1) + ":5000" // own address per route
		get := func() int {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.RemoteAddr = addr
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			return w.Code
		}
		for n := 1; n <= tc.limit; n++ {
			require.NotEqual(t, http.StatusTooManyRequests, get(), "%s: Anfrage %d von %d gehört zur normalen Nutzung", tc.path, n, tc.limit)
		}
		assert.Equal(t, http.StatusTooManyRequests, get(), "%s: die Anfrage über dem Limit", tc.path)
	}
}
