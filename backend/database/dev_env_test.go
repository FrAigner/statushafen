package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Entwicklungsausnahmen (Seed, unsichere OIDC-Aussteller, Domain-Check ohne
// Token) gelten nur bei einer ausdrücklich benannten Entwicklungsumgebung.
// Jeder andere Wert, auch ein vergessener oder falsch geschriebener, gilt als
// Produktion (fail closed).
func TestIsDevelopmentAppEnv(t *testing.T) {
	for _, env := range []string{"development", "dev", "test", "local", "Development", " LOCAL "} {
		t.Run("Entwicklung: "+env, func(t *testing.T) {
			t.Setenv("APP_ENV", env)
			assert.True(t, IsDevelopmentAppEnv())
		})
	}
	for _, env := range []string{"", "production", "Production", "prod", "staging", "live", "devel", "testing"} {
		t.Run("keine Entwicklung: "+env, func(t *testing.T) {
			t.Setenv("APP_ENV", env)
			assert.False(t, IsDevelopmentAppEnv())
		})
	}
}
