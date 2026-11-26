package handlers

import (
	"os"
	"strconv"
	"time"
)

// envDurationOrDefault reads key from the environment, interprets the value as an
// integer in unit (e.g. time.Second, time.Minute) and falls back to def if it is missing
// or invalid (previously hard-coded
// Time-Limits in auth_sso.go/maintenance_scheduler.go).
func envDurationOrDefault(key string, def time.Duration, unit time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return time.Duration(n) * unit
}

// ssoCodeExpiry: lifetime of a one-time SSO code (storeSSOCode),
// configurable via SSO_CODE_EXPIRY_SECONDS, default 60s.
func ssoCodeExpiry() time.Duration {
	return envDurationOrDefault("SSO_CODE_EXPIRY_SECONDS", 60*time.Second, time.Second)
}

// ssoCleanupInterval: tick of the cleanup loop for expired SSO codes
// (cleanupSSOCodeStore), configurable via SSO_CLEANUP_INTERVAL_MINUTES,
// default 5 minutes.
func ssoCleanupInterval() time.Duration {
	return envDurationOrDefault("SSO_CLEANUP_INTERVAL_MINUTES", 5*time.Minute, time.Minute)
}

// maintenanceSchedulerInterval: tick of the maintenance window scheduler
// (StartMaintenanceScheduler), configurable via
// MAINTENANCE_SCHEDULER_INTERVAL_MINUTES, default 1 minute.
func maintenanceSchedulerInterval() time.Duration {
	return envDurationOrDefault("MAINTENANCE_SCHEDULER_INTERVAL_MINUTES", 1*time.Minute, time.Minute)
}
