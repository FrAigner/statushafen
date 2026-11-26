package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func doJSONRequest(r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// doJSONRequestExpect is doJSONRequest plus an immediate status code check -
// saves the always identical if-w.Code error handling in every single
// t.Run block of this file.
func doJSONRequestExpect(t *testing.T, r http.Handler, method, path string, body any, wantCode int) *httptest.ResponseRecorder {
	t.Helper()
	w := doJSONRequest(r, method, path, body)
	if w.Code != wantCode {
		t.Fatalf("expected %d, got %d: %s", wantCode, w.Code, w.Body.String())
	}
	return w
}

// assertCustomDomainState reloads the status page and checks domain+status -
// a shared postcondition of several Set/Delete test cases.
func assertCustomDomainState(t *testing.T, db *gorm.DB, pageID uint, wantDomain, wantStatus string) {
	t.Helper()
	var reloaded models.StatusPage
	db.First(&reloaded, pageID)
	if reloaded.CustomDomain != wantDomain || reloaded.CustomDomainStatus != wantStatus {
		t.Errorf("expected domain=%q status=%q, got domain=%q status=%q",
			wantDomain, wantStatus, reloaded.CustomDomain, reloaded.CustomDomainStatus)
	}
}

// decodeJSON unpacks the response body as a map - a shared precondition
// of several Verify test cases that check only individual fields from it.
func decodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

func setupCustomDomainRouter(h *Handler) http.Handler {
	r := testutils.SetupRouter()
	r.PUT("/api/admin/pages/:page_id/custom-domain", h.SetCustomDomainHandler)
	r.DELETE("/api/admin/pages/:page_id/custom-domain", h.DeleteCustomDomainHandler)
	r.GET("/api/admin/pages/:page_id/custom-domain", h.GetCustomDomainHandler)
	r.POST("/api/admin/pages/:page_id/custom-domain/verify", h.VerifyCustomDomainHandler)
	r.GET("/api/public/resolve-domain", h.ResolvePublicDomainHandler)
	r.GET("/api/internal/custom-domain-check", h.InternalCustomDomainCheckHandler)
	return r
}

// TestSetCustomDomainHandler: setting a domain always resets the
// status to "pending", a domain conflict with another
// status page is rejected, an empty string deletes the domain.
func TestSetCustomDomainHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := setupCustomDomainRouter(h)

	pageA := models.StatusPage{Name: "Acme", Slug: "acme"}
	db.Create(&pageA)
	pageB := models.StatusPage{Name: "Beta", Slug: "beta"}
	db.Create(&pageB)

	t.Run("Domain setzen -> pending, mit TXT-Verifizierungstoken", func(t *testing.T) {
		w := doJSONRequestExpect(t, r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(pageA.ID))+"/custom-domain",
			map[string]string{"domain": "status.acme.example"}, http.StatusOK)
		assertCustomDomainState(t, db, pageA.ID, "status.acme.example", "pending")

		resp := decodeJSON(t, w)
		token, _ := resp["verification_token"].(string)
		if token == "" {
			t.Fatalf("expected non-empty verification_token, got %v", resp["verification_token"])
		}
		wantTXT := "statushafen-verify=" + token
		if resp["txt_record_value"] != wantTXT {
			t.Errorf("expected txt_record_value=%q, got %v", wantTXT, resp["txt_record_value"])
		}

		var reloaded models.StatusPage
		db.First(&reloaded, pageA.ID)
		if reloaded.CustomDomainVerificationToken != token {
			t.Errorf("expected stored token=%q, got %q", token, reloaded.CustomDomainVerificationToken)
		}
	})

	t.Run("Erneutes Setzen der Domain generiert neuen Token", func(t *testing.T) {
		var before models.StatusPage
		db.First(&before, pageA.ID)

		w := doJSONRequestExpect(t, r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(pageA.ID))+"/custom-domain",
			map[string]string{"domain": "status.acme.example"}, http.StatusOK)
		resp := decodeJSON(t, w)
		newToken, _ := resp["verification_token"].(string)

		if newToken == "" || newToken == before.CustomDomainVerificationToken {
			t.Errorf("expected a fresh token, got old=%q new=%q", before.CustomDomainVerificationToken, newToken)
		}
	})

	// Deliberately changed: an unproven entry no longer reserves the domain
	// (squatting); the conflict is decided only on verification, see
	// custom_domain_reservation_test.go.
	t.Run("Domain, die eine andere Seite unbewiesen trägt, lässt sich eintragen", func(t *testing.T) {
		doJSONRequestExpect(t, r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(pageB.ID))+"/custom-domain",
			map[string]string{"domain": "status.acme.example"}, http.StatusOK)
		assertCustomDomainState(t, db, pageB.ID, "status.acme.example", "pending")
	})

	t.Run("Ungültige Domain wird abgelehnt", func(t *testing.T) {
		doJSONRequestExpect(t, r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(pageB.ID))+"/custom-domain",
			map[string]string{"domain": "https://not-a-hostname/"}, http.StatusBadRequest)
	})

	t.Run("Leerer String löscht Domain inkl. Token", func(t *testing.T) {
		doJSONRequestExpect(t, r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(pageA.ID))+"/custom-domain",
			map[string]string{"domain": ""}, http.StatusOK)
		assertCustomDomainState(t, db, pageA.ID, "", "")

		var reloaded models.StatusPage
		db.First(&reloaded, pageA.ID)
		if reloaded.CustomDomainVerificationToken != "" {
			t.Errorf("expected verification token cleared, got %q", reloaded.CustomDomainVerificationToken)
		}
	})

	t.Run("DELETE entfernt Domain inkl. Token", func(t *testing.T) {
		db.Model(&pageB).Updates(map[string]any{
			"custom_domain":                    "status.beta.example",
			"custom_domain_status":             "verified",
			"custom_domain_verification_token": "leftover-token",
			"custom_domain_check_error":        "txt_not_found",
			"custom_domain_checked_at":         time.Now(),
		})
		doJSONRequestExpect(t, r, "DELETE", "/api/admin/pages/"+strconv.Itoa(int(pageB.ID))+"/custom-domain", nil, http.StatusOK)
		assertCustomDomainState(t, db, pageB.ID, "", "")

		var reloaded models.StatusPage
		db.First(&reloaded, pageB.ID)
		if reloaded.CustomDomainVerificationToken != "" {
			t.Errorf("expected verification token cleared, got %q", reloaded.CustomDomainVerificationToken)
		}
		if reloaded.CustomDomainCheckError != "" || reloaded.CustomDomainCheckedAt != nil {
			t.Errorf("expected check result cleared, got %q / %v", reloaded.CustomDomainCheckError, reloaded.CustomDomainCheckedAt)
		}
	})
}

