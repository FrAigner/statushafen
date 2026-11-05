package metrics

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// gaugesByName flattens Gather()'s metric families into a name->value map of
// gauge samples, avoiding the repeated nested-loop boilerplate across the
// saturation-metrics tests below.
func gaugesByName(families []*dto.MetricFamily) map[string]float64 {
	byName := make(map[string]float64)
	for _, mf := range families {
		for _, m := range mf.GetMetric() {
			if g := m.GetGauge(); g != nil {
				byName[mf.GetName()] = g.GetValue()
			}
		}
	}
	return byName
}

// findHistogramSample returns the first histogram sample for the given
// metric family name, if present.
func findHistogramSample(families []*dto.MetricFamily, name string) (*dto.Histogram, bool) {
	for _, mf := range families {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if h := m.GetHistogram(); h != nil {
				return h, true
			}
		}
	}
	return nil, false
}

func TestPrometheusRegistry_ObserveFeedsCounterAndHistogram(t *testing.T) {
	reg := NewRegistry()

	reg.Observe("GET", "/api/pages/:page_id/incidents", 200, 10*time.Millisecond)
	reg.Observe("GET", "/api/pages/:page_id/incidents", 200, 20*time.Millisecond)
	reg.Observe("GET", "/api/pages/:page_id/incidents", 404, 5*time.Millisecond)

	got := testutil.CollectAndCount(reg.PrometheusRegistry())
	if got == 0 {
		t.Fatal("expected at least one Prometheus metric family after Observe, got none")
	}

	wantCounter := `
# HELP statuspage_http_requests_total Total number of HTTP requests processed, labeled by route, method and status code.
# TYPE statuspage_http_requests_total counter
statuspage_http_requests_total{method="GET",route="/api/pages/:page_id/incidents",status="200"} 2
statuspage_http_requests_total{method="GET",route="/api/pages/:page_id/incidents",status="404"} 1
`
	if err := testutil.GatherAndCompare(reg.PrometheusRegistry(), strings.NewReader(wantCounter), "statuspage_http_requests_total"); err != nil {
		t.Errorf("unexpected statuspage_http_requests_total output: %v", err)
	}
}

func TestPrometheusRegistry_HistogramObservesLatencyInSeconds(t *testing.T) {
	reg := NewRegistry()
	reg.Observe("GET", "/api/slow", 200, 250*time.Millisecond)

	metricFamilies, err := reg.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	h, found := findHistogramSample(metricFamilies, "statuspage_http_request_duration_seconds")
	if !found {
		t.Fatal("expected a statuspage_http_request_duration_seconds histogram sample for /api/slow")
	}
	if h.GetSampleCount() != 1 {
		t.Errorf("expected SampleCount=1, got %d", h.GetSampleCount())
	}
	if diff := h.GetSampleSum() - 0.25; diff > 0.001 || diff < -0.001 {
		t.Errorf("expected SampleSum~=0.25s, got %f", h.GetSampleSum())
	}
}

// TestPrometheusRegistry_ExposesSaturationMetrics verifies the Schritt 4/4
// integration: the Prometheus export includes goroutine/memory/DB-pool
// gauges sourced from Registry.SaturationSnapshot, with plausible values.
func TestPrometheusRegistry_ExposesSaturationMetrics(t *testing.T) {
	reg := NewRegistry()
	reg.SetDBStatsProvider(func() sql.DBStats {
		return sql.DBStats{
			MaxOpenConnections: 25,
			OpenConnections:    10,
			InUse:              4,
			Idle:               6,
		}
	})

	families, err := reg.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}
	byName := gaugesByName(families)

	t.Run("Runtime-Gauges (Goroutines/Memory) sind exportiert", func(t *testing.T) {
		if got := byName["statuspage_goroutines"]; got <= 0 {
			t.Errorf("expected statuspage_goroutines > 0, got %v", got)
		}
		if got := byName["statuspage_memory_heap_alloc_bytes"]; got <= 0 {
			t.Errorf("expected statuspage_memory_heap_alloc_bytes > 0, got %v", got)
		}
		if _, ok := byName["statuspage_memory_heap_sys_bytes"]; !ok {
			t.Errorf("expected statuspage_memory_heap_sys_bytes to be exported")
		}
		if _, ok := byName["statuspage_memory_sys_bytes"]; !ok {
			t.Errorf("expected statuspage_memory_sys_bytes to be exported")
		}
		if _, ok := byName["statuspage_memory_gc_cycles_total"]; !ok {
			t.Errorf("expected statuspage_memory_gc_cycles_total to be exported")
		}
	})

	t.Run("DB-Pool-Gauges spiegeln den DBStatsProvider wider", func(t *testing.T) {
		if got := byName["statushafen_db_connections_max_open"]; got != 25 {
			t.Errorf("expected statushafen_db_connections_max_open=25, got %v", got)
		}
		if got := byName["statushafen_db_connections_open"]; got != 10 {
			t.Errorf("expected statushafen_db_connections_open=10, got %v", got)
		}
		if got := byName["statushafen_db_connections_in_use"]; got != 4 {
			t.Errorf("expected statushafen_db_connections_in_use=4, got %v", got)
		}
		if got := byName["statushafen_db_connections_idle"]; got != 6 {
			t.Errorf("expected statushafen_db_connections_idle=6, got %v", got)
		}
	})
}

// TestPrometheusRegistry_SaturationDBMetricsZeroWithoutProvider verifies that
// when no DBStatsProvider is registered, the DB connection-pool gauges are
// still exported (as 0) rather than omitted, mirroring
// SaturationSnapshot.DBStatsAvailable=false.
func TestPrometheusRegistry_SaturationDBMetricsZeroWithoutProvider(t *testing.T) {
	reg := NewRegistry()

	families, err := reg.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	byName := gaugesByName(families)

	for _, name := range []string{
		"statushafen_db_connections_max_open",
		"statushafen_db_connections_open",
		"statushafen_db_connections_in_use",
		"statushafen_db_connections_idle",
	} {
		if got, ok := byName[name]; !ok || got != 0 {
			t.Errorf("expected %s=0 without a DBStatsProvider, got %v (present=%v)", name, got, ok)
		}
	}
}

func TestPrometheusRegistry_IsolatedPerRegistry(t *testing.T) {
	regA := NewRegistry()
	regB := NewRegistry()

	regA.Observe("GET", "/api/only-in-a", 200, time.Millisecond)

	countB := testutil.CollectAndCount(regB.PrometheusRegistry(), "statuspage_http_requests_total")
	if countB != 0 {
		t.Errorf("expected regB's Prometheus registry to be unaffected by regA.Observe, got %d samples", countB)
	}
}
