package database

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/fraigner/statushafen/logging"

	"gorm.io/gorm/logger"
)

// The GORM default logger inserts the parameters into the statement in "SLOW SQL" and
// in error lines. As a result Google Chat webhook
// URLs (key=/token=), password hashes, confirm/reset/API token hashes and
// email addresses ended up in plain text in the log. NewGormLogger keeps the format, level
// (Warn) and slow threshold (200 ms) of the default logger, but outputs the SQL
// with placeholders ($1, ...) (ParameterizedQueries). Additionally
// driver errors are sanitized: in constraint errors Postgres names the values
// ("Key (email)=(a@b.de) already exists").

const gormSlowThreshold = 200 * time.Millisecond

// Scan() runs in GORM via a "recorder" and bypasses the logger filter:
// its default RecorderParamsFilter passes the parameters through, so
// SLOW SQL/errors of Scan queries contained plain-text
// values despite ParameterizedQueries. Hence set globally to "no parameters".
func init() {
	logger.RecorderParamsFilter = func(_ context.Context, sql string, _ ...any) (string, []any) {
		return sql, nil
	}
}

// NewGormLogger builds the GORM logger for InitDB, output to w.
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

// LogMode must preserve the wrapper, otherwise db.Debug() & co. would fall back to the
// unprotected base logger.
func (l sanitizingLogger) LogMode(level logger.LogLevel) logger.Interface {
	return sanitizingLogger{Interface: l.Interface.LogMode(level)}
}

// ParamsFilter must be offered explicitly by the wrapper: gorm checks via
// type assertion for logger.ParamsFilter, and the embedded interface
// hides the method. Without it ParameterizedQueries would be ineffective.
func (sanitizingLogger) ParamsFilter(_ context.Context, sql string, _ ...any) (string, []any) {
	return sql, nil
}

func (l sanitizingLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if err != nil {
		err = sanitizedError{msg: logging.SanitizeErr(err), cause: err}
	}
	l.Interface.Trace(ctx, begin, fc, err)
}

// sanitizedError carries the sanitized text, but keeps the error chain,
// so the base logger still recognizes gorm.ErrRecordNotFound.
type sanitizedError struct {
	msg   string
	cause error
}

func (e sanitizedError) Error() string { return e.msg }
func (e sanitizedError) Unwrap() error { return e.cause }
