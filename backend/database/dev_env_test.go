package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Development exceptions (seed, insecure OIDC issuers, domain check without
// token) apply only in an explicitly named development environment.
// Any other value, even a forgotten or misspelled one, counts as
// production (fail closed).
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
