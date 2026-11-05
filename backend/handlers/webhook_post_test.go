package handlers

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Tests für postWebhook (Fehlerklassen, Retry-After, Obergrenzen,
// kein Retry bei Permanent, keine Geheimnisse im Log) und den Limiter.
// Gesendet wird nur an httptest-Server auf Loopback; Wartezeiten laufen über
// die injizierte Schlaffunktion, nichts wartet wirklich.

var webhookTestEpoch = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

type fakeResp struct {
	status     int
	retryAfter string
}

// webhookEnv: Zeit, Schlaf und Jitter sind deterministisch; der Limiter hat
// einen großen Burst, damit er die Fälle nicht stört (eigene Tests unten).
type webhookEnv struct {
	mu     sync.Mutex
	sleeps []time.Duration
}

func newWebhookEnv(t *testing.T) *webhookEnv {
	t.Helper()
	SetWebhookLoopbackAllowedForTests(t.Cleanup)
	env := &webhookEnv{}
	origNow, origSleep, origJitter, origLimiter := webhookNow, webhookSleep, webhookJitter, webhookLimiter
	t.Cleanup(func() {
		webhookNow, webhookSleep, webhookJitter, webhookLimiter = origNow, origSleep, origJitter, origLimiter
	})
	webhookNow = func() time.Time { return webhookTestEpoch }
	webhookJitter = func(d time.Duration) time.Duration { return d }
	webhookSleep = func(_ context.Context, d time.Duration) error {
		env.mu.Lock()
		defer env.mu.Unlock()
		env.sleeps = append(env.sleeps, d)
		return nil
	}
	webhookLimiter = newWebhookRateLimiter(1000, 1000, 100, time.Hour)
	return env
}

// sequenceServer antwortet der Reihe nach mit seq (die letzte Antwort wiederholt sich).
func sequenceServer(t *testing.T, seq []fakeResp) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		r := seq[min(calls, len(seq)-1)]
		calls++
		if r.retryAfter != "" {
			w.Header().Set("Retry-After", r.retryAfter)
		}
		w.WriteHeader(r.status)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestPostWebhook_Fehlerklassen(t *testing.T) {
	date5s := webhookTestEpoch.Add(5 * time.Second).Format(http.TimeFormat)
	s := time.Second
	cases := []struct {
		name       string
		seq        []fakeResp
		wantKind   webhookKind
		wantStatus int
		wantCalls  int
		wantSleeps []time.Duration
	}{
		{"Erfolg", []fakeResp{{200, ""}}, webhookOK, 200, 1, nil},
		{"429 mit Retry-After in Sekunden, danach OK", []fakeResp{{429, "2"}, {200, ""}}, webhookOK, 200, 2, []time.Duration{2 * s}},
		{"429 mit Retry-After als Datum, danach OK", []fakeResp{{429, date5s}, {200, ""}}, webhookOK, 200, 2, []time.Duration{5 * s}},
		{"429 ohne Header: Backoff, bleibt 429", []fakeResp{{429, ""}}, webhookRateLimited, 429, 3, []time.Duration{1 * s, 2 * s}},
		{"429 mit zu langem Retry-After: kein Warten", []fakeResp{{429, "60"}}, webhookRateLimited, 429, 1, nil},
		{"429 summiert sich über 30 s: Abbruch", []fakeResp{{429, "20"}}, webhookRateLimited, 429, 2, []time.Duration{20 * s}},
		{"503 dann OK", []fakeResp{{503, ""}, {200, ""}}, webhookOK, 200, 2, []time.Duration{1 * s}},
		{"503 mit Retry-After", []fakeResp{{503, "3"}, {200, ""}}, webhookOK, 200, 2, []time.Duration{3 * s}},
		{"503 bleibt: transient nach 3 Versuchen", []fakeResp{{503, ""}}, webhookTransient, 503, 3, []time.Duration{1 * s, 2 * s}},
		{"500 ist transient", []fakeResp{{500, ""}, {200, ""}}, webhookOK, 200, 2, []time.Duration{1 * s}},
		{"400 permanent, kein Retry", []fakeResp{{400, ""}, {200, ""}}, webhookPermanent, 400, 1, nil},
		{"403 permanent, kein Retry", []fakeResp{{403, ""}, {200, ""}}, webhookPermanent, 403, 1, nil},
		{"404 permanent, kein Retry", []fakeResp{{404, ""}, {200, ""}}, webhookPermanent, 404, 1, nil},
		{"Weiterleitung gilt als permanent", []fakeResp{{302, ""}}, webhookPermanent, 302, 1, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newWebhookEnv(t)
			srv, calls := sequenceServer(t, tc.seq)
			res := postWebhook(context.Background(), srv.URL+"/hook", map[string]string{"text": "x"}, webhookModeNotify)
			if res.Kind != tc.wantKind || res.Status != tc.wantStatus {
				t.Fatalf("Kind/Status = %v/%d, erwartet %v/%d", res.Kind, res.Status, tc.wantKind, tc.wantStatus)
			}
			if *calls != tc.wantCalls || res.Attempts != tc.wantCalls {
				t.Errorf("Aufrufe = %d (Attempts %d), erwartet %d", *calls, res.Attempts, tc.wantCalls)
			}
			if len(env.sleeps) != len(tc.wantSleeps) {
				t.Fatalf("Wartezeiten = %v, erwartet %v", env.sleeps, tc.wantSleeps)
			}
			for i := range tc.wantSleeps {
				if env.sleeps[i] != tc.wantSleeps[i] {
					t.Errorf("Wartezeit %d = %v, erwartet %v", i, env.sleeps[i], tc.wantSleeps[i])
				}
			}
		})
	}
}

