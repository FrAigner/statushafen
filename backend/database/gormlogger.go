package database

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/fraigner/statushafen/logging"

	"gorm.io/gorm/logger"
)

// Der GORM-Standardlogger setzt die Parameter in "SLOW SQL" und
// in Fehlerzeilen in das Statement ein. Damit landeten Google-Chat-Webhook-
// URLs (key=/token=), Passwort-Hashes, Confirm-/Reset-/API-Token-Hashes und
// E-Mail-Adressen im Klartext im Log. NewGormLogger behält Format, Level
// (Warn) und Slow-Schwelle (200 ms) des Standardloggers, gibt aber das SQL
// mit Platzhaltern ($1, ...) aus (ParameterizedQueries). Zusätzlich werden
// Treiberfehler bereinigt: Postgres nennt in Constraint-Fehlern die Werte
// ("Key (email)=(a@b.de) already exists").

const gormSlowThreshold = 200 * time.Millisecond

// Scan() läuft in GORM über einen "Recorder" und umgeht den Logger-Filter:
// dessen Standard-RecorderParamsFilter reicht die Parameter durch, sodass
// SLOW SQL/Fehler von Scan-Abfragen trotz ParameterizedQueries Klartext-
// Werte enthielten. Deshalb global auf "keine Parameter" stellen.
func init() {
	logger.RecorderParamsFilter = func(_ context.Context, sql string, _ ...any) (string, []any) {
		return sql, nil
	}
}

// NewGormLogger baut den GORM-Logger für InitDB, Ausgabe nach w.
func NewGormLogger(w io.Writer) logger.Interface {
	return sanitizingLogger{Interface: logger.New(
		log.New(w, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             gormSlowThreshold,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: false,
			Colorful:                  true,
			ParameterizedQueries:      true,
		},
	)}
}

type sanitizingLogger struct{ logger.Interface }

// LogMode muss den Wrapper erhalten, sonst fiele db.Debug() & Co. auf den
// ungeschützten Basislogger zurück.
func (l sanitizingLogger) LogMode(level logger.LogLevel) logger.Interface {
	return sanitizingLogger{Interface: l.Interface.LogMode(level)}
}

// ParamsFilter muss vom Wrapper explizit angeboten werden: gorm prüft per
// Typzusicherung auf logger.ParamsFilter, und das eingebettete Interface
// verbirgt die Methode. Ohne sie bliebe ParameterizedQueries wirkungslos.
func (sanitizingLogger) ParamsFilter(_ context.Context, sql string, _ ...any) (string, []any) {
	return sql, nil
}

func (l sanitizingLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if err != nil {
		err = sanitizedError{msg: logging.SanitizeErr(err), cause: err}
	}
	l.Interface.Trace(ctx, begin, fc, err)
}

// sanitizedError trägt den bereinigten Text, behält aber die Fehlerkette,
// damit der Basislogger gorm.ErrRecordNotFound weiterhin erkennt.
type sanitizedError struct {
	msg   string
	cause error
}

func (e sanitizedError) Error() string { return e.msg }
func (e sanitizedError) Unwrap() error { return e.cause }
