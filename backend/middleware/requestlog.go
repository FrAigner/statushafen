package middleware

import (
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/fraigner/statushafen/logging"

	"github.com/gin-gonic/gin"
)

// RequestIDHeader trägt die Request-ID in Anfrage und Antwort.
const RequestIDHeader = "X-Request-ID"

// ContextKeyRequestID ist der gin-Context-Schlüssel der Request-ID.
const ContextKeyRequestID = "requestID"

// plausibleRequestID begrenzt übernommene IDs auf 64 harmlose Zeichen: Die
// ID landet unverändert im Log und im Antwort-Header, ein Client darf darüber
// weder Zeilenumbrüche (Log-Injection) noch beliebig lange Werte einschleusen.
var plausibleRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// RequestLogger ersetzt den gin-Default-Logger: Er übernimmt
// eine plausible X-Request-ID (z.B. vom Reverse-Proxy) oder erzeugt eine,
// gibt sie in der Antwort zurück, legt einen Logger mit request_id in den
// Request-Context (logging.FromContext) und schreibt pro Request genau eine
// strukturierte Access-Log-Zeile. Geloggt wird nur der Pfad ohne Query -
// dort stehen u.a. Abmelde- und Webhook-Tokens (?token=...), die der
// gin-Logger bisher im Klartext ausgab.
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		id := c.GetHeader(RequestIDHeader)
		if !plausibleRequestID.MatchString(id) {
			id = rand.Text()
		}
		c.Set(ContextKeyRequestID, id)
		c.Header(RequestIDHeader, id)
		logger := slog.Default().With("request_id", id)
		c.Request = c.Request.WithContext(logging.WithLogger(c.Request.Context(), logger))

		c.Next()

		status := c.Writer.Status()
		attrs := []any{
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"route", c.FullPath(),
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		}
		if uid, ok := c.Get("userID"); ok {
			attrs = append(attrs, "user_id", uid)
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "errors", c.Errors.String())
		}
		logger.Log(c.Request.Context(), accessLevel(status, c.FullPath()), "http_request", attrs...)
	}
}

// accessLevel: 5xx Error, 4xx Warn, sonst Info; der Docker-Healthcheck
// (/healthz alle paar Sekunden) nur auf Debug, damit er das Log nicht flutet.
func accessLevel(status int, route string) slog.Level {
	if status >= http.StatusInternalServerError {
		return slog.LevelError
	}
	if status >= http.StatusBadRequest {
		return slog.LevelWarn
	}
	if route == "/healthz" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

// Recovery ersetzt gin.Recovery: Panics werden mit Stacktrace und Request-ID
// strukturiert geloggt statt als Freitext auf stderr.
func Recovery() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, recovered any) {
		logging.FromContext(c.Request.Context()).Error("panic recovered",
			"panic", fmt.Sprint(recovered),
			"stack", string(debug.Stack()),
		)
		c.AbortWithStatus(http.StatusInternalServerError)
	})
}

// RequestIDFrom liefert die Request-ID des laufenden Requests ("" ohne
// RequestLogger).
func RequestIDFrom(c *gin.Context) string {
	return c.GetString(ContextKeyRequestID)
}
