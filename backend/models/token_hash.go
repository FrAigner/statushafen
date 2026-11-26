package models

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// TokenHashPrefix marks a stored token hash. A plaintext token
// (64 hex characters) and a SHA-256 hash (64 hex characters) otherwise look the same; with
// the marker, the conversion of existing plaintext values can be repeated
// any number of times without hashing hashes a second time.
const TokenHashPrefix = "sha256:"

// HashToken returns the stored form of a random token: SHA-256 with
// marker. A fast hash suffices because the tokens carry 256 bits of randomness
// (not a password, no dictionary attack). An empty token stays empty, so that
// "no token issued" continues to be stored as an empty value.
func HashToken(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return TokenHashPrefix + hex.EncodeToString(sum[:])
}

// IsTokenHash says whether the stored value is already a token hash.
func IsTokenHash(stored string) bool { return strings.HasPrefix(stored, TokenHashPrefix) }

// TokenMatches compares a presented plaintext token with the stored hash
// in constant time. Empty values never match; a stored plaintext (not
// migrated legacy value) does not match either.
func TokenMatches(stored, provided string) bool {
	if stored == "" || provided == "" || !IsTokenHash(stored) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(HashToken(provided))) == 1
}
