package middleware_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type guardProbe struct {
	reached bool
	body    string
}

func guardRouter(probe *guardProbe, limit int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.BodyLimit(limit, nil), middleware.RejectInvalidCharacters())
	r.Any("/x/:id", func(c *gin.Context) {
		probe.reached = true
		b, _ := io.ReadAll(c.Request.Body)
		probe.body = string(b)
		c.Status(http.StatusOK)
	})
	return r
}

func guardCall(r *gin.Engine, method, target, body, ctype string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRejectInvalidCharacters_BodyIsRestoredUnchangedForHandler(t *testing.T) {
	probe := &guardProbe{}
	r := guardRouter(probe, 1<<20)
	const body = `{"name":"Größe 🚀 ä \\u0000","list":[1,2]}`
	w := guardCall(r, http.MethodPost, "/x/1", body, "application/json")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, body, probe.body, "der Handler sieht denselben Body")
}

func TestRejectInvalidCharacters_RejectsWithCodeAndWithoutHandler(t *testing.T) {
	for name, body := range map[string]string{
		"escape":         `{"a":"\u0000"}`,
		"escape nach \\": `{"a":"\\\u0000"}`,
		"roh":            "{\"a\":\"\x00\"}",
		"utf8":           "{\"a\":\"\xff\"}",
		"schluessel":     `{"\u0000":1}`,
		"tief":           `[{"a":{"b":["\u0000"]}}]`,
	} {
		t.Run(name, func(t *testing.T) {
			probe := &guardProbe{}
			w := guardCall(guardRouter(probe, 1<<20), http.MethodPut, "/x/1", body, "application/json")
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.JSONEq(t, `{"error":"Eine Eingabe enthält unzulässige Zeichen.","code":"input_invalid_characters"}`, w.Body.String())
			assert.False(t, probe.reached)
		})
	}
}

func TestRejectInvalidCharacters_AllowsEscapedBackslashBeforeU0000(t *testing.T) {
	probe := &guardProbe{}
	w := guardCall(guardRouter(probe, 1<<20), http.MethodPost, "/x/1", `{"a":"\\u0000","b":"\\\\u0000"}`, "application/json")
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, probe.reached)
}

func TestRejectInvalidCharacters_TruncatedEscapeIsNoNUL(t *testing.T) {
	probe := &guardProbe{}
	// „\u00“ am Ende: kein NUL (der JSON-Parser des Handlers lehnt es selbst ab).
	w := guardCall(guardRouter(probe, 1<<20), http.MethodPost, "/x/1", `"\u00`, "application/json")
	assert.Equal(t, http.StatusOK, w.Code)
	w = guardCall(guardRouter(probe, 1<<20), http.MethodPost, "/x/1", `"\`, "application/json")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRejectInvalidCharacters_ChecksQueryAndPath(t *testing.T) {
	for name, target := range map[string]string{
		"query wert":    "/x/1?q=%00",
		"query key":     "/x/1?%00=1",
		"query utf8":    "/x/1?q=%ff",
		"pfad":          "/x/a%00b",
		"pfad utf8":     "/x/%ff",
		"zweiter wert":  "/x/1?q=ok&q=%00",
		"plus und null": "/x/1?q=a+b%00",
	} {
		t.Run(name, func(t *testing.T) {
			probe := &guardProbe{}
			w := guardCall(guardRouter(probe, 1<<20), http.MethodGet, target, "", "")
			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), "input_invalid_characters")
			assert.False(t, probe.reached)
		})
	}
	for _, target := range []string{"/x/1", "/x/1?q=%C3%A4%F0%9F%9A%80&r=a+b", "/x/1?kaputt=%zz&ok=1", "/x/M%C3%BCller"} {
		probe := &guardProbe{}
		w := guardCall(guardRouter(probe, 1<<20), http.MethodGet, target, "", "")
		assert.Equal(t, http.StatusOK, w.Code, target)
	}
}

// Formularkodierte Bodys (SAML-Rückkanal) liest die Prüfung nicht, sonst wäre
// die zweite Million Byte doppelt im Speicher.
func TestRejectInvalidCharacters_SkipsFormBodies(t *testing.T) {
	for _, ct := range []string{"application/x-www-form-urlencoded", "multipart/form-data; boundary=x"} {
		probe := &guardProbe{}
		w := guardCall(guardRouter(probe, 1<<20), http.MethodPost, "/x/1", "a=\x00", ct)
		assert.Equal(t, http.StatusOK, w.Code, ct)
		assert.Equal(t, "a=\x00", probe.body)
	}
}

func TestRejectInvalidCharacters_BodylessRequestsPass(t *testing.T) {
	probe := &guardProbe{}
	w := guardCall(guardRouter(probe, 1<<20), http.MethodPost, "/x/1", "", "application/json")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, probe.reached)
}

// Ohne bekannte Länge (chunked) greift die Grenze beim Lesen: 413 statt 400.
func TestRejectInvalidCharacters_ChunkedOversizeIs413(t *testing.T) {
	probe := &guardProbe{}
	r := guardRouter(probe, 16)
	req := httptest.NewRequest(http.MethodPost, "/x/1", io.NopCloser(strings.NewReader(strings.Repeat("a", 64))))
	req.ContentLength = -1
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Contains(t, w.Body.String(), "request_too_large")
	assert.False(t, probe.reached)
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("verbindung abgebrochen") }
func (failingBody) Close() error             { return nil }

func TestRejectInvalidCharacters_ReadErrorIs400(t *testing.T) {
	probe := &guardProbe{}
	r := guardRouter(probe, 1<<20)
	req := httptest.NewRequest(http.MethodPost, "/x/1", failingBody{})
	req.ContentLength = -1
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_request")
	assert.False(t, probe.reached)
}

// Nur die Folge \u0000 ist ein NUL. Andere \u-Escapes (auch solche, die mit
// 000 beginnen) sind gültiger Text, und ein \u0000 am Ende des Bodys zählt.
func TestRejectInvalidCharacters_OnlyU0000IsRejected(t *testing.T) {
	for body, want := range map[string]bool{
		`"\u0000"`:   false,
		`\u0000`:     false, // genau am Ende, ohne schließendes Zeichen
		`"\u00000"`:  false,
		`"\u0001"`:   true,
		`"\u000a"`:   true,
		`"\u00e4"`:   true,
		`"\u0010"`:   true,
		`"\u000"`:    true, // zu kurz
		`"\\u0000"`:  true, // maskierter Backslash
		`"\\\u0000"`: false,
		`"a\nb\"c"`:  true,
		"plain":      true,
	} {
		w := guardCall(guardRouter(&guardProbe{}, 1<<20), http.MethodPost, "/x/1", body, "application/json")
		if want {
			assert.Equal(t, http.StatusOK, w.Code, body)
		} else {
			assert.Equal(t, http.StatusBadRequest, w.Code, body)
		}
	}
}
