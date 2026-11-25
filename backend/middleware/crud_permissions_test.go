package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// create/update/delete include read, but not each other;
// the legacy value write (non-migrated role) still counts when checking as
// create+update+delete+read; a legacy token with a write claim grants nothing on its own
// (permissions always come fresh from the role in the database).
func TestRequirePermission_CrudAktionen(t *testing.T) {
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "P", Slug: "p"}
	other := models.StatusPage{Name: "Q", Slug: "q"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&other).Error)

	users := map[string]uint{}
	for name, perms := range map[string]string{
		"create": "notices:create",
		"update": "notices:update",
		"delete": "notices:delete",
		"read":   "notices:read",
		"legacy": "notices:write",
		"none":   "components:read",
	} {
		role := models.Role{Name: "R-" + name, Permissions: perms}
		require.NoError(t, db.Create(&role).Error)
		u := models.User{Email: name + "@example.com"}
		require.NoError(t, db.Create(&u).Error)
		require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: role.ID}).Error)
		users[name] = u.ID
	}

	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) {
		uid, _ := strconv.Atoi(c.GetHeader("X-Mock-User-ID"))
		c.Set("userID", uint(uid))
		c.Next()
	})
	ok := func(c *gin.Context) { c.String(http.StatusOK, "ok") }
	for _, act := range []string{"create", "read", "update", "delete"} {
		r.GET("/"+act+"/:page_id", middleware.RequirePermission("notices:"+act), ok)
	}

	id := func(v uint) string { return strconv.Itoa(int(v)) }
	// Allowed per role and requested action; everything else 403.
	allowed := map[string][]string{
		"create": {"create", "read"},
		"update": {"update", "read"},
		"delete": {"delete", "read"},
		"read":   {"read"},
		"legacy": {"create", "read", "update", "delete"},
		"none":   {},
	}
	for user, uid := range users {
		for _, act := range []string{"create", "read", "update", "delete"} {
			want := http.StatusForbidden
			for _, a := range allowed[user] {
				if a == act {
					want = http.StatusOK
				}
			}
			req := httptest.NewRequest(http.MethodGet, "/"+act+"/"+id(page.ID), nil)
			req.Header.Set("X-Mock-User-ID", id(uid))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, want, w.Code, "Rolle %s verlangt %s", user, act)

			// Tenant separation: the same permission does not apply on the other page.
			req = httptest.NewRequest(http.MethodGet, "/"+act+"/"+id(other.ID), nil)
			req.Header.Set("X-Mock-User-ID", id(uid))
			w = httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code, "fremde Seite, Rolle %s verlangt %s", user, act)
		}
	}
}

func TestRequireAllPermissions_UndResolved(t *testing.T) {
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "P", Slug: "p"}
	require.NoError(t, db.Create(&page).Error)
	mk := func(name, perms string) uint {
		role := models.Role{Name: name, Permissions: perms}
		require.NoError(t, db.Create(&role).Error)
		u := models.User{Email: name + "@example.com"}
		require.NoError(t, db.Create(&u).Error)
		require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: role.ID}).Error)
		return u.ID
	}
	both := mk("both", "external_sources:create,components:create")
	one := mk("one", "external_sources:create")
	maint := mk("maint", "maintenance:update")

	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) {
		uid, _ := strconv.Atoi(c.GetHeader("X-Mock-User-ID"))
		c.Set("userID", uint(uid))
		c.Next()
	})
	ok := func(c *gin.Context) { c.String(http.StatusOK, "ok") }
	r.GET("/all/:page_id", middleware.RequireAllPermissions("external_sources:create", "components:create"), ok)
	r.GET("/kind/:page_id", middleware.RequireResolvedPermission("update", func(c *gin.Context) string { return c.Query("res") }), ok)

	do := func(path string, uid uint) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Mock-User-ID", strconv.Itoa(int(uid)))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	p := strconv.Itoa(int(page.ID))
	assert.Equal(t, http.StatusOK, do("/all/"+p, both))
	assert.Equal(t, http.StatusForbidden, do("/all/"+p, one), "nur eines der beiden Rechte")
	assert.Equal(t, http.StatusOK, do("/kind/"+p+"?res=maintenance", maint))
	assert.Equal(t, http.StatusForbidden, do("/kind/"+p+"?res=incidents", maint), "Ressource folgt der Art")
}

// Legacy token: a token with a write claim issued before the changeover
// remains valid until expiry (signature/expiry unchanged), but the permissions
// never come from the claim, only from the role in the database.
func TestAltToken_MitWriteClaimBleibtGueltigUndGewaehrtNichts(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "P", Slug: "p"}
	require.NoError(t, db.Create(&page).Error)
	role := models.Role{Name: "Nur-Lesen", Permissions: "components:read"}
	require.NoError(t, db.Create(&role).Error)
	u := models.User{Email: "alt@example.com"}
	require.NoError(t, db.Create(&u).Error)
	require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: role.ID}).Error)

	// Token as issued by the version before the changeover.
	token, err := middleware.GenerateToken(u.ID, u.Email, "Editor", "components:read,components:write,incidents:write", false)
	require.NoError(t, err)

	r := testutils.SetupRouter()
	auth := r.Group("/api")
	auth.Use(middleware.AuthMiddleware())
	ok := func(c *gin.Context) { c.String(http.StatusOK, "ok") }
	auth.GET("/read/:page_id", middleware.RequirePermission("components:read"), ok)
	auth.GET("/create/:page_id", middleware.RequirePermission("components:create"), ok)

	do := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Auth-Token", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	p := strconv.Itoa(int(page.ID))
	assert.Equal(t, http.StatusOK, do("/api/read/"+p), "Alt-Token bleibt gültig")
	assert.Equal(t, http.StatusForbidden, do("/api/create/"+p), "der write-Claim im Token gewährt nichts")
}
