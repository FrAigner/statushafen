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

func TestRoleHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/roles", h.GetRolesHandler)
	r.POST("/api/roles", h.CreateRoleHandler)
	r.PUT("/api/roles/:id", h.UpdateRoleHandler)
	r.DELETE("/api/roles/:id", h.DeleteRoleHandler)

	var createdRoleID uint

	t.Run("Create Role", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":        "Incident Manager",
			"permissions": "incidents:write,incidents:read",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/roles", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusCreated)

		var role models.Role
		_ = json.Unmarshal(w.Body.Bytes(), &role)
		createdRoleID = role.ID

		if role.Name != "Incident Manager" {
			t.Errorf("Role created incorrectly")
		}
	})

	t.Run("Get Roles", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/roles", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var roles []models.Role
		_ = json.Unmarshal(w.Body.Bytes(), &roles)

		if len(roles) != 1 {
			t.Errorf("Expected 1 role, got %d", len(roles))
		}
	})

	t.Run("Update Role", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":        "Full Manager",
			"permissions": "incidents:write,components:write",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("PUT", "/api/roles/"+strconv.Itoa(int(createdRoleID)), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var role models.Role
		db.First(&role, createdRoleID)
		if role.Name != "Full Manager" {
			t.Errorf("Role not updated correctly")
		}
	})

	t.Run("Update Role mit ungültiger ID", func(t *testing.T) {
		req, _ := http.NewRequest("PUT", "/api/roles/not-a-number", bytes.NewBuffer([]byte(`{"name":"x"}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Update Role mit nicht existierender ID", func(t *testing.T) {
		req, _ := http.NewRequest("PUT", "/api/roles/999999", bytes.NewBuffer([]byte(`{"name":"x"}`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Delete Role mit ungültiger ID", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/roles/not-a-number", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Delete Role mit nicht existierender ID", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/roles/999999", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Delete Role", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/roles/"+strconv.Itoa(int(createdRoleID)), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var count int64
		db.Model(&models.Role{}).Where("id = ?", createdRoleID).Count(&count)
		if count != 0 {
			t.Errorf("Role was not deleted")
		}
	})
}
