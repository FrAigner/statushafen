package main

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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type revocationFixture struct {
	db       *gorm.DB
	r        *gin.Engine
	adminTok string
	victim   models.User
	victimTk string
	page     models.StatusPage
	role     models.Role
}

func newRevocationFixture(t *testing.T) *revocationFixture {
	t.Helper()
	db := testutils.SetupTestDB()
	f := &revocationFixture{db: db, r: newRouter(handlers.NewHandler(db))}

	admin := models.User{Email: "admin@example.com", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&admin).Error)
	var err error
	f.adminTok, err = middleware.GenerateToken(admin.ID, admin.Email, "", "", true)
	require.NoError(t, err)

	f.victim = models.User{Email: "opfer@example.com", PasswordHash: testutils.MustHash("AltesPasswort123")}
	require.NoError(t, db.Create(&f.victim).Error)
	f.victimTk, err = middleware.GenerateToken(f.victim.ID, f.victim.Email, "", "", false)
	require.NoError(t, err)

	f.page = models.StatusPage{Name: "P", Slug: "p-revoke"}
	require.NoError(t, db.Create(&f.page).Error)
	f.role = models.Role{Name: "Beobachter", Permissions: "subscribers:read"}
	require.NoError(t, db.Create(&f.role).Error)
	return f
}

func (f *revocationFixture) do(method, path, token string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(middleware.AuthTokenHeader, token)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

// victimRoutes are routes with and without their own database check: /api/me and
// /api/languages used to read only the token.
func (f *revocationFixture) victimStatus(token string) map[string]int {
	out := map[string]int{}
	for _, p := range []string{"/api/me", "/api/languages", "/api/me/pages"} {
		out[p] = f.do(http.MethodGet, p, token, nil).Code
	}
	return out
}

func (f *revocationFixture) updateVictim(t *testing.T, body map[string]any) {
	t.Helper()
	body["email"] = f.victim.Email
	w := f.do(http.MethodPut, "/api/users/"+strconv.Itoa(int(f.victim.ID)), f.adminTok, body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// changedTestPass: made-up test password for the password change.
const changedTestPass = "GanzNeuesPasswort99"

func TestTokenRevocation_AllRoutesRejectOldTokenAfterPasswordChange(t *testing.T) {
	f := newRevocationFixture(t)
	// Control case: before the change the token is valid everywhere.
	for p, code := range f.victimStatus(f.victimTk) {
		require.Equal(t, http.StatusOK, code, p)
	}

	f.updateVictim(t, map[string]any{"password": changedTestPass})

	for p, code := range f.victimStatus(f.victimTk) {
		assert.Equal(t, http.StatusUnauthorized, code, "%s nach Passwortwechsel", p)
	}

	// A new login issues a token with the new version.
	w := f.do(http.MethodPost, "/api/login", "", map[string]string{"email": f.victim.Email, "password": changedTestPass})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	for p, code := range f.victimStatus(resp.Token) {
		assert.Equal(t, http.StatusOK, code, "%s mit frischem Token", p)
	}
}

func TestTokenRevocation_GlobalAdminDemotionRevokesButPageRoleChangeKeepsSession(t *testing.T) {
	f := newRevocationFixture(t)
	require.NoError(t, f.db.Model(&f.victim).Update("is_global_admin", true).Error)

	// Revoking Global Admin rights revokes the token.
	f.updateVictim(t, map[string]any{"is_global_admin": false})
	assert.Equal(t, http.StatusUnauthorized, f.do(http.MethodGet, "/api/me", f.victimTk, nil).Code)

	// Page roles are read from the database on every request; a
	// change takes effect immediately and must not end the session (otherwise a
	// simultaneous deletion of the own account would fail with 401).
	var v models.User
	require.NoError(t, f.db.First(&v, f.victim.ID).Error)
	tok, err := middleware.GenerateToken(v.ID, v.Email, "", "", false, middleware.WithTokenVersion(v.TokenVersion))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, f.do(http.MethodGet, "/api/me", tok, nil).Code)
	f.updateVictim(t, map[string]any{"page_roles": []map[string]uint{{"status_page_id": f.page.ID, "role_id": f.role.ID}}})
	assert.Equal(t, http.StatusOK, f.do(http.MethodGet, "/api/me", tok, nil).Code)
}

func TestTokenRevocation_UnrelatedUpdateKeepsSession(t *testing.T) {
	f := newRevocationFixture(t)
	// Only the email spelling of the display changes, no password,
	// no permissions: the session should persist.
	f.updateVictim(t, map[string]any{})
	assert.Equal(t, http.StatusOK, f.do(http.MethodGet, "/api/me", f.victimTk, nil).Code)
}

func TestTokenRevocation_AdminDeleteAndSelfDeleteRevoke(t *testing.T) {
	f := newRevocationFixture(t)
	w := f.do(http.MethodDelete, "/api/users/"+strconv.Itoa(int(f.victim.ID)), f.adminTok, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	for p, code := range f.victimStatus(f.victimTk) {
		assert.Equal(t, http.StatusUnauthorized, code, "%s nach Löschung durch Admin", p)
	}
	var gone models.User
	require.NoError(t, f.db.Unscoped().First(&gone, f.victim.ID).Error)
	assert.Positive(t, gone.TokenVersion, "Löschung erhöht die Token-Version")

	// Self-deletion: the account is gone, the token worthless afterwards.
	other := models.User{Email: "selbst@example.com"}
	require.NoError(t, f.db.Create(&other).Error)
	tok, err := middleware.GenerateToken(other.ID, other.Email, "", "", false)
	require.NoError(t, err)
	w = f.do(http.MethodDelete, "/api/me", tok, map[string]any{"confirm_email": other.Email})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, http.StatusUnauthorized, f.do(http.MethodGet, "/api/me", tok, nil).Code)
	assert.Equal(t, http.StatusUnauthorized, f.do(http.MethodGet, "/api/languages", tok, nil).Code)
}
