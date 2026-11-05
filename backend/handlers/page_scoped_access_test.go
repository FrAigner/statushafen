package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
)

// GET /api/pages/:page_id/{components,incidents,incidents/:id,notices}
// prüften nie StatusPage.IsPrivate. Ein Aufrufer ohne Rolle auf einer privaten
// Seite (oder ganz ohne Login) konnte Components/Incidents/Notices trotzdem
// lesen, solange er die page_id kannte.
func TestPageScopedRoutesRespectIsPrivate(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/pages/:page_id/components", middleware.OptionalAuthMiddleware(), h.GetComponentsHandler)
	r.GET("/api/pages/:page_id/incidents", middleware.OptionalAuthMiddleware(), h.GetIncidentsHandler)
	r.GET("/api/pages/:page_id/incidents/:id", middleware.OptionalAuthMiddleware(), h.GetIncidentDetailHandler)
	r.GET("/api/pages/:page_id/notices", middleware.OptionalAuthMiddleware(), h.GetActiveNoticesHandler)

	privatePage := models.StatusPage{Name: "Private Page", Slug: "private-page", IsPrivate: true}
	db.Create(&privatePage)
	comp := models.Component{StatusPageID: privatePage.ID, Name: "API"}
	db.Create(&comp)
	incident := models.Incident{StatusPageID: privatePage.ID, Title: "Outage", Message: "m", Status: "investigating"}
	db.Create(&incident)
	notice := models.GeneralNotice{StatusPageID: privatePage.ID, Title: "Notice", Message: "m", Level: "info", IsActive: true}
	db.Create(&notice)

	role := models.Role{Name: "Viewer", Permissions: "incidents:read"}
	db.Create(&role)

	insider := models.User{Email: "insider@example.com"}
	db.Create(&insider)
	db.Create(&models.UserStatusPageRole{UserID: insider.ID, StatusPageID: privatePage.ID, RoleID: role.ID})
	insiderToken, _ := middleware.GenerateToken(insider.ID, insider.Email, "", "", false)

	outsider := models.User{Email: "outsider@example.com"}
	db.Create(&outsider)
	otherPage := models.StatusPage{Name: "Other Page", Slug: "other-page"}
	db.Create(&otherPage)
	db.Create(&models.UserStatusPageRole{UserID: outsider.ID, StatusPageID: otherPage.ID, RoleID: role.ID})
	outsiderToken, _ := middleware.GenerateToken(outsider.ID, outsider.Email, "", "", false)

	pid := strconv.Itoa(int(privatePage.ID))
	iid := strconv.Itoa(int(incident.ID))
	// existiert in keiner Testfixture - deckt den "Status page not found"-Zweig ab.
	const missingPageID = "999999"

	routes := []struct {
		name string
		path func(pageID string) string
	}{
		{"components", func(pageID string) string { return "/api/pages/" + pageID + "/components" }},
		{"incidents", func(pageID string) string { return "/api/pages/" + pageID + "/incidents" }},
		{"incident detail", func(pageID string) string { return "/api/pages/" + pageID + "/incidents/" + iid }},
		{"notices", func(pageID string) string { return "/api/pages/" + pageID + "/notices" }},
	}

	for _, rt := range routes {
		t.Run(rt.name+": Insider mit Rolle bekommt 200", func(t *testing.T) {
			req, _ := http.NewRequest("GET", rt.path(pid), nil)
			req.Header.Set("Authorization", "Bearer "+insiderToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		})

		t.Run(rt.name+": Outsider mit fremder Rolle bekommt 404 wie bei einer fehlenden Seite", func(t *testing.T) {
			req, _ := http.NewRequest("GET", rt.path(pid), nil)
			req.Header.Set("Authorization", "Bearer "+outsiderToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code)
		})

		t.Run(rt.name+": Unauthentifiziert bekommt 404 wie bei einer fehlenden Seite", func(t *testing.T) {
			req, _ := http.NewRequest("GET", rt.path(pid), nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code)
		})

		t.Run(rt.name+": unbekannte page_id gibt 404", func(t *testing.T) {
			req, _ := http.NewRequest("GET", rt.path(missingPageID), nil)
			req.Header.Set("Authorization", "Bearer "+insiderToken)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}

// pageIsAccessible nutzt seit dieser Änderung checkPrivatePageAccess statt
// des Auto-Join-losen userHasRoleOnPrivatePage: der bisher nur der
// Dashboard-Route (GetPublicDashboardHandler) vorbehaltene
// Auto-Join per StatusPage.DefaultRoleID soll auch für Deep Links / direkte
// API-Aufrufe auf dieselben Daten greifen, statt einen berechtigten Nutzer
// dort mit 403 abzuweisen, nur weil er nicht über das Dashboard eingestiegen ist.
func TestPageScopedRoutesAutoJoinViaDefaultRole(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/pages/:page_id/components", middleware.OptionalAuthMiddleware(), h.GetComponentsHandler)

	role := models.Role{Name: "Default Viewer", Permissions: "incidents:read"}
	db.Create(&role)

	autoJoinPage := models.StatusPage{Name: "Auto-Join Page", Slug: "auto-join-page", IsPrivate: true, DefaultRoleID: &role.ID, AutoJoinEmailDomains: "example.com"}
	db.Create(&autoJoinPage)
	comp := models.Component{StatusPageID: autoJoinPage.ID, Name: "API"}
	db.Create(&comp)
	pid := strconv.Itoa(int(autoJoinPage.ID))

	t.Run("Authentifizierter Newcomer ohne Rolle wird per Deep Link auto-gejoint", func(t *testing.T) {
		newcomer := models.User{Email: "newcomer@example.com"}
		db.Create(&newcomer)
		newcomerToken, _ := middleware.GenerateToken(newcomer.ID, newcomer.Email, "", "", false)

		var before int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", newcomer.ID, autoJoinPage.ID).Count(&before)
		assert.Equal(t, int64(0), before)

		req, _ := http.NewRequest("GET", "/api/pages/"+pid+"/components", nil)
		req.Header.Set("Authorization", "Bearer "+newcomerToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var after int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", newcomer.ID, autoJoinPage.ID).Count(&after)
		assert.Equal(t, int64(1), after)
	})

	t.Run("Auto-Join setzt trotzdem Login voraus - unauthentifiziert bleibt 404", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/pages/"+pid+"/components", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}
