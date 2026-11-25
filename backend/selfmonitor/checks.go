package selfmonitor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gorm.io/gorm"
)

const checkTimeout = 5 * time.Second

// BackupExitOffsiteFailed is the exit code of the installation's backup script when
// only the optional offsite upload fails and the local backup remains.
const BackupExitOffsiteFailed = 3

// DBCheck checks the database with a real query (not just a ping).
func DBCheck(db *gorm.DB) func(context.Context) Result {
	return func(ctx context.Context) Result {
		ctx, cancel := context.WithTimeout(ctx, checkTimeout)
		defer cancel()
		var one int
		if err := db.WithContext(ctx).Raw("SELECT 1").Scan(&one).Error; err != nil {
			return Result{Detail: "Datenbankabfrage fehlgeschlagen"}
		}
		return Result{OK: true}
	}
}

// HTTPCheck fetches the address; any 2xx status counts as healthy.
func HTTPCheck(client *http.Client, url string) func(context.Context) Result {
	return func(ctx context.Context) Result {
		ctx, cancel := context.WithTimeout(ctx, 2*checkTimeout)
		defer cancel()
		_, _, res := fetchOK(ctx, client, url, 0)
		return res
	}
}

// maxFrontendBody limits how much of the start page is read.
const maxFrontendBody = 1 << 20

// frontendEntry detects the SPA entry point in the served HTML.
const frontendEntry = `<div id="app"`

// assetRef finds the first file under /assets/ referenced by the HTML.
var assetRef = regexp.MustCompile(`(?:src|href)="(/assets/[^"?#]+)`)

// FrontendCheck checks the frontend served by the app itself. base is
// the internal address of the own application (http://127.0.0.1:<port>), never the
// public one: Cloudflare/Caddy (bot protection, access barrier) answer Go clients
// with 401/403 and would permanently set the component to outage. Expected
// are 2xx and HTML with the SPA entry point; if the HTML references a file
// under /assets/, it must also answer with 2xx. 401/403 still count as an
// error. The check deliberately does not cover Cloudflare and Caddy in front
// (monitoring from outside remains necessary).
func FrontendCheck(client *http.Client, base string) func(context.Context) Result {
	return func(ctx context.Context) Result {
		ctx, cancel := context.WithTimeout(ctx, 2*checkTimeout)
		defer cancel()
		body, _, res := fetchOK(ctx, client, base+"/", maxFrontendBody)
		if !res.OK {
			return res
		}
		if !strings.Contains(string(body), frontendEntry) {
			return Result{Detail: "Startseite ist kein Frontend-HTML"}
		}
		if m := assetRef.FindSubmatch(body); m != nil {
			_, ctype, res := fetchOK(ctx, client, base+string(m[1]), 0)
			if !res.OK {
				return Result{Detail: "Frontend-Datei: " + res.Detail}
			}
			// If the file is missing, the router falls back to index.html (200,
			// text/html). A script/CSS file must never be HTML.
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(ctype)), "text/html") {
				return Result{Detail: "Frontend-Datei fehlt (HTML statt Datei)"}
			}
		}
		return Result{OK: true}
	}
}

// fetchOK fetches url and, on 2xx, returns up to limit bytes of the body (0 = read
// nothing) as well as the response's Content-Type.
func fetchOK(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, string, Result) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", Result{Detail: "ungültige Adresse"}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", Result{Detail: "nicht erreichbar"}
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", Result{Detail: "HTTP-Status " + strconv.Itoa(resp.StatusCode)}
	}
	var body []byte
	if limit > 0 {
		body, _ = io.ReadAll(io.LimitReader(resp.Body, limit))
	}
	return body, resp.Header.Get("Content-Type"), Result{OK: true}
}

// SMTPCheck opens a connection to the mail server and expects the 220 greeting.
// Nothing is sent and no login is made. enabled == false (sending
// switched off) yields "switched off" (Result.Off): a previously set outage
// is then released.
func SMTPCheck(addr string, enabled func() bool) func(context.Context) Result {
	return func(ctx context.Context) Result {
		if !enabled() {
			return Result{Off: true}
		}
		d := net.Dialer{Timeout: checkTimeout}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return Result{Detail: "Mailserver nicht erreichbar"}
		}
		defer conn.Close() //nolint:errcheck
		_ = conn.SetReadDeadline(time.Now().Add(checkTimeout))
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "220") {
			return Result{Detail: "Mailserver meldet sich nicht"}
		}
		return Result{OK: true}
	}
}

var jobNamePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// ValidJobName: the job-run name goes into the metric's file name; path parts
// ("../", "/") are excluded.
func ValidJobName(job string) bool { return jobNamePattern.MatchString(job) }

