package database

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

// AdvisoryLock ensures that a background service (the
// polling of external status pages) runs on only one instance even with several
// instances. Postgres: session advisory lock on a dedicated
// connection from the pool. Deliberately not pg_try_advisory_xact_lock: the
// service would have to keep a transaction open during the HTTP fetches
// (up to 10 s per source). The instance holding the lock stays leader until the
// connection drops (Postgres then releases the lock itself) or Unlock
// is called; the others try again on every tick.
//
// SQLite (tests, local development) has no advisory locks and runs
// in only one process anyway: TryLock always returns true there.
//
// Not safe for concurrent use: exactly one goroutine (the polling loop) owns
// the lock.
type AdvisoryLock struct {
	db   *gorm.DB
	key  int64
	conn *sql.Conn
}

// NewAdvisoryLock creates a lock with the application-wide unique key.
func NewAdvisoryLock(db *gorm.DB, key int64) *AdvisoryLock {
	return &AdvisoryLock{db: db, key: key}
}

// TryLock tries to obtain or keep the lock. true = this
// instance may work.
func (l *AdvisoryLock) TryLock(ctx context.Context) (bool, error) {
	if l.db.Name() != "postgres" {
		return true, nil
	}
	if l.conn != nil {
		// Lock already held: only check whether the connection is still alive.
		if err := l.conn.PingContext(ctx); err == nil {
			return true, nil
		}
		_ = l.conn.Close()
		l.conn = nil
	}
	// db.DB() practically never fails (even a gorm transaction returns
	// its *sql.DB); an error runs into the same exit anyway.
	var conn *sql.Conn
	sqlDB, err := l.db.DB()
	if err == nil {
		conn, err = sqlDB.Conn(ctx)
	}
	if err != nil {
		return false, err
	}
	var got bool
	err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, l.key).Scan(&got)
	if err != nil || !got {
		// Error or lock held by another instance: return the connection.
		_ = conn.Close()
		return false, err
	}
	l.conn = conn
	return true, nil
}

// Unlock releases a held lock (e.g. on shutdown).
func (l *AdvisoryLock) Unlock() {
	if l.conn == nil {
		return
	}
	_, _ = l.conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, l.key)
	_ = l.conn.Close()
	l.conn = nil
}
