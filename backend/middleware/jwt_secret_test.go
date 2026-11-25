package middleware_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/logging/loggingtest"
	"github.com/fraigner/statushafen/middleware"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A missing JWT_SECRET terminates the process via logging.Fatal
// (logged in structured form) instead of log.Fatalf.
func TestGenerateToken_MissingJWTSecretIsFatal(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	got := loggingtest.ExpectFatal(t, func() {
		_, _ = middleware.GenerateToken(1, "a@example.com", "Viewer", "", false)
	})
	assert.Equal(t, 1, got.Code)
	assert.Equal(t, "JWT_SECRET environment variable is required but not set", got.Message)
}

// strongTestSecret is an invented, valid value (40 characters, deliberately without high entropy,
// so that secret scanners do not report it as a real key).
var strongTestSecret = strings.Repeat("Ab1$xY", 7)

// Weak signing keys allow forged login tokens for any account.
// Startup therefore aborts on values that are too short and on known placeholders.
func TestValidateJWTSecret(t *testing.T) {
	strong := strongTestSecret
	cases := []struct {
		name    string
		secret  string
		wantErr string
	}{
		{"stark", strong, ""},
		{"genau 32 Zeichen", strings.Repeat("aB3$", 8), ""},
		{"leer", "", "nicht gesetzt"},
		{"ein Zeichen", "a", "kürzer als 32"},
		{"31 Zeichen", strings.Repeat("aB3$", 7) + "xyz", "kürzer als 32"},
		{"Platzhalter der Beispielkonfiguration", "your-super-secret-jwt-key", "Platzhalter"},
		{"Platzhalter aufgefüllt", "your-super-secret-jwt-key-und-noch-viel-mehr-text", "Platzhalter"},
		{"früherer Fallback", "local_super_secret_jwt_key_12345", "Platzhalter"},
		{"Platzhalter in anderer Schreibweise", "  LOCAL_SUPER_SECRET_JWT_KEY_12345  ", "Platzhalter"},
		{"Testwert", "test-jwt-secret-not-for-production", "Platzhalter"},
		{"changeme lang", "changeme-changeme-changeme-changeme", "Platzhalter"},
		{"Platzhalter change_me", "change_me-Ab1$xY-Ab1$xY-Ab1$xY-Ab1$xY", "Platzhalter"},
		{"Platzhalter change-me", "change-me-Ab1$xY-Ab1$xY-Ab1$xY-Ab1$xY", "Platzhalter"},
		{"Platzhalter replace-me", "replace-me-Ab1$xY-Ab1$xY-Ab1$xY-Ab1$xY", "Platzhalter"},
		{"Platzhalter secret-key-here", "secret-key-here-Ab1$xY-Ab1$xY-Ab1$xY", "Platzhalter"},
		{"Platzhalter in Anführungszeichen", `"changeme-Ab1$xY-Ab1$xY-Ab1$xY-Ab1$xY"`, "Platzhalter"},
		{"nur ein Zeichen wiederholt", strings.Repeat("x", 64), "wiederholt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := middleware.ValidateJWTSecret(tc.secret)
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			if len(strings.TrimSpace(tc.secret)) > 8 {
				assert.NotContains(t, err.Error(), strings.TrimSpace(tc.secret), "der Wert gehört nicht in die Fehlermeldung")
			}
		})
	}
}

func TestRequireStrongJWTSecret_FatalOnWeakSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "your-super-secret-jwt-key")
	got := loggingtest.ExpectFatal(t, middleware.RequireStrongJWTSecret)
	assert.Equal(t, 1, got.Code)
	assert.Equal(t, "Ungültiges JWT_SECRET", got.Message)
	assert.NotContains(t, fmt.Sprint(got.Attrs), "your-super-secret-jwt-key")
}

func TestRequireStrongJWTSecret_StrongSecretPasses(t *testing.T) {
	t.Setenv("JWT_SECRET", strongTestSecret)
	assert.NotPanics(t, middleware.RequireStrongJWTSecret)
}

// The example configuration must not contain a usable value: a
// copied placeholder would otherwise be a valid production key.
func TestEnvExample_HatKeinenGueltigenJWTSecretWert(t *testing.T) {
	raw, err := os.ReadFile("../.env.example")
	require.NoError(t, err)
	found := false
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "JWT_SECRET="); ok {
			found = true
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			assert.Empty(t, v, "JWT_SECRET in .env.example muss leer bleiben")
		}
	}
	assert.True(t, found, "JWT_SECRET= fehlt in .env.example")
	assert.Contains(t, string(raw), "openssl rand -base64 48")
}