// TestVerifyCustomDomainHandler covers the TXT-based owner verification
// by replacing lookupTXT for the test with a fake
// implementation - no real network access needed.
func TestVerifyCustomDomainHandler(t *testing.T) {
	origLookup := lookupTXT
	origTarget := os.Getenv("CUSTOM_DOMAIN_CNAME_TARGET")
	_ = os.Setenv("CUSTOM_DOMAIN_CNAME_TARGET", "edge.example.com")
	defer func() {
		lookupTXT = origLookup
		_ = os.Setenv("CUSTOM_DOMAIN_CNAME_TARGET", origTarget)
	}()

	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := setupCustomDomainRouter(h)

	page := models.StatusPage{
		Name: "Acme", Slug: "acme",
		CustomDomain:                  "status.acme.example",
		CustomDomainStatus:            "pending",
		CustomDomainVerificationToken: "test-token-123",
	}
	db.Create(&page)

	verify := func(t *testing.T) map[string]any {
		t.Helper()
		w := doJSONRequestExpect(t, r, "POST", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/custom-domain/verify", nil, http.StatusOK)
		return decodeJSON(t, w)
	}

	t.Run("Fehlender TXT-Eintrag -> check_failed mit Zeitpunkt und Grund", func(t *testing.T) {
		lookupTXT = func(_ context.Context, host string) ([]string, error) { return []string{"v=spf1 -all"}, nil }
		resp := verify(t)
		if resp["verified"] != false {
			t.Errorf("expected verified=false, got %v", resp["verified"])
		}
		if resp["txt_record_value"] != "statushafen-verify=test-token-123" {
			t.Errorf("expected txt_record_value hint in response, got %v", resp["txt_record_value"])
		}
		assertCustomDomainState(t, db, page.ID, page.CustomDomain, "check_failed")
		if resp["custom_domain_check_error"] != "txt_not_found" || resp["custom_domain_checked_at"] == nil {
			t.Errorf("expected reason txt_not_found and checked_at in response, got %v / %v", resp["custom_domain_check_error"], resp["custom_domain_checked_at"])
		}
		var reloaded models.StatusPage
		db.First(&reloaded, page.ID)
		if reloaded.CustomDomainCheckedAt == nil || reloaded.CustomDomainCheckError != "txt_not_found" {
			t.Errorf("expected persisted CheckedAt and txt_not_found, got %v / %q", reloaded.CustomDomainCheckedAt, reloaded.CustomDomainCheckError)
		}
	})

	t.Run("Korrekter TXT-Eintrag wird verified (auch neben anderen Records)", func(t *testing.T) {
		lookupTXT = func(_ context.Context, host string) ([]string, error) {
			return []string{"v=spf1 -all", "statushafen-verify=test-token-123"}, nil
		}
		resp := verify(t)
		if resp["verified"] != true {
			t.Errorf("expected verified=true, got %v", resp["verified"])
		}
		var reloaded models.StatusPage
		db.First(&reloaded, page.ID)
		if reloaded.CustomDomainCheckError != "" || reloaded.CustomDomainCheckedAt == nil {
			t.Errorf("expected old failure reason cleared and CheckedAt set, got %q / %v", reloaded.CustomDomainCheckError, reloaded.CustomDomainCheckedAt)
		}
		if reloaded.CustomDomainStatus != "verified" || reloaded.CustomDomainVerifiedAt == nil {
			t.Errorf("expected status=verified with VerifiedAt set, got status=%q VerifiedAt=%v",
				reloaded.CustomDomainStatus, reloaded.CustomDomainVerifiedAt)
		}
	})

	t.Run("DNS-Fehler (SERVFAIL) -> check_failed mit Grund dns_lookup_failed", func(t *testing.T) {
		// The previous step set verified; verified would deliberately remain on an
		// error, here it is about a new domain.
		require.NoError(t, db.Model(&page).Update("custom_domain_status", "pending").Error)
		lookupTXT = func(_ context.Context, host string) ([]string, error) {
			return nil, &net.DNSError{Err: "server misbehaving", Name: host, IsTemporary: true}
		}
		resp := verify(t)
		if resp["verified"] != false {
			t.Errorf("expected verified=false, got %v", resp["verified"])
		}
		assertCustomDomainState(t, db, page.ID, page.CustomDomain, "check_failed")
		if resp["custom_domain_check_error"] != "dns_lookup_failed" {
			t.Errorf("expected reason dns_lookup_failed, got %v", resp["custom_domain_check_error"])
		}
	})

	t.Run("Neu eintragen setzt das Prüfergebnis zurück auf pending", func(t *testing.T) {
		doJSONRequestExpect(t, r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/custom-domain", map[string]string{"domain": page.CustomDomain}, http.StatusOK)
		assertCustomDomainState(t, db, page.ID, page.CustomDomain, "pending")
		var reloaded models.StatusPage
		db.First(&reloaded, page.ID)
		if reloaded.CustomDomainCheckedAt != nil || reloaded.CustomDomainCheckError != "" {
			t.Errorf("expected check result reset, got %v / %q", reloaded.CustomDomainCheckedAt, reloaded.CustomDomainCheckError)
		}
	})
}

// TestVerifyCustomDomainHandler_SaveErrorReturns500: if
// saving the check result fails, this must become visible as a 500 instead of
// silently reporting a stale state (previously an unchecked save).
func TestVerifyCustomDomainHandler_SaveErrorReturns500(t *testing.T) {
	origLookup := lookupTXT
	defer func() { lookupTXT = origLookup }()

	for name, txt := range map[string][]string{
		"fehlgeschlagene Prüfung": {"v=spf1 -all"},
		"erfolgreiche Prüfung":    {"statushafen-verify=tok"},
	} {
		t.Run(name, func(t *testing.T) {
			lookupTXT = func(context.Context, string) ([]string, error) { return txt, nil }
			db := testutils.SetupTestDB()
			r := setupCustomDomainRouter(NewHandler(db))
			page := models.StatusPage{Name: "A", Slug: "a", CustomDomain: "s.a.example", CustomDomainStatus: "pending", CustomDomainVerificationToken: "tok"}
			db.Create(&page)
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register("cd_fail", func(tx *gorm.DB) {
				_ = tx.AddError(errors.New("db down"))
			}))
			doJSONRequestExpect(t, r, "POST", "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/custom-domain/verify", nil, http.StatusInternalServerError)
		})
	}
}

// TestResolvePublicDomainHandler covers the public domain->slug resolution;
// the promotion "verified" -> "active" is done only by the internal check.
func TestResolvePublicDomainHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := setupCustomDomainRouter(h)

	verifiedPage := models.StatusPage{Name: "Acme", Slug: "acme", CustomDomain: "status.acme.example", CustomDomainStatus: "verified"}
	db.Create(&verifiedPage)
	pendingPage := models.StatusPage{Name: "Beta", Slug: "beta", CustomDomain: "status.beta.example", CustomDomainStatus: "pending"}
	db.Create(&pendingPage)

	// Deliberately changed: the anonymous call no longer promotes the status,
	// that happens only via the internal check (custom_domain_gate_test.go).
	t.Run("Verified Domain löst zu Slug auf, der Status bleibt verified", func(t *testing.T) {
		w := doJSONRequestExpect(t, r, "GET", "/api/public/resolve-domain?host=status.acme.example", nil, http.StatusOK)
		resp := decodeJSON(t, w)
		if resp["slug"] != "acme" {
			t.Errorf("expected slug=acme, got %v", resp["slug"])
		}
		var reloaded models.StatusPage
		db.First(&reloaded, verifiedPage.ID)
		if reloaded.CustomDomainStatus != "verified" {
			t.Errorf("expected status unchanged (verified), got %q", reloaded.CustomDomainStatus)
		}
	})

	t.Run("Pending Domain löst nicht auf (404)", func(t *testing.T) {
		doJSONRequestExpect(t, r, "GET", "/api/public/resolve-domain?host=status.beta.example", nil, http.StatusNotFound)
	})

	t.Run("Unbekannter Host löst nicht auf (404)", func(t *testing.T) {
		doJSONRequestExpect(t, r, "GET", "/api/public/resolve-domain?host=unknown.example", nil, http.StatusNotFound)
	})
}

