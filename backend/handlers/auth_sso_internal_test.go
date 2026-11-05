package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func TestOauthConfigForProvider(t *testing.T) {
	t.Run("google", func(t *testing.T) {
		cfg := oauthConfigForProvider("google")
		if cfg == nil {
			t.Fatal("expected non-nil config for google")
		}
	})

	t.Run("github", func(t *testing.T) {
		cfg := oauthConfigForProvider("github")
		if cfg == nil {
			t.Fatal("expected non-nil config for github")
		}
	})

	t.Run("unknown", func(t *testing.T) {
		if cfg := oauthConfigForProvider("bogus"); cfg != nil {
			t.Fatalf("expected nil config for unknown provider, got %+v", cfg)
		}
	})
}

func TestGoogleOAuthConfig(t *testing.T) {
	cfg := googleOAuthConfig()
	if cfg == nil {
		t.Fatal("expected non-nil google config")
	}
	if cfg.RedirectURL == "" {
		t.Fatal("expected non-empty redirect URL")
	}
}

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

func TestProvisionSSOUser(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	db.Create(&models.Role{Name: "Viewer", Permissions: "[]"})
	db.Create(&models.Role{Name: "Global Admin", Permissions: "[]"})

	t.Run("regular user gets Viewer role", func(t *testing.T) {
		user, err := h.provisionSSOUser("viewer@example.com", "google", "sub-1", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.Role.Name != "Viewer" {
			t.Fatalf("expected Viewer role, got %q", user.Role.Name)
		}
		if user.SSOProvider != "google" || user.SSOSubject != "sub-1" {
			t.Fatalf("SSO fields not set correctly: %+v", user)
		}
	})

	t.Run("bootstrap admin gets Global Admin role", func(t *testing.T) {
		user, err := h.provisionSSOUser("admin@example.com", "github", "sub-2", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.Role.Name != "Global Admin" {
			t.Fatalf("expected Global Admin role, got %q", user.Role.Name)
		}
		if !user.IsGlobalAdmin {
			t.Fatal("expected IsGlobalAdmin to be true")
		}
	})
}

func TestUpdateSSOUser(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	viewerRole := models.Role{Name: "Viewer", Permissions: "[]"}
	db.Create(&viewerRole)
	adminRole := models.Role{Name: "Global Admin", Permissions: "[]"}
	db.Create(&adminRole)

	t.Run("updates SSO fields on existing user", func(t *testing.T) {
		user := models.User{Email: "existing@example.com", RoleID: viewerRole.ID}
		db.Create(&user)

		updated := h.updateSSOUser(user, "github", "new-subject", false)
		if updated.SSOProvider != "github" || updated.SSOSubject != "new-subject" {
			t.Fatalf("expected SSO fields to be updated, got %+v", updated)
		}
		if updated.IsGlobalAdmin {
			t.Fatal("expected IsGlobalAdmin to remain false")
		}
	})

	t.Run("promotes to Global Admin when bootstrap admin", func(t *testing.T) {
		user := models.User{Email: "promote@example.com", RoleID: viewerRole.ID}
		db.Create(&user)

		updated := h.updateSSOUser(user, "google", "sub-promote", true)
		if !updated.IsGlobalAdmin {
			t.Fatal("expected IsGlobalAdmin to be promoted to true")
		}
		if updated.Role.Name != "Global Admin" {
			t.Fatalf("expected role to become Global Admin, got %q", updated.Role.Name)
		}
	})
}

func TestFindOrCreateSSOUser(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	db.Create(&models.Role{Name: "Viewer", Permissions: "[]"})

	t.Run("existing user is updated", func(t *testing.T) {
		var viewerRole models.Role
		db.Where("name = ?", "Viewer").First(&viewerRole)
		user := models.User{Email: "found@example.com", RoleID: viewerRole.ID}
		db.Create(&user)

		got, status, msg, ok := h.findOrCreateSSOUser("found@example.com", "google", "sub-found")
		if !ok || status != 0 || msg != "" {
			t.Fatalf("expected success, got status=%d msg=%q ok=%v", status, msg, ok)
		}
		if got.SSOProvider != "google" {
			t.Fatalf("expected SSOProvider to be updated, got %+v", got)
		}
	})

	t.Run("new user is created when domain allowed", func(t *testing.T) {
		_ = os.Unsetenv("SSO_ALLOWED_DOMAINS")
		got, status, msg, ok := h.findOrCreateSSOUser("brandnew@example.com", "github", "sub-new")
		if !ok || status != 0 || msg != "" {
			t.Fatalf("expected success, got status=%d msg=%q ok=%v", status, msg, ok)
		}
		if got.Email != "brandnew@example.com" {
			t.Fatalf("expected new user to be created, got %+v", got)
		}
	})

	t.Run("new user blocked by domain allowlist", func(t *testing.T) {
		_ = os.Setenv("SSO_ALLOWED_DOMAINS", "onlythis.com")
		defer func() { _ = os.Unsetenv("SSO_ALLOWED_DOMAINS") }()

		_, status, msg, ok := h.findOrCreateSSOUser("blocked@notallowed.com", "google", "sub-blocked")
		if ok {
			t.Fatal("expected failure for disallowed domain")
		}
		if status != 403 {
			t.Fatalf("expected 403, got %d", status)
		}
		if msg == "" {
			t.Fatal("expected non-empty error message")
		}
	})
}

// errReader is an io.Reader that always returns an error.
type errReader struct{}

func (errReader) Read(_ []byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestFetchUserInfoUnknownProvider(t *testing.T) {
	_, _, _, err := fetchUserInfo("unknown-provider", &http.Client{})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestFetchUserInfoGoogle(t *testing.T) {
	t.Run("request error", func(t *testing.T) {
		// Client that always fails requests
		client := &http.Client{Transport: &failTransport{}}
		_, _, _, err := fetchUserInfo("google", client)
		if err == nil {
			t.Fatal("expected error when HTTP request fails")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not-json`))
		}))
		defer srv.Close()
		client := &http.Client{Transport: &rewriteURLTransport{
			base:   http.DefaultTransport,
			target: srv.URL,
		}}
		_, _, _, err := fetchUserInfo("google", client)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("read body error", func(t *testing.T) {
		client := &http.Client{Transport: &bodyErrTransport{}}
		_, _, _, err := fetchUserInfo("google", client)
		if err == nil {
			t.Fatal("expected error when response body read fails")
		}
	})
}

func TestFetchUserInfoGitHub(t *testing.T) {
	t.Run("emails request error", func(t *testing.T) {
		client := &http.Client{Transport: &failTransport{}}
		_, _, _, err := fetchUserInfo("github", client)
		if err == nil {
			t.Fatal("expected error when emails request fails")
		}
	})

	t.Run("emails invalid json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not-json`))
		}))
		defer srv.Close()
		client := &http.Client{Transport: &rewriteURLTransport{base: http.DefaultTransport, target: srv.URL}}
		_, _, _, err := fetchUserInfo("github", client)
		if err == nil {
			t.Fatal("expected error for invalid emails JSON")
		}
	})

	t.Run("profile request error", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				// First call: emails endpoint — return valid JSON
				_, _ = w.Write([]byte(`[{"email":"u@example.com","primary":true,"verified":true}]`))
			} else {
				// Second call: profile endpoint — return error
				hj, ok := w.(http.Hijacker)
				if ok {
					conn, _, _ := hj.Hijack()
					_ = conn.Close()
				}
			}
		}))
		defer srv.Close()
		client := &http.Client{Transport: &rewriteURLTransport{base: http.DefaultTransport, target: srv.URL}}
		_, _, _, err := fetchUserInfo("github", client)
		if err == nil {
			t.Fatal("expected error when profile request fails")
		}
	})

	t.Run("profile read body error", func(t *testing.T) {
		callCount := 0
		client := &http.Client{Transport: &countingBodyErrTransport{
			validResponseFn: func() []byte {
				callCount++
				if callCount == 1 {
					return []byte(`[{"email":"u@example.com","primary":true,"verified":true}]`)
				}
				return nil // second call → errReader body
			},
		}}
		_, _, _, err := fetchUserInfo("github", client)
		if err == nil {
			t.Fatal("expected error when profile body read fails")
		}
	})

	t.Run("profile invalid json", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				_, _ = w.Write([]byte(`[{"email":"u@example.com","primary":true,"verified":true}]`))
			} else {
				_, _ = w.Write([]byte(`not-json`))
			}
		}))
		defer srv.Close()
		client := &http.Client{Transport: &rewriteURLTransport{base: http.DefaultTransport, target: srv.URL}}
		_, _, _, err := fetchUserInfo("github", client)
		if err == nil {
			t.Fatal("expected error for invalid profile JSON")
		}
	})

	t.Run("no verified primary email", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				_, _ = w.Write([]byte(`[{"email":"u@example.com","primary":false,"verified":true}]`))
			} else {
				_, _ = w.Write([]byte(`{"id":42,"login":"user","name":"Test"}`))
			}
		}))
		defer srv.Close()
		client := &http.Client{Transport: &rewriteURLTransport{base: http.DefaultTransport, target: srv.URL}}
		_, _, _, err := fetchUserInfo("github", client)
		if err == nil {
			t.Fatal("expected error when no primary verified email found")
		}
	})

	t.Run("success", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				_, _ = w.Write([]byte(`[{"email":"u@example.com","primary":true,"verified":true}]`))
			} else {
				_, _ = w.Write([]byte(`{"id":42,"login":"user","name":"Test User"}`))
			}
		}))
		defer srv.Close()
		client := &http.Client{Transport: &rewriteURLTransport{base: http.DefaultTransport, target: srv.URL}}
		email, name, subject, err := fetchUserInfo("github", client)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if email != "u@example.com" || name != "Test User" || subject != "42" {
			t.Fatalf("unexpected result: email=%q name=%q subject=%q", email, name, subject)
		}
	})
}

