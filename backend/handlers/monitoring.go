package handlers

import (
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fraigner/statushafen/metrics"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// In-app monitoring dashboard (Golden Signals step 3/4)
//
// Reads exclusively from the aggregation layer backend/metrics/
// (metrics.Default) created in step 1/4. Independent of the
// Prometheus export from step 2/4 — serves as an in-app fallback
// if Grafana Cloud is unavailable/not set up.
//
// Route (in main.go, inside the authGroup):
//   authGroup.GET("/admin/monitoring", middleware.RequireGlobalAdmin(), h.GetMonitoringHandler)
// ─────────────────────────────────────────────────────────────────────────────

// processStartedAt is set at program start (var initialization) and
// serves as the basis for the "requests/minute" rate: the registry only counts
// cumulative totals since process start, but has no time dimension.
var processStartedAt = time.Now()

type monitoringRouteMetrics struct {
	Method            string         `json:"method"`
	Route             string         `json:"route"`
	RequestCount      uint64         `json:"request_count"`
	ErrorCount        uint64         `json:"error_count"`
	ErrorRate         float64        `json:"error_rate"`
	RequestsPerMinute float64        `json:"requests_per_minute"`
	AvgLatencyMs      float64        `json:"avg_latency_ms"`
	P50LatencyMs      float64        `json:"p50_latency_ms"`
	P95LatencyMs      float64        `json:"p95_latency_ms"`
	MaxLatencyMs      float64        `json:"max_latency_ms"`
	StatusCounts      map[int]uint64 `json:"status_counts"`
}

type monitoringTotals struct {
	RequestCount      uint64  `json:"request_count"`
	ErrorCount        uint64  `json:"error_count"`
	ErrorRate         float64 `json:"error_rate"`
	RequestsPerMinute float64 `json:"requests_per_minute"`
	AvgLatencyMs      float64 `json:"avg_latency_ms"`
	P50LatencyMs      float64 `json:"p50_latency_ms"`
	P95LatencyMs      float64 `json:"p95_latency_ms"`
	MaxLatencyMs      float64 `json:"max_latency_ms"`
}

// monitoringSaturation mirrors metrics.SaturationSnapshot (Golden Signals
// step 4/4) for the JSON response, alongside the
// Latency/Traffic/Error signals above. Kept as its own nested struct (rather
// than flattened into monitoringResponse) so the frontend can treat it as one
// self-contained "Saturation" section, matching the shape of Totals/Routes.
type monitoringSaturation struct {
	GoroutineCount int `json:"goroutine_count"`

	HeapAllocBytes  uint64 `json:"heap_alloc_bytes"`
	HeapSysBytes    uint64 `json:"heap_sys_bytes"`
	TotalAllocBytes uint64 `json:"total_alloc_bytes"`
	SysBytes        uint64 `json:"sys_bytes"`
	NumGC           uint32 `json:"num_gc"`

	// DBStatsAvailable is false (and the remaining DB* fields are zero) if no
	// DBStatsProvider was registered, e.g. in tests — mirrors
	// metrics.SaturationSnapshot.DBStatsAvailable, see saturation.go.
	DBStatsAvailable     bool    `json:"db_stats_available"`
	DBMaxOpenConnections int     `json:"db_max_open_connections"`
	DBOpenConnections    int     `json:"db_open_connections"`
	DBInUse              int     `json:"db_in_use"`
	DBIdle               int     `json:"db_idle"`
	DBWaitCount          int64   `json:"db_wait_count"`
	DBWaitDurationMs     float64 `json:"db_wait_duration_ms"`
}

type monitoringResponse struct {
	GeneratedAt   time.Time                `json:"generated_at"`
	UptimeSeconds float64                  `json:"uptime_seconds"`
	Totals        monitoringTotals         `json:"totals"`
	Routes        []monitoringRouteMetrics `json:"routes"`
	Saturation    monitoringSaturation     `json:"saturation"`
	// GrafanaDashboardURL: link to the Grafana dashboard of the respective environment
	// (previously hard-wired in the frontend to "statushafen-staging").
	// Empty if GRAFANA_DASHBOARD_URL is not set (or not set as an http(s) URL);
	// the frontend then hides the link.
	GrafanaDashboardURL string `json:"grafana_dashboard_url"`
}

// grafanaDashboardURL reads GRAFANA_DASHBOARD_URL. Only absolute http(s) URLs
// are passed through, so that a misconfiguration (e.g. "javascript:...")
// never ends up as an href in the admin area.
func grafanaDashboardURL() string {
	raw := strings.TrimSpace(os.Getenv("GRAFANA_DASHBOARD_URL"))
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return raw
}

func (h *Handler) GetMonitoringHandler(c *gin.Context) {
	snapshots := metrics.Default.Snapshot()
	uptime := time.Since(processStartedAt)
	uptimeMinutes := uptime.Minutes()

	resp := monitoringResponse{
		GeneratedAt:   time.Now(),
		UptimeSeconds: uptime.Seconds(),
		Routes:        make([]monitoringRouteMetrics, 0, len(snapshots)),

		GrafanaDashboardURL: grafanaDashboardURL(),
	}

	var totalRequests, totalErrors uint64
	var totalSumLatencyMs, totalMaxLatencyMs float64
	totalBucketCounts := make([]uint64, len(metrics.LatencyBucketsMs)+1)

	for _, s := range snapshots {
		row := monitoringRouteMetrics{
			Method:            s.Method,
			Route:             s.Route,
			RequestCount:      s.RequestCount,
			ErrorCount:        s.ErrorCount,
			ErrorRate:         safeRatio(s.ErrorCount, s.RequestCount),
			RequestsPerMinute: safeRate(s.RequestCount, uptimeMinutes),
			AvgLatencyMs:      s.AvgLatencyMs,
			P50LatencyMs:      estimateQuantileMs(s.BucketUpperBoundsMs, s.BucketCounts, s.RequestCount, s.MaxLatencyMs, 0.50),
			P95LatencyMs:      estimateQuantileMs(s.BucketUpperBoundsMs, s.BucketCounts, s.RequestCount, s.MaxLatencyMs, 0.95),
			MaxLatencyMs:      s.MaxLatencyMs,
			StatusCounts:      s.StatusCounts,
		}
		resp.Routes = append(resp.Routes, row)

		totalRequests += s.RequestCount
		totalErrors += s.ErrorCount
		totalSumLatencyMs += s.SumLatencyMs
		if s.MaxLatencyMs > totalMaxLatencyMs {
			totalMaxLatencyMs = s.MaxLatencyMs
		}
		for i, count := range s.BucketCounts {
			totalBucketCounts[i] += count
		}
	}

	// Sort by traffic (most important routes first) instead of the
	// route/method sorting from Registry.Snapshot(), which is
	// deterministic for tests but less useful for a dashboard.
	sort.Slice(resp.Routes, func(i, j int) bool {
		if resp.Routes[i].RequestCount != resp.Routes[j].RequestCount {
			return resp.Routes[i].RequestCount > resp.Routes[j].RequestCount
		}
		if resp.Routes[i].Route != resp.Routes[j].Route {
			return resp.Routes[i].Route < resp.Routes[j].Route
		}
		return resp.Routes[i].Method < resp.Routes[j].Method
	})

	resp.Totals = monitoringTotals{
		RequestCount:      totalRequests,
		ErrorCount:        totalErrors,
		ErrorRate:         safeRatio(totalErrors, totalRequests),
		RequestsPerMinute: safeRate(totalRequests, uptimeMinutes),
		AvgLatencyMs:      safeDiv(totalSumLatencyMs, totalRequests),
		P50LatencyMs:      estimateQuantileMs(metrics.LatencyBucketsMs, totalBucketCounts, totalRequests, totalMaxLatencyMs, 0.50),
		P95LatencyMs:      estimateQuantileMs(metrics.LatencyBucketsMs, totalBucketCounts, totalRequests, totalMaxLatencyMs, 0.95),
		MaxLatencyMs:      totalMaxLatencyMs,
	}

	sat := metrics.Default.SaturationSnapshot()
	resp.Saturation = monitoringSaturation{
		GoroutineCount:       sat.GoroutineCount,
		HeapAllocBytes:       sat.HeapAllocBytes,
		HeapSysBytes:         sat.HeapSysBytes,
		TotalAllocBytes:      sat.TotalAllocBytes,
		SysBytes:             sat.SysBytes,
		NumGC:                sat.NumGC,
		DBStatsAvailable:     sat.DBStatsAvailable,
		DBMaxOpenConnections: sat.DBMaxOpenConnections,
		DBOpenConnections:    sat.DBOpenConnections,
		DBInUse:              sat.DBInUse,
		DBIdle:               sat.DBIdle,
		DBWaitCount:          sat.DBWaitCount,
		DBWaitDurationMs:     sat.DBWaitDurationMs,
	}

	c.JSON(http.StatusOK, resp)
}

func safeRatio(numerator, denominator uint64) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func safeRate(count uint64, minutes float64) float64 {
	if minutes <= 0 {
		return 0
	}
	return float64(count) / minutes
}

func safeDiv(numerator float64, denominator uint64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / float64(denominator)
}

// estimateQuantileMs estimates the latency at the given quantile (e.g. 0.95 for
// p95) from the histogram buckets, analogous to the linear interpolation of
// Prometheus' histogram_quantile(). Exact percentiles would need the raw
// individual measurements, which the registry deliberately does not retain (see
// backend/metrics/metrics.go) — the result is therefore an approximation,
// limited by the bucket granularity in LatencyBucketsMs.
func estimateQuantileMs(bounds []float64, bucketCounts []uint64, total uint64, maxMs float64, quantile float64) float64 {
	if total == 0 {
		return 0
	}
	target := quantile * float64(total)

	var cumulative uint64
	prevBound := 0.0
	for i, count := range bucketCounts {
		cumulative += count
		if float64(cumulative) < target {
			if i < len(bounds) {
				prevBound = bounds[i]
			}
			continue
		}
		if i >= len(bounds) {
			// "+Inf" bucket without an upper bound: the best known value is the
			// maximum actually observed.
			return maxMs
		}
		upperBound := bounds[i]
		if count == 0 {
			return upperBound
		}
		countBefore := cumulative - count
		fraction := (target - float64(countBefore)) / float64(count)
		return prevBound + fraction*(upperBound-prevBound)
	}
	return maxMs
}
