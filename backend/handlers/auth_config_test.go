package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /api/public/auth-config tells the login page which sign-in methods
// exist: only active providers, in OIDC_PROVIDERS order, never issuer,
// client ID or secret.
func TestAuthConfig_ListsActiveProvidersInOrder(t *testing.T) {
	t.Setenv("OIDC_PROVIDERS", "zeta,broken,alpha")
	setProvider(t, "ZETA", "https://zeta.example.org")
	t.Setenv("OIDC_ZETA_NAME", "Zeta SSO")
	setProvider(t, "ALPHA", "https://alpha.example.org")
	h := NewHandler(testutils.SetupTestDB())

	w := serveOIDC(h.GetAuthConfigHandler, http.MethodGet, "/api/public/auth-config", nil, 0)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		PasswordLogin *bool               `json:"password_login"`
		Providers     []map[string]string `json:"providers"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotNil(t, body.PasswordLogin)
	assert.Equal(t, []map[string]string{{"id": "zeta", "name": "Zeta SSO"}, {"id": "alpha", "name": "alpha"}}, body.Providers)
	assert.NotContains(t, w.Body.String(), "example.org")
	assert.NotContains(t, w.Body.String(), "secret")
}

func TestAuthConfig_NoProvidersIsAnEmptyList(t *testing.T) {
	t.Setenv("OIDC_PROVIDERS", "")
	h := NewHandler(testutils.SetupTestDB())
	w := serveOIDC(h.GetAuthConfigHandler, http.MethodGet, "/api/public/auth-config", nil, 0)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"providers":[]`)
}