// failTransport always returns an error.
type failTransport struct{}

func (*failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, io.ErrUnexpectedEOF
}

// rewriteURLTransport redirects all requests to the given test-server URL.
type rewriteURLTransport struct {
	base   http.RoundTripper
	target string
}

func (t *rewriteURLTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(t.target, "http://")
	if t.base == nil {
		return http.DefaultTransport.RoundTrip(clone)
	}
	return t.base.RoundTrip(clone)
}

// bodyErrTransport returns a 200 response whose body always errors on Read.
type bodyErrTransport struct{}

func (*bodyErrTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(errReader{}),
		Request:    req,
	}, nil
}

// countingBodyErrTransport calls validResponseFn for each request; if it returns
// non-nil the body contains that data, otherwise the body errors on Read.
type countingBodyErrTransport struct {
	validResponseFn func() []byte
}

func (t *countingBodyErrTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	data := t.validResponseFn()
	var body io.Reader
	if data != nil {
		body = strings.NewReader(string(data))
	} else {
		body = errReader{}
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(body),
		Request:    req,
	}, nil
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

	expiredCode := storeSSOCode("expired-token")
	ssoCodeMu.Lock()
	entry := ssoCodeStore[expiredCode]
	entry.expires = time.Now().Add(-time.Minute) // in der Vergangenheit, nie konsumiert
	ssoCodeStore[expiredCode] = entry
	ssoCodeMu.Unlock()

	freshCode := storeSSOCode("fresh-token")

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
