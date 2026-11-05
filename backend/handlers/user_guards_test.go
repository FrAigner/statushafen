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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Regressionstests der Benutzerverwaltung (Selbstlöschung,
// Selbst-Herabstufung, letzter Global Admin, "Global Admin" als Seitenrolle).

type userGuardEnv struct {
	db *gorm.DB
	r  *gin.Engine
}

// newUserGuardEnv registriert die Benutzer-Handler; X-Test-User setzt die ID
// des angemeldeten Nutzers (statt eines echten JWT).
func newUserGuardEnv(t *testing.T) userGuardEnv {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) {
		if v := c.GetHeader("X-Test-User"); v != "" {
			id, _ := strconv.Atoi(v)
			c.Set("userID", uint(id))
		}
		c.Next()
	})
	r.GET("/api/roles", h.GetRolesHandler)
	r.POST("/api/users", h.CreateUserHandler)
	r.PUT("/api/users/:id", h.UpdateUserHandler)
	r.DELETE("/api/users/:id", h.DeleteUserHandler)
	r.POST("/api/admin/pages/:page_id/users", h.AddStatusPageUserHandler)
	return userGuardEnv{db: db, r: r}
}

func (e userGuardEnv) do(t *testing.T, method, path string, actor uint, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Buffer
	if payload == nil {
		body = &bytes.Buffer{}
	} else {
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		body = bytes.NewBuffer(raw)
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	if actor != 0 {
		req.Header.Set("X-Test-User", strconv.FormatUint(uint64(actor), 10))
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func (e userGuardEnv) user(t *testing.T, email string, globalAdmin bool) models.User {
	t.Helper()
	u := models.User{Email: email, IsGlobalAdmin: globalAdmin}
	require.NoError(t, e.db.Create(&u).Error)
	return u
}

func userPath(u models.User) string {
	return "/api/users/" + strconv.FormatUint(uint64(u.ID), 10)
}

func requireCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, code, body["code"])
	assert.NotEmpty(t, body["error"])
}

func userExists(db *gorm.DB, id uint) bool {
	var n int64
	db.Model(&models.User{}).Where("id = ?", id).Count(&n)
	return n == 1
}

func TestDeleteUserHandler_SelfDeleteIsConflict(t *testing.T) {
	e := newUserGuardEnv(t)
	me := e.user(t, "me@example.com", true)
	e.user(t, "other-admin@example.com", true)

	w := e.do(t, http.MethodDelete, userPath(me), me.ID, nil)

	requireCode(t, w, http.StatusConflict, "cannot_delete_self")
	assert.True(t, userExists(e.db, me.ID))
}

func TestDeleteUserHandler_LastGlobalAdminIsConflict(t *testing.T) {
	e := newUserGuardEnv(t)
	only := e.user(t, "only-admin@example.com", true)
	caller := e.user(t, "caller@example.com", false)

	w := e.do(t, http.MethodDelete, userPath(only), caller.ID, nil)

	requireCode(t, w, http.StatusConflict, "last_global_admin")
	assert.True(t, userExists(e.db, only.ID))
}

func TestDeleteUserHandler_OtherGlobalAdminCanBeDeleted(t *testing.T) {
	e := newUserGuardEnv(t)
	me := e.user(t, "me@example.com", true)
	other := e.user(t, "other-admin@example.com", true)

	w := e.do(t, http.MethodDelete, userPath(other), me.ID, nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, userExists(e.db, other.ID))
}

func isGlobalAdmin(db *gorm.DB, id uint) bool {
	var u models.User
	db.First(&u, id)
	return u.IsGlobalAdmin
}

func TestUpdateUserHandler_SelfDemoteIsConflict(t *testing.T) {
	e := newUserGuardEnv(t)
	me := e.user(t, "me@example.com", true)
	e.user(t, "other-admin@example.com", true)

	w := e.do(t, http.MethodPut, userPath(me), me.ID, map[string]any{"email": me.Email, "is_global_admin": false})

	requireCode(t, w, http.StatusConflict, "cannot_demote_self")
	assert.True(t, isGlobalAdmin(e.db, me.ID))
}

func TestUpdateUserHandler_SelfEditKeepingAdminIsAllowed(t *testing.T) {
	e := newUserGuardEnv(t)
	me := e.user(t, "me@example.com", true)

	w := e.do(t, http.MethodPut, userPath(me), me.ID, map[string]any{"email": "me-neu@example.com", "is_global_admin": true})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestUpdateUserHandler_LastGlobalAdminDemoteIsConflict(t *testing.T) {
	e := newUserGuardEnv(t)
	only := e.user(t, "only-admin@example.com", true)
	caller := e.user(t, "caller@example.com", false)

	w := e.do(t, http.MethodPut, userPath(only), caller.ID, map[string]any{"email": only.Email, "is_global_admin": false})

	requireCode(t, w, http.StatusConflict, "last_global_admin")
	assert.True(t, isGlobalAdmin(e.db, only.ID))
}

func TestUpdateUserHandler_OtherGlobalAdminCanBeDemoted(t *testing.T) {
	e := newUserGuardEnv(t)
	me := e.user(t, "me@example.com", true)
	other := e.user(t, "other-admin@example.com", true)

	w := e.do(t, http.MethodPut, userPath(other), me.ID, map[string]any{"email": other.Email, "is_global_admin": false})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, isGlobalAdmin(e.db, other.ID))
}

// globalAdminRoleFixture legt die Rolle "Global Admin", eine normale Rolle und
// eine Statusseite an.
func globalAdminRoleFixture(t *testing.T, db *gorm.DB) (models.Role, models.Role, models.StatusPage) {
	t.Helper()
	ga := models.Role{Name: models.GlobalAdminRoleName, Permissions: "pages:write"}
	require.NoError(t, db.Create(&ga).Error)
	editor := models.Role{Name: "Editor", Permissions: "incidents:write"}
	require.NoError(t, db.Create(&editor).Error)
	page := models.StatusPage{Name: "Guard Page", Slug: "guard-page"}
	require.NoError(t, db.Create(&page).Error)
	return ga, editor, page
}

func pageRoles(page models.StatusPage, role models.Role) []map[string]any {
	return []map[string]any{{"status_page_id": page.ID, "role_id": role.ID}}
}

func TestGetRolesHandler_MarksGlobalAdminRoleGlobalOnly(t *testing.T) {
	e := newUserGuardEnv(t)
	ga, editor, _ := globalAdminRoleFixture(t, e.db)

	w := e.do(t, http.MethodGet, "/api/roles", 0, nil)

	require.Equal(t, http.StatusOK, w.Code)
	var roles []struct {
		ID         uint
		Name       string
		GlobalOnly bool
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &roles))
	got := map[uint]bool{}
	for _, r := range roles {
		got[r.ID] = r.GlobalOnly
	}
	assert.Equal(t, map[uint]bool{ga.ID: true, editor.ID: false}, got)
}

