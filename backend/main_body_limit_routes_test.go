package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Structure guard: every route that accepts a body sits behind the
// body limit. The test sends to every registered POST/PUT/PATCH/DELETE
// route a body just over the limit and expects 413, without a
// handler running. A new route group or middleware order that bypasses the
// global limit is thus noticed without anyone having to know the route.
var routeParam = regexp.MustCompile(`[:*][A-Za-z_]+`)

func routesWithoutBodyLimit(t *testing.T, r *gin.Engine) []string {
	t.Helper()
	var offenders []string
	filler := strings.Repeat("a", maxOverrideBytes+1)
	for _, ri := range r.Routes() {
		switch ri.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			continue
		}
		limit := int(defaultMaxBodyBytes)
		if v, ok := bodyLimitOverrides[ri.Path]; ok {
			limit = int(v)
		}
		path := routeParam.ReplaceAllString(ri.Path, "1")
		req := httptest.NewRequest(ri.Method, path, strings.NewReader(filler[:limit+1]))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.77:4321"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusRequestEntityTooLarge {
			offenders = append(offenders, ri.Method+" "+ri.Path)
		}
	}
	return offenders
}

func TestRouter_EveryRouteWithBodyIsBehindBodyLimit(t *testing.T) {
	r := newRouter(handlers.NewHandler(testutils.SetupTestDB()))
	require.Greater(t, len(r.Routes()), 100, "der Test soll die ganze Routentabelle sehen")
	assert.Empty(t, routesWithoutBodyLimit(t, r))
}

// Control check: a router without the global limit is detected.
func TestRoutesWithoutBodyLimit_FlagsRouterWithoutLimit(t *testing.T) {
	r := gin.New()
	r.POST("/offen/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	assert.Equal(t, []string{"POST /offen/:id"}, routesWithoutBodyLimit(t, r))
}

// maxOverrideBytes is the upper bound of every entry in bodyLimitOverrides.
const maxOverrideBytes = 4 << 20

// The limits themselves are pinned: the tests above measure against the
// constants and would not notice if someone set them to a huge value.
func TestBodyLimit_ConstantsStayBounded(t *testing.T) {
	assert.LessOrEqual(t, int64(defaultMaxBodyBytes), int64(2<<20), "Standardgrenze für JSON-Bodys")
	assert.GreaterOrEqual(t, int64(defaultMaxBodyBytes), int64(256<<10), "room for the largest JSON inputs")
	for path, limit := range bodyLimitOverrides {
		assert.LessOrEqual(t, limit, int64(maxOverrideBytes), path)
	}
}
