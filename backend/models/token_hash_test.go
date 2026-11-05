package models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashToken(t *testing.T) {
	h := HashToken("geheim-token")
	assert.True(t, strings.HasPrefix(h, TokenHashPrefix))
	assert.Len(t, h, len(TokenHashPrefix)+64)
	assert.NotContains(t, h, "geheim-token")
	assert.Equal(t, h, HashToken("geheim-token"), "deterministisch")
	assert.NotEqual(t, h, HashToken("anderer-token"))
	assert.Empty(t, HashToken(""), "leer bleibt leer")
	assert.True(t, IsTokenHash(h))
	assert.False(t, IsTokenHash("a8a1f5fe93082f3320c0976e10f287c0c0814352a46cef8240873f1b1d589c04"), "Klartext ist kein Hash")
	assert.False(t, IsTokenHash(""))
}

func TestTokenMatches(t *testing.T) {
	stored := HashToken("tok-1")
	assert.True(t, TokenMatches(stored, "tok-1"))
	assert.False(t, TokenMatches(stored, "tok-2"))
	assert.False(t, TokenMatches(stored, stored), "der Hash ist als Token wertlos")
	assert.False(t, TokenMatches("", ""), "leer passt nie")
	assert.False(t, TokenMatches(stored, ""))
	assert.False(t, TokenMatches("", "tok-1"))
	assert.False(t, TokenMatches("tok-1", "tok-1"), "ein nicht umgestellter Klartextwert passt nie")
}
