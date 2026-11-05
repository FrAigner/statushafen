package handlers

import (
	"os"
	"testing"
	"time"
)

// Time-Limits (SSO Code Expiry, SSO Cleanup Interval,
// Maintenance-Scheduler-Interval) waren hardcoded. envDurationOrDefault und
// die darauf aufbauenden Wrapper machen sie per Env-Var konfigurierbar, ohne
// das bisherige Default-Verhalten zu ändern.

func TestEnvDurationOrDefault(t *testing.T) {
	const key = "TEST_ENV_DURATION_OR_DEFAULT"

	t.Run("unset falls back to default", func(t *testing.T) {
		_ = os.Unsetenv(key)
		got := envDurationOrDefault(key, 42*time.Second, time.Second)
		if got != 42*time.Second {
			t.Fatalf("expected default 42s, got %v", got)
		}
	})

	t.Run("valid override is applied in the given unit", func(t *testing.T) {
		_ = os.Setenv(key, "3")
		defer func() { _ = os.Unsetenv(key) }()
		got := envDurationOrDefault(key, 42*time.Second, time.Minute)
		if got != 3*time.Minute {
			t.Fatalf("expected 3m override, got %v", got)
		}
	})

	t.Run("non-numeric value falls back to default", func(t *testing.T) {
		_ = os.Setenv(key, "not-a-number")
		defer func() { _ = os.Unsetenv(key) }()
		got := envDurationOrDefault(key, 42*time.Second, time.Second)
		if got != 42*time.Second {
			t.Fatalf("expected default 42s for non-numeric input, got %v", got)
		}
	})

	t.Run("zero or negative value falls back to default", func(t *testing.T) {
		_ = os.Setenv(key, "0")
		defer func() { _ = os.Unsetenv(key) }()
		got := envDurationOrDefault(key, 42*time.Second, time.Second)
		if got != 42*time.Second {
			t.Fatalf("expected default 42s for zero input, got %v", got)
		}
	})
}

func TestSSOCodeExpiryConfigurable(t *testing.T) {
	t.Run("default is 60 seconds", func(t *testing.T) {
		_ = os.Unsetenv("SSO_CODE_EXPIRY_SECONDS")
		if got := ssoCodeExpiry(); got != 60*time.Second {
			t.Fatalf("expected default 60s, got %v", got)
		}
	})

	t.Run("override via SSO_CODE_EXPIRY_SECONDS", func(t *testing.T) {
		_ = os.Setenv("SSO_CODE_EXPIRY_SECONDS", "120")
		defer func() { _ = os.Unsetenv("SSO_CODE_EXPIRY_SECONDS") }()
		if got := ssoCodeExpiry(); got != 120*time.Second {
			t.Fatalf("expected 120s override, got %v", got)
		}
	})
}

func TestSSOCleanupIntervalConfigurable(t *testing.T) {
	t.Run("default is 5 minutes", func(t *testing.T) {
		_ = os.Unsetenv("SSO_CLEANUP_INTERVAL_MINUTES")
		if got := ssoCleanupInterval(); got != 5*time.Minute {
			t.Fatalf("expected default 5m, got %v", got)
		}
	})

	t.Run("override via SSO_CLEANUP_INTERVAL_MINUTES", func(t *testing.T) {
		_ = os.Setenv("SSO_CLEANUP_INTERVAL_MINUTES", "10")
		defer func() { _ = os.Unsetenv("SSO_CLEANUP_INTERVAL_MINUTES") }()
		if got := ssoCleanupInterval(); got != 10*time.Minute {
			t.Fatalf("expected 10m override, got %v", got)
		}
	})
}

func TestMaintenanceSchedulerIntervalConfigurable(t *testing.T) {
	t.Run("default is 1 minute", func(t *testing.T) {
		_ = os.Unsetenv("MAINTENANCE_SCHEDULER_INTERVAL_MINUTES")
		if got := maintenanceSchedulerInterval(); got != 1*time.Minute {
			t.Fatalf("expected default 1m, got %v", got)
		}
	})

	t.Run("override via MAINTENANCE_SCHEDULER_INTERVAL_MINUTES", func(t *testing.T) {
		_ = os.Setenv("MAINTENANCE_SCHEDULER_INTERVAL_MINUTES", "2")
		defer func() { _ = os.Unsetenv("MAINTENANCE_SCHEDULER_INTERVAL_MINUTES") }()
		if got := maintenanceSchedulerInterval(); got != 2*time.Minute {
			t.Fatalf("expected 2m override, got %v", got)
		}
	})
}

// TestStoreSSOCodeUsesConfigurableExpiry deckt die Verdrahtung von
// storeSSOCode mit ssoCodeExpiry() ab, nicht nur die Helper-Funktion isoliert.
func TestStoreSSOCodeUsesConfigurableExpiry(t *testing.T) {
	_ = os.Setenv("SSO_CODE_EXPIRY_SECONDS", "1")
	defer func() { _ = os.Unsetenv("SSO_CODE_EXPIRY_SECONDS") }()

	code := storeSSOCode("short-lived-token")
	t.Cleanup(func() {
		ssoCodeMu.Lock()
		delete(ssoCodeStore, code)
		ssoCodeMu.Unlock()
	})

	if _, ok := consumeSSOCode(code); !ok {
		t.Fatal("expected code to still be valid immediately after storing")
	}

	// Code erneut speichern (consumeSSOCode hat ihn bereits gelöscht), dann
	// über die konfigurierte Expiry hinaus warten.
	code = storeSSOCode("short-lived-token-2")
	time.Sleep(1100 * time.Millisecond)
	if _, ok := consumeSSOCode(code); ok {
		t.Fatal("expected code to be expired after the configured 1s expiry")
	}
}
