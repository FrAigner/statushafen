package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func TestStatusPageUsersHandlers(t *testing.T) {
	t.Setenv("SMTP_HOST", "127.0.0.1")
	t.Setenv("SMTP_PORT", "1") // no real invitation mail
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	t.Cleanup(h.WaitMails) // Invitation mails run in the background
	r := testutils.SetupRouter()

	pageUsers := r.Group("/api/admin/pages/:page_id/users")
	pageUsers.Use(middleware.AuthMiddleware(), middleware.RequirePermission("pages:write"))
	pageUsers.GET("", h.GetStatusPageUsersHandler)
	pageUsers.POST("", h.AddStatusPageUserHandler)
	pageUsers.DELETE("/:uid", h.RemoveStatusPageUserHandler)

	page := models.StatusPage{Name: "Page 1", Slug: "page-1"}
	db.Create(&page)
	otherPage := models.StatusPage{Name: "Other Page", Slug: "other-page"}
	db.Create(&otherPage)

	editorRole := models.Role{Name: "Editor", Permissions: "incidents:write"}
	db.Create(&editorRole)
	pageAdminRole := models.Role{Name: "Page Admin", Permissions: "pages:write,incidents:write"}
	db.Create(&pageAdminRole)

	user1 := models.User{Email: "user1@example.com"}
	db.Create(&user1)
	db.Create(&models.UserStatusPageRole{UserID: user1.ID, StatusPageID: page.ID, RoleID: editorRole.ID})

	// Has "pages:write", but only on `page`, not on `otherPage`.
	pageAdminUser := models.User{Email: "page-admin@example.com"}
	db.Create(&pageAdminUser)
	db.Create(&models.UserStatusPageRole{UserID: pageAdminUser.ID, StatusPageID: page.ID, RoleID: pageAdminRole.ID})
	pageAdminToken, _ := middleware.GenerateToken(pageAdminUser.ID, pageAdminUser.Email, "", "", false)

	// Has "pages:write" nowhere — exactly the case that could previously exploit the gap.
	outsiderUser := models.User{Email: "outsider@example.com"}
	db.Create(&outsiderUser)
	db.Create(&models.UserStatusPageRole{UserID: outsiderUser.ID, StatusPageID: page.ID, RoleID: editorRole.ID})
	outsiderToken, _ := middleware.GenerateToken(outsiderUser.ID, outsiderUser.Email, "", "", false)

	t.Run("No auth -> 401", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/users", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusUnauthorized)
	})

	t.Run("Ohne pages:write auf dieser Seite -> 403 (Regressionstest für die Rechte-Eskalationslücke)", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/users", nil)
		req.Header.Set("Authorization", "Bearer "+outsiderToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusForbidden)
	})

	t.Run("pages:write auf fremder Seite reicht nicht -> 403", func(t *testing.T) {
		payload := map[string]interface{}{"user_id": pageAdminUser.ID, "role_id": pageAdminRole.ID}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/admin/pages/"+strconv.Itoa(int(otherPage.ID))+"/users", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+pageAdminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusForbidden)
	})

	t.Run("Get Status Page Users (mit pages:write)", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/users", nil)
		req.Header.Set("Authorization", "Bearer "+pageAdminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var users []map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &users)
		if len(users) != 3 {
			t.Errorf("Expected 3 users, got %d", len(users))
		}
	})

	user2 := models.User{Email: "user2@example.com"}
	db.Create(&user2)

	t.Run("Add Status Page User (mit pages:write)", func(t *testing.T) {
		// A non-Global Admin adds new accounts only by email.
		payload := map[string]interface{}{
			"email":   user2.Email,
			"role_id": editorRole.ID,
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/users", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+pageAdminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		// An invitation is created, not a membership without consent.
		var count, invitations int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", user2.ID, page.ID).Count(&count)
		db.Model(&models.PageInvitation{}).Where("email = ? AND status_page_id = ?", user2.Email, page.ID).Count(&invitations)
		if count != 0 || invitations != 1 {
			t.Errorf("Einladung statt Mitgliedschaft erwartet: Mitglied %d, Einladung %d", count, invitations)
		}
		// For the following subtest: user2 has accepted.
		db.Create(&models.UserStatusPageRole{UserID: user2.ID, StatusPageID: page.ID, RoleID: editorRole.ID})
	})

	t.Run("Re-Add Status Page User mit anderer Rolle aktualisiert statt dupliziert (Regressionstest)", func(t *testing.T) {
		// user2 already has "Editor" on `page` (previous subtest). An admin now assigns
		// "Page Admin" for the same page – this must update the existing row,
		// not create a second row. Otherwise RequirePermission keeps reading the
		// old "Editor" row via First() and the newly assigned role never takes effect.
		payload := map[string]interface{}{
			"user_id": user2.ID,
			"role_id": pageAdminRole.ID,
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/users", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+pageAdminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var count int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", user2.ID, page.ID).Count(&count)
		if count != 1 {
			t.Fatalf("Expected exactly 1 role row after re-assignment, got %d (duplicate rows = alte Rolle bleibt aktiv)", count)
		}

		var updated models.UserStatusPageRole
		if err := db.Where("user_id = ? AND status_page_id = ?", user2.ID, page.ID).First(&updated).Error; err != nil {
			t.Fatalf("Failed to load updated role: %v", err)
		}
		if updated.RoleID != pageAdminRole.ID {
			t.Errorf("Expected role_id %d (Page Admin), got %d — neu vergebene Rolle wurde nicht angewendet", pageAdminRole.ID, updated.RoleID)
		}

		// End-to-end: user2 must now actually have "pages:write" on `page`,
		// since the updated role (Page Admin) carries this permission.
		user2Token, _ := middleware.GenerateToken(user2.ID, user2.Email, "", "", false)
		checkReq, _ := http.NewRequest("GET", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/users", nil)
		checkReq.Header.Set("Authorization", "Bearer "+user2Token)
		checkW := httptest.NewRecorder()
		r.ServeHTTP(checkW, checkReq)
		requireStatusCode(t, checkW, http.StatusOK)
	})

	globalAdmin := models.User{Email: "global-admin@example.com", IsGlobalAdmin: true}
	db.Create(&globalAdmin)
	globalAdminToken, _ := middleware.GenerateToken(globalAdmin.ID, globalAdmin.Email, "", "", true)

	// page_id parse errors were discarded (id=0). For normal
	// users RequirePermission already catches an invalid page_id - for
	// Global Admins (as here) this check is skipped, so the
	// error passes through unhindered to the handler.
	t.Run("Get Status Page Users mit ungültiger page_id gibt 400 (Global Admin)", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/admin/pages/not-a-number/users", nil)
		req.Header.Set("Authorization", "Bearer "+globalAdminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Add Status Page User mit ungültiger page_id gibt 400 (Global Admin)", func(t *testing.T) {
		payload := map[string]interface{}{"user_id": user2.ID, "role_id": editorRole.ID}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/admin/pages/not-a-number/users", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+globalAdminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Remove Status Page User mit ungültiger page_id gibt 400 (Global Admin)", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/admin/pages/not-a-number/users/"+strconv.Itoa(int(user2.ID)), nil)
		req.Header.Set("Authorization", "Bearer "+globalAdminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Remove Status Page User mit ungültiger uid gibt 400 (Global Admin)", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/users/not-a-number", nil)
		req.Header.Set("Authorization", "Bearer "+globalAdminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Remove Status Page User (mit pages:write)", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/users/"+strconv.Itoa(int(user2.ID)), nil)
		req.Header.Set("Authorization", "Bearer "+pageAdminToken)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var count int64
		db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", user2.ID, page.ID).Count(&count)
		if count != 0 {
			t.Errorf("Expected user role to be removed")
		}
	})
}
