package middleware

import (
	"errors"
	"os"
	"strings"

	"github.com/fraigner/statushafen/logging"
)

// minJWTSecretLength: HS256 braucht einen Schlüssel von der Länge des
// Hash-Ausgangs (32 Byte); kürzere Werte sind erratbar oder durchprobierbar,
// und wer den Schlüssel kennt, kann Tokens für jedes Konto fälschen.
const minJWTSecretLength = 32

// knownJWTSecretPlaceholders sind Werte aus Beispielkonfigurationen, früheren
// Rückfallwerten und Testaufbauten. Der Vergleich ist ein Teilstring-Treffer in
// Kleinschreibung, damit auch aufgefüllte Varianten ("<Platzhalter>-xyz") und
// Schreibweisen mit Leerraum oder Anführungszeichen auffallen.
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

// ValidateJWTSecret prüft den Signaturschlüssel auf Mindestlänge, bekannte
// Platzhalter und offensichtlich triviale Muster. Die Fehlermeldung nennt den
// Wert nie.
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

// RequireStrongJWTSecret beendet den Prozess beim Start, wenn JWT_SECRET
// schwach ist. Es gibt keine Ausnahme für Entwicklungsumgebungen: start.sh
// erzeugt dort selbst ein zufälliges Geheimnis.
func RequireStrongJWTSecret() {
	if err := ValidateJWTSecret(os.Getenv("JWT_SECRET")); err != nil {
		logging.Fatal("Ungültiges JWT_SECRET", "reason", err.Error(), "hint", "z. B. mit `openssl rand -base64 48` erzeugen")
	}
}
