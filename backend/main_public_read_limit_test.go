package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The anonymously readable incident routes load data from several tables. They
// have a generous limit per IP (120 per minute and route) that normal
// visitors and the regular refresh of the status page never reach.
func TestRouter_PublicIncidentReadsAreRateLimited(t *testing.T) {
	db := testutils.SetupTestDB()
	r := newRouter(handlers.NewHandler(db))
	page := models.StatusPage{Name: "Lesen", Slug: "lesen-limit"}
	require.NoError(t, db.Create(&page).Error)
	id := strconv.FormatUint(uint64(page.ID), 10)

	get := func(path, addr string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = addr
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	for _, path := range []string{
		"/api/public/pages/lesen-limit/dashboard",
		"/api/pages/" + id + "/incidents",
		"/api/pages/" + id + "/incidents/1",
	} {
		t.Run(path, func(t *testing.T) {
			addr := "198.51.100.7:5000"
			// Normal use: 120 calls in one minute pass. The
			// limiter refills continuously (120 per minute = 2 per second); on a
			// slow runner (-race) a few more calls may therefore get through
			// before throttling kicks in. The test measures the time and accounts for it.
			start := time.Now()
			passed := 0
			var limited *httptest.ResponseRecorder
			for i := 0; i < 400 && limited == nil; i++ {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.RemoteAddr = addr
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				if w.Code == http.StatusTooManyRequests {
					limited = w
					break
				}
				passed++
			}
			elapsed := time.Since(start)
			require.NotNil(t, limited, "nach %d Aufrufen wurde nie gedrosselt", passed)
			assert.GreaterOrEqual(t, passed, 120, "die ersten 120 Aufrufe gehen durch")
			assert.LessOrEqual(t, float64(passed), 120+2*elapsed.Seconds()+2,
				"Drosselung setzt bei 120 Aufrufen plus Nachfüllung ein, nicht später")
			assert.NotEmpty(t, limited.Header().Get("Retry-After"))
			assert.Contains(t, limited.Body.String(), `"code":"rate_limited"`)
			// Control case: a different client is not affected.
			assert.NotEqual(t, http.StatusTooManyRequests, get(path, "198.51.100.8:5000"))
		})
	}
}