func TestCreateUserHandler_RejectsGlobalAdminPageRole(t *testing.T) {
	e := newUserGuardEnv(t)
	ga, _, page := globalAdminRoleFixture(t, e.db)

	w := e.do(t, http.MethodPost, "/api/users", 0, map[string]any{
		"email": "new@example.com", "password": "secret123", "page_roles": pageRoles(page, ga),
	})

	requireCode(t, w, http.StatusBadRequest, "global_admin_role_not_assignable")
	var n int64
	e.db.Model(&models.User{}).Where("email = ?", "new@example.com").Count(&n)
	assert.Zero(t, n)
}

func TestCreateUserHandler_AcceptsNormalPageRole(t *testing.T) {
	e := newUserGuardEnv(t)
	_, editor, page := globalAdminRoleFixture(t, e.db)

	w := e.do(t, http.MethodPost, "/api/users", 0, map[string]any{
		"email": "new@example.com", "password": "secret123", "page_roles": pageRoles(page, editor),
	})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

func TestUpdateUserHandler_RejectsNewGlobalAdminPageRole(t *testing.T) {
	e := newUserGuardEnv(t)
	ga, _, page := globalAdminRoleFixture(t, e.db)
	u := e.user(t, "u@example.com", false)

	w := e.do(t, http.MethodPut, userPath(u), 0, map[string]any{"email": u.Email, "page_roles": pageRoles(page, ga)})

	requireCode(t, w, http.StatusBadRequest, "global_admin_role_not_assignable")
	var n int64
	e.db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", u.ID).Count(&n)
	assert.Zero(t, n)
}

// Altbestand: Eine schon vorhandene Zuordnung bleibt beim Speichern erhalten,
// statt das Speichern des Nutzers zu blockieren oder ihm Rechte zu entziehen.
func TestUpdateUserHandler_KeepsExistingGlobalAdminPageRole(t *testing.T) {
	e := newUserGuardEnv(t)
	ga, _, page := globalAdminRoleFixture(t, e.db)
	u := e.user(t, "legacy@example.com", false)
	require.NoError(t, e.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: ga.ID}).Error)

	w := e.do(t, http.MethodPut, userPath(u), 0, map[string]any{"email": u.Email, "page_roles": pageRoles(page, ga)})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var n int64
	e.db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND role_id = ?", u.ID, ga.ID).Count(&n)
	assert.Equal(t, int64(1), n)
}

