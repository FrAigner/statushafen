package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// lastJSON dekodiert die letzte Zeile eines JSON-Logs.
func lastJSON(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &out), buf.String())
	return out
}

func TestNew_FormatSelection(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		wantJSON bool
	}{
		{"Default lokal: Text", map[string]string{}, false},
		{"APP_ENV=production: JSON", map[string]string{"APP_ENV": "production"}, true},
		{"LOG_FORMAT=json: JSON", map[string]string{"LOG_FORMAT": "json"}, true},
		{"LOG_FORMAT=JSON (Groß): JSON", map[string]string{"LOG_FORMAT": " JSON "}, true},
		{"LOG_FORMAT=text schlägt APP_ENV=production", map[string]string{"APP_ENV": "production", "LOG_FORMAT": "text"}, false},
		{"APP_ENV=staging: Text", map[string]string{"APP_ENV": "staging"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			New(&buf, envMap(tc.env)).Info("hallo", "k", "v")
			line := strings.TrimSpace(buf.String())
			isJSON := json.Valid([]byte(line))
			assert.Equal(t, tc.wantJSON, isJSON, line)
			if !tc.wantJSON {
				assert.Contains(t, line, "msg=hallo")
				assert.Contains(t, line, "k=v")
			}
		})
	}
}

func TestNew_JSONHasTimestampWithZone(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, envMap(map[string]string{"LOG_FORMAT": "json"})).Info("x")
	out := lastJSON(t, &buf)
	ts, ok := out["time"].(string)
	require.True(t, ok)
	// RFC3339 mit Zonen-Offset bzw. "Z".
	assert.Regexp(t, `T\d\d:\d\d:\d\d.*(Z|[+-]\d\d:\d\d)$`, ts)
	assert.Equal(t, "INFO", out["level"])
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"":        slog.LevelInfo,
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		" info ":  slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"quatsch": slog.LevelInfo,
	}
	for in, want := range cases {
		got, ok := ParseLevel(in)
		assert.Equal(t, want, got, "Eingabe %q", in)
		assert.Equal(t, in != "quatsch", ok, "ok für %q", in)
	}
}

func TestNew_LevelFiltersAndWarnsOnInvalid(t *testing.T) {
	t.Run("LOG_LEVEL=warn unterdrückt Info", func(t *testing.T) {
		var buf bytes.Buffer
		l := New(&buf, envMap(map[string]string{"LOG_LEVEL": "warn"}))
		l.Info("leise")
		l.Warn("laut")
		assert.NotContains(t, buf.String(), "leise")
		assert.Contains(t, buf.String(), "laut")
	})
	t.Run("LOG_LEVEL=debug lässt Debug durch", func(t *testing.T) {
		var buf bytes.Buffer
		New(&buf, envMap(map[string]string{"LOG_LEVEL": "debug"})).Debug("detail")
		assert.Contains(t, buf.String(), "detail")
	})
	t.Run("ungültiger LOG_LEVEL: Warnung, Info als Fallback", func(t *testing.T) {
		var buf bytes.Buffer
		l := New(&buf, envMap(map[string]string{"LOG_LEVEL": "laut", "LOG_FORMAT": "json"}))
		out := lastJSON(t, &buf)
		assert.Equal(t, "WARN", out["level"])
		assert.Equal(t, "laut", out["value"])
		buf.Reset()
		l.Debug("weg")
		l.Info("da")
		assert.NotContains(t, buf.String(), "weg")
		assert.Contains(t, buf.String(), "da")
	})
}

func TestRedaction(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, envMap(map[string]string{"LOG_FORMAT": "json"}))
	l.Info("x",
		"password", "geheim",
		"Authorization", "Bearer abc",
		"webhook_token", "tok",
		"client_secret", "s3cr3t",
		"cookie", "sid=1",
		"email", "anna.schmidt@example.com",
		"user_id", 7,
		slog.Group("req", "token", "inner", "path", "/a"),
	)
	out := lastJSON(t, &buf)
	for _, k := range []string{"password", "Authorization", "webhook_token", "client_secret", "cookie"} {
		assert.Equal(t, Redacted, out[k], k)
	}
	assert.Equal(t, "a***@example.com", out["email"])
	assert.EqualValues(t, 7, out["user_id"])
	req := out["req"].(map[string]any)
	assert.Equal(t, Redacted, req["token"])
	assert.Equal(t, "/a", req["path"])
	assert.NotContains(t, buf.String(), "geheim")
	assert.NotContains(t, buf.String(), "anna.schmidt")
}

func TestMaskEmail(t *testing.T) {
	assert.Equal(t, "a***@example.com", MaskEmail("anna@example.com"))
	assert.Equal(t, "a***@x.de", MaskEmail("a@x.de"))
	assert.Equal(t, "***", MaskEmail("keine-mail"))
	assert.Equal(t, "***", MaskEmail("@nur-domain.de"))
	assert.Equal(t, "", MaskEmail(""))
}

func TestRedaction_NonStringEmailUntouched(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, envMap(map[string]string{"LOG_FORMAT": "json"})).Info("x", "email", 42)
	assert.EqualValues(t, 42, lastJSON(t, &buf)["email"])
}

func TestInit_SetsDefault(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	Init(&buf, envMap(map[string]string{"LOG_FORMAT": "json"}))
	slog.Info("über default")
	assert.Equal(t, "über default", lastJSON(t, &buf)["msg"])
}

func TestContextLogger(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, envMap(map[string]string{"LOG_FORMAT": "json"})).With("request_id", "abc")
	ctx := WithLogger(context.Background(), l)
	FromContext(ctx).Info("im request")
	assert.Equal(t, "abc", lastJSON(t, &buf)["request_id"])

	assert.Same(t, slog.Default(), FromContext(context.Background()))
	//nolint:staticcheck // bewusst nil-Context: Fallback muss greifen
	assert.Same(t, slog.Default(), FromContext(nil))
}

func TestFatal_LogsAndExits(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var buf bytes.Buffer
	slog.SetDefault(New(&buf, envMap(map[string]string{"LOG_FORMAT": "json"})))

	var code = -1
	restore := SetExitForTest(func(c int) { code = c })
	Fatal("kaputt", "error", "boom")
	restore()

	assert.Equal(t, 1, code)
	out := lastJSON(t, &buf)
	assert.Equal(t, "ERROR", out["level"])
	assert.Equal(t, "kaputt", out["msg"])
	assert.Equal(t, "boom", out["error"])
}

func TestSetExitForTest_Restores(t *testing.T) {
	called := false
	restore := SetExitForTest(func(int) { called = true })
	restore()
	// Nach restore muss wieder os.Exit hinterlegt sein - prüfen über den
	// Funktionszeiger, ohne den Prozess tatsächlich zu beenden.
	assert.False(t, called)
	assert.True(t, isOSExit())
}

// isOSExit prüft, ob exit (wieder) auf os.Exit zeigt.
func isOSExit() bool {
	return reflect.ValueOf(exit).Pointer() == reflect.ValueOf(os.Exit).Pointer()
}
