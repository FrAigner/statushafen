package models

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// TokenHashPrefix kennzeichnet einen gespeicherten Token-Hash. Klartext-Token
// (64 Hex-Zeichen) und SHA-256-Hash (64 Hex-Zeichen) sehen sonst gleich aus; mit
// der Marke lässt sich die Umstellung bestehender Klartextwerte beliebig oft
// wiederholen, ohne Hashes ein zweites Mal zu hashen.
const TokenHashPrefix = "sha256:"

// HashToken liefert die gespeicherte Form eines Zufalls-Tokens: SHA-256 mit
// Marke. Ein schneller Hash genügt, weil die Tokens 256 Bit Zufall tragen
// (kein Passwort, kein Wörterbuchangriff). Ein leerer Token bleibt leer, damit
// "kein Token vergeben" weiter als leerer Wert gespeichert wird.
func HashToken(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return TokenHashPrefix + hex.EncodeToString(sum[:])
}

// IsTokenHash sagt, ob der gespeicherte Wert schon ein Token-Hash ist.
func IsTokenHash(stored string) bool { return strings.HasPrefix(stored, TokenHashPrefix) }

// TokenMatches vergleicht einen vorgelegten Klartext-Token zeitkonstant mit dem
// gespeicherten Hash. Leere Werte passen nie; ein gespeicherter Klartext (nicht
// migrierter Altwert) ebenfalls nicht.
func TokenMatches(stored, provided string) bool {
	if stored == "" || provided == "" || !IsTokenHash(stored) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(HashToken(provided))) == 1
}