// TestInternalCustomDomainCheckHandler covers the endpoint called by Caddy's
// "ask" endpoint.
func TestInternalCustomDomainCheckHandler(t *testing.T) {
	// Without a token, the endpoint answers only in a development environment;
	// the behavior without this exception is tested by custom_domain_gate_test.go.
	t.Setenv("APP_ENV", "test")
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := setupCustomDomainRouter(h)

	page := models.StatusPage{Name: "Acme", Slug: "acme", CustomDomain: "status.acme.example", CustomDomainStatus: "verified"}
	db.Create(&page)

	doCheckWithToken := func(t *testing.T, domain, token string, wantCode int) {
		t.Helper()
		req, _ := http.NewRequest("GET", "/api/internal/custom-domain-check?domain="+domain, nil)
		if token != "" {
			req.Header.Set("X-Internal-Token", token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != wantCode {
			t.Fatalf("expected %d, got %d", wantCode, w.Code)
		}
	}

	t.Run("Verified Domain -> 200", func(t *testing.T) {
		doCheckWithToken(t, "status.acme.example", "", http.StatusOK)
	})

	t.Run("Unbekannte Domain -> 404 (kein Zertifikat)", func(t *testing.T) {
		doCheckWithToken(t, "evil.example", "", http.StatusNotFound)
	})

	t.Run("Shared Secret gesetzt: falscher Token -> 404", func(t *testing.T) {
		_ = os.Setenv("CADDY_INTERNAL_TOKEN", "s3cret")
		defer func() { _ = os.Unsetenv("CADDY_INTERNAL_TOKEN") }()
		doCheckWithToken(t, "status.acme.example", "wrong", http.StatusNotFound)
	})

	t.Run("Shared Secret gesetzt: richtiger Token -> 200", func(t *testing.T) {
		_ = os.Setenv("CADDY_INTERNAL_TOKEN", "s3cret")
		defer func() { _ = os.Unsetenv("CADDY_INTERNAL_TOKEN") }()
		doCheckWithToken(t, "status.acme.example", "s3cret", http.StatusOK)
	})
}

// customDomainCheckFixture creates a status page with domain/token in an
// initial status and returns the router, DB and the verify path.
func customDomainCheckFixture(t *testing.T, status string) (http.Handler, *gorm.DB, models.StatusPage, string) {
	t.Helper()
	db := testutils.SetupTestDB()
	r := setupCustomDomainRouter(NewHandler(db))
	page := models.StatusPage{Name: "A", Slug: "a", CustomDomain: "s.a.example", CustomDomainStatus: status, CustomDomainVerificationToken: "tok"}
	require.NoError(t, db.Create(&page).Error)
	return r, db, page, "/api/admin/pages/" + strconv.Itoa(int(page.ID)) + "/custom-domain/verify"
}

func stubLookup(t *testing.T, fn func(context.Context, string) ([]string, error)) {
	t.Helper()
	orig := lookupTXT
	lookupTXT = fn
	t.Cleanup(func() { lookupTXT = orig })
}

// TestVerifyCustomDomainHandler_ErrorClasses (finding 3): a
// missing TXT record is a *net.DNSError{IsNotFound} in Go and must count as
// txt_not_found; only timeout/SERVFAIL/other errors as lookup errors.
func TestVerifyCustomDomainHandler_ErrorClasses(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"NXDOMAIN/keine TXT (IsNotFound)": {&net.DNSError{Err: "no such host", Name: "s.a.example", IsNotFound: true}, "txt_not_found"},
		"IsNotFound umhüllt":              {fmt.Errorf("lookup: %w", &net.DNSError{Err: "no such host", IsNotFound: true}), "txt_not_found"},
		"Timeout":                         {&net.DNSError{Err: "i/o timeout", IsTimeout: true, IsTemporary: true}, "dns_lookup_failed"},
		"SERVFAIL (temporär)":             {&net.DNSError{Err: "server misbehaving", IsTemporary: true}, "dns_lookup_failed"},
		"anderer Fehler":                  {errors.New("boom"), "dns_lookup_failed"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r, db, page, path := customDomainCheckFixture(t, "pending")
			stubLookup(t, func(context.Context, string) ([]string, error) { return nil, tc.err })
			resp := decodeJSON(t, doJSONRequestExpect(t, r, "POST", path, nil, http.StatusOK))
			require.Equal(t, tc.want, resp["custom_domain_check_error"])
			require.Equal(t, "check_failed", resp["custom_domain_status"])
			var reloaded models.StatusPage
			db.First(&reloaded, page.ID)
			require.Equal(t, tc.want, reloaded.CustomDomainCheckError)
			require.Equal(t, "check_failed", reloaded.CustomDomainStatus)
		})
	}
}

