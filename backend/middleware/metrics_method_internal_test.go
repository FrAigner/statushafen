package middleware

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The list of methods that get their own label is closed:
// an additional entry would let an attacker-chosen method become its own
// time series (memory growth). Add new methods here deliberately.
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
