package handlers

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

// The display name of a status page ends up in the From header: control characters
// (CR/LF) disappear, whitespace is collapsed and the length is limited to
// 100 characters without cutting a multi-byte character apart.
func TestSanitizeDisplayName(t *testing.T) {
	assert.Equal(t, "Acme Status Bcc: x@example.org", sanitizeDisplayName("Acme\r\nStatus\t Bcc: x@example.org"))
	assert.Equal(t, "", sanitizeDisplayName(" \r\n\t "))

	long := strings.Repeat("ä", 150)
	got := sanitizeDisplayName(long)
	assert.Equal(t, 100, utf8.RuneCountInString(got))
	assert.True(t, utf8.ValidString(got))

	// Exactly 100 characters remain unchanged.
	exact := strings.Repeat("b", 100)
	assert.Equal(t, exact, sanitizeDisplayName(exact))
}

// Control characters that are not whitespace (NUL, ESC, DEL) also become
// whitespace: collapsing whitespace alone does not catch them.
func TestSanitizeDisplayName_NonWhitespaceControlCharacters(t *testing.T) {
	assert.Equal(t, "Acme Status", sanitizeDisplayName("Acme\x00\x1b\x7fStatus"))
	assert.Equal(t, "A B", sanitizeDisplayName("A\x00B"))
}
