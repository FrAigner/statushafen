package handlers

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

// Der Anzeigename einer Statusseite landet im From-Header: Steuerzeichen
// (CR/LF) verschwinden, Leerraum wird zusammengezogen und die Länge ist auf
// 100 Zeichen begrenzt, ohne ein Mehrbyte-Zeichen zu zerschneiden.
func TestSanitizeDisplayName(t *testing.T) {
	assert.Equal(t, "Acme Status Bcc: x@example.org", sanitizeDisplayName("Acme\r\nStatus\t Bcc: x@example.org"))
	assert.Equal(t, "", sanitizeDisplayName(" \r\n\t "))

	long := strings.Repeat("ä", 150)
	got := sanitizeDisplayName(long)
	assert.Equal(t, 100, utf8.RuneCountInString(got))
	assert.True(t, utf8.ValidString(got))

	// Genau 100 Zeichen bleiben unverändert.
	exact := strings.Repeat("b", 100)
	assert.Equal(t, exact, sanitizeDisplayName(exact))
}

// Auch Steuerzeichen, die kein Leerraum sind (NUL, ESC, DEL), werden zu
// Leerraum: Die Zusammenfassung des Leerraums allein erfasst sie nicht.
func TestSanitizeDisplayName_NonWhitespaceControlCharacters(t *testing.T) {
	assert.Equal(t, "Acme Status", sanitizeDisplayName("Acme\x00\x1b\x7fStatus"))
	assert.Equal(t, "A B", sanitizeDisplayName("A\x00B"))
}
