package handlers_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Scheitert beim Ändern oder Löschen eines Kontos ein Schritt, rollt die
// Transaktion alles zurück: kein neues Passwort, keine halbe Rollenänderung,
// und die Token-Version bleibt unverändert (bestehende Sitzungen gelten weiter,
// weil nichts geändert wurde).
func TestUserUpdateDelete_FailureRollsBackIncludingTokenVersion(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	g := r.Group("/api/users", middleware.AuthMiddleware(), middleware.RequireGlobalAdmin())
	g.PUT("/:id", h.UpdateUserHandler)
	g.DELETE("/:id", h.DeleteUserHandler)

	admin := models.User{Email: "tv-admin@example.com", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&admin).Error)
	tok, err := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)
	require.NoError(t, err)
	page := models.StatusPage{Name: "P", Slug: "tv-fehler"}
	require.NoError(t, db.Create(&page).Error)
	role := models.Role{Name: "TV-Rolle", Permissions: "components:read"}
	require.NoError(t, db.Create(&role).Error)

	newTarget := func(email string) (models.User, string) {
		u := models.User{Email: email, PasswordHash: "alt-hash"}
		require.NoError(t, db.Create(&u).Error)
		return u, "/api/users/" + strconv.Itoa(int(u.ID))
	}
	reload := func(u models.User) models.User {
		var out models.User
		require.NoError(t, db.First(&out, u.ID).Error)
		return out
	}
	call := func(method, path string, body any) int {
		return idorRequest(t, r, method, path, tok, body).Code
	}

	t.Run("Seitenrolle anlegen scheitert", func(t *testing.T) {
		u, path := newTarget("rolle-fehler@example.com")
		failDB(t, db, "create", "user_status_page_roles", 0)
		code := call(http.MethodPut, path, map[string]any{"email": u.Email, "password": "neues-Passwort-1",
			"page_roles": []map[string]uint{{"status_page_id": page.ID, "role_id": role.ID}}})
		assert.Equal(t, http.StatusInternalServerError, code)
		got := reload(u)
		assert.Equal(t, "alt-hash", got.PasswordHash)
		assert.Zero(t, got.TokenVersion)
	})

	t.Run("Token-Version erhöhen scheitert beim Ändern", func(t *testing.T) {
		u, path := newTarget("bump-fehler@example.com")
		failDB(t, db, "update", "users", 1) // erstes Update (Felder) gelingt, das zweite (Version) scheitert
		code := call(http.MethodPut, path, map[string]any{"email": u.Email, "password": "neues-Passwort-1"})
		assert.Equal(t, http.StatusInternalServerError, code)
		got := reload(u)
		assert.Equal(t, "alt-hash", got.PasswordHash, "Passwortänderung zurückgerollt")
		assert.Zero(t, got.TokenVersion)
	})

	t.Run("Token-Version erhöhen scheitert beim Löschen", func(t *testing.T) {
		u, path := newTarget("loesch-fehler@example.com")
		failDB(t, db, "update", "users", 0)
		assert.Equal(t, http.StatusInternalServerError, call(http.MethodDelete, path, nil))
		assert.NoError(t, db.First(&models.User{}, u.ID).Error, "Konto bleibt bestehen")
	})
}
