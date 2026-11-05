package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
)

func TestMeExportHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	authGroup := r.Group("/api")
	authGroup.Use(middleware.AuthMiddleware())
	authGroup.GET("/me/export", h.MeExportHandler)

	role := models.Role{Name: "Editor", Permissions: "incidents:write"}
	db.Create(&role)

	page := models.StatusPage{Name: "Export Page", Slug: "export-page"}
	db.Create(&page)

	userA := models.User{Email: "export-a@example.com", PasswordHash: "super-secret-hash", PreferredLanguage: "de"}
	db.Create(&userA)
	userB := models.User{Email: "export-b@example.com", PasswordHash: "other-secret-hash"}
	db.Create(&userB)

	db.Create(&models.UserStatusPageRole{UserID: userA.ID, StatusPageID: page.ID, RoleID: role.ID})

	db.Create(&models.Subscriber{StatusPageID: page.ID, Email: "export-a@example.com", Channel: "email", CreatedByUserID: &userA.ID})
	db.Create(&models.Subscriber{StatusPageID: page.ID, Email: "export-b@example.com", Channel: "email", CreatedByUserID: &userB.ID})

	db.Create(&models.AuditLog{Action: "incident.created", Category: "incident", ActorID: &userA.ID, ActorEmail: userA.Email, TargetName: "INC-1"})
	db.Create(&models.AuditLog{Action: "incident.created", Category: "incident", ActorID: &userB.ID, ActorEmail: userB.Email, TargetName: "INC-2"})

	tokenA, _ := middleware.GenerateToken(userA.ID, userA.Email, role.Name, role.Permissions, false)
	tokenB, _ := middleware.GenerateToken(userB.ID, userB.Email, "", "", false)

	t.Run("Ohne Login -> 401", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/me/export", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("Eingeloggter Nutzer bekommt eigene Daten inkl. Header", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/me/export", nil)
		req.Header.Set("Authorization", "Bearer "+tokenA)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))

		body := w.Body.String()
		assert.NotContains(t, body, "super-secret-hash")
		assert.NotContains(t, body, "password_hash")

		var parsed map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		profile, ok := parsed["profile"].(map[string]interface{})
		if !ok {
			t.Fatalf("profile missing or wrong type: %v", parsed["profile"])
		}
		assert.Equal(t, "export-a@example.com", profile["Email"])
		assert.Equal(t, "de", profile["PreferredLanguage"])

		pageRoles, ok := parsed["page_roles"].([]interface{})
		if !ok || len(pageRoles) != 1 {
			t.Fatalf("expected exactly 1 page role for user A, got %v", parsed["page_roles"])
		}
		firstRole := pageRoles[0].(map[string]interface{})
		assert.Equal(t, "Export Page", firstRole["status_page_name"])
		assert.Equal(t, "Editor", firstRole["role_name"])

		subscriptions, ok := parsed["subscriptions"].([]interface{})
		if !ok || len(subscriptions) != 1 {
			t.Fatalf("expected exactly 1 subscription for user A, got %v", parsed["subscriptions"])
		}
		sub := subscriptions[0].(map[string]interface{})
		assert.Equal(t, "export-a@example.com", sub["Email"])

		auditLogs, ok := parsed["audit_logs"].([]interface{})
		if !ok || len(auditLogs) != 1 {
			t.Fatalf("expected exactly 1 audit log entry for user A, got %v", parsed["audit_logs"])
		}
		logEntry := auditLogs[0].(map[string]interface{})
		assert.Equal(t, "INC-1", logEntry["target_name"])
	})

	t.Run("Nutzer sieht niemals Daten eines anderen Nutzers", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/me/export", nil)
		req.Header.Set("Authorization", "Bearer "+tokenA)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		body := w.Body.String()
		assert.NotContains(t, body, "export-b@example.com")
		assert.NotContains(t, body, "other-secret-hash")
		assert.NotContains(t, body, "INC-2")
	})

	t.Run("Nutzer ohne Abos/Logs bekommt leere Listen statt Fehler", func(t *testing.T) {
		userC := models.User{Email: "export-c@example.com"}
		db.Create(&userC)
		tokenC, _ := middleware.GenerateToken(userC.ID, userC.Email, "", "", false)

		req, _ := http.NewRequest("GET", "/api/me/export", nil)
		req.Header.Set("Authorization", "Bearer "+tokenC)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var parsed map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}
		assert.Empty(t, parsed["page_roles"])
		assert.Empty(t, parsed["subscriptions"])
		assert.Empty(t, parsed["audit_logs"])
		if _, exists := parsed["page_roles"]; !exists {
			t.Fatalf("page_roles key missing entirely")
		}
	})

	t.Run("Gültiger Token aber User zwischenzeitlich gelöscht -> 401", func(t *testing.T) {
		userD := models.User{Email: "export-d@example.com"}
		db.Create(&userD)
		tokenD, _ := middleware.GenerateToken(userD.ID, userD.Email, "", "", false)
		db.Unscoped().Delete(&userD)

		req, _ := http.NewRequest("GET", "/api/me/export", nil)
		req.Header.Set("Authorization", "Bearer "+tokenD)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("Fremder Token für user B sieht nur eigene Daten", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/me/export", nil)
		req.Header.Set("Authorization", "Bearer "+tokenB)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, "export-b@example.com")
		assert.NotContains(t, body, "export-a@example.com")
		assert.NotContains(t, body, "INC-1")
	})
}
