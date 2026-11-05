package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Google lieferte die E-Mail ohne Prüfung von verified_email,
// und findOrCreateSSOUser verknüpfte jedes bestehende Konto allein über die
// E-Mail. Zusammen mit SSO_BOOTSTRAP_ADMIN_EMAILS ließen sich so Passwort-
// und Admin-Konten übernehmen.
func TestFetchUserInfoGoogle_VerifiedEmail(t *testing.T) {
	serve := func(t *testing.T, body string) (string, string, string, error) {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		client := &http.Client{Transport: &rewriteURLTransport{base: http.DefaultTransport, target: srv.URL}}
		return fetchUserInfo("google", client)
	}

	t.Run("verified_email false wird abgelehnt", func(t *testing.T) {
		_, _, _, err := serve(t, `{"id":"g-1","email":"admin@example.test","name":"X","verified_email":false}`)
		if err == nil {
			t.Fatal("expected an error for an unverified Google e-mail")
		}
	})

	t.Run("fehlendes verified_email wird abgelehnt", func(t *testing.T) {
		_, _, _, err := serve(t, `{"id":"g-2","email":"admin@example.test","name":"X"}`)
		if err == nil {
			t.Fatal("expected an error when verified_email is missing")
		}
	})

	t.Run("verified_email true liefert die Daten", func(t *testing.T) {
		email, name, subject, err := serve(t, `{"id":"g-3","email":"ok@example.test","name":"Okay","verified_email":true}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if email != "ok@example.test" || name != "Okay" || subject != "g-3" {
			t.Fatalf("unexpected user info: %q %q %q", email, name, subject)
		}
	})
}

func TestFindOrCreateSSOUser_LinkingRules(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	db.Create(&models.Role{Name: "Viewer", Permissions: "[]"})
	db.Create(&models.Role{Name: "Global Admin", Permissions: "[]"})

	reload := func(t *testing.T, id uint) models.User {
		t.Helper()
		var u models.User
		if err := db.First(&u, id).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		return u
	}
	expectRejected := func(t *testing.T, status int, ok bool) {
		t.Helper()
		if ok {
			t.Fatal("expected the SSO login to be rejected")
		}
		if status != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", status)
		}
	}

	t.Run("bereits verknüpftes Konto mit gleichem Subject: ok", func(t *testing.T) {
		u := models.User{Email: "linked-321@example.com", SSOProvider: "google", SSOSubject: "sub-a"}
		db.Create(&u)
		_, status, _, ok := h.findOrCreateSSOUser(u.Email, "google", "sub-a")
		if !ok || status != 0 {
			t.Fatalf("expected success, got status=%d ok=%v", status, ok)
		}
	})

	t.Run("fremdes Subject auf verknüpftem Konto: abgelehnt, nichts geändert", func(t *testing.T) {
		u := models.User{Email: "other-sub-321@example.com", SSOProvider: "google", SSOSubject: "sub-owner"}
		db.Create(&u)
		_, status, _, ok := h.findOrCreateSSOUser(u.Email, "google", "sub-attacker")
		expectRejected(t, status, ok)
		if got := reload(t, u.ID); got.SSOSubject != "sub-owner" {
			t.Fatalf("expected SSOSubject to stay, got %q", got.SSOSubject)
		}
	})

	t.Run("gleiches Subject, anderer Provider: abgelehnt", func(t *testing.T) {
		u := models.User{Email: "other-prov-321@example.com", SSOProvider: "github", SSOSubject: "42"}
		db.Create(&u)
		_, status, _, ok := h.findOrCreateSSOUser(u.Email, "google", "42")
		expectRejected(t, status, ok)
	})

	t.Run("unverknüpftes Passwort-Konto: abgelehnt, Konto unverändert", func(t *testing.T) {
		u := models.User{Email: "pw-321@example.com", PasswordHash: testutils.MustHash("geheim-123")}
		db.Create(&u)
		_, status, _, ok := h.findOrCreateSSOUser(u.Email, "google", "sub-pw")
		expectRejected(t, status, ok)
		if got := reload(t, u.ID); got.SSOProvider != "" || got.SSOSubject != "" || got.PasswordHash != u.PasswordHash {
			t.Fatalf("expected the account to stay untouched, got %+v", got)
		}
	})

	t.Run("unverknüpfter Global Admin ohne Passwort: abgelehnt", func(t *testing.T) {
		u := models.User{Email: "ga-321@example.com", IsGlobalAdmin: true}
		db.Create(&u)
		_, status, _, ok := h.findOrCreateSSOUser(u.Email, "google", "sub-ga")
		expectRejected(t, status, ok)
	})

	t.Run("Bootstrap-Adresse auf Passwort-Konto: abgelehnt, keine Beförderung", func(t *testing.T) {
		t.Setenv("SSO_BOOTSTRAP_ADMIN_EMAILS", "boot-321@example.com")
		u := models.User{Email: "boot-321@example.com", PasswordHash: testutils.MustHash("geheim-456")}
		db.Create(&u)
		_, status, _, ok := h.findOrCreateSSOUser(u.Email, "google", "sub-boot")
		expectRejected(t, status, ok)
		if got := reload(t, u.ID); got.IsGlobalAdmin {
			t.Fatal("expected no promotion to Global Admin")
		}
	})

	t.Run("Gegenprobe: vom Admin angelegtes SSO-Konto ohne Passwort wird beim ersten Login verknüpft", func(t *testing.T) {
		u := models.User{Email: "invited-321@example.com"}
		db.Create(&u)
		got, status, _, ok := h.findOrCreateSSOUser(u.Email, "google", "sub-invited")
		if !ok || status != 0 || got.SSOSubject != "sub-invited" {
			t.Fatalf("expected first-time linking, got status=%d ok=%v user=%+v", status, ok, got)
		}
	})
}
