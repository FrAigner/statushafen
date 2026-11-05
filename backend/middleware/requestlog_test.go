package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/logging"
	"github.com/fraigner/statushafen/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureJSONLog setzt für den Test einen JSON-Default-Logger (Level debug)
// auf einen Puffer und liefert die geparsten Zeilen.
func captureJSONLog(t *testing.T) func() []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(logging.New(&buf, func(k string) string {
		return map[string]string{"LOG_FORMAT": "json", "LOG_LEVEL": "debug"}[k]
	}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() []map[string]any {
		var out []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
			if line == "" {
				continue
			}
			var m map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &m), line)
			out = append(out, m)
		}
		return out
	}
}

func findMsg(entries []map[string]any, msg string) map[string]any {
	for _, e := range entries {
		if e["msg"] == msg {
			return e
		}
	}
	return nil
}

func newLoggedRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestLogger(), middleware.Recovery())
	r.GET("/items/:id", func(c *gin.Context) {
		logging.FromContext(c.Request.Context()).Info("im handler")
		c.Set("userID", uint(5))
		c.String(http.StatusOK, "ok")
	})
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/fail", func(c *gin.Context) {
		_ = c.Error(assert.AnError)
		c.String(http.StatusInternalServerError, "x")
	})
	r.GET("/bad", func(c *gin.Context) { c.String(http.StatusBadRequest, "x") })
	r.GET("/panic", func(c *gin.Context) { panic("kaputt") })
	return r
}

func serve(r *gin.Engine, path string, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if header != "" {
		req.Header.Set(middleware.RequestIDHeader, header)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRequestLogger_GeneratesIDAndLogsAccess(t *testing.T) {
	logs := captureJSONLog(t)
	w := serve(newLoggedRouter(), "/items/7?token=geheim&x=1", "")

	id := w.Header().Get(middleware.RequestIDHeader)
	assert.Len(t, id, 26, "rand.Text liefert 26 Zeichen")

	entries := logs()
	inHandler := findMsg(entries, "im handler")
	require.NotNil(t, inHandler)
	assert.Equal(t, id, inHandler["request_id"], "Handler-Logger muss die Request-ID tragen")

	access := findMsg(entries, "http_request")
	require.NotNil(t, access)
	assert.Equal(t, "INFO", access["level"])
	assert.Equal(t, id, access["request_id"])
	assert.Equal(t, "GET", access["method"])
	assert.Equal(t, "/items/7", access["path"], "Query (mit Token) darf nicht im Log landen")
	assert.Equal(t, "/items/:id", access["route"])
	assert.EqualValues(t, 200, access["status"])
	assert.EqualValues(t, 5, access["user_id"])
	assert.Contains(t, access, "duration_ms")
	assert.Contains(t, access, "client_ip")
	assert.NotContains(t, access, "errors")
	for _, e := range entries {
		b, _ := json.Marshal(e)
		assert.NotContains(t, string(b), "geheim")
	}
}

func TestRequestLogger_AdoptsPlausibleIncomingID(t *testing.T) {
	logs := captureJSONLog(t)
	w := serve(newLoggedRouter(), "/items/1", "edge-1234_abc.DEF:9")
	assert.Equal(t, "edge-1234_abc.DEF:9", w.Header().Get(middleware.RequestIDHeader))
	assert.Equal(t, "edge-1234_abc.DEF:9", findMsg(logs(), "http_request")["request_id"])
}

func TestRequestLogger_RejectsImplausibleIncomingID(t *testing.T) {
	for _, bad := range []string{
		strings.Repeat("a", 65),
		"mit leerzeichen",
		"zeile\numbruch",
		"<script>",
	} {
		w := serve(newLoggedRouter(), "/items/1", bad)
		got := w.Header().Get(middleware.RequestIDHeader)
		assert.NotEqual(t, bad, got)
		assert.Len(t, got, 26, "für %q muss eine neue ID erzeugt werden", bad)
	}
	// Genau 64 Zeichen sind noch erlaubt.
	ok := strings.Repeat("b", 64)
	assert.Equal(t, ok, serve(newLoggedRouter(), "/items/1", ok).Header().Get(middleware.RequestIDHeader))
}

func TestRequestLogger_LevelByStatus(t *testing.T) {
	cases := []struct {
		path  string
		level string
	}{
		{"/fail", "ERROR"},
		{"/bad", "WARN"},
		{"/gibts-nicht", "WARN"},
		{"/healthz", "DEBUG"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			logs := captureJSONLog(t)
			serve(newLoggedRouter(), tc.path, "")
			access := findMsg(logs(), "http_request")
			require.NotNil(t, access)
			assert.Equal(t, tc.level, access["level"])
		})
	}
}

func TestRequestLogger_IncludesGinErrors(t *testing.T) {
	logs := captureJSONLog(t)
	serve(newLoggedRouter(), "/fail", "")
	access := findMsg(logs(), "http_request")
	assert.Contains(t, access["errors"], assert.AnError.Error())
	assert.NotContains(t, access, "user_id")
}

func TestRecovery_LogsStackAndReturns500(t *testing.T) {
	logs := captureJSONLog(t)
	w := serve(newLoggedRouter(), "/panic", "fixed-id")
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	entries := logs()
	p := findMsg(entries, "panic recovered")
	require.NotNil(t, p)
	assert.Equal(t, "ERROR", p["level"])
	assert.Equal(t, "fixed-id", p["request_id"])
	assert.Equal(t, "kaputt", p["panic"])
	assert.Contains(t, p["stack"], "requestlog_test.go")

	access := findMsg(entries, "http_request")
	require.NotNil(t, access)
	assert.EqualValues(t, 500, access["status"])
}

func TestRequestIDFrom(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestLogger())
	var got string
	r.GET("/", func(c *gin.Context) { got = middleware.RequestIDFrom(c) })
	serve(r, "/", "abc")
	assert.Equal(t, "abc", got)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.Equal(t, "", middleware.RequestIDFrom(c))
}

// Query-Strings (?token=, ?key=), Authorization-Header und URLs in
// gin-Fehlern dürfen nie im Access-Log stehen.
func TestRequestLogger_KeineGeheimnisseImLog(t *testing.T) {
	logs := captureJSONLog(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestLogger())
	r.GET("/hook", func(c *gin.Context) {
		_ = c.Error(&url.Error{Op: "Post", URL: "https://chat.example/v1?key=GEHEIMKEY&token=GEHEIMTOKEN", Err: assert.AnError})
		c.String(http.StatusInternalServerError, "x")
	})
	req := httptest.NewRequest(http.MethodGet, "/hook?token=GEHEIMTOKEN&key=GEHEIMKEY", nil)
	req.Header.Set("Authorization", "Bearer GEHEIMBEARER")
	req.Header.Set("Cookie", "session=GEHEIMCOOKIE")
	r.ServeHTTP(httptest.NewRecorder(), req)

	access := findMsg(logs(), "http_request")
	raw, err := json.Marshal(access)
	require.NoError(t, err)
	for _, s := range []string{"GEHEIMKEY", "GEHEIMTOKEN", "GEHEIMBEARER", "GEHEIMCOOKIE"} {
		assert.NotContains(t, string(raw), s)
	}
	assert.Equal(t, "/hook", access["path"])
}
