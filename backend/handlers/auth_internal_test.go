package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func TestLoginHandler_RejectsMalformedBody(t *testing.T) {
	h := NewHandler(testutils.SetupTestDB())
	for name, body := range map[string]string{"kein JSON": "{", "Passwort fehlt": `{"email":"a@example.com"}`, "E-Mail fehlt": `{"password":"pw-example"}`} {
		t.Run(name, func(t *testing.T) {
			c, w := pageTestContext(http.MethodPost, body, nil)
			h.LoginHandler(c)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), `"code":"invalid_request"`)
		})
	}
}

// /me gibt Rolle und Berechtigungen aus dem Token-Kontext zurück; fehlen sie
// oder haben einen falschen Typ, bleiben die Felder leer statt zu paniken.
func TestMeHandler_ReadsRoleAndPermissionsFromContext(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	user := models.User{Email: "me@example.com"}
	require.NoError(t, db.Create(&user).Error)

	decode := func(t *testing.T, body []byte) map[string]any {
		var out map[string]any
		require.NoError(t, json.Unmarshal(body, &out))
		return out
	}

	c, w := pageTestContext(http.MethodGet, "", user.ID)
	c.Set("email", user.Email)
	c.Set("role", "Editor")
	c.Set("permissions", "pages:read,incidents:write")
	h.MeHandler(c)
	require.Equal(t, http.StatusOK, w.Code)
	out := decode(t, w.Body.Bytes())
	assert.Equal(t, "Editor", out["role"])
	assert.Equal(t, "pages:read,incidents:write", out["permissions"])

	c, w = pageTestContext(http.MethodGet, "", user.ID)
	c.Set("email", user.Email)
	c.Set("role", 42)
	c.Set("permissions", []string{"x"})
	h.MeHandler(c)
	require.Equal(t, http.StatusOK, w.Code)
	out = decode(t, w.Body.Bytes())
	assert.Equal(t, "", out["role"])
	assert.Equal(t, "", out["permissions"])
}

func TestGetMyPagesHandler_UnknownUserIsUnauthorized(t *testing.T) {
	h := NewHandler(testutils.SetupTestDB())
	c, w := pageTestContext(http.MethodGet, "", uint(9999))
	h.GetMyPagesHandler(c)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"unauthorized"`)
}
