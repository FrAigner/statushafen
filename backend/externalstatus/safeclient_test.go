package externalstatus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func get(t *testing.T, c *http.Client, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := c.Do(req)
	if err == nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	return resp, err
}

// redirectHandler: /redirect?to=<ziel> leitet weiter, /chain/<n> leitet n-mal
// weiter, sonst 200 "ok" (Cookie wird gesetzt, darf nie zurückkommen).
func redirectHandler(seenCookie *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			*seenCookie = true
		}
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "geheim"})
		switch {
		case r.URL.Path == "/redirect":
			http.Redirect(w, r, r.URL.Query().Get("to"), http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/chain/"):
			n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/chain/"))
			if n > 0 {
				http.Redirect(w, r, fmt.Sprintf("https://example.com/chain/%d", n-1), http.StatusFound)
				return
			}
			_, _ = io.WriteString(w, "ok")
		default:
			_, _ = io.WriteString(w, "ok:"+r.Header.Get("User-Agent"))
		}
	})
}

func TestSafeClient_ErlaubtesZiel(t *testing.T) {
	var cookie bool
	tt := newTestTarget(t, redirectHandler(&cookie))
	resp, err := get(t, tt.client, "https://example.com/x")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok:Go-http-client/1.1", string(body), "User-Agent setzt erst getJSON")
	assert.Equal(t, []string{"93.184.216.34:443"}, tt.dialedAddrs(), "verbindet mit der geprüften IP auf 443")
}

func TestSafeClient_BlockiertZiele(t *testing.T) {
	var cookie bool
	tt := newTestTarget(t, redirectHandler(&cookie))
	tt.resolver.answers["intern.example"] = [][]string{{"10.0.0.5"}}
	tt.resolver.answers["gemischt.example"] = [][]string{{"93.184.216.34", "192.168.1.10"}}
	tt.resolver.answers["v6intern.example"] = [][]string{{"fd00::1"}}
	tt.resolver.answers["metadata.example"] = [][]string{{"169.254.169.254"}}
	tt.resolver.answers["cgnat.example"] = [][]string{{"100.64.1.1"}}

	cases := map[string]string{
		"http statt https":                     "http://example.com/",
		"anderer Port":                         "https://example.com:8443/",
		"Hostname löst auf private IP auf":     "https://intern.example/",
		"gemischte DNS-Antwort":                "https://gemischt.example/",
		"Hostname löst auf Unique-Local auf":   "https://v6intern.example/",
		"Link-Local/Cloud-Metadaten per DNS":   "https://metadata.example/",
		"CGNAT per DNS":                        "https://cgnat.example/",
		"IP-Literal Loopback":                  "https://127.0.0.1/",
		"IP-Literal IPv6 Loopback":             "https://[::1]/",
		"IP-Literal 0.0.0.0":                   "https://0.0.0.0/",
		"IP-Literal Multicast":                 "https://224.0.0.1/",
		"IP-Literal 6to4 mit Zone":             "https://[2002:c0a8:1::1%25eth0]/",
		"IP-Literal NAT64 mit Zone":            "https://[64:ff9b::a9fe:a9fe%251]/",
		"IP-Literal Site-Local mit Zone":       "https://[fec0::1%25eth0]/",
		"Redirect auf 127.0.0.1":               "https://example.com/redirect?to=https://127.0.0.1/admin",
		"Redirect auf privaten Hostnamen":      "https://example.com/redirect?to=https://intern.example/",
		"Redirect auf http":                    "https://example.com/redirect?to=http://example.com/",
		"Redirect auf anderen Port":            "https://example.com/redirect?to=https://example.com:8443/",
		"Redirect mit Zugangsdaten in der URL": "https://example.com/redirect?to=https://user:pw@example.com/",
		"mehr als 3 Weiterleitungen":           "https://example.com/chain/4",
	}
	for name, url := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := get(t, tt.client, url)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrBlockedTarget), "erwartet ErrBlockedTarget, war: %v", err)
		})
	}
	for _, addr := range tt.dialedAddrs() {
		assert.Equal(t, "93.184.216.34:443", addr, "nur die erlaubte Adresse darf je gewählt werden")
	}
	assert.False(t, cookie, "Cookies/Credentials dürfen nie mitgeschickt werden")
}

