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

// BackupExitOffsiteFailed ist der Exit-Code des Backup-Skripts der Installation, wenn
// nur der optionale Offsite-Upload scheitert und das lokale Backup bleibt.
const BackupExitOffsiteFailed = 3

// DBCheck prüft die Datenbank mit einer echten Abfrage (nicht nur Ping).
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

// HTTPCheck ruft die Adresse ab; jeder 2xx-Status gilt als gesund.
func HTTPCheck(client *http.Client, url string) func(context.Context) Result {
	return func(ctx context.Context) Result {
		ctx, cancel := context.WithTimeout(ctx, 2*checkTimeout)
		defer cancel()
		_, _, res := fetchOK(ctx, client, url, 0)
		return res
	}
}

// maxFrontendBody begrenzt, wie viel der Startseite gelesen wird.
const maxFrontendBody = 1 << 20

// frontendEntry erkennt den Einstieg der SPA im ausgelieferten HTML.
const frontendEntry = `<div id="app"`

// assetRef findet die erste vom HTML referenzierte Datei unter /assets/.
var assetRef = regexp.MustCompile(`(?:src|href)="(/assets/[^"?#]+)`)

// FrontendCheck prüft das von der App selbst ausgelieferte Frontend. base ist
// die interne Adresse der eigenen Anwendung (http://127.0.0.1:<Port>), nie die
// öffentliche: Cloudflare/Caddy (Bot-Schutz, Zugangssperre) antworten Go-Clients
// mit 401/403 und würden die Komponente dauerhaft auf Störung setzen. Erwartet
// werden 2xx und HTML mit dem Einstieg der SPA; verweist das HTML auf eine Datei
// unter /assets/, muss auch sie mit 2xx antworten. 401/403 zählen weiterhin als
// Fehler. Cloudflare und Caddy davor erfasst die Prüfung bewusst nicht
// (Überwachung von außen bleibt nötig).
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
			// Fehlt die Datei, fällt der Router auf index.html (200,
			// text/html) zurück. Eine Skript-/CSS-Datei darf nie HTML sein.
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(ctype)), "text/html") {
				return Result{Detail: "Frontend-Datei fehlt (HTML statt Datei)"}
			}
		}
		return Result{OK: true}
	}
}

// fetchOK ruft url ab und liefert bei 2xx bis zu limit Byte des Bodys (0 = nichts
// lesen) sowie den Content-Type der Antwort.
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

// SMTPCheck baut eine Verbindung zum Mailserver auf und erwartet den 220-Gruß.
// Es wird nichts versendet und nicht angemeldet. enabled == false (Versand
// abgeschaltet) ergibt "abgeschaltet" (Result.Off): eine zuvor gesetzte Störung
// wird dann freigegeben.
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

// ValidJobName: Der job-run-Name geht in den Dateinamen der Metrik; Pfadanteile
// ("../", "/") sind ausgeschlossen.
func ValidJobName(job string) bool { return jobNamePattern.MatchString(job) }

// maxMetricsFile begrenzt die Metrikdatei; echte Dateien sind
// wenige Zeilen groß.
const maxMetricsFile = 64 << 10

// openMetrics öffnet die Metrikdatei; Funktionsvariable als Injektionsnaht für
// den Test des Fensters zwischen Lstat und Open.
var openMetrics = func(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // Pfad aus der Betreiber-Konfiguration
}

// readMetricsFile liest die Metrikdatei gehärtet: nur reguläre
// Datei (kein Symlink, FIFO, Gerät), nicht blockierend geöffnet, höchstens
// maxMetricsFile Byte. Fehler liefern einen festen Text ohne Pfad.
func readMetricsFile(dir, path string) (raw []byte, res Result, ok bool) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		// Betreiber-Entscheidung: Fehlt das Verzeichnis selbst (Mount
		// fehlt) bei gesetztem Job, ist das ein Fehlschlag; fehlt nur die Datei im
		// vorhandenen Verzeichnis (z. B. Staging ohne Timer), ist es nicht prüfbar.
		if _, derr := os.Stat(dir); os.IsNotExist(derr) {
			return nil, Result{Detail: "Backup-Metrik-Verzeichnis fehlt"}, false
		}
		return nil, Result{Skip: true}, false
	}
	if err != nil {
		// Berechtigung/Mount-Fehler bei gesetztem Job ist kein
		// "nicht prüfbar", sonst wäre die Backup-Überwachung still blind.
		return nil, Result{Detail: "Backup-Metrik nicht lesbar"}, false
	}
	if !info.Mode().IsRegular() {
		return nil, Result{Detail: "Backup-Metrik ist keine reguläre Datei"}, false
	}
	// O_NOFOLLOW verhindert einen untergeschobenen Symlink; O_NONBLOCK lässt das
	// Öffnen eines FIFO nicht hängen. Zwischen Lstat und Open kann die Datei aber
	// ausgetauscht werden: Erst der fstat auf den OFFENEN Deskriptor schließt das
	// Fenster (nur reguläre Dateien). Go meldet O_NONBLOCK-Dateien beim Netpoller
	// an, ein Read auf einen FIFO mit stillem Schreiber würde also warten; der
	// fstat kommt deshalb vor dem ersten Read, die Frist ist zusätzlicher Schutz
	// (der Monitor hält währenddessen m.mu).
	f, err := openMetrics(path)
	if err != nil {
		return nil, Result{Detail: "Backup-Metrik nicht lesbar"}, false
	}
	defer f.Close() //nolint:errcheck
	if st, serr := f.Stat(); serr != nil || !st.Mode().IsRegular() {
		return nil, Result{Detail: "Backup-Metrik ist keine reguläre Datei"}, false
	}
	_ = f.SetReadDeadline(time.Now().Add(metricsReadTimeout)) // reguläre Dateien kennen keine Frist: Fehler ist egal
	return readLimited(f)
}

// metricsReadTimeout begrenzt das Lesen der Metrikdatei, soweit der Deskriptor Fristen kennt.
const metricsReadTimeout = 2 * time.Second

// readLimited liest höchstens maxMetricsFile Byte; mehr ist ein Fehlschlag.
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

// BackupCheck liest die job-run-Metrik backup-<env> (Prometheus-Textfile, vom
// Backup-Job der Installation geschrieben) und prüft das Alter des letzten Backups. Fehlt
// bei gesetztem Job das Metrik-Verzeichnis (Mount fehlt), ist das ein
// Fehlschlag; fehlt nur die Datei im vorhandenen Verzeichnis
// (z. B. Staging ohne Timer), ist das nicht prüfbar; jeder andere Lesefehler
// ist ein Fehlschlag. Ein Lauf, der nur am Offsite-Upload scheiterte
// (Exit-Code 3), zählt als frisch: das lokale Backup liegt vor.
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

// parseJobMetrics liest Zeilen der Form `name{job_name="x"} 123`.
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
