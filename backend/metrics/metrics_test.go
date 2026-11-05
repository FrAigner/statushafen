package metrics

import (
	"sync"
	"testing"
	"time"
)

func TestObserve_CountsTrafficLatencyAndStatus(t *testing.T) {
	reg := NewRegistry()

	reg.Observe("GET", "/api/pages/:page_id/incidents", 200, 10*time.Millisecond)
	reg.Observe("GET", "/api/pages/:page_id/incidents", 200, 20*time.Millisecond)
	reg.Observe("GET", "/api/pages/:page_id/incidents", 404, 5*time.Millisecond)
	reg.Observe("GET", "/api/pages/:page_id/incidents", 500, 30*time.Millisecond)

	snaps := reg.Snapshot()
	if len(snaps) != 1 {
		t.Fatalf("expected exactly 1 aggregated route, got %d: %+v", len(snaps), snaps)
	}

	s := snaps[0]
	if s.Method != "GET" || s.Route != "/api/pages/:page_id/incidents" {
		t.Fatalf("unexpected route key: %+v", s)
	}
	if s.RequestCount != 4 {
		t.Errorf("expected RequestCount=4, got %d", s.RequestCount)
	}
	if s.ErrorCount != 1 {
		t.Errorf("expected ErrorCount=1 (only the 500), got %d", s.ErrorCount)
	}
	if s.StatusCounts[200] != 2 {
		t.Errorf("expected 2x status 200, got %d", s.StatusCounts[200])
	}
	if s.StatusCounts[404] != 1 {
		t.Errorf("expected 1x status 404, got %d", s.StatusCounts[404])
	}
	if s.StatusCounts[500] != 1 {
		t.Errorf("expected 1x status 500, got %d", s.StatusCounts[500])
	}
	wantAvg := (10.0 + 20.0 + 5.0 + 30.0) / 4.0
	if diff := s.AvgLatencyMs - wantAvg; diff > 0.01 || diff < -0.01 {
		t.Errorf("expected AvgLatencyMs~=%.2f, got %.2f", wantAvg, s.AvgLatencyMs)
	}
	if diff := s.MaxLatencyMs - 30.0; diff > 0.01 || diff < -0.01 {
		t.Errorf("expected MaxLatencyMs~=30, got %.2f", s.MaxLatencyMs)
	}

	var bucketTotal uint64
	for _, c := range s.BucketCounts {
		bucketTotal += c
	}
	if bucketTotal != s.RequestCount {
		t.Errorf("histogram bucket counts (%d) should sum to RequestCount (%d)", bucketTotal, s.RequestCount)
	}
}

func TestObserve_DistinctRoutesAndMethodsAreSeparateBuckets(t *testing.T) {
	reg := NewRegistry()

	reg.Observe("GET", "/api/pages/:page_id/components", 200, time.Millisecond)
	reg.Observe("POST", "/api/pages/:page_id/components", 201, time.Millisecond)
	reg.Observe("GET", "/api/pages/:page_id/incidents", 200, time.Millisecond)

	snaps := reg.Snapshot()
	if len(snaps) != 3 {
		t.Fatalf("expected 3 distinct (method, route) buckets, got %d: %+v", len(snaps), snaps)
	}

	seen := make(map[string]bool)
	for _, s := range snaps {
		seen[s.Method+" "+s.Route] = true
	}
	for _, want := range []string{
		"GET /api/pages/:page_id/components",
		"POST /api/pages/:page_id/components",
		"GET /api/pages/:page_id/incidents",
	} {
		if !seen[want] {
			t.Errorf("expected a bucket for %q, got %+v", want, snaps)
		}
	}
}

func TestObserve_ConcurrentUseIsRace_Free(t *testing.T) {
	// Run with `go test -race` to actually validate the locking strategy;
	// this test exercises high concurrency across a handful of routes and
	// verifies the counters are exact (no lost updates), which they must be
	// even under `-race` given the atomic-only update path.
	reg := NewRegistry()

	const goroutines = 50
	const perGoroutine = 200
	routes := []string{"/api/a", "/api/b", "/api/c"}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			route := routes[g%len(routes)]
			for i := 0; i < perGoroutine; i++ {
				status := 200
				if i%10 == 0 {
					status = 500
				} else if i%7 == 0 {
					status = 404
				}
				reg.Observe("GET", route, status, time.Duration(i%5)*time.Millisecond)
			}
		}(g)
	}
	wg.Wait()

	var total uint64
	for _, s := range reg.Snapshot() {
		total += s.RequestCount
	}
	want := uint64(goroutines * perGoroutine)
	if total != want {
		t.Fatalf("expected total RequestCount=%d across all routes, got %d (lost updates under concurrency)", want, total)
	}
}

