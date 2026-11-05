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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /api/pages/:page_id/tags verlangte components:read. Eine
// Seitenrolle mit nur incidents:write (ohne components:*) bekam 403, das
// Frontend blendete das Tag-Feld im Vorfallformular still aus. Der Test geht
// über den echten Router (newRouter), nicht über eine nachgebaute Route.
func TestRouter_PageTags_IncidentRolesDuerfenTagsLesen(t *testing.T) {
	db := testutils.SetupTestDB()
	r := newRouter(handlers.NewHandler(db))
	page := models.StatusPage{Name: "Tags", Slug: "tags-631"}
	other := models.StatusPage{Name: "Fremd", Slug: "tags-631-fremd"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&models.Tag{Name: "eigen", StatusPageID: &page.ID}).Error)
	require.NoError(t, db.Create(&models.Tag{Name: "fremd", StatusPageID: &other.ID}).Error)

	tokenFor := func(email, perms string, onPage *models.StatusPage) string {
		u := models.User{Email: email}
		require.NoError(t, db.Create(&u).Error)
		if onPage != nil {
			role := models.Role{Name: "R " + email, Permissions: perms}
			require.NoError(t, db.Create(&role).Error)
			require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: onPage.ID, RoleID: role.ID}).Error)
		}
		tok, _ := middleware.GenerateToken(u.ID, u.Email, "", perms, false)
		return tok
	}
	get := func(p models.StatusPage, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/pages/"+strconv.Itoa(int(p.ID))+"/tags", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("nur incidents:write sieht die Tags der eigenen Seite", func(t *testing.T) {
		w := get(page, tokenFor("iw@example.com", "incidents:write", &page))
		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "eigen")
		assert.NotContains(t, w.Body.String(), "fremd")
	})
	t.Run("nur incidents:read genügt ebenfalls", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, get(page, tokenFor("ir@example.com", "incidents:read", &page)).Code)
	})
	t.Run("components:read bleibt erlaubt", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, get(page, tokenFor("cr@example.com", "components:read", &page)).Code)
	})
	t.Run("Rolle ohne components:* und incidents:* -> 403 mit Code", func(t *testing.T) {
		w := get(page, tokenFor("nw@example.com", "notices:write", &page))
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), `"code":"forbidden"`)
	})
	t.Run("Mandantentrennung: Rolle der anderen Seite -> 403", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, get(page, tokenFor("fx@example.com", "incidents:write", &other)).Code)
	})
	t.Run("ohne Rolle -> 403", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, get(page, tokenFor("nr@example.com", "", nil)).Code)
	})
}