// maxMetricsFile limits the metrics file; real files are
// a few lines in size.
const maxMetricsFile = 64 << 10

// openMetrics opens the metrics file; function variable as an injection seam for
// testing the window between Lstat and Open.
var openMetrics = func(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // Path comes from the operator configuration
}

// readMetricsFile reads the metrics file hardened: only a regular
// file (no symlink, FIFO, device), opened non-blocking, at most
// maxMetricsFile bytes. Errors return a fixed text without a path.
func readMetricsFile(dir, path string) (raw []byte, res Result, ok bool) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		// Operator decision: if the directory itself is missing (mount
		// missing) with a job set, that is a failure; if only the file is missing in an
		// existing directory (e.g. staging without a timer), it is not checkable.
		if _, derr := os.Stat(dir); os.IsNotExist(derr) {
			return nil, Result{Detail: "Backup-Metrik-Verzeichnis fehlt"}, false
		}
		return nil, Result{Skip: true}, false
	}
	if err != nil {
		// A permission/mount error with a job set is not
		// "not checkable", otherwise the backup monitoring would be silently blind.
		return nil, Result{Detail: "Backup-Metrik nicht lesbar"}, false
	}
	if !info.Mode().IsRegular() {
		return nil, Result{Detail: "Backup-Metrik ist keine reguläre Datei"}, false
	}
	// O_NOFOLLOW prevents a planted symlink; O_NONBLOCK keeps opening a
	// FIFO from hanging. Between Lstat and Open, however, the file can be
	// swapped: only the fstat on the OPEN descriptor closes the
	// window (regular files only). Go registers O_NONBLOCK files with the netpoller,
	// so a read on a FIFO with a silent writer would wait; the
	// fstat therefore comes before the first read, the deadline is additional protection
	// (the monitor holds m.mu meanwhile).
	f, err := openMetrics(path)
	if err != nil {
		return nil, Result{Detail: "Backup-Metrik nicht lesbar"}, false
	}
	defer f.Close() //nolint:errcheck
	if st, serr := f.Stat(); serr != nil || !st.Mode().IsRegular() {
		return nil, Result{Detail: "Backup-Metrik ist keine reguläre Datei"}, false
	}
	_ = f.SetReadDeadline(time.Now().Add(metricsReadTimeout)) // regular files know no deadline: error is irrelevant
	return readLimited(f)
}

// metricsReadTimeout limits reading the metrics file, as far as the descriptor supports deadlines.
const metricsReadTimeout = 2 * time.Second

// readLimited reads at most maxMetricsFile bytes; more is a failure.
func readLimited(r io.Reader) (raw []byte, res Result, ok bool) {
	raw, err := io.ReadAll(io.LimitReader(r, maxMetricsFile+1))
	if err != nil {
		return nil, Result{Detail: "Backup-Metrik nicht lesbar"}, false
	}
	if len(raw) > maxMetricsFile {
		return nil, Result{Detail: "Backup-Metrik ist zu groß"}, false
	}
	return raw, Result{}, true
}

// BackupCheck reads the job-run metric backup-<env> (Prometheus textfile, written by the
// installation's backup job) and checks the age of the last backup. If, with
// a job set, the metrics directory is missing (mount missing), that is a
// failure; if only the file is missing in the existing directory
// (e.g. staging without a timer), it is not checkable; every other read error
// is a failure. A run that failed only at the offsite upload
// (exit code 3) counts as fresh: the local backup exists.
func BackupCheck(dir, job string, maxAge time.Duration, now func() time.Time) func(context.Context) Result {
	return func(context.Context) Result {
		raw, res, ok := readMetricsFile(dir, filepath.Join(dir, "job_"+job+".prom"))
		if !ok {
			return res
		}
		m := parseJobMetrics(string(raw))
		fresh := m["job_last_success_timestamp_seconds"]
		if m["job_last_exit_code"] == BackupExitOffsiteFailed {
			fresh = max(fresh, m["job_last_run_timestamp_seconds"])
		}
		if fresh <= 0 {
			return Result{Detail: "noch kein erfolgreiches Backup"}
		}
		age := now().Sub(time.Unix(int64(fresh), 0))
		if age > maxAge {
			return Result{Detail: fmt.Sprintf("letztes Backup vor %d Stunden", int(age.Hours()))}
		}
		return Result{OK: true}
	}
}

// parseJobMetrics reads lines of the form `name{job_name="x"} 123`.
func parseJobMetrics(text string) map[string]float64 {
	out := map[string]float64{}
	for _, line := range strings.Split(text, "\n") {
		name, rest, ok := strings.Cut(line, "{")
		if !ok {
			continue
		}
		_, val, ok := strings.Cut(rest, "} ")
		if !ok {
			continue
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
			out[name] = f
		}
	}
	return out
}
