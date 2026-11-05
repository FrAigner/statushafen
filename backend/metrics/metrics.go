// Package metrics implements the shared in-memory "Golden Signals"
// (Latency, Traffic, Errors, Saturation) aggregation for the backend.
//
// This started as Schritt 1/4 of the Golden-Signals initiative,
// which only collected and aggregated data in memory. Schritt 2/4 adds
// a Prometheus-compatible export of that same data (see prometheus.go and
// Registry.PrometheusRegistry) for an external Grafana Cloud scraper — the
// atomic in-memory aggregation below is untouched and still the source of
// truth for the still-open follow-ups:
// - Schritt 3/4: in-app dashboard
// - Schritt 4/4: saturation signals
//
// Both follow-ups are expected to read from the shared Registry exposed
// here (metrics.Default / Registry.Snapshot) independently of each other.
//
// Locking strategy: recording a request never takes a mutex. Each
// (method, route) pair gets its own *RouteMetrics entry looked up via a
// sync.Map (safe for concurrent reads, and the common case once all routes
// have been seen once is a pure lock-free read). All counters inside a
// RouteMetrics entry are sync/atomic values, so concurrent requests for the
// same route only ever contend on individual atomic add operations, never on
// a shared mutex around the whole request. The only case that briefly uses
// a lock is the discovery of a brand-new HTTP status code for a given route
// (via sync.Map.LoadOrStore), which happens at most once per distinct
// (method, route, status) combination for the lifetime of the process.
package metrics

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// LatencyBucketsMs defines the upper bounds (in milliseconds) of the
// latency histogram buckets used to aggregate request durations. There is
// one implicit final "+Inf" bucket for anything slower than the largest
// boundary. The values mirror commonly used Prometheus defaults so a later
// Prometheus exporter (Schritt 2/4) can reuse them as-is.
var LatencyBucketsMs = []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000}

// RouteMetrics aggregates Golden-Signals counters for exactly one
// (HTTP method, normalized route) pair. All fields are updated using atomic
// operations only, so recording a request never blocks on a mutex held
// across other routes/requests.
type RouteMetrics struct {
	Method string
	Route  string

	requestCount atomic.Uint64
	errorCount   atomic.Uint64 // status code >= 500 (4xx are normal client cases, not errors)
	sumLatencyNs atomic.Uint64
	maxLatencyNs atomic.Uint64

	statusCounts sync.Map // int32 status code -> *atomic.Uint64

	// bucketCounts[i] counts requests with latency <= LatencyBucketsMs[i].
	// The final element (index len(LatencyBucketsMs)) is the "+Inf" bucket.
	bucketCounts []atomic.Uint64
}

func newRouteMetrics(method, route string) *RouteMetrics {
	return &RouteMetrics{
		Method:       method,
		Route:        route,
		bucketCounts: make([]atomic.Uint64, len(LatencyBucketsMs)+1),
	}
}

// record atomically folds one observed request into this route's counters.
func (rm *RouteMetrics) record(statusCode int, latency time.Duration) {
	rm.requestCount.Add(1)
	if statusCode >= 500 {
		rm.errorCount.Add(1)
	}

	latencyNs := uint64(latency.Nanoseconds())
	rm.sumLatencyNs.Add(latencyNs)
	for {
		cur := rm.maxLatencyNs.Load()
		if latencyNs <= cur {
			break
		}
		if rm.maxLatencyNs.CompareAndSwap(cur, latencyNs) {
			break
		}
	}

	rm.incrementStatusCount(statusCode)

	latencyMs := float64(latencyNs) / 1e6
	idx := len(LatencyBucketsMs) // default: "+Inf" bucket
	for i, upperBound := range LatencyBucketsMs {
		if latencyMs <= upperBound {
			idx = i
			break
		}
	}
	rm.bucketCounts[idx].Add(1)
}

func (rm *RouteMetrics) incrementStatusCount(statusCode int) {
	if v, ok := rm.statusCounts.Load(statusCode); ok {
		v.(*atomic.Uint64).Add(1)
		return
	}
	fresh := &atomic.Uint64{}
	fresh.Add(1)
	actual, loaded := rm.statusCounts.LoadOrStore(statusCode, fresh)
	if loaded {
		// Someone else created the entry concurrently between our Load and
		// LoadOrStore; the "fresh" counter we created is discarded, so make
		// sure our observation is still counted on the entry that won.
		actual.(*atomic.Uint64).Add(1)
	}
}

// Snapshot is a point-in-time, read-only copy of one route's aggregated
// Golden Signals, safe to hand to callers (Prometheus exporter, in-app
// dashboard, tests) without any risk of racing further updates.
type Snapshot struct {
	Method       string
	Route        string
	RequestCount uint64
	ErrorCount   uint64
	SumLatencyMs float64
	AvgLatencyMs float64
	MaxLatencyMs float64
	StatusCounts map[int]uint64
	// BucketUpperBoundsMs mirrors LatencyBucketsMs at snapshot time.
	BucketUpperBoundsMs []float64
	// BucketCounts[i] is the (cumulative-free) count of requests whose
	// latency fell into the bucket bounded above by BucketUpperBoundsMs[i].
	// The final element is the "+Inf" bucket.
	BucketCounts []uint64
}

