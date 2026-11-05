package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Die anonym lesbaren Vorfall-Routen laden Daten aus mehreren Tabellen. Sie
// haben ein großzügiges Limit je IP (120 pro Minute und Route), das normale
// Besucher und die regelmäßige Aktualisierung der Statusseite nie erreichen.
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
			// Normale Nutzung: 120 Aufrufe in einer Minute gehen durch.
			for i := 0; i < 120; i++ {
				require.NotEqual(t, http.StatusTooManyRequests, get(path, addr), "Aufruf %d", i+1)
			}
			// Angriff: der 121. Aufruf wird gedrosselt, mit Retry-After.
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = addr
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusTooManyRequests, w.Code)
			assert.NotEmpty(t, w.Header().Get("Retry-After"))
			assert.Contains(t, w.Body.String(), `"code":"rate_limited"`)
			// Gegenprobe: ein anderer Client ist nicht betroffen.
			assert.NotEqual(t, http.StatusTooManyRequests, get(path, "198.51.100.8:5000"))
		})
	}
}
