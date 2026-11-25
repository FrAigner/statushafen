package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// PASSWORD_LOGIN_ENABLED switches the password login. Off (the default)
// means: POST /api/login and every endpoint that sets or changes a login
// password answer 403 password_login_disabled.

func TestPasswordLoginEnabled_ParsesTheSwitch(t *testing.T) {
	for value, want := range map[string]bool{
		"": false, "false": false, "0": false, "no": false, "off": false, "on": false, "enabled": false,
		"true": true, "TRUE": true, "True": true, "1": true, "yes": true, "YES": true, " yes ": true,
	} {
		t.Setenv("PASSWORD_LOGIN_ENABLED", value)
		assert.Equal(t, want, passwordLoginEnabled(), "PASSWORD_LOGIN_ENABLED=%q", value)
	}
}

type passwordEnv struct {
	db    *gorm.DB
	h     *Handler
	r     *gin.Engine
	admin models.User
	user  models.User
}

// Test passwords are generated per run instead of being string literals, so secret scanners
// (GitGuardian) do not flag them as hardcoded credentials.
var (
	passwordEnvSecret = rand.Text()
	otherTestPassword = rand.Text()
)

func newPasswordEnv(t *testing.T) passwordEnv {
	t.Helper()
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	role := models.Role{Name: "Viewer", Permissions: "incidents:read"}
	require.NoError(t, db.Create(&role).Error)
	pageAdmin := models.Role{Name: "Page Admin", Permissions: "pages:update"}
	require.NoError(t, db.Create(&pageAdmin).Error)
	admin := models.User{Email: "admin@example.org", IsGlobalAdmin: true, PasswordHash: testutils.MustHash(passwordEnvSecret)}
	require.NoError(t, db.Create(&admin).Error)
	user := models.User{Email: "nutzer@example.org", RoleID: role.ID, PasswordHash: testutils.MustHash(passwordEnvSecret)}
	require.NoError(t, db.Create(&user).Error)
	r := testutils.SetupRouter()
	asAdmin := func(c *gin.Context) { c.Set("userID", admin.ID) }
	r.POST("/api/login", h.LoginHandler)
	r.POST("/api/users", asAdmin, h.CreateUserHandler)
	r.PUT("/api/users/:id", asAdmin, h.UpdateUserHandler)
	r.POST("/api/public/orders/confirm", h.ConfirmOrderHandler)
	r.POST("/api/public/orders/set-password", h.SetOrderPasswordHandler)
	r.GET("/api/public/auth-config", h.GetAuthConfigHandler)
	return passwordEnv{db: db, h: h, r: r, admin: admin, user: user}
}

func (e passwordEnv) do(method, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func assertPasswordDisabled(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"code":"password_login_disabled"`)
}

func TestPasswordLoginDisabled_LoginIsRefusedEvenWithTheRightPassword(t *testing.T) {
	t.Setenv("PASSWORD_LOGIN_ENABLED", "")
	e := newPasswordEnv(t)

	assertPasswordDisabled(t, e.do(http.MethodPost, "/api/login", map[string]string{"email": e.user.Email, "password": passwordEnvSecret}))
	assertPasswordDisabled(t, e.do(http.MethodPost, "/api/login", map[string]string{}))
	var n int64
	e.db.Model(&models.AuditLog{}).Where("action = ?", "auth.login").Count(&n)
	assert.Zero(t, n)

	// Counter-check: switched on, the same request signs in.
	t.Setenv("PASSWORD_LOGIN_ENABLED", "true")
	w := e.do(http.MethodPost, "/api/login", map[string]string{"email": e.user.Email, "password": passwordEnvSecret})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestPasswordLoginDisabled_AdminUserManagement(t *testing.T) {
	t.Setenv("PASSWORD_LOGIN_ENABLED", "false")
	e := newPasswordEnv(t)

	// Creating a user with a password is refused ...
	assertPasswordDisabled(t, e.do(http.MethodPost, "/api/users", map[string]any{"email": "neu@example.org", "password": otherTestPassword}))
	var n int64
	e.db.Model(&models.User{}).Where("email = ?", "neu@example.org").Count(&n)
	assert.Zero(t, n)
	// ... without a password the account is created and signs in via OIDC.
	w := e.do(http.MethodPost, "/api/users", map[string]any{"email": "neu@example.org"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created models.User
	require.NoError(t, e.db.Where("email = ?", "neu@example.org").First(&created).Error)
	assert.Empty(t, created.PasswordHash)

	// Changing a password is refused, other changes still work.
	path := "/api/users/" + strconv.Itoa(int(e.user.ID))
	assertPasswordDisabled(t, e.do(http.MethodPut, path, map[string]any{"email": e.user.Email, "password": "neues-passwort-1"}))
	var stored models.User
	require.NoError(t, e.db.First(&stored, e.user.ID).Error)
	assert.Equal(t, e.user.PasswordHash, stored.PasswordHash)
	w = e.do(http.MethodPut, path, map[string]any{"email": "umbenannt@example.org"})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestPasswordLoginEnabled_CreateUserStillNeedsAPassword(t *testing.T) {
	t.Setenv("PASSWORD_LOGIN_ENABLED", "true")
	e := newPasswordEnv(t)
	w := e.do(http.MethodPost, "/api/users", map[string]any{"email": "neu@example.org"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"invalid_request"`)
}