// TestVerifyCustomDomainHandler_LiveStatusKept (finding 4): a
// failed check must not downgrade verified/active (otherwise
// ResolvePublicDomainHandler no longer resolves the domain and Caddy does not renew the
// certificate); only CheckedAt/CheckError are written.
func TestVerifyCustomDomainHandler_LiveStatusKept(t *testing.T) {
	t.Setenv("APP_ENV", "test") // internal check without token only in development
	failures := map[string]error{
		"TXT fehlt":     &net.DNSError{Err: "no such host", IsNotFound: true},
		"DNS-Aussetzer": &net.DNSError{Err: "i/o timeout", IsTimeout: true},
	}
	for _, status := range []string{"verified", "active"} {
		for name, lookupErr := range failures {
			t.Run(status+" bleibt bei "+name, func(t *testing.T) {
				r, db, page, path := customDomainCheckFixture(t, status)
				stubLookup(t, func(context.Context, string) ([]string, error) { return nil, lookupErr })
				resp := decodeJSON(t, doJSONRequestExpect(t, r, "POST", path, nil, http.StatusOK))
				require.Equal(t, status, resp["custom_domain_status"])
				require.Equal(t, false, resp["verified"])
				var reloaded models.StatusPage
				db.First(&reloaded, page.ID)
				require.Equal(t, status, reloaded.CustomDomainStatus)
				require.NotNil(t, reloaded.CustomDomainCheckedAt)
				require.NotEmpty(t, reloaded.CustomDomainCheckError)
				// The domain continues to be resolved and approved by Caddy.
				doJSONRequestExpect(t, r, "GET", "/api/internal/custom-domain-check?domain=s.a.example", nil, http.StatusOK)
				doJSONRequestExpect(t, r, "GET", "/api/public/resolve-domain?host=s.a.example", nil, http.StatusOK)
			})
		}
	}

	t.Run("erfolgreiche Prüfung löscht den Fehler, active bleibt active", func(t *testing.T) {
		r, db, page, path := customDomainCheckFixture(t, "active")
		require.NoError(t, db.Model(&page).Update("custom_domain_check_error", "dns_lookup_failed").Error)
		stubLookup(t, func(context.Context, string) ([]string, error) { return []string{"statushafen-verify=tok"}, nil })
		resp := decodeJSON(t, doJSONRequestExpect(t, r, "POST", path, nil, http.StatusOK))
		require.Equal(t, true, resp["verified"])
		require.Equal(t, "active", resp["custom_domain_status"])
		var reloaded models.StatusPage
		db.First(&reloaded, page.ID)
		require.Equal(t, "active", reloaded.CustomDomainStatus)
		require.Empty(t, reloaded.CustomDomainCheckError)
		require.NotNil(t, reloaded.CustomDomainCheckedAt)
	})
}

