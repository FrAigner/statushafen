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

// RequestIDHeader carries the request ID in request and response.
const RequestIDHeader = "X-Request-ID"

// ContextKeyRequestID is the gin context key of the request ID.
const ContextKeyRequestID = "requestID"

// plausibleRequestID restricts adopted IDs to 64 harmless characters: the
// ID ends up unchanged in the log and the response header, a client must not use it to smuggle in
// either line breaks (log injection) or arbitrarily long values.
var plausibleRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// RequestLogger replaces the gin default logger: it adopts
// a plausible X-Request-ID (e.g. from the reverse proxy) or generates one,
// returns it in the response, puts a logger with request_id into the
// request context (logging.FromContext) and writes exactly one
// structured access log line per request. Only the path without query is logged -
// that is where unsubscribe and webhook tokens (?token=...) live, which the
// gin logger previously printed in plain text.
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

// accessLevel: 5xx Error, 4xx Warn, otherwise Info; the Docker healthcheck
// (/healthz every few seconds) only at Debug, so that it does not flood the log.
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

// Recovery replaces gin.Recovery: panics are logged in structured form
// with stack trace and request ID instead of as free text on stderr.
func Recovery() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, recovered any) {
		logging.FromContext(c.Request.Context()).Error("panic recovered",
			"panic", fmt.Sprint(recovered),
			"stack", string(debug.Stack()),
		)
		c.AbortWithStatus(http.StatusInternalServerError)
	})
}

// RequestIDFrom returns the request ID of the current request ("" without
// RequestLogger).
func RequestIDFrom(c *gin.Context) string {
	return c.GetString(ContextKeyRequestID)
}
