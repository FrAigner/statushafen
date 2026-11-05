package handlers

import (
	"os"
	"strconv"
	"time"
)

// envDurationOrDefault liest key aus der Umgebung, interpretiert den Wert als
// Ganzzahl in unit (z.B. time.Second, time.Minute) und fällt bei fehlendem
// oder ungültigem Wert auf def zurück (vorher hardcodierte
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

// ssoCodeExpiry: Lebensdauer eines Einmal-SSO-Codes (storeSSOCode),
// konfigurierbar über SSO_CODE_EXPIRY_SECONDS, Default 60s.
func ssoCodeExpiry() time.Duration {
	return envDurationOrDefault("SSO_CODE_EXPIRY_SECONDS", 60*time.Second, time.Second)
}

// ssoCleanupInterval: Takt des Aufräum-Loops für abgelaufene SSO-Codes
// (cleanupSSOCodeStore), konfigurierbar über SSO_CLEANUP_INTERVAL_MINUTES,
// Default 5 Minuten.
func ssoCleanupInterval() time.Duration {
	return envDurationOrDefault("SSO_CLEANUP_INTERVAL_MINUTES", 5*time.Minute, time.Minute)
}

// maintenanceSchedulerInterval: Takt des Wartungsfenster-Schedulers
// (StartMaintenanceScheduler), konfigurierbar über
// MAINTENANCE_SCHEDULER_INTERVAL_MINUTES, Default 1 Minute.
func maintenanceSchedulerInterval() time.Duration {
	return envDurationOrDefault("MAINTENANCE_SCHEDULER_INTERVAL_MINUTES", 1*time.Minute, time.Minute)
}