// TestVerifyCustomDomainHandler_ConcurrentChange (finding 5): if
// the domain or token is changed/removed during the lookup, the
// handler answers 409 and overwrites nothing (previously: saving the whole row brought
// a removed domain back as "verified").
func TestVerifyCustomDomainHandler_ConcurrentChange(t *testing.T) {
	changes := map[string]map[string]any{
		"Domain entfernt":    {"custom_domain": "", "custom_domain_status": "", "custom_domain_verification_token": ""},
		"Domain ersetzt":     {"custom_domain": "neu.example", "custom_domain_status": "pending", "custom_domain_verification_token": "tok2"},
		"nur Token erneuert": {"custom_domain_verification_token": "tok2"},
	}
	for name, change := range changes {
		for _, found := range []bool{true, false} {
			t.Run(name+" (TXT gefunden: "+strconv.FormatBool(found)+")", func(t *testing.T) {
				r, db, page, path := customDomainCheckFixture(t, "pending")
				stubLookup(t, func(context.Context, string) ([]string, error) {
					require.NoError(t, db.Model(&models.StatusPage{}).Where("id = ?", page.ID).Updates(change).Error)
					if found {
						return []string{"statushafen-verify=tok"}, nil
					}
					return nil, &net.DNSError{Err: "no such host", IsNotFound: true}
				})
				w := doJSONRequestExpect(t, r, "POST", path, nil, http.StatusConflict)
				require.Equal(t, "custom_domain_changed", decodeJSON(t, w)["code"])
				var reloaded models.StatusPage
				db.First(&reloaded, page.ID)
				require.NotEqual(t, "verified", reloaded.CustomDomainStatus)
				require.NotEqual(t, "check_failed", reloaded.CustomDomainStatus)
				require.Nil(t, reloaded.CustomDomainCheckedAt)
				require.Nil(t, reloaded.CustomDomainVerifiedAt)
				require.Empty(t, reloaded.CustomDomainCheckError)
			})
		}
	}
}

