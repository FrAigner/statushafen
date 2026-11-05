package metrics

import (
	"database/sql"
	"runtime"
)

// ─────────────────────────────────────────────────────────────────────────────
// Saturation (Golden Signals Schritt 4/4)
//
// Latency/Traffic/Errors are covered by RouteMetrics/Observe (Schritt 1/4).
// This file adds the fourth Golden Signal, resource *Saturation*:
//   - DB connection-pool utilization (database/sql.DBStats, which is what
//     GORM's *gorm.DB.DB() returns unmodified — no GORM-specific API needed).
//   - Goroutine count (runtime.NumGoroutine()).
//   - Memory usage (runtime.MemStats).
//
// Deliberately independent of Observe/RouteMetrics: saturation isn't tied to
// any single (method, route) request, it's a process-wide, point-in-time
// reading. SaturationSnapshot() is therefore its own additive method on
// Registry, reading nothing from and writing nothing into the request-
// tracking sync.Map above.
// ─────────────────────────────────────────────────────────────────────────────

// DBStatsProvider returns the current connection-pool statistics for the
// database this process talks to. In production this is typically
// sqlDB.Stats, where sqlDB, _ := database.DB.DB() (GORM's escape hatch to
// the underlying *sql.DB) — see SetDBStatsProvider.
type DBStatsProvider func() sql.DBStats

// SetDBStatsProvider registers the source of DB connection-pool statistics
// used by SaturationSnapshot. Safe to call concurrently with
// SaturationSnapshot (and safe to call more than once, e.g. from tests) —
// the pointer is swapped atomically. Passing nil clears the provider, after
// which SaturationSnapshot reports DBStatsAvailable=false.
//
// Kept separate from NewRegistry/the Registry struct literal on purpose:
// main.go only knows about the DB connection *after* database.InitDB() has
// run, so the provider has to be attached after construction rather than at
// registry-creation time.
func (r *Registry) SetDBStatsProvider(p DBStatsProvider) {
	if p == nil {
		r.dbStatsProvider.Store(nil)
		return
	}
	r.dbStatsProvider.Store(&p)
}

// SaturationSnapshot is a point-in-time, read-only reading of the fourth
// Golden Signal (resource Saturation): DB connection-pool utilization,
// goroutine count, and memory usage. Unlike Snapshot() (which aggregates
// data recorded over the process lifetime), every call to
// SaturationSnapshot re-measures the process's current state, so successive
// calls are expected to differ as load rises and falls.
type SaturationSnapshot struct {
	// GoroutineCount is runtime.NumGoroutine() at snapshot time.
	GoroutineCount int

	// Memory usage, taken from runtime.MemStats at snapshot time.
	HeapAllocBytes  uint64 // bytes of allocated, reachable heap objects
	HeapSysBytes    uint64 // bytes of heap obtained from the OS
	TotalAllocBytes uint64 // cumulative bytes allocated over the process lifetime (monotonic, includes freed memory)
	SysBytes        uint64 // total bytes obtained from the OS for this process
	NumGC           uint32 // number of completed GC cycles

	// DB connection-pool utilization, taken from a DBStatsProvider
	// registered via SetDBStatsProvider. DBStatsAvailable is false (and the
	// remaining DB* fields are zero) if no provider was registered, e.g. in
	// tests/handlers that only care about the request-tracking signals.
	DBStatsAvailable     bool
	DBMaxOpenConnections int
	DBOpenConnections    int
	DBInUse              int
	DBIdle               int
	// DBWaitCount/DBWaitDurationMs count how often and how long callers had
	// to wait for a connection because the pool was already at
	// MaxOpenConnections — the clearest sign of actual DB saturation, as
	// opposed to merely "the pool happens to be busy right now".
	DBWaitCount      int64
	DBWaitDurationMs float64
}

// SaturationSnapshot re-measures the process's current resource
// utilization. It never blocks/allocates unboundedly: runtime.ReadMemStats
// and the registered DBStatsProvider are both expected to be cheap,
// non-blocking calls (that's also true of GORM/database/sql's DBStats).
func (r *Registry) SaturationSnapshot() SaturationSnapshot {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	snap := SaturationSnapshot{
		GoroutineCount:  runtime.NumGoroutine(),
		HeapAllocBytes:  mem.HeapAlloc,
		HeapSysBytes:    mem.HeapSys,
		TotalAllocBytes: mem.TotalAlloc,
		SysBytes:        mem.Sys,
		NumGC:           mem.NumGC,
	}

	if pp := r.dbStatsProvider.Load(); pp != nil {
		stats := (*pp)()
		snap.DBStatsAvailable = true
		snap.DBMaxOpenConnections = stats.MaxOpenConnections
		snap.DBOpenConnections = stats.OpenConnections
		snap.DBInUse = stats.InUse
		snap.DBIdle = stats.Idle
		snap.DBWaitCount = stats.WaitCount
		snap.DBWaitDurationMs = float64(stats.WaitDuration.Nanoseconds()) / 1e6
	}

	return snap
}
