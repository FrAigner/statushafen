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

// Environment of the self-monitoring.
const (
	// selfMonitorBackupJobEnv: job-run name of the backup metric (e.g. "backup-prod").
	// Empty = backup check off.
	selfMonitorBackupJobEnv = "SELFMONITOR_BACKUP_JOB"
	// selfMonitorMetricsDirEnv: folder with the job-run text files (mounted
	// read-only in the container, see docker-compose.app.yml).
	selfMonitorMetricsDirEnv     = "SELFMONITOR_METRICS_DIR"
	defaultSelfMonitorMetricsDir = "/job-metrics"
	// The nightly backup runs daily at 03:00 UTC; 36 h do not raise an alarm
	// immediately for one missed run, but do for a second one.
	selfMonitorBackupMaxAge = 36 * time.Hour
	// SMTP connection test only every 5 ticks (minutes), to go easy on the provider.
	selfMonitorMailEvery = 5
)

// SMTPAddress returns the dial target of the mail server (as used for sending).
func SMTPAddress() string { return loadSMTPConfig().addr() }

// selfMonitorChecks builds the checks from the environment. API (loopback on its
// own port) and database always run; frontend only with FRONTEND_URL
// (sign of a production environment with a deployed frontend), but what is checked
// is the own delivery on the loopback, never the public address
// (Cloudflare/Caddy answer Go clients with 401/403). Backup only with
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
		// Review: The name goes into the file name; invalid = no check.
		// Fixed text, the value itself is not logged.
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

// NewSelfMonitor builds the monitor (switch: system setting selfmonitor.enabled).
func (h *Handler) NewSelfMonitor(getenv func(string) string) *selfmonitor.Monitor {
	return selfmonitor.New(h.DB, h.selfMonitorChecks(getenv), func() (bool, error) {
		return h.readSystemSettingBool(settingSelfMonitorEnabled, selfMonitorEnabledDefault)
	})
}

// StartSelfMonitorFromEnv starts the self-monitoring; called in main.go:
// defer h.StartSelfMonitorFromEnv(ctx, os.Getenv)().
func (h *Handler) StartSelfMonitorFromEnv(ctx context.Context, getenv func(string) string) (stop func()) {
	// Clean up legacy cases, independent of the switch (the texts are
	// public). Idempotent, on every start; an error (everything rolled back)
	// does not prevent startup, the next start repeats it.
	if err := selfmonitor.NeutralizeHiddenIncidents(h.DB); err != nil {
		slog.Error("Selbstüberwachung: Vorfalltexte nicht bereinigt", "error", err)
	}
	slog.Info("Selbstüberwachung der Systemseite gestartet")
	return h.NewSelfMonitor(getenv).Start(ctx)
}
