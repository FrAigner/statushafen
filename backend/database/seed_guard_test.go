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

// runInitDBWithSeed legt eine Datenbank mit einem Bestandskonto an und startet
// InitDB danach mit SEED_DB=true und dem gegebenen APP_ENV. Das Ergebnis sagt,
// ob das Bestandskonto noch existiert und ob die Demokonten angelegt wurden.
func runInitDBWithSeed(t *testing.T, appEnv string) (sentinelKept, demoSeeded bool, logOut string) {
	t.Helper()
	return runInitDBWithSeedFlag(t, appEnv, "true")
}

// runInitDBWithSeedFlag wie runInitDBWithSeed, mit frei wählbarem Wert von SEED_DB.
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

// Seed und Reset (Tabellen löschen!) laufen nur in einer ausdrücklich
// benannten Entwicklungsumgebung.
func TestInitDB_SeedOnlyInDevelopmentEnvironments(t *testing.T) {
	for _, env := range []string{"development", "dev", "test", "local", "Development", " LOCAL "} {
		t.Run("erlaubt: "+env, func(t *testing.T) {
			kept, demo, _ := runInitDBWithSeed(t, env)
			assert.False(t, kept, "Reset löscht die Bestandsdaten")
			assert.True(t, demo, "Demokonten angelegt")
		})
	}
}

// Auch in der Entwicklungsumgebung löscht und füllt nur der ausdrückliche
// Wunsch SEED_DB=true die Datenbank; ohne die Variable oder mit einem anderen
// Wert bleiben die Bestandsdaten unberührt.
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

// Scheitert das Hashen der Demo-Passwörter, legt der Seed keine Konten ohne
// Passwort an, sondern bricht ab.
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
