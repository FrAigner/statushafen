package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ssoScopeFixture: V hat Rollen auf den privaten Seiten A und B
// und ist inzwischen Global Admin; die SSO-Sitzung gilt nur für Seite A.
type ssoScopeFixture struct {
	db           *gorm.DB
	h            *Handler
	v            models.User
	pageA, pageB models.StatusPage
}

func newSSOScopeFixture(t *testing.T) ssoScopeFixture {
	t.Helper()
	db := testutils.SetupTestDB()
	f := ssoScopeFixture{db: db, h: NewHandler(db)}
	role := models.Role{Name: "Page Admin", Permissions: "incidents:write"}
	require.NoError(t, db.Create(&role).Error)
	f.pageA = models.StatusPage{Name: "A", Slug: "scope-a", IsPrivate: true}
	f.pageB = models.StatusPage{Name: "B", Slug: "scope-b", IsPrivate: true}
	require.NoError(t, db.Create(&f.pageA).Error)
	require.NoError(t, db.Create(&f.pageB).Error)
	f.v = models.User{Email: "v@example.com", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&f.v).Error)
	for _, p := range []models.StatusPage{f.pageA, f.pageB} {
		require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: f.v.ID, StatusPageID: p.ID, RoleID: role.ID}).Error)
	}
	return f
}

// ctx baut einen Request-Kontext für V, mit Seitenbezug scope (0 = ohne).
func (f ssoScopeFixture) ctx(scope uint) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Set("userID", f.v.ID)
	c.Set("email", f.v.Email)
	c.Set("ssoPageID", scope)
	return c, w
}

func TestSSOPageScope_PrivatePageChecks(t *testing.T) {
	f := newSSOScopeFixture(t)

	c, _ := f.ctx(f.pageA.ID)
	_, _, okA := f.h.checkPrivatePageAccess(c, f.pageA)
	status, _, okB := f.h.checkPrivatePageAccess(c, f.pageB)
	assert.True(t, okA)
	assert.False(t, okB)
	assert.Equal(t, http.StatusForbidden, status)
	assert.True(t, f.h.userHasRoleOnPrivatePage(c, f.pageA.ID))
	assert.False(t, f.h.userHasRoleOnPrivatePage(c, f.pageB.ID))
	assert.True(t, f.h.userHasPermissionOnPrivatePage(c, f.pageA.ID, "incidents:write"))
	assert.False(t, f.h.userHasPermissionOnPrivatePage(c, f.pageB.ID, "incidents:write"))
	_, isAdmin := f.h.userTagsAndAdmin(c)
	assert.False(t, isAdmin, "Seiten-SSO-Sitzung ist nie Global Admin")

	// Ohne Seitenbezug wie bisher: Global Admin sieht alles.
	plain, _ := f.ctx(0)
	_, _, okB = f.h.checkPrivatePageAccess(plain, f.pageB)
	assert.True(t, okB)
	assert.True(t, f.h.userHasRoleOnPrivatePage(plain, f.pageB.ID))
	assert.True(t, f.h.userHasPermissionOnPrivatePage(plain, f.pageB.ID, "incidents:write"))
	_, isAdmin = f.h.userTagsAndAdmin(plain)
	assert.True(t, isAdmin)
}

// Auto-Join (DefaultRoleID) darf einer Seiten-SSO-Sitzung keine Rolle auf
// einer fremden Seite verschaffen.
func TestSSOPageScope_NoAutoJoinOnForeignPage(t *testing.T) {
	f := newSSOScopeFixture(t)
	viewer := models.Role{Name: "Viewer", Permissions: "incidents:read"}
	require.NoError(t, f.db.Create(&viewer).Error)
	pageC := models.StatusPage{Name: "C", Slug: "scope-c", IsPrivate: true, DefaultRoleID: &viewer.ID}
	require.NoError(t, f.db.Create(&pageC).Error)

	c, _ := f.ctx(f.pageA.ID)
	_, _, ok := f.h.checkPrivatePageAccess(c, pageC)
	assert.False(t, ok)
	var n int64
	f.db.Model(&models.UserStatusPageRole{}).Where("status_page_id = ?", pageC.ID).Count(&n)
	assert.Zero(t, n)
}

func TestSSOPageScope_MeAndMyPages(t *testing.T) {
	f := newSSOScopeFixture(t)

	c, w := f.ctx(f.pageA.ID)
	f.h.GetMyPagesHandler(c)
	require.Equal(t, http.StatusOK, w.Code)
	var pages []myPageEntry
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &pages))
	require.Len(t, pages, 1)
	assert.Equal(t, f.pageA.ID, pages[0].ID)
	assert.Equal(t, "Page Admin", pages[0].RoleName)

	c, w = f.ctx(f.pageA.ID)
	f.h.MeHandler(c)
	var me map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &me))
	assert.Equal(t, false, me["is_global_admin"])

	// Ohne Seitenbezug: alle Seiten als Global Admin.
	c, w = f.ctx(0)
	f.h.GetMyPagesHandler(c)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &pages))
	assert.Len(t, pages, 2)
	c, w = f.ctx(0)
	f.h.MeHandler(c)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &me))
	assert.Equal(t, true, me["is_global_admin"])
}

// Ohne Global Admin: die Rollenliste wird auf die SSO-Seite gefiltert.
func TestSSOPageScope_MyPagesFiltersRoles(t *testing.T) {
	f := newSSOScopeFixture(t)
	require.NoError(t, f.db.Model(&f.v).Update("is_global_admin", false).Error)

	for scope, want := range map[uint]int{f.pageB.ID: 1, 0: 2} {
		c, w := f.ctx(scope)
		f.h.GetMyPagesHandler(c)
		var pages []myPageEntry
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &pages))
		require.Len(t, pages, want)
		if scope != 0 {
			assert.Equal(t, f.pageB.ID, pages[0].ID)
		}
	}
}
