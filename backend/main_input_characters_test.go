package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ein NUL-Byte (oder ungültiges UTF-8) in einem JSON-Text lehnt PostgreSQL erst
// beim Schreiben ab, der Handler antwortete mit 500. Die globale Prüfung weist
// solche Anfragen mit 400 input_invalid_characters ab, bevor ein Handler läuft.
// Die Tests laufen gegen den echten Router; SQLite nimmt NUL klaglos an, die
// Tests prüfen deshalb die Abweisung selbst, nicht den Datenbankfehler.

const invalidCharactersCode = `"code":"input_invalid_characters"`

type inputGuardFixture struct {
	r         http.Handler
	pageID    string
	adminTok  string
	allRoutes []gin.RouteInfo
}

func setupInputGuardFixture(t *testing.T) inputGuardFixture {
	t.Helper()
	db := testutils.SetupTestDB()
	r := newRouter(handlers.NewHandler(db))
	page := models.StatusPage{Name: "Zeichen", Slug: "zeichen-seite"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&models.Component{Name: "K", StatusPageID: page.ID}).Error)
	admin := models.User{Email: "zeichen-admin@example.com", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&admin).Error)
	tok, err := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)
	require.NoError(t, err)
	return inputGuardFixture{r: r, pageID: strconv.FormatUint(uint64(page.ID), 10), adminTok: tok, allRoutes: r.Routes()}
}

func (f inputGuardFixture) do(method, target, body, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Authorization", "Bearer "+f.adminTok)
	req.RemoteAddr = "192.0.2.10:5000"
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

func concretePath(path, pageID string) string {
	path = strings.ReplaceAll(path, ":page_id", pageID)
	return routeParam.ReplaceAllString(path, "1")
}

// Tabellentest über alle Schreibrouten aus dem Router: Jede Route mit Body, die
// NUL im JSON trägt, wird abgewiesen, bevor ein Handler läuft. Das gilt auch für
// die Routen ohne Anmeldung (Login, Abo, Bestellung) und die mit eigener Prüfung.
func TestRouter_EveryWriteRouteRejectsNULInJSONBody(t *testing.T) {
	f := setupInputGuardFixture(t)
	const nulBody = `{"name":"a\u0000b","title":"x","email":"nul@example.com"}`
	var checked int
	for _, ri := range f.allRoutes {
		switch ri.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			continue
		}
		if ri.Path == "/api/auth/saml/:page_id/acs" {
			continue // formularkodiert, kein JSON
		}
		checked++
		w := f.do(ri.Method, concretePath(ri.Path, f.pageID), nulBody, "application/json")
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s %s: %s", ri.Method, ri.Path, w.Body.String())
		assert.Contains(t, w.Body.String(), invalidCharactersCode, "%s %s", ri.Method, ri.Path)
	}
	require.Greater(t, checked, 80, "der Test soll die ganze Routentabelle sehen")
}

// Repräsentative Routen mit Anmeldung: je ein NUL im Wert, im Schlüssel und tief
// verschachtelt. Zusätzlich die Wege, die ein einfaches Feld-für-Feld-Prüfen
// verfehlt: fehlender Content-Type, andere Schreibweise, rohes Byte.
func TestRouter_NULVariantsAreRejectedBeforeHandlers(t *testing.T) {
	f := setupInputGuardFixture(t)
	page := f.pageID
	cases := []struct {
		name, method, path, body, ctype string
	}{
		{"Komponente Name", http.MethodPost, "/api/pages/" + page + "/components", `{"name":"a\u0000b","status":"operational"}`, "application/json"},
		{"Statusseite Beschreibung", http.MethodPut, "/api/admin/pages/" + page, `{"name":"ok","description":"x\u0000y"}`, "application/json"},
		{"Vorfall Titel", http.MethodPost, "/api/pages/" + page + "/incidents", `{"title":"\u0000","status":"investigating"}`, "application/json"},
		{"Tag Name", http.MethodPost, "/api/tags", `{"name":"t\u0000"}`, "application/json"},
		{"Schlüssel statt Wert", http.MethodPost, "/api/tags", `{"na\u0000me":"t"}`, "application/json"},
		{"verschachtelt", http.MethodPost, "/api/pages/" + page + "/incidents", `{"title":"ok","meta":{"a":[{"b":"c\u0000"}]}}`, "application/json"},
		{"Content-Type mit Parameter", http.MethodPost, "/api/tags", `{"name":"t\u0000"}`, "application/json; charset=utf-8"},
		{"Content-Type fehlt", http.MethodPost, "/api/tags", `{"name":"t\u0000"}`, ""},
		{"Content-Type text/plain", http.MethodPost, "/api/tags", `{"name":"t\u0000"}`, "text/plain"},
		{"vorangestellter Backslash-Paar", http.MethodPost, "/api/tags", `{"name":"\\\u0000"}`, "application/json"},
		{"rohes NUL-Byte", http.MethodPost, "/api/tags", "{\"name\":\"t\x00\"}", "application/json"},
		{"ungültiges UTF-8", http.MethodPost, "/api/tags", "{\"name\":\"t\xff\xfe\"}", "application/json"},
		{"abgeschnittenes UTF-8", http.MethodPost, "/api/tags", "{\"name\":\"t\xc3\"}", "application/json"},
		{"Query mit NUL", http.MethodGet, "/api/admin/ui-overrides?language_code=%00", "", ""},
		{"Query-Schlüssel mit NUL", http.MethodGet, "/api/admin/ui-overrides?a%00b=1", "", ""},
		{"Query mit ungültigem UTF-8", http.MethodGet, "/api/admin/ui-overrides?language_code=%ff", "", ""},
		{"Pfadparameter mit NUL", http.MethodGet, "/api/public/pages/a%00b/dashboard", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := f.do(c.method, c.path, c.body, c.ctype)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), invalidCharactersCode)
		})
	}
}

// Gegenprobe: Umlaute, Emoji, Escape-Folgen, die kein NUL ergeben, und ein
// Backslash vor „u0000“ als Text laufen unverändert zum Handler.
func TestRouter_LegitimateTextPassesTheCharacterCheck(t *testing.T) {
	f := setupInputGuardFixture(t)
	for _, name := range []string{
		`Größe Übergänge`,
		`Statusseite 🚀 ✅`,
		`\u00e4\u00f6\u00fc`,
		`\ud83d\ude80`,
		`Pfad C:\\u0000 als Text`,
		`Zeile\nzwei\tTab`,
	} {
		body := `{"name":"` + name + `"}`
		w := f.do(http.MethodPost, "/api/tags", body, "application/json")
		assert.Equal(t, http.StatusCreated, w.Code, "%q -> %s", name, w.Body.String())
	}
	w := f.do(http.MethodGet, "/api/admin/ui-overrides?language_code=de", "", "")
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = f.do(http.MethodGet, "/api/pages/"+f.pageID+"/components?x=%C3%A4", "", "")
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// Reihenfolge: Die Größengrenze steht vor der Zeichenprüfung. Ein zu großer
// Body wird mit 413 abgewiesen, ohne dass die Prüfung ihn vollständig liest;
// andernfalls müsste der Server erst beliebig viele Bytes einlesen.
func TestRouter_OversizedBodyGets413BeforeTheCharacterCheck(t *testing.T) {
	f := setupInputGuardFixture(t)
	body := `{"name":"a\u0000b","pad":"` + strings.Repeat("x", defaultMaxBodyBytes+10) + `"}`
	w := f.do(http.MethodPost, "/api/tags", body, "application/json")
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "request_too_large")
}
