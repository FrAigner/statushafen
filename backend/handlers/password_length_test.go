package handlers_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Passwords over 72 bytes used to run into an empty bcrypt hash, which the
// handlers reported as success (account without password, order link consumed).
// Now validation rejects them with password_too_long.

func TestUserCreate_PasswordTooLongRejected(t *testing.T) {
	db, r := newInputValidationRouter(t)

	w := doStatusPageRequest(r, "POST", "/api/users", map[string]any{"email": "lang@example.com", "password": strings.Repeat("a", 73)})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "password_too_long")
	assert.Zero(t, count(db, &models.User{}, "email = ?", "lang@example.com"), "kein Konto bei zu langem Passwort")

	// Control case: exactly 72 bytes work, also with multi-byte characters (36 x "ä").
	for i, pw := range []string{strings.Repeat("a", 72), strings.Repeat("ä", 36)} {
		w = doStatusPageRequest(r, "POST", "/api/users", map[string]any{"email": "ok" + itoa(uint(i)) + "@example.com", "password": pw})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	}
	w = doStatusPageRequest(r, "POST", "/api/users", map[string]any{"email": "bytes@example.com", "password": strings.Repeat("ä", 37)})
	require.Equal(t, http.StatusBadRequest, w.Code, "Länge zählt in Bytes, nicht in Zeichen")
	requireErrorCode(t, w.Body.Bytes(), "password_too_long")
}

func TestUserUpdate_PasswordTooLongKeepsHash(t *testing.T) {
	db, r := newInputValidationRouter(t)
	user := models.User{Email: "u-lang@example.com", PasswordHash: "alt-hash"}
	require.NoError(t, db.Create(&user).Error)

	w := doStatusPageRequest(r, "PUT", "/api/users/"+itoa(user.ID), map[string]any{"email": user.Email, "password": strings.Repeat("a", 73)})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "password_too_long")
	require.NoError(t, db.First(&user, user.ID).Error)
	assert.Equal(t, "alt-hash", user.PasswordHash)
	assert.Zero(t, user.TokenVersion, "abgelehnte Änderung widerruft keine Sitzung")
}

func TestSetOrderPassword_TooLongKeepsLinkUsable(t *testing.T) {
	db, r := newInputValidationRouter(t)
	user := models.User{Email: "bestell-lang@example.com"}
	require.NoError(t, db.Create(&user).Error)
	expiry := time.Now().Add(time.Hour)
	order := models.PageOrder{CompanyName: "Acme", Email: user.Email, Slug: "bestell-lang", Status: "confirmed",
		SetPasswordTokenHash: models.HashToken("link-lang-72"), SetPasswordTokenExpiry: &expiry, UserID: &user.ID}
	require.NoError(t, db.Create(&order).Error)

	w := doStatusPageRequest(r, "POST", "/api/public/orders/set-password", map[string]any{"token": "link-lang-72", "password": strings.Repeat("a", 73)})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "password_too_long")

	require.NoError(t, db.First(&order, order.ID).Error)
	assert.Equal(t, "confirmed", order.Status)
	assert.Equal(t, models.HashToken("link-lang-72"), order.SetPasswordTokenHash, "Link bleibt benutzbar")
	require.NoError(t, db.First(&user, user.ID).Error)
	assert.Empty(t, user.PasswordHash)

	// Control case: with a matching password the same link works afterwards.
	w = doStatusPageRequest(r, "POST", "/api/public/orders/set-password", map[string]any{"token": "link-lang-72", "password": strings.Repeat("a", 72)})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, db.First(&user, user.ID).Error)
	assert.NotEmpty(t, user.PasswordHash)
}