// TestVerifyCustomDomainHandler_LookupTimeout (finding 5): the lookup
// runs with a deadline; a hanging resolver does not block the request
// indefinitely, the result is dns_lookup_failed.
func TestVerifyCustomDomainHandler_LookupTimeout(t *testing.T) {
	origTimeout := customDomainLookupTimeout
	customDomainLookupTimeout = 30 * time.Millisecond
	t.Cleanup(func() { customDomainLookupTimeout = origTimeout })

	r, db, page, path := customDomainCheckFixture(t, "pending")
	hadDeadline := false
	stubLookup(t, func(ctx context.Context, host string) ([]string, error) {
		_, hadDeadline = ctx.Deadline()
		select {
		case <-ctx.Done():
			return nil, &net.DNSError{Err: "i/o timeout", Name: host, IsTimeout: true, IsTemporary: true}
		case <-time.After(5 * time.Second):
			return nil, errors.New("Timeout hat nicht gegriffen")
		}
	})
	start := time.Now()
	resp := decodeJSON(t, doJSONRequestExpect(t, r, "POST", path, nil, http.StatusOK))
	require.Less(t, time.Since(start), 2*time.Second)
	require.True(t, hadDeadline, "Lookup muss mit Frist (Context) aufgerufen werden")
	require.Equal(t, "dns_lookup_failed", resp["custom_domain_check_error"])
	var reloaded models.StatusPage
	db.First(&reloaded, page.ID)
	require.Equal(t, "dns_lookup_failed", reloaded.CustomDomainCheckError)
}

