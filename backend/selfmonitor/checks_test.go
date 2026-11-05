package selfmonitor

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBCheck(t *testing.T) {
	db := testutils.SetupTestDB()
	assert.True(t, DBCheck(db)(context.Background()).OK)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	res := DBCheck(db)(context.Background())
	assert.False(t, res.OK)
	assert.False(t, res.Skip)
}

func TestHTTPCheck(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	defer srv.Close()
	run := func(url string) Result { return HTTPCheck(srv.Client(), url)(context.Background()) }

	assert.True(t, run(srv.URL).OK)
	status = http.StatusServiceUnavailable
	assert.Equal(t, "HTTP-Status 503", run(srv.URL).Detail)
	status = http.StatusMultipleChoices
	assert.False(t, run(srv.URL).OK, "3xx ohne Ziel ist kein gesunder Zustand")
	assert.Equal(t, "ungültige Adresse", run("http://[::1").Detail)
	srv.Close()
	assert.Equal(t, "nicht erreichbar", run(srv.URL).Detail)
}

// fakeSMTP lauscht lokal und sendet den Gruß; leerer Gruß schließt sofort.
func fakeSMTP(t *testing.T, greeting string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte(greeting))
			_ = c.Close()
		}
	}()
	return ln.Addr().String()
}

func TestSMTPCheck(t *testing.T) {
	on := func() bool { return true }
	ctx := context.Background()
	assert.True(t, SMTPCheck(fakeSMTP(t, "220 mail ready\r\n"), on)(ctx).OK)
	assert.Equal(t, "Mailserver meldet sich nicht", SMTPCheck(fakeSMTP(t, "554 nein\r\n"), on)(ctx).Detail)
	assert.Equal(t, "Mailserver meldet sich nicht", SMTPCheck(fakeSMTP(t, ""), on)(ctx).Detail)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	assert.Equal(t, "Mailserver nicht erreichbar", SMTPCheck(addr, on)(ctx).Detail)

	res := SMTPCheck(addr, func() bool { return false })(ctx)
	assert.True(t, res.Off, "Versand abgeschaltet: Störung wird freigegeben, kein Ausfall")
}

func writeProm(t *testing.T, dir, job string, success, run, exit int64) {
	t.Helper()
	body := "job_last_run_timestamp_seconds{job_name=\"" + job + "\"} " + strconv.FormatInt(run, 10) + "\n" +
		"job_last_success_timestamp_seconds{job_name=\"" + job + "\"} " + strconv.FormatInt(success, 10) + "\n" +
		"job_last_exit_code{job_name=\"" + job + "\"} " + strconv.FormatInt(exit, 10) + "\n" +
		"job_last_duration_seconds{job_name=\"" + job + "\"} 4\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "job_"+job+".prom"), []byte(body), 0o600))
}

func TestBackupCheck(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	maxAge := 36 * time.Hour
	hoursAgo := func(h int) int64 { return now.Add(-time.Duration(h) * time.Hour).Unix() }
	dir := t.TempDir()
	check := func() Result { return BackupCheck(dir, "backup-prod", maxAge, nowFn)(context.Background()) }

	assert.True(t, check().Skip, "Datei fehlt (z. B. Staging ohne Timer): nicht prüfbar")

	// Lesefehler (hier: Pfad ist ein Verzeichnis) sind kein Skip
	require.NoError(t, os.Mkdir(filepath.Join(dir, "job_backup-prod.prom"), 0o750))
	res0 := check()
	assert.False(t, res0.Skip)
	assert.False(t, res0.OK)
	assert.Equal(t, "Backup-Metrik ist keine reguläre Datei", res0.Detail)
	require.NoError(t, os.Remove(filepath.Join(dir, "job_backup-prod.prom")))

	writeProm(t, dir, "backup-prod", hoursAgo(9), hoursAgo(9), 0)
	assert.True(t, check().OK)

	writeProm(t, dir, "backup-prod", hoursAgo(40), hoursAgo(2), 1)
	res := check()
	assert.False(t, res.OK)
	assert.Equal(t, "letztes Backup vor 40 Stunden", res.Detail)

	writeProm(t, dir, "backup-prod", 0, hoursAgo(2), 1)
	assert.Equal(t, "noch kein erfolgreiches Backup", check().Detail)

	// Nur der Offsite-Upload scheiterte (Exit 3): lokales Backup liegt vor
	writeProm(t, dir, "backup-prod", hoursAgo(60), hoursAgo(9), BackupExitOffsiteFailed)
	assert.True(t, check().OK)
	writeProm(t, dir, "backup-prod", hoursAgo(60), hoursAgo(50), BackupExitOffsiteFailed)
	assert.False(t, check().OK, "auch Exit 3 ist irgendwann zu alt")
}