func TestSafeClient_DreiWeiterleitungenErlaubt(t *testing.T) {
	var cookie bool
	tt := newTestTarget(t, redirectHandler(&cookie))
	resp, err := get(t, tt.client, "https://example.com/chain/3")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.False(t, cookie, "kein Cookie-Jar: der gesetzte Cookie kommt bei der Weiterleitung nicht zurück")
}

// DNS-Rebinding: Jede Verbindung löst neu auf und prüft erneut - die zweite
// Antwort (interne IP) wird abgelehnt, obwohl die erste öffentlich war.
func TestSafeClient_DNSRebinding(t *testing.T) {
	var cookie bool
	tt := newTestTarget(t, redirectHandler(&cookie))
	tt.resolver.answers["example.com"] = [][]string{{"93.184.216.34"}, {"127.0.0.1"}}
	_, err := get(t, tt.client, "https://example.com/a")
	require.NoError(t, err)
	_, err = get(t, tt.client, "https://example.com/b")
	require.ErrorIs(t, err, ErrBlockedTarget)
}

func TestSafeClient_ResolverFehler(t *testing.T) {
	var cookie bool
	tt := newTestTarget(t, redirectHandler(&cookie))

	_, err := get(t, tt.client, "https://unbekannt.example/")
	require.Error(t, err, "leere DNS-Antwort")
	assert.Contains(t, err.Error(), "keine Adresse")

	tt.resolver.answers["kaputt.example"] = [][]string{{"kein-ip"}}
	_, err = get(t, tt.client, "https://kaputt.example/")
	require.ErrorIs(t, err, ErrBlockedTarget, "unlesbare Adresse gilt als gesperrt")

	tt.resolver.err = errors.New("dns kaputt")
	_, err = get(t, tt.client, "https://example.com/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dns kaputt")
}

func TestSafeClient_DialFehler(t *testing.T) {
	resolver := &fakeResolver{answers: map[string][][]string{"example.com": {{"93.184.216.34", "93.184.216.35"}}}}
	var tried []string
	c := NewSafeClient(ClientConfig{Resolver: resolver, Dial: func(_ context.Context, _, addr string) (net.Conn, error) {
		tried = append(tried, addr)
		return nil, errors.New("verbindung abgelehnt")
	}})
	_, err := get(t, c, "https://example.com/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verbindung abgelehnt")
	assert.Equal(t, []string{"93.184.216.34:443", "93.184.216.35:443"}, tried, "alle geprüften Adressen werden versucht")
}

func TestGuardedDial_UngueltigeAdresse(t *testing.T) {
	_, err := guardedDial(&fakeResolver{}, nil)(context.Background(), "tcp", "ohne-port")
	require.Error(t, err)
}

func TestNewSafeClient_Defaults(t *testing.T) {
	c := NewSafeClient(ClientConfig{})
	assert.Equal(t, RequestTimeout, c.Timeout)
	assert.Nil(t, c.Jar, "kein Cookie-Jar")
	tr := c.Transport.(*http.Transport)
	assert.Nil(t, tr.Proxy, "kein Proxy, sonst liefe die IP-Prüfung ins Leere")
}

