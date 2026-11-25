package middleware

import (
	"errors"
	"os"
	"strings"

	"github.com/fraigner/statushafen/logging"
)

// minJWTSecretLength: HS256 needs a key of the length of the
// hash output (32 bytes); shorter values are guessable or brute-forceable,
// and whoever knows the key can forge tokens for any account.
const minJWTSecretLength = 32

// knownJWTSecretPlaceholders are values from example configurations, former
// fallback values and test setups. The comparison is a lowercase substring
// match, so that padded variants ("<placeholder>-xyz") and
// spellings with whitespace or quotes are also caught.
var knownJWTSecretPlaceholders = []string{
	"your-super-secret-jwt-key",
	"local_super_secret_jwt_key",
	"test-jwt-secret",
	"changeme",
	"change_me",
	"change-me",
	"replace-me",
	"secret-key-here",
}

// ValidateJWTSecret checks the signing key for minimum length, known
// placeholders and obviously trivial patterns. The error message never names
// the value.
func ValidateJWTSecret(secret string) error {
	if secret == "" {
		return errors.New("JWT_SECRET ist nicht gesetzt")
	}
	normalized := strings.ToLower(strings.Trim(strings.TrimSpace(secret), `"'`))
	for _, p := range knownJWTSecretPlaceholders {
		if strings.Contains(normalized, p) {
			return errors.New("JWT_SECRET entspricht einem bekannten Platzhalter")
		}
	}
	if len(secret) < minJWTSecretLength {
		return errors.New("JWT_SECRET ist kürzer als 32 Zeichen")
	}
	if strings.Trim(secret, secret[:1]) == "" {
		return errors.New("JWT_SECRET besteht aus einem einzigen wiederholten Zeichen")
	}
	return nil
}

// RequireStrongJWTSecret terminates the process at startup if JWT_SECRET
// is weak. There is no exception for development environments: start.sh
// generates a random secret there itself.
func RequireStrongJWTSecret() {
	if err := ValidateJWTSecret(os.Getenv("JWT_SECRET")); err != nil {
		logging.Fatal("Ungültiges JWT_SECRET", "reason", err.Error(), "hint", "z. B. mit `openssl rand -base64 48` erzeugen")
	}
}