func TestReset_ClearsAllData(t *testing.T) {
	reg := NewRegistry()
	reg.Observe("GET", "/api/foo", 200, time.Millisecond)

	if len(reg.Snapshot()) == 0 {
		t.Fatal("expected at least one route after Observe")
	}

	reg.Reset()

	if got := len(reg.Snapshot()); got != 0 {
		t.Fatalf("expected empty snapshot after Reset, got %d entries", got)
	}
}

func TestDefaultRegistry_IsUsable(t *testing.T) {
	if Default == nil {
		t.Fatal("expected package-level Default registry to be initialized")
	}
	Default.Observe("GET", "/api/default-registry-smoke-test", 200, time.Millisecond)
	found := false
	for _, s := range Default.Snapshot() {
		if s.Route == "/api/default-registry-smoke-test" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected observation to show up in Default.Snapshot()")
	}
}

// Regression: normale 4xx-Antworten (404 ohne Abo, 409, 422, 429) sind keine
// Fehler; nur Status >= 500 zaehlt in ErrorCount.
func TestRegistry_OnlyServerErrorsCountAsErrors(t *testing.T) {
	reg := NewRegistry()
	for _, code := range []int{200, 302, 399, 400, 404, 409, 422, 429, 499} {
		reg.Observe("GET", "/api/x", code, time.Millisecond)
	}
	if got := reg.Snapshot()[0].ErrorCount; got != 0 {
		t.Fatalf("4xx must not count as error, got ErrorCount=%d", got)
	}
	reg.Observe("GET", "/api/x", 500, time.Millisecond)
	reg.Observe("GET", "/api/x", 503, time.Millisecond)
	s := reg.Snapshot()[0]
	if s.ErrorCount != 2 || s.RequestCount != 11 {
		t.Fatalf("expected 2 errors of 11 requests, got %+v", s)
	}
}

// Routen ohne Requests erscheinen mit 0 im Snapshot.
func TestRegisterRoute_AppearsWithZeroRequests(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterRoute("GET", "/api/pages/:page_id/incidents")

	snaps := reg.Snapshot()
	if len(snaps) != 1 {
		t.Fatalf("expected 1 route, got %+v", snaps)
	}
	s := snaps[0]
	if s.Method != "GET" || s.Route != "/api/pages/:page_id/incidents" {
		t.Errorf("unexpected key: %+v", s)
	}
	if s.RequestCount != 0 || s.ErrorCount != 0 || s.AvgLatencyMs != 0 || s.MaxLatencyMs != 0 {
		t.Errorf("expected all-zero counters, got %+v", s)
	}
}

func TestRegisterRoute_IdempotentAndKeepsCounts(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterRoute("GET", "/a")
	reg.RegisterRoute("GET", "/a")
	reg.Observe("GET", "/a", 200, time.Millisecond)
	reg.RegisterRoute("GET", "/a") // darf den Zähler nicht zurücksetzen
	reg.RegisterRoute("POST", "/a")

	snaps := reg.Snapshot()
	if len(snaps) != 2 {
		t.Fatalf("expected 2 entries (GET/POST), got %+v", snaps)
	}
	if snaps[0].Method != "GET" || snaps[0].RequestCount != 1 {
		t.Errorf("GET /a must keep its count, got %+v", snaps[0])
	}
	if snaps[1].Method != "POST" || snaps[1].RequestCount != 0 {
		t.Errorf("POST /a must be 0, got %+v", snaps[1])
	}
}

// Prometheus-Kardinalität: registrierte, nie aufgerufene Routen erzeugen
// keine Zeitreihen.
func TestRegisterRoute_NoPrometheusSeries(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterRoute("GET", "/ungenutzt")

	mfs, err := reg.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetValue() == "/ungenutzt" {
					t.Errorf("unexpected Prometheus series in %s for unused route", mf.GetName())
				}
			}
		}
	}
}
