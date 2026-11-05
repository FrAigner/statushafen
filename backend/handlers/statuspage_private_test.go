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

func TestPrivateStatusPage(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	// We need OptionalAuthMiddleware for the public routes
	publicGroup := r.Group("/api/public")
	publicGroup.Use(middleware.OptionalAuthMiddleware())
	publicGroup.GET("/pages/:slug/dashboard", h.GetPublicDashboardHandler)

	// Create a test role (Viewer)
	viewerRole := models.Role{Name: "Viewer", Permissions: "pages:read"}
	db.Create(&viewerRole)

	// Create a private page without default role
	privatePageNoRole := models.StatusPage{Name: "Private No Role", Slug: "private-no-role", IsPrivate: true, DefaultRoleID: nil}
	db.Create(&privatePageNoRole)

	// Create a private page with default role
	privatePageWithRole := models.StatusPage{Name: "Private With Role", Slug: "private-with-role", IsPrivate: true, DefaultRoleID: &viewerRole.ID, AutoJoinEmailDomains: "example.com"}
	db.Create(&privatePageWithRole)

	// Create a public page
	publicPage := models.StatusPage{Name: "Public", Slug: "public", IsPrivate: false}
	db.Create(&publicPage)

	// Create a regular user
	user := models.User{Email: "test-user@example.com"}
	db.Create(&user)
	token, _ := middleware.GenerateToken(user.ID, user.Email, "", "", false)

	t.Run("Public Page - No Auth", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/public/pages/public/dashboard", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Private Page - No Auth", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/public/pages/private-no-role/dashboard", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "404 wie bei einer fehlenden Seite, nicht 401")
	})

	t.Run("Private Page (No Default Role) - With Auth", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/public/pages/private-no-role/dashboard", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// User has no role and no default role is set: 404 wie bei einer fehlenden Seite
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Private Page (With Default Role) - With Auth", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/public/pages/private-with-role/dashboard", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// User has no role, but default role is set, so it should be created on the fly -> 200 OK
		assert.Equal(t, http.StatusOK, w.Code)

		// Verify the role was actually created
		var userRole models.UserStatusPageRole
		err := db.Where("user_id = ? AND status_page_id = ?", user.ID, privatePageWithRole.ID).First(&userRole).Error
		assert.NoError(t, err)
		assert.Equal(t, viewerRole.ID, userRole.RoleID)
	})
}