func TestPostWebhook_RateLimitedMeldetRetryAfter(t *testing.T) {
	newWebhookEnv(t)
	srv, _ := sequenceServer(t, []fakeResp{{429, "60"}})
	res := postWebhook(context.Background(), srv.URL, "x", webhookModeNotify)
	if res.RetryAfter != 60*time.Second || res.RetryAfterSeconds() != 60 {
		t.Errorf("RetryAfter = %v / %ds, erwartet 60 s", res.RetryAfter, res.RetryAfterSeconds())
	}
	if (WebhookResult{}).RetryAfterSeconds() != 1 {
		t.Error("ohne Angabe muss mindestens 1 Sekunde gemeldet werden")
	}
}

func TestPostWebhook_ProbeWartetNieUndSendetEinmal(t *testing.T) {
	for _, st := range []int{429, 503, 500} {
		env := newWebhookEnv(t)
		srv, calls := sequenceServer(t, []fakeResp{{st, "7"}, {200, ""}})
		res := postWebhook(context.Background(), srv.URL, "x", webhookModeProbe)
		if res.OK() || *calls != 1 || len(env.sleeps) != 0 {
			t.Errorf("Status %d: ok=%v Aufrufe=%d Wartezeiten=%v, erwartet genau 1 Versuch ohne Warten", st, res.OK(), *calls, env.sleeps)
		}
		if st == 429 && (res.Kind != webhookRateLimited || res.RetryAfterSeconds() != 7) {
			t.Errorf("429: Kind=%v retry=%d, erwartet rate_limited/7", res.Kind, res.RetryAfterSeconds())
		}
	}
}

func TestPostWebhook_Timeout(t *testing.T) {
	newWebhookEnv(t)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	orig := webhookTestClient.Timeout
	webhookTestClient.Timeout = 50 * time.Millisecond
	t.Cleanup(func() { webhookTestClient.Timeout = orig })

	res := postWebhook(context.Background(), srv.URL, "x", webhookModeProbe)
	if res.Kind != webhookTransient || res.Class != "timeout" || res.Status != 0 {
		t.Fatalf("Ergebnis = %+v, erwartet transient/timeout", res)
	}
}

