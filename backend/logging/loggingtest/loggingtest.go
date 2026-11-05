// Package loggingtest stellt Testhilfen für logging.Fatal bereit:
// Fatal-Pfade sollen getestet werden, ohne den Testprozess zu beenden.
package loggingtest

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/fraigner/statushafen/logging"
)

// FatalCall beschreibt einen abgefangenen logging.Fatal-Aufruf.
type FatalCall struct {
	Code    int
	Message string
	Attrs   map[string]any
}

type exitSignal struct{ code int }

// captureHandler merkt sich den letzten Error-Eintrag.
type captureHandler struct {
	mu   *sync.Mutex
	last *FatalCall
}

func (h captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h captureHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h captureHandler) WithGroup(string) slog.Handler            { return h }
func (h captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	call := FatalCall{Message: r.Message, Attrs: map[string]any{}}
	r.Attrs(func(a slog.Attr) bool {
		call.Attrs[a.Key] = a.Value.Any()
		return true
	})
	*h.last = call
	return nil
}

// ExpectFatal führt fn aus und erwartet, dass sie logging.Fatal aufruft. Der
// Exit wird per panic abgefangen, damit der Code nach Fatal (wie in der
// Produktion nach os.Exit) nicht weiterläuft. Ohne Fatal schlägt der Test fehl.
func ExpectFatal(t testing.TB, fn func()) (got FatalCall) {
	t.Helper()
	var last FatalCall
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(captureHandler{mu: &sync.Mutex{}, last: &last}))
	restore := logging.SetExitForTest(func(code int) { panic(exitSignal{code}) })
	defer func() {
		restore()
		slog.SetDefault(prevLogger)
		r := recover()
		if r == nil {
			t.Errorf("logging.Fatal wurde nicht aufgerufen")
			return
		}
		sig, ok := r.(exitSignal)
		if !ok {
			panic(r)
		}
		got = last
		got.Code = sig.code
	}()
	fn()
	return got
}
