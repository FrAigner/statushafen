package middleware

import (
	"net/http"
	"time"

	"github.com/fraigner/statushafen/metrics"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// GoldenSignals: central instrumentation middleware (step 1/4).
// Records for EVERY request the route (normalized, see below), HTTP method,
// status code and latency and writes them to the supplied metrics.Registry.
//
// Mount globally in main.go, analogous to CORSMiddleware()/SecurityHeaders():
//
//	r.Use(middleware.GoldenSignals(metrics.Default))
//
// Deliberately a single central middleware instead of changes to individual
// handlers, so that all ~45-50 endpoints are recorded uniformly.
//
// Route normalization: c.FullPath() returns the Gin route template (e.g.
// "/api/pages/:page_id/incidents"), NOT the concrete URL with real IDs –
// this is essential to avoid a cardinality explosion in the aggregation
// (otherwise a separate counter per incident/page ID). For requests
// that match no route (404 via NoRoute), c.FullPath() is empty; these
// are deliberately bundled under the fixed constant "NOT_FOUND" instead of under
// the raw path, for the same cardinality reason.
//
// Performance: the middleware itself holds no locks at all – it computes
// only the latency and delegates to metrics.Registry.Observe, which updates
// only atomic counters per route (see backend/metrics/metrics.go).
// ─────────────────────────────────────────────────────────────────────────────

// notFoundRouteLabel bundles all unmatched routes, so that an
// attacker cannot create arbitrarily many aggregation entries
// via arbitrary paths.
const notFoundRouteLabel = "NOT_FOUND"

// otherMethodLabel bundles all HTTP methods outside knownMethods. The
// method is a freely choosable token; as a label every invented
// method (even up to about 1 MB long) would create a new time series.
const otherMethodLabel = "OTHER"

// knownMethods are the methods the API uses or sensibly
// answers.
var knownMethods = map[string]struct{}{
	http.MethodGet: {}, http.MethodPost: {}, http.MethodPut: {}, http.MethodPatch: {},
	http.MethodDelete: {}, http.MethodHead: {}, http.MethodOptions: {},
}

func methodLabel(method string) string {
	if _, ok := knownMethods[method]; ok {
		return method
	}
	return otherMethodLabel
}

// GoldenSignals returns a Gin middleware that aggregates every request into reg.
// reg is typically metrics.Default; tests can pass an
// isolated metrics.NewRegistry() to avoid interfering with
// other tests.
func GoldenSignals(reg *metrics.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		latency := time.Since(start)

		route := c.FullPath()
		if route == "" {
			route = notFoundRouteLabel
		}

		reg.Observe(methodLabel(c.Request.Method), route, c.Writer.Status(), latency)
	}
}