func TestParseJobMetrics_IgnoriertMuell(t *testing.T) {
	m := parseJobMetrics("kein_label 5\nname{x=\"y\"}ohneleerzeichen\nname{x=\"y\"} abc\nok{x=\"y\"} 7\n")
	assert.Equal(t, map[string]float64{"ok": 7}, m)
}

func TestBackupCheck_GrenzeGenau36Stunden(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	check := BackupCheck(dir, "backup-prod", 36*time.Hour, func() time.Time { return now })
	writeProm(t, dir, "backup-prod", now.Add(-36*time.Hour).Unix(), now.Unix(), 1)
	assert.True(t, check(context.Background()).OK, "genau 36 h ist noch frisch")
	writeProm(t, dir, "backup-prod", now.Add(-36*time.Hour-time.Second).Unix(), now.Unix(), 1)
	assert.False(t, check(context.Background()).OK, "eine Sekunde mehr ist veraltet")
}

func TestHTTPCheck_StatusgrenzenUndZeitueberschreitung(t *testing.T) {
	for code, want := range map[int]bool{200: true, 204: true, 299: true, 300: false, 404: false, 500: false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
		assert.Equal(t, want, HTTPCheck(srv.Client(), srv.URL)(context.Background()).OK, "Status %d", code)
		srv.Close()
	}
}

// Die vom Betreiber festgelegten Werte.
func TestStandardwerte(t *testing.T) {
	assert.Equal(t, 3, DefaultThreshold)
	assert.Equal(t, 15*time.Minute, DefaultIncidentAfter)
	assert.Equal(t, time.Minute, DefaultInterval)
	assert.Equal(t, 3, BackupExitOffsiteFailed)
	m := New(nil, nil, nil)
	assert.Equal(t, 3, m.Threshold)
	assert.Equal(t, 15*time.Minute, m.IncidentAfter)
}

func frontendServer(t *testing.T, index func(http.ResponseWriter), asset int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/assets/app.js" {
			w.WriteHeader(asset)
			return
		}
		index(w)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFrontendCheck(t *testing.T) {
	const page = `<html><div id="app"></div><script type="module" src="/assets/app.js?v=1"></script></html>`
	write := func(s string) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) { _, _ = w.Write([]byte(s)) }
	}
	run := func(srv *httptest.Server) Result { return FrontendCheck(srv.Client(), srv.URL)(context.Background()) }

	assert.True(t, run(frontendServer(t, write(page), 200)).OK)
	assert.True(t, run(frontendServer(t, write(`<div id="app"></div>`), 500)).OK, "ohne Asset-Verweis nur die Startseite")

	res := run(frontendServer(t, write(page), 404))
	assert.False(t, res.OK)
	assert.Equal(t, "Frontend-Datei: HTTP-Status 404", res.Detail)

	res = run(frontendServer(t, write("<html>Maintenance</html>"), 200))
	assert.Equal(t, "Startseite ist kein Frontend-HTML", res.Detail)

	// fehlende Asset-Datei -> Router liefert index.html mit 200
	htmlAsset := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(htmlAsset.Close)
	res = run(htmlAsset)
	assert.False(t, res.OK)
	assert.Equal(t, "Frontend-Datei fehlt (HTML statt Datei)", res.Detail)

	// 401/403 bleiben Fehler (nicht aufgeweicht)
	for _, code := range []int{401, 403, 503} {
		res = run(frontendServer(t, func(w http.ResponseWriter) { w.WriteHeader(code) }, 200))
		assert.False(t, res.OK)
		assert.Equal(t, "HTTP-Status "+strconv.Itoa(code), res.Detail)
	}

	assert.Equal(t, "ungültige Adresse", FrontendCheck(http.DefaultClient, "http://[::1")(context.Background()).Detail)
	srv := frontendServer(t, write(page), 200)
	srv.Close()
	assert.Equal(t, "nicht erreichbar", run(srv).Detail)
}