// customDomainCNAMETarget: CUSTOM_DOMAIN_CNAME_TARGET wins; without a setting the
// target host is derived as "edge.<host from FRONTEND_URL>" (no hard-wired
// Betreibername).
func TestCustomDomainCNAMETarget(t *testing.T) {
	t.Run("explizite Einstellung gewinnt", func(t *testing.T) {
		t.Setenv("CUSTOM_DOMAIN_CNAME_TARGET", "cname.kunde.example")
		t.Setenv("FRONTEND_URL", "https://status.example.org")
		if got := customDomainCNAMETarget(); got != "cname.kunde.example" {
			t.Errorf("customDomainCNAMETarget() = %q, want %q", got, "cname.kunde.example")
		}
	})
	t.Run("Standard leitet sich aus FRONTEND_URL ab", func(t *testing.T) {
		t.Setenv("CUSTOM_DOMAIN_CNAME_TARGET", "")
		t.Setenv("FRONTEND_URL", "https://status.example.org")
		if got := customDomainCNAMETarget(); got != "edge.status.example.org" {
			t.Errorf("customDomainCNAMETarget() = %q, want %q", got, "edge.status.example.org")
		}
	})
	t.Run("ohne FRONTEND_URL gilt der lokale Standardhost", func(t *testing.T) {
		t.Setenv("CUSTOM_DOMAIN_CNAME_TARGET", "")
		_ = os.Unsetenv("FRONTEND_URL")
		if got := customDomainCNAMETarget(); got != "edge.localhost" {
			t.Errorf("customDomainCNAMETarget() = %q, want %q", got, "edge.localhost")
		}
	})
}