func TestPostWebhook_NetzfehlerIstTransient(t *testing.T) {
	env := newWebhookEnv(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	res := postWebhook(context.Background(), url, "x", webhookModeNotify)
	if res.Kind != webhookTransient || res.Class != "network" || res.Attempts != 3 || len(env.sleeps) != 2 {
		t.Fatalf("Ergebnis = %+v, Wartezeiten %v; erwartet transient/network nach 3 Versuchen", res, env.sleeps)
	}
	if strings.Contains(res.Err().Error(), "127.0.0.1") {
		t.Errorf("Err() darf die URL nicht enthalten: %v", res.Err())
	}
}

func TestPostWebhook_AbgebrochenerKontextBeendetWarten(t *testing.T) {
	newWebhookEnv(t)
	webhookSleep = func(context.Context, time.Duration) error { return context.Canceled }
	srv, calls := sequenceServer(t, []fakeResp{{429, "1"}, {200, ""}})
	res := postWebhook(context.Background(), srv.URL, "x", webhookModeNotify)
	if res.OK() || *calls != 1 {
		t.Errorf("ok=%v Aufrufe=%d, erwartet Abbruch nach dem ersten Versuch", res.OK(), *calls)
	}
}

func TestPostWebhook_UngueltigeEingaben(t *testing.T) {
	newWebhookEnv(t)
	if res := postWebhook(context.Background(), "http://x", make(chan int), webhookModeNotify); res.Kind != webhookPermanent || res.Class != "invalid_payload" {
		t.Errorf("ungültige Nutzlast: %+v", res)
	}
	if res := postWebhook(context.Background(), "http://exa mple.com/\x7f", "x", webhookModeNotify); res.Kind != webhookPermanent || res.Class != "invalid_url" {
		t.Errorf("ungültige URL: %+v", res)
	}
}

// Geheimnisse: URL, Pfad, key und token dürfen in keiner Logzeile und in
// keinem Err() auftauchen, egal welche Fehlerklasse.
func TestPostWebhook_LogEnthaeltNieDieURL(t *testing.T) {
	const secretPath = "/v1/spaces/GEHEIMERSPACE/messages"
	const secretQuery = "?key=GEHEIMERKEY&token=GEHEIMERTOKEN"
	for _, seq := range [][]fakeResp{{{429, "2"}, {200, ""}}, {{429, ""}}, {{503, ""}}, {{403, ""}}, {{200, ""}}} {
		newWebhookEnv(t)
		var buf bytes.Buffer
		orig := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
		t.Cleanup(func() { slog.SetDefault(orig) })

		srv, _ := sequenceServer(t, seq)
		full := srv.URL + secretPath + secretQuery
		res := postWebhook(context.Background(), full, "x", webhookModeNotify)
		out := buf.String() + errText(res)
		for _, secret := range []string{"GEHEIMER", srv.URL, strings.TrimPrefix(srv.URL, "http://"), "key=", "token="} {
			if strings.Contains(out, secret) {
				t.Errorf("Folge %v: Ausgabe enthält %q:\n%s", seq, secret, out)
			}
		}
		if seq[0].status != 200 && !strings.Contains(out, "url_id="+webhookURLID(full)) {
			t.Errorf("Folge %v: Kurzkennung fehlt im Log:\n%s", seq, out)
		}
		slog.SetDefault(orig)
	}
}

func errText(r WebhookResult) string {
	if err := r.Err(); err != nil {
		return err.Error()
	}
	return ""
}

func TestPostWebhook_LogtRateLimitMitWartezeit(t *testing.T) {
	newWebhookEnv(t)
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })
	srv, _ := sequenceServer(t, []fakeResp{{429, "2"}, {200, ""}})
	postWebhook(context.Background(), srv.URL, "x", webhookModeNotify)
	for _, want := range []string{"webhook rate limited, retry in 2s", "status=429", "class=http_429", "attempt=1"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("Log enthält %q nicht:\n%s", want, buf.String())
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := webhookTestEpoch
	cases := []struct {
		in     string
		want   time.Duration
		wantOK bool
	}{
		{"", 0, false},
		{"abc", 0, false},
		{"5", 5 * time.Second, true},
		{" 5 ", 5 * time.Second, true},
		{"0", 0, true},
		{"-3", 0, true},
		{"999999999", 24 * time.Hour, true},
		{now.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second, true},
		{now.Add(-10 * time.Second).Format(http.TimeFormat), 0, true},
	}
	for _, tc := range cases {
		got, ok := parseRetryAfter(tc.in, now)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("parseRetryAfter(%q) = %v,%v; erwartet %v,%v", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestWebhookKind_String(t *testing.T) {
	for k, want := range map[webhookKind]string{webhookOK: "ok", webhookRateLimited: "rate_limited", webhookTransient: "transient", webhookPermanent: "permanent"} {
		if k.String() != want {
			t.Errorf("%d.String() = %q, erwartet %q", k, k.String(), want)
		}
	}
	if (WebhookResult{Kind: webhookOK}).Err() != nil {
		t.Error("OK darf keinen Fehler liefern")
	}
	if got := (WebhookResult{Kind: webhookTransient, Class: "timeout"}).Err().Error(); got != "webhook timeout" {
		t.Errorf("Err() = %q", got)
	}
}

// --- Limiter ---

func TestWebhookRateLimiter_BurstUndNachfuellung(t *testing.T) {
	l := newWebhookRateLimiter(1, 2, 10, time.Hour)
	now := webhookTestEpoch
	for i := 0; i < 2; i++ {
		if ok, wait := l.reserve("a", now, 0); !ok || wait != 0 {
			t.Fatalf("Burst %d: ok=%v wait=%v", i, ok, wait)
		}
	}
	if ok, wait := l.reserve("a", now, 0); ok || wait != time.Second {
		t.Fatalf("dritte Nachricht ohne Warten: ok=%v wait=%v, erwartet abgelehnt/1s", ok, wait)
	}
	if ok, wait := l.reserve("a", now, 5*time.Second); !ok || wait != time.Second {
		t.Fatalf("mit Warteerlaubnis: ok=%v wait=%v, erwartet 1s", ok, wait)
	}
	// Die Reservierung zählt: die nächste muss 2 s warten.
	if ok, wait := l.reserve("a", now, 5*time.Second); !ok || wait != 2*time.Second {
		t.Fatalf("zweite Reservierung: ok=%v wait=%v, erwartet 2s", ok, wait)
	}
	if ok, wait := l.reserve("a", now, time.Second); ok || wait != 3*time.Second {
		t.Fatalf("über der Obergrenze: ok=%v wait=%v, erwartet abgelehnt/3s", ok, wait)
	}
	// Nach 10 s ist der Bucket wieder voll (höchstens Burst).
	later := now.Add(10 * time.Second)
	for i := 0; i < 2; i++ {
		if ok, _ := l.reserve("a", later, 0); !ok {
			t.Fatalf("nach Nachfüllung Burst %d abgelehnt", i)
		}
	}
	if ok, _ := l.reserve("a", later, 0); ok {
		t.Error("nach Nachfüllung darf der Burst nicht überschritten werden")
	}
}

func TestWebhookRateLimiter_SchluesselUnabhaengig(t *testing.T) {
	l := newWebhookRateLimiter(1, 1, 10, time.Hour)
	if ok, _ := l.reserve("a", webhookTestEpoch, 0); !ok {
		t.Fatal("a")
	}
	if ok, _ := l.reserve("b", webhookTestEpoch, 0); !ok {
		t.Error("b darf von a nicht gedrosselt werden")
	}
}

func TestWebhookRateLimiter_SpeicherIstBegrenzt(t *testing.T) {
	l := newWebhookRateLimiter(1, 1, 3, time.Minute)
	now := webhookTestEpoch
	l.reserve("alt1", now, 0)
	l.reserve("alt2", now, 0)
	l.reserve("jung", now.Add(90*time.Second), 0)
	// Voll: alt1/alt2 sind nach 2 Minuten abgelaufen und fliegen raus, "jung" bleibt.
	l.reserve("neu", now.Add(2*time.Minute), 0)
	if _, ok := l.buckets["alt1"]; ok {
		t.Error("abgelaufener Eintrag alt1 wurde nicht entfernt")
	}
	if len(l.buckets) != 2 {
		t.Errorf("Einträge = %d, erwartet 2 (jung, neu)", len(l.buckets))
	}
	// Dauerlauf mit lauter frischen Schlüsseln: nie über der Obergrenze.
	for i := 0; i < 50; i++ {
		l.reserve(string(rune('A'+i)), now.Add(2*time.Minute), 0)
		if len(l.buckets) > 3 {
			t.Fatalf("Einträge = %d, Obergrenze 3 überschritten", len(l.buckets))
		}
	}
}

// Vorfall-Sturm: viele Nachrichten an dieselbe URL werden vom Limiter auf
// etwa eine pro Sekunde gestreckt (nicht alle sofort gesendet).
func TestPostWebhook_LimiterStrecktSturm(t *testing.T) {
	env := newWebhookEnv(t)
	webhookLimiter = newWebhookRateLimiter(1, 2, 10, time.Hour)
	srv, calls := sequenceServer(t, []fakeResp{{200, ""}})
	for i := 0; i < 5; i++ {
		if res := postWebhook(context.Background(), srv.URL, "x", webhookModeNotify); !res.OK() {
			t.Fatalf("Nachricht %d: %+v", i, res)
		}
	}
	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}
	if *calls != 5 || len(env.sleeps) != len(want) {
		t.Fatalf("Aufrufe=%d Wartezeiten=%v, erwartet 5 Aufrufe und %v", *calls, env.sleeps, want)
	}
	for i := range want {
		if env.sleeps[i] != want[i] {
			t.Errorf("Wartezeit %d = %v, erwartet %v", i, env.sleeps[i], want[i])
		}
	}
}

func TestPostWebhook_ProbeUmgehtDenLimiterNichtUndWartetNicht(t *testing.T) {
	env := newWebhookEnv(t)
	webhookLimiter = newWebhookRateLimiter(1, 1, 10, time.Hour)
	srv, calls := sequenceServer(t, []fakeResp{{200, ""}})
	if res := postWebhook(context.Background(), srv.URL, "x", webhookModeProbe); !res.OK() {
		t.Fatalf("erste Nachricht: %+v", res)
	}
	res := postWebhook(context.Background(), srv.URL, "x", webhookModeProbe)
	if res.Kind != webhookRateLimited || res.Class != "local_limit" || res.RetryAfterSeconds() != 1 || res.Attempts != 0 {
		t.Fatalf("zweite Nachricht: %+v, erwartet lokal gedrosselt, 1 s", res)
	}
	if *calls != 1 || len(env.sleeps) != 0 {
		t.Errorf("Aufrufe=%d Wartezeiten=%v, erwartet 1 Aufruf ohne Warten", *calls, env.sleeps)
	}
}

// Ist nach einem gesendeten Versuch der lokale Limiter am Zug und die
// Obergrenze erreicht, gilt das Ergebnis des gesendeten Versuchs.
func TestPostWebhook_LokalerStauNachVersuchLiefertLetztesErgebnis(t *testing.T) {
	newWebhookEnv(t)
	webhookLimiter = newWebhookRateLimiter(0.01, 1, 10, time.Hour) // 100 s je Token
	srv, calls := sequenceServer(t, []fakeResp{{503, "1"}})
	res := postWebhook(context.Background(), srv.URL, "x", webhookModeNotify)
	if res.Kind != webhookTransient || res.Status != 503 || *calls != 1 {
		t.Fatalf("Ergebnis %+v, Aufrufe %d; erwartet transient/503 nach 1 Versuch", res, *calls)
	}
}

func TestWebhookSleep_StandardImplementierung(t *testing.T) {
	if err := webhookSleep(context.Background(), time.Millisecond); err != nil {
		t.Errorf("kurzes Warten: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := webhookSleep(ctx, time.Hour); err == nil {
		t.Error("abgebrochener Kontext muss das Warten beenden")
	}
}

// Bricht der Kontext beim Warten auf den lokalen Limiter ab, wird nichts
// gesendet (erster Versuch) bzw. das Ergebnis des letzten Versuchs geliefert.
func TestPostWebhook_AbbruchBeimLimiterWarten(t *testing.T) {
	newWebhookEnv(t)
	webhookLimiter = newWebhookRateLimiter(1, 1, 10, time.Hour)
	srv, calls := sequenceServer(t, []fakeResp{{503, "1"}})
	// Token verbrauchen, damit der nächste Aufruf warten muss.
	webhookLimiter.reserve(webhookKey(srv.URL), webhookTestEpoch, 0)
	webhookSleep = func(context.Context, time.Duration) error { return context.Canceled }
	res := postWebhook(context.Background(), srv.URL, "x", webhookModeNotify)
	if res.Kind != webhookTransient || res.Class != "canceled" || *calls != 0 {
		t.Fatalf("Ergebnis %+v, Aufrufe %d; erwartet transient/canceled ohne Versand", res, *calls)
	}
	if got := abortedWebhook(WebhookResult{Kind: webhookTransient, Status: 503}, 1); got.Status != 503 {
		t.Errorf("nach einem Versuch gilt dessen Ergebnis: %+v", got)
	}
}

func TestWebhookJitter_Standard(t *testing.T) {
	for i := 0; i < 100; i++ {
		d := time.Duration(1000) * time.Millisecond
		// Original-Implementierung: webhookJitter wurde in newWebhookEnv nicht ersetzt.
		got := webhookJitter(d)
		if got < 750*time.Millisecond || got > 1250*time.Millisecond {
			t.Fatalf("Jitter %v außerhalb von +-25 %%", got)
		}
	}
}
