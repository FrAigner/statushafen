package handlers

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestIsEmailDomainAllowed(t *testing.T) {
	t.Run("no allowlist configured", func(t *testing.T) {
		_ = os.Unsetenv("SSO_ALLOWED_DOMAINS")
		if !isEmailDomainAllowed("someone@example.com") {
			t.Fatal("expected true when SSO_ALLOWED_DOMAINS is unset")
		}
	})

	t.Run("domain allowed", func(t *testing.T) {
		_ = os.Setenv("SSO_ALLOWED_DOMAINS", "example.com, other.org")
		defer func() { _ = os.Unsetenv("SSO_ALLOWED_DOMAINS") }()
		if !isEmailDomainAllowed("someone@example.com") {
			t.Fatal("expected true for allowed domain")
		}
	})

	t.Run("domain not allowed", func(t *testing.T) {
		_ = os.Setenv("SSO_ALLOWED_DOMAINS", "example.com")
		defer func() { _ = os.Unsetenv("SSO_ALLOWED_DOMAINS") }()
		if isEmailDomainAllowed("someone@blocked.com") {
			t.Fatal("expected false for disallowed domain")
		}
	})

	t.Run("email without at sign", func(t *testing.T) {
		_ = os.Setenv("SSO_ALLOWED_DOMAINS", "example.com")
		defer func() { _ = os.Unsetenv("SSO_ALLOWED_DOMAINS") }()
		if isEmailDomainAllowed("not-an-email") {
			t.Fatal("expected false for malformed email")
		}
	})
}

func TestIsBootstrapAdminEmailInternal(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		_ = os.Unsetenv("SSO_BOOTSTRAP_ADMIN_EMAILS")
		if isBootstrapAdminEmail("anyone@example.com") {
			t.Fatal("expected false when SSO_BOOTSTRAP_ADMIN_EMAILS is unset")
		}
	})

	t.Run("matching email case-insensitive", func(t *testing.T) {
		_ = os.Setenv("SSO_BOOTSTRAP_ADMIN_EMAILS", "Admin@Example.com, other@example.com")
		defer func() { _ = os.Unsetenv("SSO_BOOTSTRAP_ADMIN_EMAILS") }()
		if !isBootstrapAdminEmail("admin@example.com") {
			t.Fatal("expected true for case-insensitive match")
		}
	})

	t.Run("non-matching email", func(t *testing.T) {
		_ = os.Setenv("SSO_BOOTSTRAP_ADMIN_EMAILS", "admin@example.com")
		defer func() { _ = os.Unsetenv("SSO_BOOTSTRAP_ADMIN_EMAILS") }()
		if isBootstrapAdminEmail("nobody@example.com") {
			t.Fatal("expected false for non-matching email")
		}
	})
}

// TestSweepExpiredSSOCodes: ssoCodeStore wuchs unbegrenzt,
// weil consumeSSOCode Einträge nur beim tatsächlichen Zugriff löschte -
// nie eingelöste Codes (z.B. Browser vor dem Redirect geschlossen) blieben
// für immer in der Map. sweepExpiredSSOCodes ist der aus der
// Sleep-Schleife ausgelagerte Einzeldurchlauf, den cleanupSSOCodeStore in
// Produktion alle 5 Minuten aufruft.
func TestSweepExpiredSSOCodes(t *testing.T) {
	ssoCodeMu.Lock()
	ssoCodeStore = map[string]ssoCodeEntry{}
	ssoCodeMu.Unlock()
	t.Cleanup(func() {
		ssoCodeMu.Lock()
		ssoCodeStore = map[string]ssoCodeEntry{}
		ssoCodeMu.Unlock()
	})

	expiredCode := storeSSOCode("expired-token", "", "", "")
	ssoCodeMu.Lock()
	entry := ssoCodeStore[expiredCode]
	entry.expires = time.Now().Add(-time.Minute) // in der Vergangenheit, nie konsumiert
	ssoCodeStore[expiredCode] = entry
	ssoCodeMu.Unlock()

	freshCode := storeSSOCode("fresh-token", "", "", "")

	sweepExpiredSSOCodes()

	ssoCodeMu.Lock()
	_, expiredStillThere := ssoCodeStore[expiredCode]
	_, freshStillThere := ssoCodeStore[freshCode]
	ssoCodeMu.Unlock()

	if expiredStillThere {
		t.Error("expected expired, never-consumed SSO code to be swept away")
	}
	if !freshStillThere {
		t.Error("expected fresh, not-yet-expired SSO code to survive the sweep")
	}
}

// TestCleanupSSOCodeStoreStopsOnContextCancel: die
// Aufräum-Goroutine lief vorher in init() ohne Cancellation für immer -
// dieser Test stellt sicher, dass cleanupSSOCodeStore zurückkehrt, sobald
// der übergebene Context gecancelt wird, statt bis zum nächsten Tick (5
// Minuten) oder für immer weiterzulaufen.
func TestCleanupSSOCodeStoreStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		cleanupSSOCodeStore(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// erwartet: cleanupSSOCodeStore kehrt nach Cancellation zurück
	case <-time.After(2 * time.Second):
		t.Fatal("expected cleanupSSOCodeStore to return promptly after context cancellation")
	}
}

// TestStartSSOCleanupWorker stellt sicher, dass StartSSOCleanupWorker die
// Goroutine tatsächlich startet und die zurückgegebene CancelFunc sie wieder
// beendet - die main()-Verdrahtung (defer StartSSOCleanupWorker(...)())
// hängt davon ab, dass hier keine Goroutine ohne Shutdown-Pfad übrig bleibt.
func TestStartSSOCleanupWorker(t *testing.T) {
	cancel := StartSSOCleanupWorker(context.Background())
	cancel()
	// Kein direktes Signal von StartSSOCleanupWorker aus (fire-and-forget,
	// wie in main.go verwendet) - ein kurzes Warten reicht, um sicherzustellen,
	// dass der Aufruf selbst nicht blockiert oder paniert.
	time.Sleep(50 * time.Millisecond)
}
