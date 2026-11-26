package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// revocationRouter attaches AuthMiddleware and OptionalAuthMiddleware to one
// route each, which only returns the detected user ID.
func revocationRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/strict", middleware.AuthMiddleware(), func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/optional", middleware.OptionalAuthMiddleware(), func(c *gin.Context) {
		if _, ok := c.Get("userID"); ok {
			c.String(http.StatusOK, "user")
			return
		}
		c.String(http.StatusOK, "anonymous")
	})
	return r
}

func callWithToken(r *gin.Engine, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set(middleware.AuthTokenHeader, token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAuthMiddleware_RejectsTokenAfterTokenVersionBump(t *testing.T) {
	db := testutils.SetupTestDB()
	u := models.User{Email: "tv@example.com"}
	require.NoError(t, db.Create(&u).Error)
	r := revocationRouter()

	old, err := middleware.GenerateToken(u.ID, u.Email, "", "", false)
	require.NoError(t, err)
	// Control check: before the change the token is valid.
	assert.Equal(t, http.StatusOK, callWithToken(r, "/strict", old).Code)

	require.NoError(t, db.Model(&u).Update("token_version", 1).Error)
	w := callWithToken(r, "/strict", old)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"unauthorized"`)

	// A newly issued token with the current version is valid again.
	require.NoError(t, db.First(&u, u.ID).Error)
	fresh, err := middleware.GenerateToken(u.ID, u.Email, "", "", false, middleware.WithTokenVersion(u.TokenVersion))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, callWithToken(r, "/strict", fresh).Code)
}

func TestAuthMiddleware_RejectsTokenOfDeletedOrUnknownUser(t *testing.T) {
	db := testutils.SetupTestDB()
	u := models.User{Email: "weg@example.com"}
	require.NoError(t, db.Create(&u).Error)
	r := revocationRouter()
	tok, err := middleware.GenerateToken(u.ID, u.Email, "", "", false)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, callWithToken(r, "/strict", tok).Code)

	require.NoError(t, db.Delete(&u).Error) // soft delete
	assert.Equal(t, http.StatusUnauthorized, callWithToken(r, "/strict", tok).Code)

	unknown, err := middleware.GenerateToken(999999, "nie@example.com", "", "", false)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, callWithToken(r, "/strict", unknown).Code)
}

func TestAuthMiddleware_FailsClosedOnDatabaseError(t *testing.T) {
	db := testutils.SetupTestDB()
	u := models.User{Email: "dbfehler@example.com"}
	require.NoError(t, db.Create(&u).Error)
	r := revocationRouter()
	tok, err := middleware.GenerateToken(u.ID, u.Email, "", "", false)
	require.NoError(t, err)

	require.NoError(t, db.Migrator().DropTable(&models.User{}))
	w := callWithToken(r, "/strict", tok)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"server_error"`)
	// Optional: on a DB error anonymous instead of with an unconfirmed identity.
	assert.Equal(t, "anonymous", callWithToken(r, "/optional", tok).Body.String())
}

func TestOptionalAuthMiddleware_IgnoresStaleToken(t *testing.T) {
	db := testutils.SetupTestDB()
	u := models.User{Email: "opt@example.com"}
	require.NoError(t, db.Create(&u).Error)
	r := revocationRouter()
	tok, err := middleware.GenerateToken(u.ID, u.Email, "", "", false)
	require.NoError(t, err)
	assert.Equal(t, "user", callWithToken(r, "/optional", tok).Body.String())

	require.NoError(t, db.Model(&u).Update("token_version", 5).Error)
	assert.Equal(t, "anonymous", callWithToken(r, "/optional", tok).Body.String())
}

func TestGenerateTokenFull_CarriesTokenVersion(t *testing.T) {
	db := testutils.SetupTestDB()
	u := models.User{Email: "full@example.com", TokenVersion: 3}
	require.NoError(t, db.Create(&u).Error)
	r := revocationRouter()

	sso, err := middleware.GenerateTokenFull(u, "", "Name", "google")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, callWithToken(r, "/strict", sso).Code)

	page, err := middleware.GeneratePageSSOToken(u, "Name", "oidc", 7)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, callWithToken(r, "/strict", page).Code)

	require.NoError(t, db.Model(&u).Update("token_version", 4).Error)
	assert.Equal(t, http.StatusUnauthorized, callWithToken(r, "/strict", sso).Code)
	assert.Equal(t, http.StatusUnauthorized, callWithToken(r, "/strict", page).Code)
}

// WithSSOPageID limits a password session to one page (custom domain login).
func TestGenerateToken_WithSSOPageIDLimitsTheSession(t *testing.T) {
	db := testutils.SetupTestDB()
	u := models.User{Email: "scoped@example.com"}
	require.NoError(t, db.Create(&u).Error)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/scope", middleware.AuthMiddleware(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"page": middleware.SSOPageID(c), "other": middleware.SSOAllowsPage(c, 8)})
	})
	scoped, err := middleware.GenerateToken(u.ID, u.Email, "", "", false, middleware.WithSSOPageID(7))
	require.NoError(t, err)
	assert.JSONEq(t, `{"page":7,"other":false}`, callWithToken(r, "/scope", scoped).Body.String())

	full, err := middleware.GenerateToken(u.ID, u.Email, "", "", false, middleware.WithSSOPageID(0))
	require.NoError(t, err)
	assert.JSONEq(t, `{"page":0,"other":true}`, callWithToken(r, "/scope", full).Body.String())
}
