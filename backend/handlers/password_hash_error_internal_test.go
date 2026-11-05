package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
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
)

// Scheitert das Hashen, brechen alle Handler mit server_error ab: kein Konto
// ohne Passwort, keine geänderte Zeile und kein verbrauchter Bestelllink.
func TestPasswordHashFailure_HandlersAbortWithServerError(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/users", h.CreateUserHandler)
	r.PUT("/users/:id", h.UpdateUserHandler)
	r.POST("/set-password", h.SetOrderPasswordHandler)

	do := func(method, path string, body map[string]any) (int, map[string]any) {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	user := models.User{Email: "hashfehler@example.com", PasswordHash: "alt-hash"}
	require.NoError(t, db.Create(&user).Error)
	fresh := models.User{Email: "frisch-hash@example.com"}
	require.NoError(t, db.Create(&fresh).Error)
	expiry := time.Now().Add(time.Hour)
	order := models.PageOrder{CompanyName: "Acme", Email: fresh.Email, Slug: "hashfehler", Status: "confirmed",
		SetPasswordTokenHash: models.HashToken("link-hash-fehler"), SetPasswordTokenExpiry: &expiry, UserID: &fresh.ID}
	require.NoError(t, db.Create(&order).Error)

	orig := hashPassword
	hashPassword = func(string) (string, error) { return "", errors.New("hash kaputt") }
	t.Cleanup(func() { hashPassword = orig })

	code, out := do(http.MethodPost, "/users", map[string]any{"email": "neu-hash@example.com", "password": "langgenug-1"})
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Equal(t, "server_error", out["code"])
	var n int64
	db.Model(&models.User{}).Where("email = ?", "neu-hash@example.com").Count(&n)
	assert.Zero(t, n, "kein Konto ohne Passwort angelegt")

	code, out = do(http.MethodPut, "/users/"+strconv.FormatUint(uint64(user.ID), 10), map[string]any{"email": user.Email, "password": "langgenug-1"})
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Equal(t, "server_error", out["code"])
	require.NoError(t, db.First(&user, user.ID).Error)
	assert.Equal(t, "alt-hash", user.PasswordHash)
	assert.Zero(t, user.TokenVersion)

	code, out = do(http.MethodPost, "/set-password", map[string]any{"token": "link-hash-fehler", "password": "langgenug-1"})
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Equal(t, "server_error", out["code"])
	require.NoError(t, db.First(&order, order.ID).Error)
	assert.Equal(t, "confirmed", order.Status)
	assert.Equal(t, models.HashToken("link-hash-fehler"), order.SetPasswordTokenHash, "Bestelllink nicht verbraucht")
	require.NoError(t, db.First(&fresh, fresh.ID).Error)
	assert.Empty(t, fresh.PasswordHash)
}
