package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Datenbankfehler brechen das Dashboard ab (500) statt eine scheinbar leere
// Seite zu liefern; beide Abfragen (offene, behobene Vorfälle) sind abgedeckt.
func TestPublicDashboard_IncidentQueryErrorFailsClosed(t *testing.T) {
	for name, skip := range map[string]int{"offene": 0, "behobene": 1} {
		t.Run(name, func(t *testing.T) {
			db := testutils.SetupTestDB()
			h := NewHandler(db)
			r := testutils.SetupRouter()
			r.GET("/api/public/pages/:slug/dashboard", h.GetPublicDashboardHandler)
			page := models.StatusPage{Name: "Fehler", Slug: "dashboard-fehler"}
			require.NoError(t, db.Create(&page).Error)
			require.NoError(t, db.Create(&models.Incident{StatusPageID: page.ID, Title: "v", Message: "m", Status: "resolved"}).Error)

			remove := injectDBError(t, db, "query", "incidents", skip)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/public/pages/dashboard-fehler/dashboard", nil))
			remove()

			assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"code":"server_error"`)
		})
	}
}

// Scheitert allein die Abfrage der offenen Vorfälle (die der behobenen gelingt),
// bricht das Dashboard ebenfalls ab: Sonst fehlten offene Störungen unbemerkt.
func TestPublicDashboard_OpenIncidentQueryErrorAloneFailsClosed(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/pages/:slug/dashboard", h.GetPublicDashboardHandler)
	page := models.StatusPage{Name: "Fehler", Slug: "dashboard-offen-fehler"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&models.Incident{StatusPageID: page.ID, Title: "v", Message: "m", Status: "investigating"}).Error)

	calls := 0
	const hook = "test:intern:erste_vorfallabfrage"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.Table != "incidents" {
			return
		}
		calls++
		if calls == 1 { // die Abfrage der offenen Vorfälle läuft zuerst
			_ = tx.AddError(errors.New("injizierter Fehler"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(hook) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/public/pages/dashboard-offen-fehler/dashboard", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}
