package metrics

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// openTestSQLiteDB opens an isolated, file-based SQLite DB (mirrors
// testutils.SetupTestDB's approach) and returns the underlying *sql.DB, so
// tests can exercise a real database/sql connection pool without depending
// on Postgres being available in CI. It intentionally lives here rather
// than importing statuspage/testutils, to keep the metrics package's test
// dependencies self-contained (and avoid pulling in the whole models/
// AutoMigrate surface for what only needs a connection pool).
func openTestSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()

	dbName := filepath.Join(t.TempDir(), "metrics.db")
	gdb, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite test db: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("failed to get underlying sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

// holdOpenTransaction begins a transaction, signals via started once it has
// a connection checked out, then blocks until release is closed before
// rolling back - used to artificially hold open-connection-pool slots busy
// under TestSaturationSnapshot_DBConnectionsRiseUnderParallelLoad's parallel
// load scenario.
func holdOpenTransaction(t *testing.T, sqlDB *sql.DB, wg *sync.WaitGroup, started chan<- struct{}, release <-chan struct{}) {
	defer wg.Done()
	tx, err := sqlDB.Begin()
	if err != nil {
		t.Errorf("failed to begin tx: %v", err)
		return
	}
	if _, err := tx.Exec("SELECT 1"); err != nil {
		t.Errorf("failed to exec in tx: %v", err)
	}
	started <- struct{}{}
	<-release
	_ = tx.Rollback()
}

// pollUntilBusy polls the registry's SaturationSnapshot until DBInUse
// reaches want or the timeout elapses, returning whichever snapshot was last
// observed. The DB connection pool's bookkeeping updates asynchronously
// relative to Begin(), so a short poll absorbs CI scheduling jitter instead
// of asserting on a single potentially-stale snapshot.
func pollUntilBusy(reg *Registry, want int, timeout time.Duration) SaturationSnapshot {
	deadline := time.Now().Add(timeout)
	var snap SaturationSnapshot
	for time.Now().Before(deadline) {
		snap = reg.SaturationSnapshot()
		if snap.DBInUse >= want {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return snap
}

func TestSaturationSnapshot_ReportsGoroutineCountAndMemStats(t *testing.T) {
	reg := NewRegistry()

	snap := reg.SaturationSnapshot()

	if snap.GoroutineCount <= 0 {
		t.Errorf("expected GoroutineCount > 0, got %d", snap.GoroutineCount)
	}
	if snap.HeapAllocBytes == 0 {
		t.Errorf("expected HeapAllocBytes > 0, got 0")
	}
	if snap.SysBytes == 0 {
		t.Errorf("expected SysBytes > 0, got 0")
	}
	if snap.DBStatsAvailable {
		t.Errorf("expected DBStatsAvailable=false when no DBStatsProvider is registered, got true")
	}
}

// Der Test verglich früher mit einer Baseline vor dem Start der Last. Unter -race und voller
// Paketlast lief dort gelegentlich noch eine Goroutine eines vorherigen Tests aus (wg.Done ist vor dem
// tatsächlichen Ende der Goroutine erreicht): Sie zählte in der Baseline mit, war unter Last schon weg, der
// Anstieg betrug 199 statt 200 (reproduziert mit go test -race -count=50 -cpu 1,4 ./metrics/). Jetzt wird
// gemessen, wenn alle n Goroutinen nachweislich laufen, und mit dem Stand nach ihrem Ende verglichen.
// Auslaufende fremde Goroutinen vergrößern diese Differenz nur, statt sie zu verkleinern.
func TestSaturationSnapshot_GoroutineCountRisesUnderLoad(t *testing.T) {
	reg := NewRegistry()

	const n = 200
	release := make(chan struct{})
	var releaseOnce sync.Once
	var started, done sync.WaitGroup
	started.Add(n)
	done.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer done.Done()
			started.Done()
			<-release
		}()
	}
	releaseAll := func() {
		releaseOnce.Do(func() { close(release) })
		done.Wait()
	}
	defer releaseAll()

	started.Wait()
	underLoad := reg.SaturationSnapshot().GoroutineCount
	if underLoad < n {
		t.Fatalf("expected GoroutineCount >= %d while %d goroutines block, got %d", n, n, underLoad)
	}

	releaseAll()

	// done.Done läuft noch in der Goroutine; bis sie wirklich beendet ist, kurz pollen.
	deadline := time.Now().Add(2 * time.Second)
	after := reg.SaturationSnapshot().GoroutineCount
	for underLoad-after < n && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		after = reg.SaturationSnapshot().GoroutineCount
	}

	if underLoad-after < n {
		t.Fatalf("expected GoroutineCount to be at least %d higher under load than afterwards (under load=%d, afterwards=%d)", n, underLoad, after)
	}
}

