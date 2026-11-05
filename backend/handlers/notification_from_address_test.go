package handlers

import (
	"net/mail"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"
)

// TestValidateNotificationFromAddress: NotificationFromAddress
// is optional (empty = fall back to a slug-based default), but if
// set it must parse as a valid RFC 5322 address (same format as the existing
// SMTP_FROM env var, e.g. "Acme Status <status@acme.example>") and must not
// contain CR/LF, since it ends up unescaped in the SMTP "From:" header built
// by sendAlertEmail.
func TestValidateNotificationFromAddress(t *testing.T) {
	cases := []struct {
		name    string
		address string
		wantOK  bool
	}{
		{"empty is allowed (fallback to slug default)", "", true},
		{"plain address", "status@acme.example", true},
		{"display name plus address", "Acme Status <status@acme.example>", true},
		{"missing @ is rejected", "not-an-email", false},
		{"CRLF header injection attempt is rejected", "Acme <status@acme.example>\r\nBcc: attacker@evil.example", false},
		{"bare newline is rejected", "Acme <status@acme.example>\nBcc: attacker@evil.example", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := &models.StatusPage{NotificationFromAddress: tc.address}
			_, ok := validateNotificationFromAddress(page)
			if ok != tc.wantOK {
				t.Errorf("validateNotificationFromAddress(%q) ok = %v, want %v", tc.address, ok, tc.wantOK)
			}
		})
	}
}

// TestResolveNotificationFromAddress: Die Absenderadresse gehört immer dem
// Betreiber (SMTP_FROM); die Seite steuert nur den Anzeigenamen bei. Die
// frühere Semantik (Seitenadresse bzw. "<slug>@<Frontend-Host>" als Absender)
// ließ Seiten-Admins beliebige Absender fälschen und ist bewusst entfallen.
func TestResolveNotificationFromAddress(t *testing.T) {
	const operator = "Statushafen <alerts@mail.example.com>"

	t.Run("operator address with page name as display name", func(t *testing.T) {
		page := models.StatusPage{Name: "Acme Status", Slug: "acme", NotificationFromAddress: "Acme Status <status@acme.example>"}
		got := resolveNotificationFromAddress(operator, page)
		addr, err := mail.ParseAddress(got)
		if err != nil {
			t.Fatalf("From %q nicht parsebar: %v", got, err)
		}
		if addr.Address != "alerts@mail.example.com" || addr.Name != "Acme Status" {
			t.Errorf("From = %q, want name %q and operator address", got, "Acme Status")
		}
	})

	t.Run("page address never becomes the sender", func(t *testing.T) {
		page := models.StatusPage{Name: "Acme", Slug: "acme", NotificationFromAddress: "Bank <security@bank.example>"}
		if got := resolveNotificationFromAddress(operator, page); strings.Contains(got, "bank.example") {
			t.Errorf("From = %q enthält die Seitenadresse", got)
		}
	})

	t.Run("empty page name keeps operator sender", func(t *testing.T) {
		got := resolveNotificationFromAddress("alerts@mail.example.com", models.StatusPage{Slug: "acme"})
		if got != "<alerts@mail.example.com>" && got != "alerts@mail.example.com" {
			t.Errorf("From = %q, want operator address", got)
		}
	})

	t.Run("control characters in the page name are neutralised", func(t *testing.T) {
		got := resolveNotificationFromAddress(operator, models.StatusPage{Name: "A\r\nBcc: x@evil.example"})
		if hasHeaderInjectionChars(got) {
			t.Errorf("From = %q enthält CR/LF", got)
		}
	})

	t.Run("unparsable operator config stays unchanged", func(t *testing.T) {
		if got := resolveNotificationFromAddress("kein-at-zeichen", models.StatusPage{Name: "Acme"}); got != "kein-at-zeichen" {
			t.Errorf("From = %q, want unchanged", got)
		}
	})
}

func TestResolveNotificationReplyTo(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"plain address", "status@acme.example", "<status@acme.example>"},
		{"display name", "Acme Status <status@acme.example>", `"Acme Status" <status@acme.example>`},
		{"invalid", "kein-at", ""},
		{"header injection", "Acme <a@acme.example>\r\nBcc: x@evil.example", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveNotificationReplyTo(models.StatusPage{NotificationFromAddress: tc.in}); got != tc.want {
				t.Errorf("resolveNotificationReplyTo(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
