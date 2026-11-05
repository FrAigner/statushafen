package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fraigner/statushafen/metrics"
	"github.com/fraigner/statushafen/middleware"

	"github.com/gin-gonic/gin"
)

func newInstrumentedTestRouter(reg *metrics.Registry) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.GoldenSignals(reg))

	r.GET("/api/pages/:page_id/incidents", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/api/pages/:page_id/incidents/:id", func(c *gin.Context) {
		id := c.Param("id")
		if id == "missing" {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": id})
	})
	r.POST("/api/login", func(c *gin.Context) {
		// Simulate a validation/auth failure path.
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
	})
	r.GET("/api/boom", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "boom"})
	})
	r.GET("/api/slow", func(c *gin.Context) {
		time.Sleep(5 * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	return r
}

func doRequest(r *gin.Engine, method, path string) int {
	req, _ := http.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestGoldenSignals_RecordsSuccessTrafficAndLatency(t *testing.T) {
	reg := metrics.NewRegistry()
	r := newInstrumentedTestRouter(reg)

	if code := doRequest(r, "GET", "/api/pages/42/incidents"); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if code := doRequest(r, "GET", "/api/pages/99/incidents"); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}

	snaps := reg.Snapshot()
	var found *metrics.Snapshot
	for i := range snaps {
		if snaps[i].Route == "/api/pages/:page_id/incidents" && snaps[i].Method == "GET" {
			found = &snaps[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a normalized-route entry for /api/pages/:page_id/incidents, got %+v", snaps)
	}
	// Two requests with different concrete page IDs (42, 99) must be folded
	// into the SAME aggregated route entry (normalized via c.FullPath()),
	// not counted as two distinct routes -- this is the cardinality-safety
	// requirement from the issue.
	if found.RequestCount != 2 {
		t.Errorf("expected RequestCount=2 for the normalized route (both concrete IDs folded together), got %d", found.RequestCount)
	}
	if found.StatusCounts[200] != 2 {
		t.Errorf("expected 2x status 200, got %d", found.StatusCounts[200])
	}
	if found.ErrorCount != 0 {
		t.Errorf("expected ErrorCount=0 for all-success traffic, got %d", found.ErrorCount)
	}
}

func TestGoldenSignals_RecordsClientAndServerErrors(t *testing.T) {
	reg := metrics.NewRegistry()
	r := newInstrumentedTestRouter(reg)

	// Deliberately trigger a 404 (client error) and a 401 (client error) and
	// a 500 (server error).
	if code := doRequest(r, "GET", "/api/pages/1/incidents/missing"); code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
	if code := doRequest(r, "POST", "/api/login"); code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", code)
	}
	if code := doRequest(r, "GET", "/api/boom"); code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", code)
	}

	snaps := reg.Snapshot()
	byKey := make(map[string]metrics.Snapshot)
	for _, s := range snaps {
		byKey[s.Method+" "+s.Route] = s
	}

	incidentDetail, ok := byKey["GET /api/pages/:page_id/incidents/:id"]
	if !ok {
		t.Fatalf("missing snapshot for incident detail route, got %+v", snaps)
	}
	if incidentDetail.RequestCount != 1 || incidentDetail.ErrorCount != 0 || incidentDetail.StatusCounts[404] != 1 {
		t.Errorf("expected 1 request / 0 errors / 1x404 for incident detail route, got %+v", incidentDetail)
	}

	login, ok := byKey["POST /api/login"]
	if !ok {
		t.Fatalf("missing snapshot for login route, got %+v", snaps)
	}
	if login.RequestCount != 1 || login.ErrorCount != 0 || login.StatusCounts[401] != 1 {
		t.Errorf("expected 1 request / 0 errors / 1x401 for login route, got %+v", login)
	}

	boom, ok := byKey["GET /api/boom"]
	if !ok {
		t.Fatalf("missing snapshot for boom route, got %+v", snaps)
	}
	if boom.RequestCount != 1 || boom.ErrorCount != 1 || boom.StatusCounts[500] != 1 {
		t.Errorf("expected 1 request / 1 error / 1x500 for boom route, got %+v", boom)
	}
}

func TestGoldenSignals_RecordsLatency(t *testing.T) {
	reg := metrics.NewRegistry()
	r := newInstrumentedTestRouter(reg)

	if code := doRequest(r, "GET", "/api/slow"); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}

	snaps := reg.Snapshot()
	var found *metrics.Snapshot
	for i := range snaps {
		if snaps[i].Route == "/api/slow" {
			found = &snaps[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a snapshot for /api/slow, got %+v", snaps)
	}
	// Handler sleeps ~5ms, so recorded latency must be at least a few ms,
	// proving the middleware measures real elapsed time around c.Next().
	if found.MaxLatencyMs < 3 {
		t.Errorf("expected MaxLatencyMs >= ~3ms for a handler sleeping 5ms, got %.3f", found.MaxLatencyMs)
	}
	if found.AvgLatencyMs <= 0 {
		t.Errorf("expected AvgLatencyMs > 0, got %.3f", found.AvgLatencyMs)
	}
}

func TestGoldenSignals_UnmatchedRoutesAreBucketedTogether(t *testing.T) {
	reg := metrics.NewRegistry()
	r := newInstrumentedTestRouter(reg)
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})

	doRequest(r, "GET", "/api/does-not-exist-1")
	doRequest(r, "GET", "/api/does-not-exist-2")

	snaps := reg.Snapshot()
	if len(snaps) != 1 {
		t.Fatalf("expected all unmatched routes to be folded into a single bucket, got %d entries: %+v", len(snaps), snaps)
	}
	if snaps[0].RequestCount != 2 {
		t.Errorf("expected RequestCount=2 for the shared not-found bucket, got %d", snaps[0].RequestCount)
	}
}
