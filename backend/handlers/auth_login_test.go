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

	"github.com/gin-gonic/gin"
)

func TestAuthLoginAndMeHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.POST("/api/login", h.LoginHandler)
	r.GET("/api/me", func(c *gin.Context) {
		// Mock the JWT middleware by directly setting context values based on header
		userIDStr := c.GetHeader("X-Mock-User-ID")
		if userIDStr != "" {
			uid, _ := strconv.Atoi(userIDStr)
			c.Set("userID", uint(uid))
			// Need to fetch user from DB and set email for MeHandler
			var u models.User
			db.First(&u, uid)
			c.Set("email", u.Email)
			c.Next()
		} else {
			c.AbortWithStatus(http.StatusUnauthorized)
		}
	}, h.MeHandler)

	password := "secret123"
	hashedPassword := testutils.MustHash(password)

	testUser := models.User{
		Email:        "login_test@example.com",
		PasswordHash: hashedPassword,
	}
	db.Create(&testUser)

	t.Run("Login - Valid Credentials", func(t *testing.T) {
		payload := map[string]string{
			"email":    "login_test@example.com",
			"password": "secret123",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["token"] == nil {
			t.Errorf("Expected JWT token in response")
		}
		// ohne id erkennt die Benutzerverwaltung die eigene Zeile nicht.
		if resp["id"] != float64(testUser.ID) {
			t.Errorf("Expected id %d in login response, got %v", testUser.ID, resp["id"])
		}
	})

	t.Run("Login - Invalid Credentials", func(t *testing.T) {
		payload := map[string]string{
			"email":    "login_test@example.com",
			"password": "wrongpassword",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401 Unauthorized, got %d", w.Code)
		}
	})

	t.Run("Get Me - Authenticated", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/me", nil)
		req.Header.Set("X-Mock-User-ID", strconv.Itoa(int(testUser.ID)))

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200 OK, got %d", w.Code)
		}

		var user models.User
		_ = json.Unmarshal(w.Body.Bytes(), &user)
		if user.Email != "login_test@example.com" {
			t.Errorf("Expected email to match, got %s", user.Email)
		}
	})

	t.Run("Get Me - Unauthenticated", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/me", nil)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// The mock middleware won't set userID, so MeHandler returns 401
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401 Unauthorized, got %d", w.Code)
		}
	})
}
