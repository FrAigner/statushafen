package translation

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// LibreTranslate- und DeepL-Anbindung gegen httptest-Server.

func TestLibreTranslate_Erfolg(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/translate" || r.Method != http.MethodPost {
			t.Errorf("unerwarteter Aufruf %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"translatedText":"Hallo"}`))
	}))
	defer srv.Close()

	p := NewLibreTranslate(srv.URL+"/", "geheim", srv.Client())
	out, err := p.Translate("Hello", "EN", "DE")
	if err != nil || out != "Hallo" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got["q"] != "Hello" || got["source"] != "en" || got["target"] != "de" || got["api_key"] != "geheim" || got["format"] != "text" {
		t.Errorf("Anfrage unerwartet: %v", got)
	}
}

func TestLibreTranslate_OhneSchluesselKeinApiKeyFeld(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"translatedText":"x"}`))
	}))
	defer srv.Close()
	if _, err := NewLibreTranslate(srv.URL, "", srv.Client()).Translate("a", "de", "en"); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["api_key"]; ok {
		t.Error("api_key darf ohne Schlüssel nicht gesendet werden")
	}
}

func TestProvider_Fehlerabbildung(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{http.StatusUnauthorized, `{}`, ErrAuth},
		{http.StatusForbidden, `{}`, ErrAuth},
		{http.StatusTooManyRequests, `{}`, ErrQuota},
		{456, `{}`, ErrQuota},
		{http.StatusBadRequest, `{}`, ErrUnsupportedLanguage},
		{http.StatusUnprocessableEntity, `{}`, ErrUnsupportedLanguage},
		{http.StatusInternalServerError, `{}`, ErrUnavailable},
		{http.StatusOK, `kein json`, ErrUnavailable},
		{http.StatusOK, `{"translatedText":"","translations":[]}`, ErrUnavailable},
		{http.StatusOK, `{"translations":[{"text":""}]}`, ErrUnavailable},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		for name, p := range map[string]Provider{
			"libre": NewLibreTranslate(srv.URL, "k", srv.Client()),
			"deepl": NewDeepL("k", srv.URL, srv.Client()),
		} {
			if _, err := p.Translate("a", "de", "en"); !errors.Is(err, tc.want) {
				t.Errorf("%s status %d: err=%v, erwartet %v", name, tc.status, err, tc.want)
			}
		}
		srv.Close()
	}
}

func TestProvider_NetzwerkfehlerOhneAdresseImText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // Verbindung scheitert
	for _, p := range []Provider{NewLibreTranslate(url, "TOPSECRET", http.DefaultClient), NewDeepL("TOPSECRET", url, http.DefaultClient)} {
		_, err := p.Translate("a", "de", "en")
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("err=%v", err)
		}
		if strings.Contains(err.Error(), url) || strings.Contains(err.Error(), "TOPSECRET") {
			t.Errorf("Fehlertext enthält Adresse oder Schlüssel: %v", err)
		}
	}
}

func TestPostJSON_UngueltigeAdresseUndNichtSerialisierbar(t *testing.T) {
	if _, _, err := postJSON(http.DefaultClient, "http://[::1", nil, map[string]string{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("ungültige URL: %v", err)
	}
	if _, _, err := postJSON(http.DefaultClient, "http://x", nil, make(chan int)); !errors.Is(err, ErrUnavailable) {
		t.Errorf("nicht serialisierbar: %v", err)
	}
}

type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("kaputt") }
func (errBody) Close() error             { return nil }

type bodyTransport struct{}

func (bodyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: errBody{}, Header: http.Header{}}, nil
}

func TestPostJSON_LesefehlerImBody(t *testing.T) {
	c := &http.Client{Transport: bodyTransport{}, Timeout: time.Second}
	if _, _, err := postJSON(c, "http://x", nil, map[string]string{}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err=%v", err)
	}
}

func TestDeepL_AnfrageUndHostWahl(t *testing.T) {
	var auth string
	var got struct {
		Text   []string `json:"text"`
		Source string   `json:"source_lang"`
		Target string   `json:"target_lang"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v2/translate" {
			t.Errorf("Pfad %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"translations":[{"text":"Hello"}]}`))
	}))
	defer srv.Close()

	out, err := NewDeepL("abc:fx", srv.URL+"/", srv.Client()).Translate("Hallo", "de", "en")
	if err != nil || out != "Hello" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if auth != "DeepL-Auth-Key abc:fx" || got.Source != "DE" || got.Target != "EN-GB" || len(got.Text) != 1 || got.Text[0] != "Hallo" {
		t.Errorf("auth=%q req=%+v", auth, got)
	}

	if d := NewDeepL("abc:fx", "", nil); d.baseURL != deeplFreeURL {
		t.Errorf("Free-Schlüssel: %s", d.baseURL)
	}
	if d := NewDeepL("abc", "", nil); d.baseURL != deeplProURL {
		t.Errorf("Pro-Schlüssel: %s", d.baseURL)
	}
}

