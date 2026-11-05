package handlers_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func pageRoleCount(t *testing.T, db *gorm.DB, where string, arg uint) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Unscoped().Model(&models.UserStatusPageRole{}).Where(where, arg).Count(&n).Error)
	return n
}

// Beim Löschen eines Benutzers blieben seine Zeilen in
// user_status_page_roles stehen (Soft-Delete löst kein FK-OnDelete aus).
func TestDeleteUserHandler_RemovesPageRoles(t *testing.T) {
	e := newUserGuardEnv(t)
	me := e.user(t, "me@example.com", true)
	victim := e.user(t, "victim@example.com", false)
	keeper := e.user(t, "keeper@example.com", false)
	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	require.NoError(t, e.db.Create(&page).Error)
	role := models.Role{Name: "Viewer", Permissions: "x"}
	require.NoError(t, e.db.Create(&role).Error)
	for _, u := range []models.User{victim, keeper} {
		require.NoError(t, e.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: role.ID}).Error)
	}

	w := e.do(t, http.MethodDelete, userPath(victim), me.ID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.EqualValues(t, 0, pageRoleCount(t, e.db, "user_id = ?", victim.ID), "Zuordnung des gelöschten Benutzers muss weg sein")
	assert.EqualValues(t, 1, pageRoleCount(t, e.db, "user_id = ?", keeper.ID), "fremde Zuordnungen bleiben")
}

// Dasselbe für das Löschen einer Rolle.
func TestDeleteRoleHandler_RemovesPageRoles(t *testing.T) {
	e := newUserGuardEnv(t)
	h := handlers.NewHandler(e.db)
	e.r.DELETE("/api/roles/:id", h.DeleteRoleHandler)
	u := e.user(t, "u@example.com", false)
	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	require.NoError(t, e.db.Create(&page).Error)
	gone := models.Role{Name: "Gone", Permissions: "x"}
	kept := models.Role{Name: "Kept", Permissions: "x"}
	require.NoError(t, e.db.Create(&gone).Error)
	require.NoError(t, e.db.Create(&kept).Error)
	// je (Konto, Seite) nur eine Rolle, die behaltene liegt auf einer zweiten Seite.
	page2 := models.StatusPage{Name: "Beta", Slug: "beta"}
	require.NoError(t, e.db.Create(&page2).Error)
	require.NoError(t, e.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: gone.ID}).Error)
	require.NoError(t, e.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: page2.ID, RoleID: kept.ID}).Error)

	w := e.do(t, http.MethodDelete, "/api/roles/"+strconv.FormatUint(uint64(gone.ID), 10), 0, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.EqualValues(t, 0, pageRoleCount(t, e.db, "role_id = ?", gone.ID))
	assert.EqualValues(t, 1, pageRoleCount(t, e.db, "role_id = ?", kept.ID))
}

// Name einer gelöschten Rolle und E-Mail eines gelöschten Benutzers
// sind über die Handler wieder vergebbar (partielle Indizes aus testutils).
func TestRoleAndUserHandlers_ReuseAfterDelete(t *testing.T) {
	e := newUserGuardEnv(t)
	h := handlers.NewHandler(e.db)
	e.r.POST("/api/roles", h.CreateRoleHandler)
	e.r.DELETE("/api/roles/:id", h.DeleteRoleHandler)
	me := e.user(t, "me@example.com", true)

	w := e.do(t, http.MethodPost, "/api/roles", 0, map[string]string{"name": "Editor", "permissions": "x"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	roleID := decodeID(t, w.Body.Bytes())
	w = e.do(t, http.MethodDelete, "/api/roles/"+strconv.FormatUint(uint64(roleID), 10), 0, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = e.do(t, http.MethodPost, "/api/roles", 0, map[string]string{"name": "Editor", "permissions": "x"})
	require.Equal(t, http.StatusCreated, w.Code, "gelöschter Rollenname muss frei sein: %s", w.Body.String())

	victim := e.user(t, "again@example.com", false)
	w = e.do(t, http.MethodDelete, userPath(victim), me.ID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = e.do(t, http.MethodPost, "/api/users", me.ID, map[string]any{"email": "again@example.com", "password": "pw-123456"})
	require.Equal(t, http.StatusCreated, w.Code, "gelöschte E-Mail muss frei sein: %s", w.Body.String())
}

// Der Slug der Systemseite lässt sich nicht ändern, und das
// Löschverbot gilt über das Kennzeichen auch bei abweichendem Slug.
func TestSystemPage_SlugLockedAndDeleteProtectedByFlag(t *testing.T) {
	db, r := setupSoftDeleteReuseRouter(t)
	database.SeedSystemStatusPage(db)
	var system models.StatusPage
	require.NoError(t, db.Where("is_system = ?", true).First(&system).Error)
	path := "/api/admin/pages/" + strconv.FormatUint(uint64(system.ID), 10)

	w := doTagRequest(r, http.MethodPut, path, map[string]any{"Name": "System Status", "Slug": "umbenannt"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "system_page_slug_locked")

	// Gleicher Slug bleibt änderbar (z. B. Name/Beschreibung).
	w = doTagRequest(r, http.MethodPut, path, map[string]any{"Name": "Unser Status", "Slug": database.SystemStatusPageSlug})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, db.First(&system, system.ID).Error)
	assert.True(t, system.IsSystem, "Kennzeichen darf durch ein Update nicht verloren gehen")

	// Löschschutz hängt am Kennzeichen, nicht am Slug.
	require.NoError(t, db.Model(&system).UpdateColumn("slug", "anders").Error)
	w = doTagRequest(r, http.MethodDelete, path, nil)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "system_page_protected")
}
