package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/selfmonitor"
)

// Umgebung der Selbstüberwachung.
const (
	// selfMonitorBackupJobEnv: job-run-Name der Backup-Metrik (z. B. "backup-prod").
	// Leer = Backup-Prüfung aus.
	selfMonitorBackupJobEnv = "SELFMONITOR_BACKUP_JOB"
	// selfMonitorMetricsDirEnv: Ordner mit den job-run-Textfiles (im Container
	// read-only eingehängt, siehe docker-compose.app.yml).
	selfMonitorMetricsDirEnv     = "SELFMONITOR_METRICS_DIR"
	defaultSelfMonitorMetricsDir = "/job-metrics"
	// Das nächtliche Backup läuft täglich 03:00 UTC; 36 h lassen einen
	// verpassten Lauf nicht sofort alarmieren, einen zweiten schon.
	selfMonitorBackupMaxAge = 36 * time.Hour
	// SMTP-Verbindungstest nur alle 5 Takte (Minuten), um den Anbieter zu schonen.
	selfMonitorMailEvery = 5
)

// SMTPAddress liefert das Dial-Target des Mailservers (wie der Versand).
func SMTPAddress() string { return loadSMTPConfig().addr() }

// selfMonitorChecks baut die Prüfungen aus der Umgebung. API (Loopback auf dem
// eigenen Port) und Datenbank laufen immer; Frontend nur mit FRONTEND_URL
// (Zeichen einer Produktionsumgebung mit ausgeliefertem Frontend), geprüft wird
// aber die eigene Auslieferung auf dem Loopback, nie die öffentliche Adresse
// (Cloudflare/Caddy antworten Go-Clients mit 401/403). Backup nur mit
// SELFMONITOR_BACKUP_JOB.
func (h *Handler) selfMonitorChecks(getenv func(string) string) []selfmonitor.Check {
	client := &http.Client{Timeout: 15 * time.Second}
	port := getenv("PORT")
	if port == "" {
		port = "8080"
	}
	slug := database.CapabilitySlug
	checks := []selfmonitor.Check{
		{Slug: slug("plattform-api"), Name: "API", FailedStatus: componentstatus.MajorOutage,
			Run: selfmonitor.HTTPCheck(client, "http://127.0.0.1:"+port+"/healthz")},
		{Slug: slug("plattform-datenbank"), Name: "Datenbank", FailedStatus: componentstatus.MajorOutage,
			Run: selfmonitor.DBCheck(h.DB)},
		{Slug: slug("plattform-email"), Name: "E-Mail-Versand", FailedStatus: componentstatus.PartialOutage,
			Every: selfMonitorMailEvery,
			Run:   selfmonitor.SMTPCheck(SMTPAddress(), func() bool { return h.getSystemSettingBool(settingEmailEnabled, true) })},
	}
	if strings.TrimSpace(getenv("FRONTEND_URL")) != "" {
		checks = append(checks, selfmonitor.Check{Slug: slug("plattform-frontend"), Name: "Frontend",
			FailedStatus: componentstatus.MajorOutage, Run: selfmonitor.FrontendCheck(client, "http://127.0.0.1:"+port)})
	}
	if job := strings.TrimSpace(getenv(selfMonitorBackupJobEnv)); job != "" && !selfmonitor.ValidJobName(job) {
		// Review Der Name geht in den Dateinamen; ungültig = keine Prüfung.
		// Fester Text, der Wert selbst wird nicht geloggt.
		slog.Error("Selbstüberwachung: SELFMONITOR_BACKUP_JOB ungültig (erlaubt: a-z, 0-9, -), Backup-Prüfung nicht registriert")
	} else if job != "" {
		dir := getenv(selfMonitorMetricsDirEnv)
		if dir == "" {
			dir = defaultSelfMonitorMetricsDir
		}
		checks = append(checks, selfmonitor.Check{Slug: slug("plattform-backup"), Name: "Datenbank-Backup",
			FailedStatus: componentstatus.DegradedPerformance, Every: selfMonitorMailEvery,
			Run: selfmonitor.BackupCheck(dir, job, selfMonitorBackupMaxAge, time.Now)})
	}
	return checks
}

// NewSelfMonitor baut den Monitor (Schalter: Systemeinstellung selfmonitor.enabled).
func (h *Handler) NewSelfMonitor(getenv func(string) string) *selfmonitor.Monitor {
	return selfmonitor.New(h.DB, h.selfMonitorChecks(getenv), func() (bool, error) {
		return h.readSystemSettingBool(settingSelfMonitorEnabled, selfMonitorEnabledDefault)
	})
}

// StartSelfMonitorFromEnv startet die Selbstüberwachung; Aufruf in main.go:
// defer h.StartSelfMonitorFromEnv(ctx, os.Getenv)().
func (h *Handler) StartSelfMonitorFromEnv(ctx context.Context, getenv func(string) string) (stop func()) {
	// Altfälle bereinigen, unabhängig vom Schalter (die Texte sind
	// öffentlich). Idempotent, bei jedem Start; ein Fehler (alles zurückgerollt)
	// verhindert den Start nicht, der nächste Start wiederholt ihn.
	if err := selfmonitor.NeutralizeHiddenIncidents(h.DB); err != nil {
		slog.Error("Selbstüberwachung: Vorfalltexte nicht bereinigt", "error", err)
	}
	slog.Info("Selbstüberwachung der Systemseite gestartet")
	return h.NewSelfMonitor(getenv).Start(ctx)
}
