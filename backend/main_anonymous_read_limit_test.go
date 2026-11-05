package main

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Strukturwächter: Jede ohne Anmeldung lesbare GET-Route hat ein Limit je
// Client-Adresse. Welche Routen anonym erreichbar sind, ermittelt der Test am
// Router selbst: Eine Route, die ohne Token weder 401 noch 403 liefert, ist
// anonym. Eine neue anonyme Leseroute ohne Limit fällt damit auf.
//
// Die Liste bekannteUnbegrenzteLeseRouten ist eine Ratsche: Sie darf nur
// kürzer werden. Wird eine dieser Routen begrenzt, muss sie hier gestrichen
// werden (der Test meldet das); neue Einträge sind nicht vorgesehen.
//
// /healthz bleibt bewusst ohne Limit: Orchestrator und Lastverteiler fragen die
// Route im Sekundentakt ab und teilen sich dabei oft eine Adresse; ein Limit
// ließe die Prüfung fehlschlagen und den Dienst unnötig neu starten. Die Route
// liest nichts aus der Datenbank außer einem Ping und liefert 15 Byte.
var bekannteUnbegrenzteLeseRouten = map[string]bool{
	"/healthz": true,
}

func TestRouter_EveryAnonymousReadRouteIsRateLimited(t *testing.T) {
	r := newRouter(handlers.NewHandler(testutils.SetupTestDB()))
	const maxRequests = 400 // über jedem Limit der Leseroutes (höchstens 120 je Minute)

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
		addr := "203.0.113." + strconv.Itoa(i%250+1) + ":4000" // eigene Adresse je Route
		get := func() int {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = addr
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			return w.Code
		}
		if first := get(); first == http.StatusUnauthorized || first == http.StatusForbidden {
			continue // nur mit Anmeldung lesbar
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

// Die Limits der zuvor unbegrenzten Routen (120 je Minute, die OAuth-Routen 20):
// Das 121. bzw. 21. Anfrage wird mit 429 abgewiesen, normale Nutzung nicht.
func TestRouter_FormerlyUnlimitedReadRoutesHaveTheirLimit(t *testing.T) {
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
		{"/api/auth/google", 20},
		{"/api/auth/google/callback", 20},
	}
	for i, tc := range cases {
		addr := "198.51.100." + strconv.Itoa(i+1) + ":5000" // eigene Adresse je Route
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
