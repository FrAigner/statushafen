// Package logging bundles the setup of the global
// slog logger: JSON in production or with LOG_FORMAT=json, readable text
// locally, level via LOG_LEVEL and central redaction of sensitive attributes.
// The redaction deliberately sits in the handler (ReplaceAttr) instead of in the individual
// callers: this way a new slog call with an "email"/"token" attribute cannot
// accidentally write plain text into the log.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Redacted replaces the value of sensitive attributes.
const Redacted = "[REDACTED]"

// sensitiveKeyParts: if an attribute name (lowercased) contains one of these
// parts, the value is replaced entirely.
var sensitiveKeyParts = []string{"password", "token", "secret", "authorization", "cookie"}

// ParseLevel translates LOG_LEVEL. Empty means Info; ok=false for an unknown
// value (then also Info, the caller warns).
func ParseLevel(s string) (slog.Level, bool) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" || v == "info" {
		return slog.LevelInfo, true
	}
	if v == "debug" {
		return slog.LevelDebug, true
	}
	if v == "warn" || v == "warning" {
		return slog.LevelWarn, true
	}
	if v == "error" {
		return slog.LevelError, true
	}
	return slog.LevelInfo, false
}

// useJSON: LOG_FORMAT takes precedence, otherwise JSON exactly when
// APP_ENV=production (log collectors in prod need machine-readable lines,
// locally a human reads along).
func useJSON(getenv func(string) string) bool {
	format := strings.ToLower(strings.TrimSpace(getenv("LOG_FORMAT")))
	if format != "" {
		return format == "json"
	}
	return getenv("APP_ENV") == "production"
}

// New builds a logger according to LOG_FORMAT/APP_ENV/LOG_LEVEL from getenv.
func New(w io.Writer, getenv func(string) string) *slog.Logger {
	level, ok := ParseLevel(getenv("LOG_LEVEL"))
	opts := &slog.HandlerOptions{Level: level, ReplaceAttr: redact}
	var h slog.Handler = slog.NewTextHandler(w, opts)
	if useJSON(getenv) {
		h = slog.NewJSONHandler(w, opts)
	}
	logger := slog.New(h)
	if !ok {
		logger.Warn("Unbekannter LOG_LEVEL, verwende info", "value", getenv("LOG_LEVEL"))
	}
	return logger
}

// Init sets the logger built with New as the slog default.
func Init(w io.Writer, getenv func(string) string) {
	slog.SetDefault(New(w, getenv))
}

func redact(_ []string, a slog.Attr) slog.Attr {
	key := strings.ToLower(a.Key)
	for _, part := range sensitiveKeyParts {
		if strings.Contains(key, part) {
			return slog.String(a.Key, Redacted)
		}
	}
	if key == "email" && a.Value.Kind() == slog.KindString {
		return slog.String(a.Key, MaskEmail(a.Value.String()))
	}
	// Clean up URLs and constraint values in strings and errors.
	if a.Value.Kind() == slog.KindString {
		return slog.String(a.Key, SanitizeText(a.Value.String()))
	}
	if err, ok := a.Value.Any().(error); ok {
		return slog.String(a.Key, SanitizeErr(err))
	}
	return a
}

// MaskEmail leaves only the first letter and the domain
// ("anna@example.com" -> "a***@example.com"): enough to match an
// error message without revealing the address in the log.
func MaskEmail(email string) string {
	if email == "" {
		return ""
	}
	at := strings.LastIndex(email, "@")
	if at < 1 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
}

type ctxKey struct{}

// WithLogger stores a (request-scoped) logger in the context.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext returns the logger from the context or slog.Default().
func FromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
			return l
		}
	}
	return slog.Default()
}

// exit is replaceable so that tests can run through the Fatal paths without
// terminating the test process.
var exit = os.Exit

// Fatal replaces log.Fatalf: log the error in structured form, then exit code 1.
func Fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	exit(1)
}

// SetExitForTest swaps the exit function and returns the reset.
// For tests only (see loggingtest.ExpectFatal).
func SetExitForTest(f func(int)) (restore func()) {
	prev := exit
	exit = f
	return func() { exit = prev }
}
