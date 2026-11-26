package handlers_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/logging"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// automatic translation - settings, connection test and
// translation suggestion.

const translationSecret = "GEHEIMER-SCHLUESSEL-676"

type fixedResolver struct{ ip string }

func (r fixedResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP(r.ip)}}, nil
}

// fakeTranslationServer responds like LibreTranslate (path /translate) or
// DeepL (/v2/translate) and counts the calls.
func fakeTranslationServer(t *testing.T, tls bool, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		var req struct {
			Q    json.RawMessage `json:"q"`
			Text []string        `json:"text"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		if strings.HasSuffix(r.URL.Path, "/v2/translate") {
			items := make([]string, 0, len(req.Text))
			for range req.Text {
				items = append(items, `{"text":"übersetzt"}`)
			}
			_, _ = w.Write([]byte(`{"translations":[` + strings.Join(items, ",") + `]}`))
			return
		}
		if strings.HasPrefix(strings.TrimSpace(string(req.Q)), "[") { // batch: list in, list out
			var qs []string
			_ = json.Unmarshal(req.Q, &qs)
			items := make([]string, len(qs))
			for i := range qs {
				items[i] = `"übersetzt"`
			}
			_, _ = w.Write([]byte(`{"translatedText":[` + strings.Join(items, ",") + `]}`))
			return
		}
		_, _ = w.Write([]byte(`{"translatedText":"übersetzt"}`))
	})
	var srv *httptest.Server
	if tls {
		srv = httptest.NewTLSServer(handler)
	} else {
		srv = httptest.NewServer(handler)
	}
	t.Cleanup(srv.Close)
	return srv, &calls
}

// safeClientTo returns an SSRF-protected client whose name resolution
// reports resolvedIP and which connects to the test server after the check.
func safeClientTo(srv *httptest.Server, resolvedIP string) *http.Client {
	return externalstatus.NewSafeClient(externalstatus.ClientConfig{
		Resolver: fixedResolver{ip: resolvedIP},
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		},
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // nur Test
	})
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(logging.New(&buf, func(string) string { return "" }))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

type translationFixture struct {
	db *gorm.DB
	r  *gin.Engine
	h  *handlers.Handler
	pg models.StatusPage
}

func newTranslationFixture(t *testing.T) translationFixture {
	t.Helper()
	t.Setenv("LIBRETRANSLATE_URL", "")
	t.Setenv("LIBRETRANSLATE_API_KEY", "")
	db, r, h := newTestSetup()
	pg := newTestPage(db)
	db.Create(&models.StatusPageLanguage{StatusPageID: pg.ID, LanguageCode: "de", IsDefault: true})
	db.Create(&models.StatusPageLanguage{StatusPageID: pg.ID, LanguageCode: "en"})
	r.GET("/api/settings/translation", h.GetTranslationSettingsHandler)
	r.PUT("/api/settings/translation", h.UpdateTranslationSettingsHandler)
	r.POST("/api/settings/translation/test", h.TestTranslationSettingsHandler)
	r.POST("/api/pages/:page_id/translate", h.TranslateTextHandler)
	return translationFixture{db: db, r: r, h: h, pg: pg}
}

func (f translationFixture) translatePath() string {
	return "/api/pages/" + strconv.Itoa(int(f.pg.ID)) + "/translate"
}

func (f translationFixture) put(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	w := doStatusPageRequest(f.r, "PUT", "/api/settings/translation", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func codeOf(t *testing.T, body []byte) string {
	t.Helper()
	var out struct{ Code string }
	require.NoError(t, json.Unmarshal(body, &out))
	return out.Code
}

func TestTranslationSettings_SchluesselWirdNieAusgegebenOderGeloggt(t *testing.T) {
	f := newTranslationFixture(t)
	buf := captureLog(t)
	out := f.put(t, map[string]any{"provider": "deepl", "api_key": translationSecret})
	assert.Equal(t, true, out["api_key_set"])
	assert.Equal(t, "deepl", out["provider"])
	assert.Equal(t, true, out["configured"])

	w := doStatusPageRequest(f.r, "GET", "/api/settings/translation", nil)
	assert.NotContains(t, w.Body.String(), translationSecret)

	// Error path with a secret as input: service rejects, nothing in the log.
	srv, _ := fakeTranslationServer(t, true, http.StatusForbidden)
	f.h.TranslationSafeClient = safeClientTo(srv, "93.184.216.34")
	f.h.TranslationDeepLURL = "https://deepl.example.org"
	w = doStatusPageRequest(f.r, "POST", "/api/settings/translation/test", nil)
	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Equal(t, "translation_auth_failed", codeOf(t, w.Body.Bytes()))
	assert.NotContains(t, w.Body.String(), translationSecret)

	var audits []models.AuditLog
	f.db.Find(&audits)
	for _, a := range audits {
		assert.NotContains(t, a.Details+a.TargetName+a.TargetID, translationSecret)
	}
	assert.NotContains(t, buf.String(), translationSecret)
	assert.Contains(t, buf.String(), "Übersetzung fehlgeschlagen")
}

func TestTranslationSettings_Aktualisieren(t *testing.T) {
	f := newTranslationFixture(t)

	w := doStatusPageRequest(f.r, "PUT", "/api/settings/translation", map[string]any{})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "settings_field_required", codeOf(t, w.Body.Bytes()))

	w = doStatusPageRequest(f.r, "PUT", "/api/settings/translation", "kein objekt")
	assert.Equal(t, "invalid_request", codeOf(t, w.Body.Bytes()))

	w = doStatusPageRequest(f.r, "PUT", "/api/settings/translation", map[string]any{"provider": "google"})
	assert.Equal(t, "translation_provider_invalid", codeOf(t, w.Body.Bytes()))

	// Internal or insecure addresses are already rejected on saving.
	for _, u := range []string{"http://libretranslate:5000", "https://translate.example.org:8443", "ftp://x"} {
		w = doStatusPageRequest(f.r, "PUT", "/api/settings/translation", map[string]any{"url": u})
		assert.Equal(t, http.StatusBadRequest, w.Code, u)
		assert.Equal(t, "translation_url_invalid", codeOf(t, w.Body.Bytes()), u)
	}

	out := f.put(t, map[string]any{"provider": " libretranslate ", "url": " https://Translate.Example.org/ ", "api_key": "k"})
	assert.Equal(t, "libretranslate", out["provider"])
	assert.Equal(t, "https://translate.example.org", out["url"])
	assert.Equal(t, true, out["api_key_set"])

	// The key stays if api_key is missing; an empty string deletes it.
	out = f.put(t, map[string]any{"url": ""})
	assert.Equal(t, true, out["api_key_set"])
	assert.Equal(t, "", out["url"])
	out = f.put(t, map[string]any{"api_key": ""})
	assert.Equal(t, false, out["api_key_set"])
	// Setting again after deletion (upsert)
	out = f.put(t, map[string]any{"api_key": "neu"})
	assert.Equal(t, true, out["api_key_set"])
}

func TestTranslationSettings_DBFehlerGibt500(t *testing.T) {
	f := newTranslationFixture(t)
	require.NoError(t, f.db.Migrator().DropTable(&models.SystemSetting{}))
	w := doStatusPageRequest(f.r, "PUT", "/api/settings/translation", map[string]any{"provider": "deepl"})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestTranslationSettings_UmgebungAnzeige(t *testing.T) {
	f := newTranslationFixture(t)
	t.Setenv("LIBRETRANSLATE_URL", "http://libretranslate:5000")
	t.Setenv("LIBRETRANSLATE_API_KEY", translationSecret)
	w := doStatusPageRequest(f.r, "GET", "/api/settings/translation", nil)
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, true, out["env_url_set"])
	assert.Equal(t, true, out["env_api_key_set"])
	assert.Equal(t, true, out["configured"])
	assert.Equal(t, "libretranslate", out["effective_provider"])
	assert.NotContains(t, w.Body.String(), translationSecret)
	assert.NotContains(t, w.Body.String(), "libretranslate:5000")
}

func TestTranslate_NichtKonfiguriert(t *testing.T) {
	f := newTranslationFixture(t)
	for name, setup := range map[string]func(){
		"nichts gesetzt":               func() {},
		"deepl ohne Schlüssel":         func() { f.put(t, map[string]any{"provider": "deepl"}) },
		"libretranslate ohne Adresse":  func() { f.put(t, map[string]any{"provider": "libretranslate"}) },
		"unbekannter Dienst in der DB": func() { f.db.Create(&models.SystemSetting{Key: "translation.provider", Value: "x"}) },
	} {
		t.Run(name, func(t *testing.T) {
			setup()
			w := doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": "Hallo"})
			assert.Equal(t, http.StatusServiceUnavailable, w.Code)
			assert.Equal(t, "translation_not_configured", codeOf(t, w.Body.Bytes()))
			w = doStatusPageRequest(f.r, "POST", "/api/settings/translation/test", nil)
			assert.Equal(t, "translation_not_configured", codeOf(t, w.Body.Bytes()))
			f.db.Unscoped().Where("1 = 1").Delete(&models.SystemSetting{})
		})
	}
}

func TestTranslate_StandardAdresseAusUmgebungDarfInternSein(t *testing.T) {
	f := newTranslationFixture(t)
	srv, calls := fakeTranslationServer(t, false, http.StatusOK) // http, loopback: forbidden by the SSRF block
	t.Setenv("LIBRETRANSLATE_URL", srv.URL)

	w := doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": " EN ", "title": "Störung", "message": "Details"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "übersetzt", out["title"])
	assert.Equal(t, "übersetzt", out["message"])
	assert.Equal(t, "libretranslate", out["provider"])
	assert.Equal(t, "de", out["source_language"])
	assert.EqualValues(t, 1, calls.Load(), "Titel und Nachricht in EINER Anfrage")

	// Without a message also one call; an empty message stays empty.
	calls.Store(0)
	w = doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": "Nur Titel", "message": "  "})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 1, calls.Load())

	// A set client is used for the environment address (test hook).
	var used atomic.Int32
	f.h.TranslationTrustedClient = &http.Client{Transport: countingTransport{n: &used}}
	doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": "x"})
	assert.EqualValues(t, 1, used.Load())
	f.h.TranslationTrustedClient = nil

	// Connection test uses the same address
	w = doStatusPageRequest(f.r, "POST", "/api/settings/translation/test", nil)
	assert.Equal(t, http.StatusOK, w.Code)

	// libretranslate chosen, but no own address: the environment applies
	f.put(t, map[string]any{"provider": "libretranslate"})
	w = doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": "x"})
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestTranslate_EingetrageneInterneAdresseWirdAbgelehnt(t *testing.T) {
	f := newTranslationFixture(t)
	srv, calls := fakeTranslationServer(t, true, http.StatusOK)

	// Hostname resolves to an internal address (rebinding): the dialer blocks.
	f.h.TranslationSafeClient = safeClientTo(srv, "10.0.0.5")
	f.put(t, map[string]any{"provider": "libretranslate", "url": "https://translate.example.org"})
	w := doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": "x"})
	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Equal(t, "translation_unavailable", codeOf(t, w.Body.Bytes()))
	assert.EqualValues(t, 0, calls.Load(), "interner Zielhost darf nie erreicht werden")

	// Invalid address stored directly in the DB (e.g. legacy data): not configured.
	f.db.Model(&models.SystemSetting{}).Where("key = ?", "translation.url").Update("value", "http://libretranslate:5000")
	w = doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": "x"})
	assert.Equal(t, "translation_not_configured", codeOf(t, w.Body.Bytes()))

	// Public address is reachable.
	f.h.TranslationSafeClient = safeClientTo(srv, "93.184.216.34")
	f.put(t, map[string]any{"url": "https://translate.example.org"})
	w = doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": "x"})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.EqualValues(t, 2-1, calls.Load())
}

func TestTranslate_DeepL(t *testing.T) {
	f := newTranslationFixture(t)
	srv, _ := fakeTranslationServer(t, true, http.StatusOK)
	f.h.TranslationSafeClient = safeClientTo(srv, "93.184.216.34")
	f.h.TranslationDeepLURL = "https://deepl.example.org"
	f.put(t, map[string]any{"provider": "deepl", "api_key": translationSecret})
	w := doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": "x", "message": "y"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"provider":"deepl"`)
}

