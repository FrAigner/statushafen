package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/logging/loggingtest"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Die Start-Guards in main.go laufen über logging.Fatal und sind
// damit testbar, ohne den Testprozess zu beenden.
func TestCORSMiddleware_WildcardIsFatal(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "https://a.example, *")
	got := loggingtest.ExpectFatal(t, func() { CORSMiddleware() })
	assert.Equal(t, 1, got.Code)
	assert.Contains(t, got.Message, "CORS_ORIGINS darf kein '*' enthalten")
}

// initLogging muss .env VOR dem Logger-Aufbau laden, damit LOG_FORMAT/
// LOG_LEVEL aus einer lokalen .env greifen; lokal ohne Vorgabe: Text.
func TestInitLogging_UsesLogFormatFromDotEnv(t *testing.T) {
	prevLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prevLogger) })
	t.Setenv("APP_ENV", "")
	t.Setenv("LOG_FORMAT", "")
	require.NoError(t, os.Unsetenv("LOG_FORMAT"))
	t.Chdir(t.TempDir())

	initLogging()
	_, isText := slog.Default().Handler().(*slog.TextHandler)
	assert.True(t, isText, "ohne .env/APP_ENV: Text-Handler")

	require.NoError(t, os.WriteFile(".env", []byte("LOG_FORMAT=json\n"), 0o600))
	initLogging()
	_, isJSON := slog.Default().Handler().(*slog.JSONHandler)
	assert.True(t, isJSON, "LOG_FORMAT=json aus .env muss greifen")
	require.NoError(t, os.Unsetenv("LOG_FORMAT"))
}

func TestNewRouter_InvalidTrustedProxiesIsFatal(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	t.Setenv("TRUSTED_PROXIES", "kein-cidr")
	h := handlers.NewHandler(testutils.SetupTestDB())
	got := loggingtest.ExpectFatal(t, func() { newRouter(h) })
	assert.Equal(t, "Ungültige TRUSTED_PROXIES-Konfiguration", got.Message)
	assert.NotNil(t, got.Attrs["error"])
}

// newRouter nutzt RequestLogger statt des gin-Default-Loggers -
// jede Antwort trägt eine X-Request-ID.
func TestNewRouter_SetsRequestID(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	t.Setenv("TRUSTED_PROXIES", "")
	r := newRouter(handlers.NewHandler(testutils.SetupTestDB()))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.NotEmpty(t, w.Header().Get(middleware.RequestIDHeader))
}
