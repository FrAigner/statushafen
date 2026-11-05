package database

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

// AdvisoryLock sorgt dafür, dass ein Hintergrunddienst (der
// Abruf externer Statusseiten) auch bei mehreren Instanzen nur auf einer
// läuft. Postgres: Session-Advisory-Lock auf einer eigens gehaltenen
// Verbindung aus dem Pool. Bewusst kein pg_try_advisory_xact_lock: Der
// Dienst würde dafür während der HTTP-Abrufe (bis 10 s je Quelle) eine
// Transaktion offen halten. Die Instanz mit der Sperre bleibt Leader, bis die
// Verbindung abreißt (Postgres gibt die Sperre dann selbst frei) oder Unlock
// aufgerufen wird; die anderen versuchen es bei jedem Takt erneut.
//
// SQLite (Tests, lokale Entwicklung) kennt keine Advisory Locks und läuft
// ohnehin nur in einem Prozess: TryLock liefert dort immer true.
//
// Nicht nebenläufig sicher: genau eine Goroutine (die Abfrage-Schleife) besitzt
// die Sperre.
type AdvisoryLock struct {
	db   *gorm.DB
	key  int64
	conn *sql.Conn
}

// NewAdvisoryLock legt eine Sperre mit dem anwendungsweit eindeutigen key an.
func NewAdvisoryLock(db *gorm.DB, key int64) *AdvisoryLock {
	return &AdvisoryLock{db: db, key: key}
}

// TryLock versucht, die Sperre zu bekommen bzw. zu behalten. true = diese
// Instanz darf arbeiten.
func (l *AdvisoryLock) TryLock(ctx context.Context) (bool, error) {
	if l.db.Name() != "postgres" {
		return true, nil
	}
	if l.conn != nil {
		// Sperre schon gehalten: nur prüfen, ob die Verbindung noch lebt.
		if err := l.conn.PingContext(ctx); err == nil {
			return true, nil
		}
		_ = l.conn.Close()
		l.conn = nil
	}
	// db.DB() scheitert praktisch nie (auch eine gorm-Transaktion liefert
	// ihr *sql.DB); ein Fehler läuft trotzdem in denselben Ausgang.
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
		// Fehler oder Sperre bei einer anderen Instanz: Verbindung zurückgeben.
		_ = conn.Close()
		return false, err
	}
	l.conn = conn
	return true, nil
}

// Unlock gibt eine gehaltene Sperre frei (z. B. beim Herunterfahren).
func (l *AdvisoryLock) Unlock() {
	if l.conn == nil {
		return
	}
	_, _ = l.conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, l.key)
	_ = l.conn.Close()
	l.conn = nil
}
