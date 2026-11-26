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

// TestSweepExpiredSSOCodes: ssoCodeStore grew without bound,
// because consumeSSOCode only deleted entries on actual access -
// codes that were never redeemed (e.g. browser closed before the redirect) stayed
// in the map forever. sweepExpiredSSOCodes is the single pass extracted from the
// sleep loop that cleanupSSOCodeStore calls in
// production every 5 minutes.
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
	entry.expires = time.Now().Add(-time.Minute) // in the past, never consumed
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

// TestCleanupSSOCodeStoreStopsOnContextCancel: the
// cleanup goroutine previously ran forever in init() without cancellation -
// this test ensures that cleanupSSOCodeStore returns as soon as
// the passed context is cancelled, instead of running on until the next tick (5
// minutes) or forever.
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
		// expected: cleanupSSOCodeStore returns after cancellation
	case <-time.After(2 * time.Second):
		t.Fatal("expected cleanupSSOCodeStore to return promptly after context cancellation")
	}
}

// TestStartSSOCleanupWorker ensures that StartSSOCleanupWorker actually starts the
// goroutine and the returned CancelFunc stops it again -
// the main() wiring (defer StartSSOCleanupWorker(...)())
// depends on no goroutine being left over here without a shutdown path.
func TestStartSSOCleanupWorker(t *testing.T) {
	cancel := StartSSOCleanupWorker(context.Background())
	cancel()
	// No direct signal from StartSSOCleanupWorker (fire-and-forget,
	// as used in main.go) - a short wait is enough to ensure
	// that the call itself neither blocks nor panics.
	time.Sleep(50 * time.Millisecond)
}
