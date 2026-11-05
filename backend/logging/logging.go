// Package logging bündelt die Einrichtung des globalen
// slog-Loggers: JSON in Produktion bzw. mit LOG_FORMAT=json, lesbarer Text
// lokal, Level per LOG_LEVEL und eine zentrale Redaktion sensibler Attribute.
// Die Redaktion sitzt bewusst im Handler (ReplaceAttr) statt in den einzelnen
// Aufrufern: so kann ein neuer slog-Aufruf mit "email"/"token"-Attribut nicht
// versehentlich Klartext ins Log schreiben.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Redacted ersetzt den Wert sensibler Attribute.
const Redacted = "[REDACTED]"

// sensitiveKeyParts: enthält ein Attributname (kleingeschrieben) einen dieser
// Teile, wird der Wert komplett ersetzt.
var sensitiveKeyParts = []string{"password", "token", "secret", "authorization", "cookie"}

// ParseLevel übersetzt LOG_LEVEL. Leer heißt Info; ok=false bei unbekanntem
// Wert (dann ebenfalls Info, der Aufrufer warnt).
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

// useJSON: LOG_FORMAT hat Vorrang, sonst JSON genau dann, wenn
// APP_ENV=production (Log-Sammler in Prod brauchen maschinenlesbare Zeilen,
// lokal liest ein Mensch mit).
func useJSON(getenv func(string) string) bool {
	format := strings.ToLower(strings.TrimSpace(getenv("LOG_FORMAT")))
	if format != "" {
		return format == "json"
	}
	return getenv("APP_ENV") == "production"
}

// New baut einen Logger nach LOG_FORMAT/APP_ENV/LOG_LEVEL aus getenv.
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

// Init setzt den mit New gebauten Logger als slog-Default.
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
	// URLs und Constraint-Werte in Strings und Fehlern bereinigen.
	if a.Value.Kind() == slog.KindString {
		return slog.String(a.Key, SanitizeText(a.Value.String()))
	}
	if err, ok := a.Value.Any().(error); ok {
		return slog.String(a.Key, SanitizeErr(err))
	}
	return a
}

// MaskEmail lässt nur den ersten Buchstaben und die Domain stehen
// ("anna@example.com" -> "a***@example.com"): reicht zum Zuordnen einer
// Fehlermeldung, ohne die Adresse im Log offenzulegen.
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

// WithLogger legt einen (request-scoped) Logger im Context ab.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext liefert den Logger aus dem Context oder slog.Default().
func FromContext(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
			return l
		}
	}
	return slog.Default()
}

// exit ist austauschbar, damit Tests die Fatal-Pfade durchlaufen können, ohne
// den Testprozess zu beenden.
var exit = os.Exit

// Fatal ersetzt log.Fatalf: Fehler strukturiert loggen, dann Exit-Code 1.
func Fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	exit(1)
}

// SetExitForTest tauscht die Exit-Funktion aus und liefert die Rücksetzung.
// Nur für Tests (siehe loggingtest.ExpectFatal).
func SetExitForTest(f func(int)) (restore func()) {
	prev := exit
	exit = f
	return func() { exit = prev }
}
