package middleware_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Ein NUL-Byte gilt an jeder Stelle des Bodys, auch als allererstes Byte.
func TestRejectInvalidCharacters_NULAsFirstByteIsRejected(t *testing.T) {
	for name, body := range map[string]string{
		"nur NUL":      "\x00",
		"NUL vor JSON": "\x00{\"a\":1}",
	} {
		t.Run(name, func(t *testing.T) {
			probe := &guardProbe{}
			w := guardCall(guardRouter(probe, 1<<20), http.MethodPost, "/x/1", body, "application/json")
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.False(t, probe.reached)
		})
	}
}