func TestDeeplTarget(t *testing.T) {
	for in, want := range map[string]string{"en": "EN-GB", "pt": "PT-PT", "de": "DE", "pt-br": "PT-BR"} {
		if got := deeplTarget(in); got != want {
			t.Errorf("%s: %s, erwartet %s", in, got, want)
		}
	}
}

// Stapel: mehrere Texte in einer Anfrage (Titel + Nachricht, Betreiber-Wunsch 31.10.2026).
func TestLibreTranslate_TranslateManyEineAnfrage(t *testing.T) {
	var got map[string]any
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"translatedText":["Title","Body"]}`))
	}))
	defer srv.Close()

	out, err := NewLibreTranslate(srv.URL, "geheim", srv.Client()).TranslateMany([]string{"Titel", "Text"}, "DE", "EN")

	if err != nil || len(out) != 2 || out[0] != "Title" || out[1] != "Body" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if calls != 1 {
		t.Errorf("erwartet eine Anfrage, waren %d", calls)
	}
	q, _ := got["q"].([]any)
	if len(q) != 2 || got["source"] != "de" || got["target"] != "en" || got["api_key"] != "geheim" {
		t.Errorf("Anfrage unerwartet: %v", got)
	}
}

func TestDeepL_TranslateManyEineAnfrage(t *testing.T) {
	var got struct {
		Text []string `json:"text"`
	}
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"translations":[{"text":"Title"},{"text":"Body"}]}`))
	}))
	defer srv.Close()

	out, err := NewDeepL("key", srv.URL, srv.Client()).TranslateMany([]string{"Titel", "Text"}, "de", "en")

	if err != nil || len(out) != 2 || out[1] != "Body" || calls != 1 || len(got.Text) != 2 {
		t.Fatalf("out=%v err=%v calls=%d text=%v", out, err, calls, got.Text)
	}
}

func TestTranslateMany_FehlerfaelleBeiderAnbieter(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		deepl      bool
		want       error
	}{
		{"LT zu wenige Texte", `{"translatedText":["x"]}`, http.StatusOK, false, ErrUnavailable},
		{"LT leerer Text", `{"translatedText":["x",""]}`, http.StatusOK, false, ErrUnavailable},
		{"LT kaputtes JSON", `kaputt`, http.StatusOK, false, ErrUnavailable},
		{"LT Kontingent", ``, http.StatusTooManyRequests, false, ErrQuota},
		{"DeepL zu wenige Texte", `{"translations":[{"text":"x"}]}`, http.StatusOK, true, ErrUnavailable},
		{"DeepL leerer Text", `{"translations":[{"text":"x"},{"text":""}]}`, http.StatusOK, true, ErrUnavailable},
		{"DeepL Kontingent", ``, http.StatusTooManyRequests, true, ErrQuota},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		var err error
		if c.deepl {
			_, err = NewDeepL("k", srv.URL, srv.Client()).TranslateMany([]string{"a", "b"}, "de", "en")
		} else {
			_, err = NewLibreTranslate(srv.URL, "", srv.Client()).TranslateMany([]string{"a", "b"}, "de", "en")
		}
		srv.Close()
		if !errors.Is(err, c.want) {
			t.Errorf("%s: erwartet %v, war %v", c.name, c.want, err)
		}
	}
}

func TestTranslateMany_NetzfehlerGehtDurch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nicht erreichbar
	if _, err := NewLibreTranslate(url, "", srv.Client()).TranslateMany([]string{"a"}, "de", "en"); err == nil {
		t.Error("LibreTranslate: Netzfehler erwartet")
	}
	if _, err := NewDeepL("k", url, srv.Client()).TranslateMany([]string{"a"}, "de", "en"); err == nil {
		t.Error("DeepL: Netzfehler erwartet")
	}
}

type einzelProvider struct{ fail bool }

func (p einzelProvider) Translate(text, _, _ string) (string, error) {
	if p.fail {
		return "", ErrUnavailable
	}
	return "[" + text + "]", nil
}

func TestTranslateMany_OhneStapelUnterstuetzungNacheinander(t *testing.T) {
	out, err := TranslateMany(einzelProvider{}, []string{"a", "b"}, "de", "en")
	if err != nil || len(out) != 2 || out[0] != "[a]" || out[1] != "[b]" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if _, err := TranslateMany(einzelProvider{fail: true}, []string{"a"}, "de", "en"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Fehler erwartet, war %v", err)
	}
	if out, err := TranslateMany(einzelProvider{}, nil, "de", "en"); out != nil || err != nil {
		t.Errorf("leere Eingabe: %v %v", out, err)
	}
}
