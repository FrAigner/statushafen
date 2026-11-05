package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
)

func TestOverviewHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())
	authGroup.GET("/admin/overview", middleware.RequireGlobalAdmin(), h.GetOverviewHandler)

	adminUser := models.User{Email: "overview-admin@example.com", IsGlobalAdmin: true}
	db.Create(&adminUser)
	adminToken, _ := middleware.GenerateToken(adminUser.ID, adminUser.Email, "", "", true)

	regularUser := models.User{Email: "overview-regular@example.com"}
	db.Create(&regularUser)
	regularToken, _ := middleware.GenerateToken(regularUser.ID, regularUser.Email, "", "", false)

	pageA := models.StatusPage{Name: "Page A", Slug: "page-a"}
	db.Create(&pageA)
	compA := models.Component{StatusPageID: pageA.ID, Name: "API", Status: "major_outage"}
	db.Create(&compA)
	db.Create(&models.Incident{StatusPageID: pageA.ID, Title: "Down", Message: "m", Status: "investigating"})
	db.Create(&models.Subscriber{StatusPageID: pageA.ID, Channel: "email", Email: "sub@example.com"})

	pageB := models.StatusPage{Name: "Page B", Slug: "page-b"}
	db.Create(&pageB)
	compB := models.Component{StatusPageID: pageB.ID, Name: "Web", Status: "operational"}
	db.Create(&compB)

	t.Run("Non-admin -> 403", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/overview", nil)
		req.Header.Set("Authorization", "Bearer "+regularToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("Global admin sees aggregated overview", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/overview", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"total_pages":2`)
		assert.Contains(t, w.Body.String(), `"total_open_incidents":1`)
		assert.Contains(t, w.Body.String(), `"total_subscribers":1`)
		assert.Contains(t, w.Body.String(), `"worst_status":"major_outage"`)
	})
}
