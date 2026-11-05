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

// Strukturwächter: Jede Route, die einen Body annimmt, steht hinter der
// Body-Grenze. Der Test schickt an jede registrierte POST/PUT/PATCH/DELETE-
// Route einen Body knapp über der Grenze und erwartet 413, ohne dass ein
// Handler läuft. Eine neue Routengruppe oder Middleware-Reihenfolge, die die
// globale Grenze umgeht, fällt damit auf, ohne dass jemand die Route kennen muss.
var routeParam = regexp.MustCompile(`[:*][A-Za-z_]+`)

func routesWithoutBodyLimit(t *testing.T, r *gin.Engine) []string {
	t.Helper()
	var offenders []string
	filler := strings.Repeat("a", samlACSMaxBodyBytes+1)
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

// Gegenprobe: Ein Router ohne die globale Grenze wird erkannt.
func TestRoutesWithoutBodyLimit_FlagsRouterWithoutLimit(t *testing.T) {
	r := gin.New()
	r.POST("/offen/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	assert.Equal(t, []string{"POST /offen/:id"}, routesWithoutBodyLimit(t, r))
}

// Die Grenzen selbst sind festgenagelt: Die Tests oben messen gegen die
// Konstanten und bemerkten sonst nicht, wenn jemand sie auf einen riesigen Wert
// setzt. Die SAML-Antwort (bis 1 MiB, formularkodiert) braucht mehr als die
// Standardgrenze, aber keine unbegrenzte.
func TestBodyLimit_ConstantsStayBounded(t *testing.T) {
	assert.LessOrEqual(t, int64(defaultMaxBodyBytes), int64(2<<20), "Standardgrenze für JSON-Bodys")
	assert.GreaterOrEqual(t, int64(defaultMaxBodyBytes), int64(256<<10), "größte legitime Eingabe: SAML-Metadaten mit 256 KiB")
	assert.LessOrEqual(t, int64(samlACSMaxBodyBytes), int64(4<<20), "ACS-Pfad")
	assert.Greater(t, int64(samlACSMaxBodyBytes), int64(1<<20), "ACS-Pfad muss die SAML-Antwort samt Formularkodierung fassen")
	for path, limit := range bodyLimitOverrides {
		assert.LessOrEqual(t, limit, int64(4<<20), path)
	}
}
