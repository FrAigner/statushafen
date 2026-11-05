package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// prometheusExporter mirrors every Registry.Observe call into standard
// Prometheus collectors (Schritt 2/4), so the /metrics endpoint
// exposes the exact same data as the in-memory Snapshot() used by tests and
// the future in-app dashboard — just in Prometheus exposition format
// for an external Grafana Agent/Alloy scraper.
//
// It lives on its own prometheus.Registry (not prometheus.DefaultRegisterer)
// so that creating multiple metrics.Registry instances in tests never
// triggers a "duplicate metrics collector registration" panic.
type prometheusExporter struct {
	registry        *prometheus.Registry
	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
}

// newPrometheusExporter wires up the Prometheus collectors for both the
// request-tracking Golden Signals (Latency/Traffic/Errors, fed via observe()
// below) and the Saturation golden signal, which is exposed via
// saturationSnapshot — typically Registry.SaturationSnapshot as a method
// value, see NewRegistry.
func newPrometheusExporter(saturationSnapshot func() SaturationSnapshot) *prometheusExporter {
	requestsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "statuspage_http_requests_total",
		Help: "Total number of HTTP requests processed, labeled by route, method and status code.",
	}, []string{"route", "method", "status"})

	requestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "statuspage_http_request_duration_seconds",
		Help: "HTTP request latency in seconds, labeled by route.",
		// Reuse the exact same bucket boundaries as the in-memory histogram
		// (LatencyBucketsMs, converted ms -> s) so both exposition paths agree.
		Buckets: latencyBucketsSeconds(),
	}, []string{"route"})

	reg := prometheus.NewRegistry()
	reg.MustRegister(requestsTotal, requestDuration, newSaturationCollector(saturationSnapshot))

	return &prometheusExporter{
		registry:        reg,
		requestsTotal:   requestsTotal,
		requestDuration: requestDuration,
	}
}

func latencyBucketsSeconds() []float64 {
	buckets := make([]float64, len(LatencyBucketsMs))
	for i, ms := range LatencyBucketsMs {
		buckets[i] = ms / 1000.0
	}
	return buckets
}

func (p *prometheusExporter) observe(method, route string, statusCode int, latency time.Duration) {
	p.requestsTotal.WithLabelValues(route, method, strconv.Itoa(statusCode)).Inc()
	p.requestDuration.WithLabelValues(route).Observe(latency.Seconds())
}

// ─────────────────────────────────────────────────────────────────────────────
// Saturation export (Golden Signals Schritt 4/4)
//
// The collection logic itself (goroutine count, memory stats, DB
// connection-pool stats) lives in saturation.go / Registry.SaturationSnapshot
// and was deliberately kept independent of Prometheus. saturationCollector
// below is the thin adapter that exposes the same numbers as
// statuspage_-prefixed gauges, following the naming convention of
// requestsTotal/requestDuration above.
//
// Implemented as a prometheus.Collector (rather than one prometheus.GaugeFunc
// per metric) so a single scrape triggers exactly one SaturationSnapshot()
// call — and therefore one runtime.ReadMemStats — no matter how many gauges
// are exposed.
// ─────────────────────────────────────────────────────────────────────────────

type saturationCollector struct {
	snapshot func() SaturationSnapshot

	goroutines *prometheus.Desc
	heapAlloc  *prometheus.Desc
	heapSys    *prometheus.Desc
	totalAlloc *prometheus.Desc
	sysBytes   *prometheus.Desc
	numGC      *prometheus.Desc

	dbMaxOpenConnections *prometheus.Desc
	dbOpenConnections    *prometheus.Desc
	dbInUse              *prometheus.Desc
	dbIdle               *prometheus.Desc
}

