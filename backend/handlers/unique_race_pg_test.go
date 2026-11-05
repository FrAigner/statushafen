package handlers_test

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/testutils"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/stretchr/testify/require"
)

// Gleichzeitiges Anlegen desselben Slugs/Tags/Namens gegen ein
// echtes Postgres. Jede Anfrage muss mit 201 oder 409 enden, genau eine mit
// 201 (vorher gelegentlich 500 durch die Unique-Verletzung zwischen
// Vorabprüfung und Insert). Nur mit MIGRATION_PG_TEST=1 (lädt Postgres, startet
// nicht als root).
func TestUniqueRace_ParallelPostgres(t *testing.T) {
	if os.Getenv("MIGRATION_PG_TEST") != "1" {
		t.Skip("nur mit MIGRATION_PG_TEST=1")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).Port(uint32(port)).Database("statushafen").
		Encoding("UTF8").Locale("C").RuntimePath(t.TempDir()).Logger(nil))
	require.NoError(t, pg.Start())
	t.Cleanup(func() { _ = pg.Stop() })

	oldDB := database.DB
	t.Cleanup(func() { database.DB = oldDB })
	t.Setenv("DB_DSN", fmt.Sprintf("host=localhost port=%d user=postgres password=postgres dbname=statushafen sslmode=disable", port))
	t.Setenv("SEED_DB", "")
	database.InitDB()

	h := handlers.NewHandler(database.DB)
	r := testutils.SetupRouter()
	r.POST("/api/admin/pages", h.CreateStatusPageHandler)
	r.POST("/api/tags", h.CreateTagHandler)
	r.POST("/api/admin/roles", h.CreateRoleHandler)

	const workers = 16
	cases := []struct {
		name, path string
		payload    func(round int) map[string]any
	}{
		{"Statusseite", "/api/admin/pages", func(i int) map[string]any {
			return map[string]any{"Name": "Race", "Slug": fmt.Sprintf("race-%d", i)}
		}},
		{"Tag", "/api/tags", func(i int) map[string]any { return map[string]any{"name": fmt.Sprintf("race-%d", i)} }},
		{"Rolle", "/api/admin/roles", func(i int) map[string]any { return map[string]any{"name": fmt.Sprintf("race-%d", i)} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for round := 0; round < 5; round++ {
				codes := make([]int, workers)
				start := make(chan struct{})
				var wg sync.WaitGroup
				for i := 0; i < workers; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						<-start
						codes[i] = doTagRequest(r, http.MethodPost, tc.path, tc.payload(round)).Code
					}()
				}
				close(start)
				wg.Wait()
				created := 0
				for _, code := range codes {
					require.Contains(t, []int{http.StatusCreated, http.StatusConflict}, code, "Antwortcodes: %v", codes)
					if code == http.StatusCreated {
						created++
					}
				}
				require.Equal(t, 1, created, "Antwortcodes: %v", codes)
			}
		})
	}
}