func createConfirmableOrder(t *testing.T, db *gorm.DB, email string) string {
	t.Helper()
	token := randomToken(16)
	expiry := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&models.PageOrder{
		CompanyName: "Firma", Email: email, Status: "pending_confirmation",
		ConfirmTokenHash: models.HashToken(token), ConfirmTokenExpiry: &expiry,
	}).Error)
	return token
}

// Without password login the self-service order ends at confirmation: the
// account exists without a password and the order is active; its owner
// signs in through a global OIDC provider with the ordered e-mail address.
func TestPasswordLoginDisabled_OrderIsActivatedWithoutPassword(t *testing.T) {
	t.Setenv("PASSWORD_LOGIN_ENABLED", "")
	e := newPasswordEnv(t)
	token := createConfirmableOrder(t, e.db, "besteller@example.org")

	w := e.do(http.MethodPost, "/api/public/orders/confirm", map[string]string{"token": token})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "", body["set_password_token"])
	assert.NotEmpty(t, body["slug"])
	var order models.PageOrder
	require.NoError(t, e.db.First(&order).Error)
	assert.Equal(t, "active", order.Status)
	assert.Empty(t, order.SetPasswordTokenHash)
	var user models.User
	require.NoError(t, e.db.Where("email = ?", "besteller@example.org").First(&user).Error)
	assert.Empty(t, user.PasswordHash)

	assertPasswordDisabled(t, e.do(http.MethodPost, "/api/public/orders/set-password", map[string]string{"token": "egal", "password": otherTestPassword}))
}

func TestPasswordLoginEnabled_OrderKeepsTheSetPasswordStep(t *testing.T) {
	t.Setenv("PASSWORD_LOGIN_ENABLED", "yes")
	e := newPasswordEnv(t)
	token := createConfirmableOrder(t, e.db, "besteller@example.org")

	w := e.do(http.MethodPost, "/api/public/orders/confirm", map[string]string{"token": token})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotEmpty(t, body["set_password_token"])
	w = e.do(http.MethodPost, "/api/public/orders/set-password", map[string]string{"token": body["set_password_token"], "password": otherTestPassword})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestAuthConfig_ReportsThePasswordSwitch(t *testing.T) {
	e := newPasswordEnv(t)
	for value, want := range map[string]string{"": `"password_login":false`, "true": `"password_login":true`} {
		t.Setenv("PASSWORD_LOGIN_ENABLED", value)
		w := e.do(http.MethodGet, "/api/public/auth-config", nil)
		assert.Contains(t, w.Body.String(), want)
	}
}

func TestWarnAuthConfig_NoSignInMethod(t *testing.T) {
	t.Run("no provider and no password login", func(t *testing.T) {
		t.Setenv("OIDC_PROVIDERS", "")
		t.Setenv("PASSWORD_LOGIN_ENABLED", "")
		buf := captureDefaultLog(t)
		WarnAuthConfig()
		assert.Contains(t, buf.String(), "No sign-in method is active")
	})
	t.Run("password login on", func(t *testing.T) {
		t.Setenv("OIDC_PROVIDERS", "")
		t.Setenv("PASSWORD_LOGIN_ENABLED", "true")
		buf := captureDefaultLog(t)
		WarnAuthConfig()
		assert.Empty(t, buf.String())
	})
	t.Run("provider configured", func(t *testing.T) {
		t.Setenv("PASSWORD_LOGIN_ENABLED", "")
		t.Setenv("OIDC_PROVIDERS", "gitlab")
		setProvider(t, "GITLAB", "https://gitlab.example.org")
		buf := captureDefaultLog(t)
		WarnAuthConfig()
		assert.Empty(t, buf.String())
	})
}
