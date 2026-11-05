package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func TestUserHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/users", h.GetUsersHandler)
	r.POST("/api/users", h.CreateUserHandler)
	r.PUT("/api/users/:id", h.UpdateUserHandler)
	r.DELETE("/api/users/:id", h.DeleteUserHandler)

	page := models.StatusPage{Name: "User Page", Slug: "user-page"}
	db.Create(&page)

	role := models.Role{Name: "Editor", Permissions: "incidents:write"}
	db.Create(&role)

	// Der Test macht den neuen User zeitweise zum Global Admin und
	// stuft ihn wieder herab bzw. löscht ihn; ohne einen weiteren Global Admin
	// wäre er der letzte (409).
	db.Create(&models.User{Email: "other-admin@example.com", IsGlobalAdmin: true})

	var createdUserID uint

	t.Run("Create User with PageRoles", func(t *testing.T) {
		payload := map[string]interface{}{
			"email":             "newuser@example.com",
			"password":          "secret123",
			"allowed_countries": "global",
			"is_global_admin":   false,
			"page_roles": []map[string]interface{}{
				{
					"status_page_id": page.ID,
					"role_id":        role.ID,
				},
			},
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/users", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusCreated)

		var user models.User
		_ = json.Unmarshal(w.Body.Bytes(), &user)
		createdUserID = user.ID

		if user.Email != "newuser@example.com" {
			t.Errorf("User created incorrectly")
		}

		var count int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", user.ID).Count(&count)
		if count != 1 {
			t.Errorf("PageRoles not created in database. Expected 1, got %d", count)
		}
	})

	t.Run("Update User (Change PageRoles)", func(t *testing.T) {
		payload := map[string]interface{}{
			"email":           "newuser@example.com",
			"is_global_admin": true,
			"page_roles":      []map[string]interface{}{}, // Remove roles, make global admin
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("PUT", "/api/users/"+strconv.Itoa(int(createdUserID)), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var count int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", createdUserID).Count(&count)
		if count != 0 {
			t.Errorf("PageRoles not removed correctly. Expected 0, got %d", count)
		}

		var dbUser models.User
		db.First(&dbUser, createdUserID)
		if !dbUser.IsGlobalAdmin {
			t.Errorf("User not upgraded to Global Admin")
		}
	})

	t.Run("Update User mit neuen PageRoles legt Zuordnung an", func(t *testing.T) {
		role2 := models.Role{Name: "Viewer", Permissions: ""}
		db.Create(&role2)

		payload := map[string]interface{}{
			"email":           "newuser@example.com",
			"is_global_admin": false,
			"page_roles": []map[string]interface{}{
				{
					"status_page_id": page.ID,
					"role_id":        role2.ID,
				},
			},
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("PUT", "/api/users/"+strconv.Itoa(int(createdUserID)), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var link models.UserStatusPageRole
		if err := db.Where("user_id = ? AND status_page_id = ? AND role_id = ?", createdUserID, page.ID, role2.ID).First(&link).Error; err != nil {
			t.Errorf("expected new page role to be created, got error: %v", err)
		}
	})

	t.Run("Get Users listet alle User", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/users", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var users []models.User
		_ = json.Unmarshal(w.Body.Bytes(), &users)
		if len(users) == 0 {
			t.Errorf("expected at least one user in response")
		}
	})

	t.Run("Update User mit ungültiger ID", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{"email": "x@example.com"})
		req, _ := http.NewRequest("PUT", "/api/users/not-a-number", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Update User mit nicht existierender ID", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{"email": "x@example.com"})
		req, _ := http.NewRequest("PUT", "/api/users/999999", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Delete User mit ungültiger ID", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/users/not-a-number", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Delete User mit nicht existierender ID", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/users/999999", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Delete User", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/users/"+strconv.Itoa(int(createdUserID)), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var count int64
		db.Model(&models.User{}).Where("id = ?", createdUserID).Count(&count)
		if count != 0 {
			t.Errorf("User was not deleted")
		}
	})
}
