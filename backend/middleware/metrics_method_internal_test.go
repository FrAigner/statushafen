package middleware

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Die Liste der Methoden, die ein eigenes Label bekommen, ist abgeschlossen:
// Ein zusätzlicher Eintrag ließe eine angreifer-wählbare Methode als eigene
// Zeitreihe zu (Speicherwachstum). Neue Methoden bewusst hier eintragen.
func TestKnownMethods_AreExactlyTheStandardOnes(t *testing.T) {
	want := []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodHead, http.MethodOptions,
	}
	assert.Len(t, knownMethods, len(want))
	for _, m := range want {
		assert.Equal(t, m, methodLabel(m))
	}
	for _, m := range []string{"FOO", "get", "CONNECT", "TRACE", "PROPFIND", ""} {
		assert.Equal(t, otherMethodLabel, methodLabel(m), "%q", m)
	}
}