func TestSaturationSnapshot_DBStatsUnavailableWithoutProvider(t *testing.T) {
	reg := NewRegistry()

	snap := reg.SaturationSnapshot()
	if snap.DBStatsAvailable {
		t.Fatal("expected DBStatsAvailable=false before SetDBStatsProvider is called")
	}
	if snap.DBOpenConnections != 0 || snap.DBInUse != 0 || snap.DBIdle != 0 {
		t.Fatalf("expected zero-value DB fields without a provider, got %+v", snap)
	}
}

func TestSaturationSnapshot_DBStatsReflectProvider(t *testing.T) {
	reg := NewRegistry()

	reg.SetDBStatsProvider(func() sql.DBStats {
		return sql.DBStats{
			MaxOpenConnections: 42,
			OpenConnections:    7,
			InUse:              3,
			Idle:               4,
			WaitCount:          5,
			WaitDuration:       250 * time.Millisecond,
		}
	})

	snap := reg.SaturationSnapshot()
	if !snap.DBStatsAvailable {
		t.Fatal("expected DBStatsAvailable=true once a provider is registered")
	}
	if snap.DBMaxOpenConnections != 42 || snap.DBOpenConnections != 7 || snap.DBInUse != 3 || snap.DBIdle != 4 || snap.DBWaitCount != 5 {
		t.Errorf("unexpected DB stats passthrough: %+v", snap)
	}
	if snap.DBWaitDurationMs != 250 {
		t.Errorf("expected DBWaitDurationMs=250, got %f", snap.DBWaitDurationMs)
	}

	// Clearing the provider (nil) reverts to "unavailable" rather than
	// keeping stale data around.
	reg.SetDBStatsProvider(nil)
	cleared := reg.SaturationSnapshot()
	if cleared.DBStatsAvailable {
		t.Fatal("expected DBStatsAvailable=false after clearing the provider with nil")
	}
}

// TestSaturationSnapshot_DBConnectionsRiseUnderParallelLoad is the
// Verifikation scenario from "Unter künstlich erzeugter Last
// (z.B. viele parallele DB-Anfragen im Test) steigen die Saturation-Werte
// sichtbar an." It drives a real database/sql connection pool (backed by an
// in-process SQLite DB, so it needs no external Postgres) with many
// concurrent long-held connections and asserts DBInUse/DBOpenConnections
// visibly rise compared to the idle baseline, then fall back down again
// once the load is released.
func TestSaturationSnapshot_DBConnectionsRiseUnderParallelLoad(t *testing.T) {
	sqlDB := openTestSQLiteDB(t)

	const maxOpen = 8
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxOpen)

	reg := NewRegistry()
	reg.SetDBStatsProvider(sqlDB.Stats)

	idle := reg.SaturationSnapshot()
	if idle.DBInUse != 0 {
		t.Fatalf("expected DBInUse=0 before any queries run, got %d", idle.DBInUse)
	}

	// Hold maxOpen connections open concurrently (each inside its own
	// transaction, so the pool can't just reuse a single connection
	// serially) until told to release them.
	release := make(chan struct{})
	started := make(chan struct{}, maxOpen)
	var wg sync.WaitGroup
	wg.Add(maxOpen)
	for i := 0; i < maxOpen; i++ {
		go holdOpenTransaction(t, sqlDB, &wg, started, release)
	}

	for i := 0; i < maxOpen; i++ {
		<-started
	}

	// Poll briefly: the pool's internal bookkeeping updates as soon as
	// Begin() hands out a connection, but give it a little slack under CI
	// scheduling jitter.
	busy := pollUntilBusy(reg, maxOpen, 2*time.Second)

	close(release)
	wg.Wait()

	if !busy.DBStatsAvailable {
		t.Fatal("expected DBStatsAvailable=true while queries were in flight")
	}
	if busy.DBInUse < maxOpen {
		t.Fatalf("expected DBInUse to rise to >= %d under parallel load, got %d", maxOpen, busy.DBInUse)
	}
	if busy.DBOpenConnections < maxOpen {
		t.Fatalf("expected DBOpenConnections to rise to >= %d under parallel load, got %d", maxOpen, busy.DBOpenConnections)
	}
	if busy.DBInUse <= idle.DBInUse {
		t.Fatalf("expected DBInUse to visibly rise above the idle baseline (%d), got %d", idle.DBInUse, busy.DBInUse)
	}

	// After releasing all connections back to the pool, InUse should drop
	// back down (idle connections are still "open" but no longer "in use").
	after := reg.SaturationSnapshot()
	if after.DBInUse != 0 {
		t.Errorf("expected DBInUse to fall back to 0 after releasing all connections, got %d", after.DBInUse)
	}
}
