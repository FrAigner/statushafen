package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/models"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// create-admin gegen ein echtes Postgres: Der Befehl migriert wie der
// Serverstart, legt den Admin an, das Passwort passt, und ein zweiter Aufruf
// ändert nichts. Nur mit MIGRATION_PG_TEST=1 (startet nicht als root).
func TestRunCLI_CreateAdminGegenPostgres(t *testing.T) {
	if os.Getenv("MIGRATION_PG_TEST") != "1" {
		t.Skip("nur mit MIGRATION_PG_TEST=1 (wie der Postgres-Migrationstest)")
	}
	pgPort := freeTCPPort(t)
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Port(uint32(pgPort)).
		Database("statushafen").
		Encoding("UTF8").
		Locale("C").
		RuntimePath(t.TempDir()).
		Logger(nil))
	require.NoError(t, pg.Start())
	t.Cleanup(func() { _ = pg.Stop() })

	prevLogger, prevDB := slog.Default(), database.DB
	t.Cleanup(func() { slog.SetDefault(prevLogger); database.DB = prevDB })
	t.Chdir(t.TempDir())
	t.Setenv("DB_DSN", fmt.Sprintf("host=localhost port=%d user=postgres password=postgres dbname=statushafen sslmode=disable", pgPort))
	t.Setenv("SEED_DB", "")
	t.Setenv("APP_ENV", "production")

	args := []string{"create-admin", "--email", "Admin@Example.com", "--password-stdin"}
	var o, e bytes.Buffer
	require.Zero(t, runCLI(args, strings.NewReader(cliTestPassword+"\n"), &o, &e), e.String())

	var u models.User
	require.NoError(t, database.DB.Where("email = ?", "admin@example.com").First(&u).Error)
	assert.True(t, u.IsGlobalAdmin)
	assert.True(t, database.CheckPasswordHash(cliTestPassword, u.PasswordHash))

	o.Reset()
	e.Reset()
	assert.NotZero(t, runCLI(args, strings.NewReader("anderes-testpasswort\n"), &o, &e))
	require.NoError(t, database.DB.Where("email = ?", "admin@example.com").First(&u).Error)
	assert.True(t, database.CheckPasswordHash(cliTestPassword, u.PasswordHash), "das Passwort bleibt unverändert")
}