func TestValidateBaseURL(t *testing.T) {
	ok := map[string]string{
		"https://status.example.com":          "https://status.example.com",
		" https://Status.Example.Com/ ":       "https://status.example.com",
		"https://status.example.com:443/":     "https://status.example.com",
		"https://example.com/status/":         "https://example.com/status",
		"https://www.githubstatus.com//":      "https://www.githubstatus.com",
		"https://status.example.com/a%20b/c/": "https://status.example.com/a%20b/c",
	}
	for in, want := range ok {
		got, err := ValidateBaseURL(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	bad := []string{
		"http://status.example.com", "ftp://x", "https://", "status.example.com",
		"https://x.example:8443", "https://u:p@x.example", "https://x.example/?a=1",
		"https://x.example/#frag", "https://[::1", "javascript:alert(1)",
	}
	for _, in := range bad {
		_, err := ValidateBaseURL(in)
		assert.Error(t, err, in)
	}
}

func TestGetJSON(t *testing.T) {
	var gotHeaders http.Header
	tt := newTestTarget(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		switch r.URL.Path {
		case "/304":
			w.WriteHeader(http.StatusNotModified)
		case "/500":
			w.WriteHeader(http.StatusInternalServerError)
		case "/gross":
			_, _ = w.Write(make([]byte, MaxResponseBytes+1))
		case "/genau":
			_, _ = w.Write(make([]byte, MaxResponseBytes))
		default:
			w.Header().Set("ETag", `W/"neu"`)
			w.Header().Set("Last-Modified", "Thu, 29 Oct 2026 12:00:00 GMT")
			_, _ = io.WriteString(w, `{"a":1}`)
		}
	}))
	ctx := context.Background()
	req := FetchRequest{ETag: `W/"alt"`, LastModified: "Wed, 28 Oct 2026 12:00:00 GMT"}

	res, err := getJSON(ctx, tt.client, "https://example.com/ok", req)
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`, string(res.Body))
	assert.Equal(t, `W/"neu"`, res.ETag)
	assert.Equal(t, "Thu, 29 Oct 2026 12:00:00 GMT", res.LastModified)
	assert.Equal(t, UserAgent, gotHeaders.Get("User-Agent"))
	assert.Equal(t, `W/"alt"`, gotHeaders.Get("If-None-Match"))
	assert.Equal(t, "Wed, 28 Oct 2026 12:00:00 GMT", gotHeaders.Get("If-Modified-Since"))
	assert.Equal(t, "application/json", gotHeaders.Get("Accept"))

	_, err = getJSON(ctx, tt.client, "https://example.com/ok", FetchRequest{})
	require.NoError(t, err)
	assert.Empty(t, gotHeaders.Get("If-None-Match"), "ohne ETag kein If-None-Match")
	assert.Empty(t, gotHeaders.Get("If-Modified-Since"))

	res, err = getJSON(ctx, tt.client, "https://example.com/304", req)
	require.NoError(t, err)
	assert.True(t, res.NotModified)
	assert.Equal(t, req.ETag, res.ETag, "304 behält die alten Validatoren")
	assert.Equal(t, req.LastModified, res.LastModified)

	_, err = getJSON(ctx, tt.client, "https://example.com/500", req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")

	_, err = getJSON(ctx, tt.client, "https://example.com/gross", req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "größer als")

	res, err = getJSON(ctx, tt.client, "https://example.com/genau", req)
	require.NoError(t, err, "genau das Limit ist noch erlaubt")
	assert.Len(t, res.Body, MaxResponseBytes)

	_, err = getJSON(ctx, tt.client, "://kaputt", req)
	require.Error(t, err)

	_, err = getJSON(ctx, tt.client, "https://127.0.0.1/", req)
	require.ErrorIs(t, err, ErrBlockedTarget)
}

// abbrechenderBody liefert einen Lesefehler mitten in der Antwort.
func TestGetJSON_Lesefehler(t *testing.T) {
	tt := newTestTarget(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "abgeschnitten")
	}))
	_, err := getJSON(context.Background(), tt.client, "https://example.com/", FetchRequest{})
	require.Error(t, err)
}

// Der User-Agent nennt das Projekt, aber keine Betreiber-Domain: Das Repository ist
// für alle Installationen gleich, ein fest verdrahteter Betreibername wäre falsch.
func TestUserAgentNenntKeineBetreiberDomain(t *testing.T) {
	assert.Contains(t, UserAgent, "Statushafen")
	assert.NotContains(t, UserAgent, "statushafen.de")
	assert.Contains(t, UserAgent, "https://github.com/fraigner/statushafen")
}