func pageUsersPath(page models.StatusPage) string {
	return "/api/admin/pages/" + strconv.FormatUint(uint64(page.ID), 10) + "/users"
}

func TestAddStatusPageUserHandler_RejectsGlobalAdminRole(t *testing.T) {
	e := newUserGuardEnv(t)
	ga, _, page := globalAdminRoleFixture(t, e.db)
	u := e.user(t, "u@example.com", false)
	admin := e.user(t, "admin@example.com", true)

	w := e.do(t, http.MethodPost, pageUsersPath(page), admin.ID, map[string]any{"user_id": u.ID, "role_id": ga.ID})

	requireCode(t, w, http.StatusBadRequest, "global_admin_role_not_assignable")
	var n int64
	e.db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", u.ID).Count(&n)
	assert.Zero(t, n)
}

func TestAddStatusPageUserHandler_AcceptsNormalRole(t *testing.T) {
	e := newUserGuardEnv(t)
	_, editor, page := globalAdminRoleFixture(t, e.db)
	u := e.user(t, "u@example.com", false)
	admin := e.user(t, "admin@example.com", true)

	w := e.do(t, http.MethodPost, pageUsersPath(page), admin.ID, map[string]any{"user_id": u.ID, "role_id": editor.ID})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

// Konto per E-Mail (Global Admin) und Zugriff ohne Seitenrolle.
func TestAddStatusPageUserHandler_GlobalAdminByEmail(t *testing.T) {
	e := newUserGuardEnv(t)
	_, editor, page := globalAdminRoleFixture(t, e.db)
	u := e.user(t, "Mixed@Example.com", false)
	admin := e.user(t, "admin@example.com", true)

	w := e.do(t, http.MethodPost, pageUsersPath(page), admin.ID, map[string]any{"email": "mixed@example.com", "role_id": editor.ID})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var n int64
	e.db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", u.ID, page.ID).Count(&n)
	assert.Equal(t, int64(1), n)

	w = e.do(t, http.MethodPost, pageUsersPath(page), admin.ID, map[string]any{"email": "nobody@example.com", "role_id": editor.ID})
	requireCode(t, w, http.StatusNotFound, "user_not_found")

	w = e.do(t, http.MethodPost, pageUsersPath(page), admin.ID, map[string]any{"role_id": editor.ID})
	requireCode(t, w, http.StatusBadRequest, "user_email_required")
}

func TestAddStatusPageUserHandler_ActorWithoutPageRoleIsRejected(t *testing.T) {
	e := newUserGuardEnv(t)
	_, editor, page := globalAdminRoleFixture(t, e.db)
	u := e.user(t, "u@example.com", false)
	stranger := e.user(t, "stranger@example.com", false)

	// Unbekannter Aufrufer (kein Konto): wie ein Nicht-Admin ohne Rolle.
	w := e.do(t, http.MethodPost, pageUsersPath(page), 99999, map[string]any{"email": u.Email, "role_id": editor.ID})
	requireCode(t, w, http.StatusForbidden, "role_exceeds_own_permissions")
	// Bekanntes Konto ohne Rolle auf der Seite.
	w = e.do(t, http.MethodPost, pageUsersPath(page), stranger.ID, map[string]any{"email": u.Email, "role_id": editor.ID})
	requireCode(t, w, http.StatusForbidden, "role_exceeds_own_permissions")
	var n int64
	e.db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", u.ID).Count(&n)
	assert.Zero(t, n)
}