func newSaturationCollector(snapshot func() SaturationSnapshot) *saturationCollector {
	return &saturationCollector{
		snapshot: snapshot,

		goroutines: prometheus.NewDesc(
			"statuspage_goroutines",
			"Current number of goroutines (runtime.NumGoroutine()).",
			nil, nil),
		heapAlloc: prometheus.NewDesc(
			"statuspage_memory_heap_alloc_bytes",
			"Bytes of allocated, reachable heap objects (runtime.MemStats.HeapAlloc).",
			nil, nil),
		heapSys: prometheus.NewDesc(
			"statuspage_memory_heap_sys_bytes",
			"Bytes of heap obtained from the OS (runtime.MemStats.HeapSys).",
			nil, nil),
		totalAlloc: prometheus.NewDesc(
			"statuspage_memory_total_alloc_bytes",
			"Cumulative bytes allocated over the process lifetime, includes freed memory (runtime.MemStats.TotalAlloc).",
			nil, nil),
		sysBytes: prometheus.NewDesc(
			"statuspage_memory_sys_bytes",
			"Total bytes obtained from the OS for this process (runtime.MemStats.Sys).",
			nil, nil),
		numGC: prometheus.NewDesc(
			"statuspage_memory_gc_cycles_total",
			"Number of completed garbage-collection cycles (runtime.MemStats.NumGC).",
			nil, nil),

		dbMaxOpenConnections: prometheus.NewDesc(
			"statushafen_db_connections_max_open",
			"Configured maximum number of open DB connections (database/sql.DBStats.MaxOpenConnections). 0 if unavailable.",
			nil, nil),
		dbOpenConnections: prometheus.NewDesc(
			"statushafen_db_connections_open",
			"Current number of established DB connections, in use or idle (database/sql.DBStats.OpenConnections). 0 if unavailable.",
			nil, nil),
		dbInUse: prometheus.NewDesc(
			"statushafen_db_connections_in_use",
			"Current number of DB connections in use (database/sql.DBStats.InUse). 0 if unavailable.",
			nil, nil),
		dbIdle: prometheus.NewDesc(
			"statushafen_db_connections_idle",
			"Current number of idle DB connections (database/sql.DBStats.Idle). 0 if unavailable.",
			nil, nil),
	}
}

func (c *saturationCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.goroutines
	ch <- c.heapAlloc
	ch <- c.heapSys
	ch <- c.totalAlloc
	ch <- c.sysBytes
	ch <- c.numGC
	ch <- c.dbMaxOpenConnections
	ch <- c.dbOpenConnections
	ch <- c.dbInUse
	ch <- c.dbIdle
}

func (c *saturationCollector) Collect(ch chan<- prometheus.Metric) {
	snap := c.snapshot()

	ch <- prometheus.MustNewConstMetric(c.goroutines, prometheus.GaugeValue, float64(snap.GoroutineCount))
	ch <- prometheus.MustNewConstMetric(c.heapAlloc, prometheus.GaugeValue, float64(snap.HeapAllocBytes))
	ch <- prometheus.MustNewConstMetric(c.heapSys, prometheus.GaugeValue, float64(snap.HeapSysBytes))
	ch <- prometheus.MustNewConstMetric(c.totalAlloc, prometheus.GaugeValue, float64(snap.TotalAllocBytes))
	ch <- prometheus.MustNewConstMetric(c.sysBytes, prometheus.GaugeValue, float64(snap.SysBytes))
	ch <- prometheus.MustNewConstMetric(c.numGC, prometheus.GaugeValue, float64(snap.NumGC))

	// DB connection-pool gauges are only meaningful once a DBStatsProvider
	// has been registered (see saturation.go); report 0 rather than omitting
	// the series entirely, so dashboards/alerts built against these metric
	// names don't have to special-case "series absent" vs. "genuinely zero".
	if snap.DBStatsAvailable {
		ch <- prometheus.MustNewConstMetric(c.dbMaxOpenConnections, prometheus.GaugeValue, float64(snap.DBMaxOpenConnections))
		ch <- prometheus.MustNewConstMetric(c.dbOpenConnections, prometheus.GaugeValue, float64(snap.DBOpenConnections))
		ch <- prometheus.MustNewConstMetric(c.dbInUse, prometheus.GaugeValue, float64(snap.DBInUse))
		ch <- prometheus.MustNewConstMetric(c.dbIdle, prometheus.GaugeValue, float64(snap.DBIdle))
	} else {
		ch <- prometheus.MustNewConstMetric(c.dbMaxOpenConnections, prometheus.GaugeValue, 0)
		ch <- prometheus.MustNewConstMetric(c.dbOpenConnections, prometheus.GaugeValue, 0)
		ch <- prometheus.MustNewConstMetric(c.dbInUse, prometheus.GaugeValue, 0)
		ch <- prometheus.MustNewConstMetric(c.dbIdle, prometheus.GaugeValue, 0)
	}
}
