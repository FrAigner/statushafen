package loggingtest

import (
	"log/slog"
	"testing"

	"github.com/fraigner/statushafen/logging"

	"github.com/stretchr/testify/assert"
)

func TestExpectFatal_ReturnsMessageAndAttrs(t *testing.T) {
	got := ExpectFatal(t, func() {
		logging.Fatal("weg damit", "error", "x")
		t.Fatal("nach Fatal darf nichts mehr laufen")
	})
	assert.Equal(t, "weg damit", got.Message)
	assert.Equal(t, "x", got.Attrs["error"])
	assert.Equal(t, 1, got.Code)
}

func TestExpectFatal_RestoresDefaultLogger(t *testing.T) {
	before := slog.Default()
	ExpectFatal(t, func() { logging.Fatal("x") })
	assert.Same(t, before, slog.Default())
}

// fakeT intercepts Errorf so that the error path (fn ends without Fatal) is checkable.
type fakeT struct {
	testing.TB
	failed bool
}

func (f *fakeT) Helper()               {}
func (f *fakeT) Errorf(string, ...any) { f.failed = true }

func TestExpectFatal_FailsWithoutFatal(t *testing.T) {
	ft := &fakeT{TB: t}
	got := ExpectFatal(ft, func() {})
	assert.True(t, ft.failed)
	assert.Equal(t, "", got.Message)
}

// A derived logger (With/WithGroup) must keep writing into the capture,
// otherwise Fatal calls via slog.Default().With(...) would be lost.
func TestExpectFatal_DerivedLoggerStillCaptured(t *testing.T) {
	got := ExpectFatal(t, func() {
		slog.Default().With("a", 1).WithGroup("g").Error("abgeleitet")
		logging.Fatal("ende")
	})
	assert.Equal(t, "ende", got.Message)
}

func TestExpectFatal_RepanicsForeignPanic(t *testing.T) {
	assert.PanicsWithValue(t, "fremd", func() {
		ExpectFatal(t, func() { panic("fremd") })
	})
}
