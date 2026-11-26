package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// GET /api/pages/:page_id/languages and
// GET /api/pages/:page_id/ui-overrides were readable by EVERY logged-in user
// on EVERY page. The public frontend does not need them
// (anonymous visitors use /api/public/ui-overrides; the page endpoints
// are only called by the admin area: AdminLayout.vue, useContentLanguages.js),
// so they are now bound to a role on the page.
func TestRouter_PageLanguagesAndUiOverridesRequireRoleOnPage(t *testing.T) {
	db := testutils.SetupTestDB()
	r := newRouter(handlers.NewHandler(db))

	own := models.StatusPage{Name: "Eigene", Slug: "eigene-read"}
	foreign := models.StatusPage{Name: "Fremde", Slug: "fremde-read"}
	db.Create(&own)
	db.Create(&foreign)
	role := models.Role{Name: "Read Viewer", Permissions: "incidents:read"}
	db.Create(&role)
	member := models.User{Email: "member@example.com"}
	admin := models.User{Email: "global@example.com", IsGlobalAdmin: true}
	db.Create(&member)
	db.Create(&admin)
	db.Create(&models.UserStatusPageRole{UserID: member.ID, StatusPageID: own.ID, RoleID: role.ID})
	memberToken, _ := middleware.GenerateToken(member.ID, member.Email, "", "", false)
	adminToken, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)

	get := func(path, token string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	page := func(p models.StatusPage, suffix string) string {
		return "/api/pages/" + strconv.FormatUint(uint64(p.ID), 10) + suffix
	}

	for _, suffix := range []string{"/languages", "/ui-overrides?language_code=de"} {
		if got := get(page(own, suffix), memberToken); got != http.StatusOK {
			t.Errorf("GET %s with role on the page = %d, want 200", suffix, got)
		}
		if got := get(page(foreign, suffix), memberToken); got != http.StatusForbidden {
			t.Errorf("GET %s on a foreign page = %d, want 403", suffix, got)
		}
		if got := get(page(foreign, suffix), adminToken); got != http.StatusOK {
			t.Errorf("GET %s as global admin = %d, want 200", suffix, got)
		}
	}
}
