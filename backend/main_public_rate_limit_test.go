package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Strukturwächter: Jede anonym erreichbare schreibende Route (antwortet ohne
// Anmeldung nicht mit 401/403) hat ein Anfragelimit je Client. Der Test ruft
// jede dieser Routen mit leerem Body wiederholt von einer Adresse auf und
// erwartet irgendwann 429. Eine neue öffentliche Route ohne Limit (Login-
// Raten, Mail-Bombing, Ressourcenverbrauch) fällt damit auf.
var publicRouteParam = regexp.MustCompile(`[:*][A-Za-z_]+`)

func anonymousRoutesWithoutRateLimit(t *testing.T, r *gin.Engine) []string {
	t.Helper()
	const maxRequests = 100
	var offenders []string
	checked := 0
	for i, ri := range r.Routes() {
		if ri.Method != http.MethodPost && ri.Method != http.MethodPut && ri.Method != http.MethodDelete && ri.Method != http.MethodPatch {
			continue
		}
		path := publicRouteParam.ReplaceAllString(ri.Path, "1")
		addr := "198.51.100." + strconv.Itoa(i%250+1) + ":4000" // eigene Adresse je Route
		first := doAnonymous(r, ri.Method, path, addr)
		if first == http.StatusUnauthorized || first == http.StatusForbidden {
			continue // nur mit Anmeldung erreichbar
		}
		checked++
		limited := false
		for n := 0; n < maxRequests && !limited; n++ {
			limited = doAnonymous(r, ri.Method, path, addr) == http.StatusTooManyRequests
		}
		if !limited {
			offenders = append(offenders, ri.Method+" "+ri.Path)
		}
	}
	require.Positive(t, checked, "mindestens eine anonyme Route erwartet")
	t.Logf("%d anonyme schreibende Routen geprüft", checked)
	return offenders
}

func doAnonymous(r *gin.Engine, method, path, addr string) int {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = addr
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestRouter_EveryAnonymousWriteRouteIsRateLimited(t *testing.T) {
	r := newRouter(handlers.NewHandler(testutils.SetupTestDB()))
	require.Greater(t, len(r.Routes()), 100)
	assert.Empty(t, anonymousRoutesWithoutRateLimit(t, r))
}

// Gegenprobe: Eine offene Route ohne Limit wird erkannt, eine geschützte nicht.
func TestAnonymousRoutesWithoutRateLimit_FlagsOpenRoute(t *testing.T) {
	r := gin.New()
	r.POST("/offen", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.POST("/geschuetzt", func(c *gin.Context) { c.Status(http.StatusUnauthorized) })
	assert.Equal(t, []string{"POST /offen"}, anonymousRoutesWithoutRateLimit(t, r))
}
