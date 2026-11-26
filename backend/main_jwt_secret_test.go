package main

import (
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/fraigner/statushafen/logging"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A weak signing secret must abort startup before the database is
// opened. main() sets up its own log (on stdout); the test captures
// that output. The database connection points to a closed port:
// if main() skipped the check, startup would only fail there, with a different
// message.
func TestMain_WeakJWTSecretStopsStartupBeforeDatabase(t *testing.T) {
	for name, secret := range map[string]string{
		"leer":        "",
		"zu kurz":     "kurz",
		"Platzhalter": "your-super-secret-jwt-key",
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir()) // no local .env
			t.Setenv("JWT_SECRET", secret)
			t.Setenv("DB_DSN", "host=127.0.0.1 port=1 user=x password=x dbname=x sslmode=disable connect_timeout=1")

			prevLogger := slog.Default()
			t.Cleanup(func() { slog.SetDefault(prevLogger) }) // main() sets up the log anew
			r, w, err := os.Pipe()
			require.NoError(t, err)
			prevStdout := os.Stdout
			os.Stdout = w
			restoreExit := logging.SetExitForTest(func(code int) { panic(code) })
			var exitCode any
			func() {
				defer func() { exitCode = recover() }()
				main()
			}()
			restoreExit()
			os.Stdout = prevStdout
			require.NoError(t, w.Close())
			out, err := io.ReadAll(r)
			require.NoError(t, err)

			assert.Equal(t, 1, exitCode, "main() muss mit Exit 1 abbrechen")
			assert.Contains(t, string(out), "Ungültiges JWT_SECRET")
			assert.NotContains(t, string(out), "Failed to connect to database", "die Prüfung gehört vor die Datenbank")
			if secret != "" {
				assert.NotContains(t, string(out), secret, "der Wert gehört nicht ins Log")
			}
		})
	}
}
