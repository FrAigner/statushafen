package database

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runInitDBWithSeed creates a database with an existing account and then starts
// InitDB with SEED_DB=true and the given APP_ENV. The result says
// whether the existing account still exists and whether the demo accounts were created.
func runInitDBWithSeed(t *testing.T, appEnv string) (sentinelKept, demoSeeded bool, logOut string) {
	t.Helper()
	return runInitDBWithSeedFlag(t, appEnv, "true")
}

// runInitDBWithSeedFlag like runInitDBWithSeed, with a freely selectable value of SEED_DB.
func runInitDBWithSeedFlag(t *testing.T, appEnv, seedFlag string) (sentinelKept, demoSeeded bool, logOut string) {
	t.Helper()
	prevDialector, prevDB := openDialector, DB
	t.Cleanup(func() { openDialector, DB = prevDialector, prevDB })
	openDialector = sqlite.Open
	t.Setenv("DB_DSN", filepath.Join(t.TempDir(), "seed.db"))
	t.Setenv("APP_ENV", "")
	t.Setenv("SEED_DB", "")
	InitDB()
	require.NoError(t, DB.Create(&models.User{Email: "bestand@example.com"}).Error)

	t.Setenv("SEED_DB", seedFlag)
	t.Setenv("APP_ENV", appEnv)
	out := captureLog(t)
	InitDB()

	var kept, demo int64
	require.NoError(t, DB.Model(&models.User{}).Where("email = ?", "bestand@example.com").Count(&kept).Error)
	require.NoError(t, DB.Model(&models.User{}).Where("email = ?", "admin@statushafen.local").Count(&demo).Error)
	return kept == 1, demo == 1, out.String()
}

// Seed and reset (deleting tables!) run only in an explicitly
// named development environment.
func TestInitDB_SeedOnlyInDevelopmentEnvironments(t *testing.T) {
	for _, env := range []string{"development", "dev", "test", "local", "Development", " LOCAL "} {
		t.Run("erlaubt: "+env, func(t *testing.T) {
			kept, demo, _ := runInitDBWithSeed(t, env)
			assert.False(t, kept, "Reset löscht die Bestandsdaten")
			assert.True(t, demo, "Demokonten angelegt")
		})
	}
}

// Even in the development environment only the explicit
// wish SEED_DB=true deletes and fills the database; without the variable or with another
// value the existing data stays untouched.
func TestInitDB_NoSeedWithoutExplicitFlag(t *testing.T) {
	for _, flag := range []string{"", "false", "1", "TRUE", "yes"} {
		t.Run("SEED_DB="+flag, func(t *testing.T) {
			kept, demo, _ := runInitDBWithSeedFlag(t, "development", flag)
			assert.True(t, kept, "Bestandsdaten bleiben unberührt")
			assert.False(t, demo, "keine Demokonten")
		})
	}
}

func TestInitDB_SeedBlockedWithoutExplicitDevelopmentEnvironment(t *testing.T) {
	for _, env := range []string{"", "production", "Production", "prod", "staging", "live", "devel", "testing"} {
		t.Run("gesperrt: "+env, func(t *testing.T) {
			kept, demo, logOut := runInitDBWithSeed(t, env)
			assert.True(t, kept, "Bestandsdaten bleiben unberührt")
			assert.False(t, demo, "keine Demokonten")
			assert.Contains(t, logOut, "SEED_DB wird ignoriert")
			assert.NotContains(t, logOut, "Dropping tables")
		})
	}
}

// If hashing the demo passwords fails, the seed creates no accounts without
// a password, but aborts.
func TestSeedData_AbortsWhenHashingFails(t *testing.T) {
	prevDialector, prevDB, prevHash := openDialector, DB, seedHashPassword
	t.Cleanup(func() { openDialector, DB, seedHashPassword = prevDialector, prevDB, prevHash })
	openDialector = sqlite.Open
	seedHashPassword = func(string) (string, error) { return "", errors.New("hash kaputt") }
	t.Setenv("DB_DSN", filepath.Join(t.TempDir(), "seedfail.db"))
	t.Setenv("APP_ENV", "test")
	t.Setenv("SEED_DB", "true")
	out := captureLog(t)

	InitDB()

	assert.Contains(t, out.String(), "Demo-Benutzer konnten nicht angelegt werden")
	var users int64
	require.NoError(t, DB.Model(&models.User{}).Count(&users).Error)
	assert.Zero(t, users)
}
