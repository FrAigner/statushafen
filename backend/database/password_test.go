package database

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// bcrypt processes at most 72 bytes. HashPassword must never return an empty
// hash as success for longer input.
func TestHashPassword_LimitIs72Bytes(t *testing.T) {
	ok := strings.Repeat("a", MaxPasswordBytes)
	hash, err := HashPassword(ok)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.True(t, CheckPasswordHash(ok, hash), "Gegenprobe: 72 Byte lassen sich hashen und prüfen")

	hash, err = HashPassword(ok + "a")
	assert.Empty(t, hash)
	assert.ErrorIs(t, err, ErrPasswordTooLong)
}

func TestHashPassword_CountsBytesNotRunes(t *testing.T) {
	// 36 times "ä" = 72 bytes, 37 times = 74 bytes.
	_, err := HashPassword(strings.Repeat("ä", 36))
	assert.NoError(t, err)
	_, err = HashPassword(strings.Repeat("ä", 37))
	assert.ErrorIs(t, err, ErrPasswordTooLong)
}

func TestHashPassword_EmptyHashNeverVerifies(t *testing.T) {
	assert.False(t, CheckPasswordHash("", ""))
	assert.False(t, CheckPasswordHash("egal", ""))
}

// The dummy hash for unknown accounts must have the same bcrypt cost as
// a real hash, otherwise the login runtime differs nevertheless.
func TestDummyPasswordHash_HasProductionCost(t *testing.T) {
	real, err := HashPassword("irgendein-passwort")
	require.NoError(t, err)
	realCost, err := bcrypt.Cost([]byte(real))
	require.NoError(t, err)
	dummyCost, err := bcrypt.Cost([]byte(dummyPasswordHash))
	require.NoError(t, err)
	assert.Equal(t, realCost, dummyCost)
}

func TestCheckPasswordConstantTime(t *testing.T) {
	hash, err := HashPassword("richtig-123")
	require.NoError(t, err)
	assert.True(t, CheckPasswordConstantTime("richtig-123", hash))
	assert.False(t, CheckPasswordConstantTime("falsch", hash))
	// Empty hash (unknown account, SSO account): never true, not even for an empty password.
	assert.False(t, CheckPasswordConstantTime("kein-echtes-konto-nur-zeitausgleich", ""))
	assert.False(t, CheckPasswordConstantTime("", ""))
}