func (rm *RouteMetrics) snapshot() Snapshot {
	requestCount := rm.requestCount.Load()
	sumLatencyNs := rm.sumLatencyNs.Load()

	statusCounts := make(map[int]uint64)
	rm.statusCounts.Range(func(key, value any) bool {
		statusCounts[key.(int)] = value.(*atomic.Uint64).Load()
		return true
	})

	bucketCounts := make([]uint64, len(rm.bucketCounts))
	for i := range rm.bucketCounts {
		bucketCounts[i] = rm.bucketCounts[i].Load()
	}

	bucketUpperBounds := make([]float64, len(LatencyBucketsMs))
	copy(bucketUpperBounds, LatencyBucketsMs)

	var avgLatencyMs float64
	if requestCount > 0 {
		avgLatencyMs = float64(sumLatencyNs) / float64(requestCount) / 1e6
	}

	return Snapshot{
		Method:              rm.Method,
		Route:               rm.Route,
		RequestCount:        requestCount,
		ErrorCount:          rm.errorCount.Load(),
		SumLatencyMs:        float64(sumLatencyNs) / 1e6,
		AvgLatencyMs:        avgLatencyMs,
		MaxLatencyMs:        float64(rm.maxLatencyNs.Load()) / 1e6,
		StatusCounts:        statusCounts,
		BucketUpperBoundsMs: bucketUpperBounds,
		BucketCounts:        bucketCounts,
	}
}

// routeKey identifies one (method, route) aggregation bucket.
type routeKey struct {
	Method string
	Route  string
}

// Registry holds the Golden-Signals aggregation for every (method, route)
// combination observed so far. It is safe for concurrent use; see the
// package doc comment for the locking strategy.
type Registry struct {
	routes sync.Map // routeKey -> *RouteMetrics
	prom   *prometheusExporter

	// dbStatsProvider backs SaturationSnapshot's DB connection-pool numbers;
	// see saturation.go (Schritt 4/4). Kept as its own field
	// (rather than folded into the request-tracking fields/paths above) so
	// it applies/merges cleanly independent of the other in-flight
	// Golden-Signals follow-ups.
	dbStatsProvider atomic.Pointer[DBStatsProvider]
}

// NewRegistry creates an empty, ready-to-use Registry. Handlers/tests that
// want an isolated set of counters (e.g. to avoid cross-test pollution of
// the shared Default registry) should create their own via NewRegistry().
func NewRegistry() *Registry {
	r := &Registry{}
	// r.SaturationSnapshot is passed as a method value so the exporter can
	// pull a fresh Saturation reading on every Prometheus scrape (see
	// saturationCollector in prometheus.go) without prometheus.go needing to
	// know anything about Registry's internals. r is already a valid,
	// addressable *Registry at this point (only its zero-value fields, e.g.
	// dbStatsProvider, aren't populated yet), so capturing it here is safe.
	r.prom = newPrometheusExporter(r.SaturationSnapshot)
	return r
}

// PrometheusRegistry returns the prometheus.Registry that Observe keeps in
// sync with the in-memory aggregation, ready to be exposed via
// promhttp.HandlerFor (see backend/handlers/metrics.go).
func (r *Registry) PrometheusRegistry() *prometheus.Registry {
	return r.prom.registry
}

// Default is the process-wide registry that the global Gin instrumentation
// middleware (middleware.GoldenSignals) records into. The Prometheus
// exporter (Schritt 2/4) and in-app dashboard (Schritt 3/4) are expected to
// read from this same instance independently of each other.
var Default = NewRegistry()

// Observe atomically folds one request observation into the registry.
// method/route should already be normalized (e.g. route is a Gin route
// template such as "/api/pages/:page_id/incidents", never the raw URL with
// concrete IDs) to avoid unbounded cardinality growth.
func (r *Registry) Observe(method, route string, statusCode int, latency time.Duration) {
	key := routeKey{Method: method, Route: route}

	val, ok := r.routes.Load(key)
	if !ok {
		val, _ = r.routes.LoadOrStore(key, newRouteMetrics(method, route))
	}
	val.(*RouteMetrics).record(statusCode, latency)

	// Feed the Prometheus collectors in addition to (not instead of) the
	// atomic in-memory aggregation above, so /metrics stays consistent
	// with Snapshot() without the instrumentation middleware having to know
	// about both.
	r.prom.observe(method, route, statusCode, latency)
}

// RegisterRoute legt (method, route) mit 0 Requests an, falls noch kein Eintrag
// existiert: Die Monitoring-Tabelle soll auch Routen zeigen, die
// seit dem Start nie aufgerufen wurden. Idempotent; ein bereits gezählter
// Eintrag bleibt unverändert. Bewusst NUR die In-Memory-Aggregation, nicht die
// Prometheus-Collectors: Dort würde jede Route Zeitreihen (Counter je Status,
// Histogramm-Buckets) erzeugen, also ein Kardinalitätssprung. Prometheus legt
// sie wie bisher erst beim ersten Request an. route muss ein Gin-Template
// sein (nie ein konkreter Pfad mit IDs).
func (r *Registry) RegisterRoute(method, route string) {
	key := routeKey{Method: method, Route: route}
	if _, ok := r.routes.Load(key); ok {
		return
	}
	r.routes.LoadOrStore(key, newRouteMetrics(method, route))
}

// Snapshot returns a stable, point-in-time copy of every route's
// aggregated Golden Signals, sorted by route then method for deterministic
// output.
func (r *Registry) Snapshot() []Snapshot {
	snapshots := make([]Snapshot, 0)
	r.routes.Range(func(_, value any) bool {
		snapshots = append(snapshots, value.(*RouteMetrics).snapshot())
		return true
	})
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].Route != snapshots[j].Route {
			return snapshots[i].Route < snapshots[j].Route
		}
		return snapshots[i].Method < snapshots[j].Method
	})
	return snapshots
}

// Reset clears all aggregated data. Primarily useful for tests.
func (r *Registry) Reset() {
	r.routes.Range(func(key, _ any) bool {
		r.routes.Delete(key)
		return true
	})
}
