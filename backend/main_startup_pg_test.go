package main

// Start-Smoke-Test für main() gegen ein echtes Postgres. main()
// startet seit der Umstellung auch den Abruf externer Statusseiten, der beim Shutdown
// (nach runServer) sauber beendet werden muss. Bisher lief main() in keinem
// Test - jede Zeile darin war für das Coverage-Gate unprüfbar. Der Test
// startet den Server wie in Produktion, wartet auf /healthz und schickt sich
// selbst SIGTERM; main() muss dann zurückkehren (Graceful Shutdown inkl.
// Stopp der Hintergrund-Schleife). Wie der Migrationstest nur mit
// MIGRATION_PG_TEST=1 (lädt Postgres-Binaries, startet nicht als root).

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fraigner/statushafen/database"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/stretchr/testify/require"
)

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

func TestMain_StartetUndFaehrtSauberHerunter(t *testing.T) {
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
	t.Chdir(t.TempDir()) // keine lokale .env, kein ./static
	httpPort := freeTCPPort(t)
	t.Setenv("PORT", fmt.Sprint(httpPort))
	t.Setenv("DB_DSN", fmt.Sprintf("host=localhost port=%d user=postgres password=postgres dbname=statushafen sslmode=disable", pgPort))
	t.Setenv("SEED_DB", "")
	t.Setenv("JWT_SECRET", strings.Repeat("Ab1$xY", 7))
	t.Setenv("EXTERNAL_STATUS_POLLING_ENABLED", "true")

	done := make(chan struct{})
	go func() {
		defer close(done)
		main()
	}()

	healthz := fmt.Sprintf("http://127.0.0.1:%d/healthz", httpPort)
	require.Eventually(t, func() bool {
		resp, err := http.Get(healthz) //nolint:noctx // Test-Polling
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Minute, 100*time.Millisecond, "Server kommt nicht hoch")
	// runServer registriert signal.Notify direkt nach dem Start des Listeners;
	// kurz warten, damit das SIGTERM sicher dort ankommt.
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGTERM))

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("main() kehrt nach SIGTERM nicht zurück (Shutdown hängt)")
	}
}
