package handlers

import (
	"os"

	"github.com/fraigner/statushafen/metrics"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Env vars for /metrics Basic-Auth (Schritt 2/4). A Grafana
// Agent/Alloy scraper (or any other Prometheus-compatible client) sends them
// as a normal HTTP Basic-Auth header.
const (
	MetricsBasicAuthUserEnv     = "METRICS_BASIC_AUTH_USER"
	MetricsBasicAuthPasswordEnv = "METRICS_BASIC_AUTH_PASSWORD"
)

// RegisterMetricsEndpoint mounts a Prometheus-exposition-format /metrics
// endpoint on r, fed from reg (typically metrics.Default). It reports
// whether the endpoint was actually mounted.
//
// Basic-Auth was chosen over an IP allowlist because the scrape source is an
// external Grafana Agent/Alloy instance whose source IP is not fixed/known
// ahead of time (the deployment topology is still open) —
// credentials travel with the scrape config regardless of where the agent
// runs. If METRICS_BASIC_AUTH_USER/METRICS_BASIC_AUTH_PASSWORD aren't both
// set, /metrics is intentionally not mounted at all (fail-closed) rather
// than exposing it unauthenticated or crashing the whole server on startup
// for what is, for now, an opt-in feature unused by existing deployments.
func RegisterMetricsEndpoint(r gin.IRouter, reg *metrics.Registry) bool {
	user := os.Getenv(MetricsBasicAuthUserEnv)
	pass := os.Getenv(MetricsBasicAuthPasswordEnv)
	if user == "" || pass == "" {
		return false
	}

	promHandler := gin.WrapH(promhttp.HandlerFor(reg.PrometheusRegistry(), promhttp.HandlerOpts{}))
	r.GET("/metrics", gin.BasicAuth(gin.Accounts{user: pass}), promHandler)
	return true
}