func TestTranslate_FehlerDesDienstes(t *testing.T) {
	cases := map[int]struct {
		status int
		code   string
	}{
		http.StatusTooManyRequests:     {http.StatusTooManyRequests, "translation_quota_exceeded"},
		http.StatusBadRequest:          {http.StatusUnprocessableEntity, "translation_language_unsupported"},
		http.StatusUnauthorized:        {http.StatusBadGateway, "translation_auth_failed"},
		http.StatusInternalServerError: {http.StatusBadGateway, "translation_unavailable"},
	}
	for upstream, want := range cases {
		f := newTranslationFixture(t)
		srv, _ := fakeTranslationServer(t, false, upstream)
		t.Setenv("LIBRETRANSLATE_URL", srv.URL)
		for _, path := range []string{f.translatePath(), "/api/settings/translation/test"} {
			w := doStatusPageRequest(f.r, "POST", path, map[string]any{"target_language": "en", "title": "x", "message": "y"})
			assert.Equal(t, want.status, w.Code, "%d %s", upstream, path)
			assert.Equal(t, want.code, codeOf(t, w.Body.Bytes()))
		}
	}
}

func TestTranslate_FehlerBeiNachrichtUndEingabepruefung(t *testing.T) {
	f := newTranslationFixture(t)
	// Service answers the batch (title + message) with too few texts.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"translatedText":["ok"]}`))
	}))
	defer srv.Close()
	t.Setenv("LIBRETRANSLATE_URL", srv.URL)
	w := doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": "x", "message": "y"})
	assert.Equal(t, "translation_unavailable", codeOf(t, w.Body.Bytes()))

	w = doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"title": "x"})
	assert.Equal(t, "invalid_request", codeOf(t, w.Body.Bytes()))

	w = doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": strings.Repeat("ä", 5001)})
	assert.Equal(t, "translation_text_too_long", codeOf(t, w.Body.Bytes()))
	w = doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": strings.Repeat("ä", 2500), "message": strings.Repeat("ä", 2501)})
	assert.Equal(t, "translation_text_too_long", codeOf(t, w.Body.Bytes()))

	// Default language and non-enabled language are not targets.
	for _, lang := range []string{"de", "fr"} {
		w = doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": lang, "title": "x"})
		assert.Equal(t, "language_not_available", codeOf(t, w.Body.Bytes()), lang)
	}

	w = doStatusPageRequest(f.r, "POST", "/api/pages/abc/translate", map[string]any{"target_language": "en", "title": "x"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

type countingTransport struct{ n *atomic.Int32 }

func (c countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return http.DefaultTransport.RoundTrip(r)
}

// Fallback service: if DeepL fails, the operator's LibreTranslate service translates.
func deepLFixture(t *testing.T, deepLStatus int) translationFixture {
	t.Helper()
	f := newTranslationFixture(t)
	deepl, _ := fakeTranslationServer(t, true, deepLStatus)
	f.h.TranslationSafeClient = safeClientTo(deepl, "93.184.216.34")
	f.h.TranslationDeepLURL = "https://deepl.example.org"
	f.put(t, map[string]any{"provider": "deepl", "api_key": translationSecret})
	return f
}

func translateBody() map[string]any {
	return map[string]any{"target_language": "en", "title": "x", "message": "y"}
}

func TestTranslate_DeepLFehlerFuehrtZuLibreTranslate(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusUnauthorized, http.StatusInternalServerError, http.StatusBadRequest} {
		f := deepLFixture(t, status)
		lt, calls := fakeTranslationServer(t, false, http.StatusOK)
		t.Setenv("LIBRETRANSLATE_URL", lt.URL)

		w := doStatusPageRequest(f.r, "POST", f.translatePath(), translateBody())

		require.Equal(t, http.StatusOK, w.Code, "DeepL %d: %s", status, w.Body.String())
		assert.Contains(t, w.Body.String(), `"provider":"libretranslate"`, status)
		assert.Contains(t, w.Body.String(), `"fallback":true`, status)
		assert.EqualValues(t, 1, calls.Load(), "Titel und Nachricht in einer Anfrage an den Ersatzdienst")
	}
}

func TestTranslate_DeepLOkNutztKeinenErsatz(t *testing.T) {
	f := deepLFixture(t, http.StatusOK)
	lt, calls := fakeTranslationServer(t, false, http.StatusOK)
	t.Setenv("LIBRETRANSLATE_URL", lt.URL)

	w := doStatusPageRequest(f.r, "POST", f.translatePath(), translateBody())

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"provider":"deepl"`)
	assert.Contains(t, w.Body.String(), `"fallback":false`)
	assert.Zero(t, calls.Load())
}

func TestTranslate_DeepLFehlerOhneErsatzdienstMeldetDenDeepLFehler(t *testing.T) {
	f := deepLFixture(t, http.StatusTooManyRequests) // no LIBRETRANSLATE_URL

	w := doStatusPageRequest(f.r, "POST", f.translatePath(), translateBody())

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "translation_quota_exceeded", codeOf(t, w.Body.Bytes()))
}

func TestTranslate_ScheitertAuchDerErsatzdienstGiltDerFehlerVonDeepL(t *testing.T) {
	f := deepLFixture(t, http.StatusTooManyRequests)
	lt, _ := fakeTranslationServer(t, false, http.StatusInternalServerError)
	t.Setenv("LIBRETRANSLATE_URL", lt.URL)

	w := doStatusPageRequest(f.r, "POST", f.translatePath(), translateBody())

	assert.Equal(t, http.StatusTooManyRequests, w.Code, "der Fehler des Hauptdienstes, nicht der des Ersatzes")
	assert.Equal(t, "translation_quota_exceeded", codeOf(t, w.Body.Bytes()))
}

func TestTranslate_ErsatzdienstOhneTitelUebersetztNurDieNachricht(t *testing.T) {
	f := deepLFixture(t, http.StatusInternalServerError)
	lt, calls := fakeTranslationServer(t, false, http.StatusOK)
	t.Setenv("LIBRETRANSLATE_URL", lt.URL)

	w := doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": "x", "message": ""})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.EqualValues(t, 1, calls.Load())
}

// Update messages have no title. Only the message is
// translated (one call), the title stays empty; without text in title AND
// message there is a 400. Control check: previously the endpoint required a title.
func TestTranslate_NurNachrichtOhneTitel(t *testing.T) {
	f := newTranslationFixture(t)
	srv, calls := fakeTranslationServer(t, false, http.StatusOK)
	t.Setenv("LIBRETRANSLATE_URL", srv.URL)

	w := doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "message": "Wir arbeiten daran"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "", out["title"])
	assert.Equal(t, "übersetzt", out["message"])
	assert.EqualValues(t, 1, calls.Load(), "leerer Titel wird nicht an den Dienst geschickt")

	// A blank title counts as none.
	calls.Store(0)
	w = doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": "  ", "message": "Text"})
	require.Equal(t, http.StatusOK, w.Code)
	assert.EqualValues(t, 1, calls.Load())

	for _, body := range []map[string]any{
		{"target_language": "en"},
		{"target_language": "en", "title": " ", "message": " "},
	} {
		w = doStatusPageRequest(f.r, "POST", f.translatePath(), body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "invalid_request", codeOf(t, w.Body.Bytes()))
	}

	// A service error on the message without a title is reported as usual.
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer failing.Close()
	t.Setenv("LIBRETRANSLATE_URL", failing.URL)
	w = doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": "Titel", "message": "Text"})
	assert.Equal(t, "translation_unavailable", codeOf(t, w.Body.Bytes()), "Fehler beim Titel")
}

// Together with the fallback service (review): update texts without a title also go via
// DeepL and the fallback service, without sending an empty title to the service; without text 400.
func TestTranslate_OhneTitelUeberDeepLUndErsatzdienst(t *testing.T) {
	f := deepLFixture(t, http.StatusInternalServerError)
	lt, calls := fakeTranslationServer(t, false, http.StatusOK)
	t.Setenv("LIBRETRANSLATE_URL", lt.URL)

	w := doStatusPageRequest(f.r, "POST", f.translatePath(), map[string]any{"target_language": "en", "title": "", "message": "x"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"fallback":true`)
	assert.Contains(t, w.Body.String(), `"title":""`)
	assert.EqualValues(t, 1, calls.Load())

	for _, body := range []map[string]any{
		{"target_language": "en", "title": "", "message": ""},
		{"target_language": "en"},
	} {
		w = doStatusPageRequest(f.r, "POST", f.translatePath(), body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "invalid_request", codeOf(t, w.Body.Bytes()))
	}
	assert.EqualValues(t, 1, calls.Load(), "bei 400 keine weitere Anfrage")
}
