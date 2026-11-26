package handlers

import "testing"

// TestLoadSMTPConfig_FallsBackToMailpitDefaults: without any
// SMTP_* env vars set, the existing local behavior (Mailpit via
// docker-compose.yml, localhost:1025, no auth) must remain unchanged.
func TestLoadSMTPConfig_FallsBackToMailpitDefaults(t *testing.T) {
	for _, key := range []string{"SMTP_HOST", "SMTP_PORT", "SMTP_USER", "SMTP_PASSWORD", "SMTP_FROM"} {
		t.Setenv(key, "")
	}

	cfg := loadSMTPConfig()

	if got := cfg.addr(); got != "localhost:1025" {
		t.Errorf("expected fallback dial target 'localhost:1025', got %q", got)
	}
	if cfg.From != "alerts@statushafen.local" {
		t.Errorf("expected fallback sender 'alerts@statushafen.local', got %q", cfg.From)
	}
	if cfg.auth() != nil {
		t.Errorf("expected no auth when SMTP_USER/SMTP_PASSWORD unset, got %v", cfg.auth())
	}
}

// TestLoadSMTPConfig_UsesEnvVarsWhenSet: if SMTP_HOST/SMTP_PORT are
// set, the configured value must be used, not the hardcoded
// localhost:1025 (core of the reported bug).
func TestLoadSMTPConfig_UsesEnvVarsWhenSet(t *testing.T) {
	t.Setenv("SMTP_HOST", "smtp.example-provider.com")
	t.Setenv("SMTP_PORT", "587")
	t.Setenv("SMTP_USER", "apikey")
	t.Setenv("SMTP_PASSWORD", "test-fixture-not-a-real-password")
	t.Setenv("SMTP_FROM", "alerts@example.com")

	cfg := loadSMTPConfig()

	if got := cfg.addr(); got != "smtp.example-provider.com:587" {
		t.Errorf("expected configured dial target 'smtp.example-provider.com:587', got %q", got)
	}
	if got := cfg.addr(); got == "localhost:1025" {
		t.Errorf("expected configured dial target to override hardcoded localhost:1025, got %q", got)
	}
	if cfg.From != "alerts@example.com" {
		t.Errorf("expected configured sender 'alerts@example.com', got %q", cfg.From)
	}
	if cfg.auth() == nil {
		t.Error("expected PlainAuth to be configured when SMTP_USER/SMTP_PASSWORD are set")
	}
}

// TestEnvelopeFrom: the envelope sender (MAIL FROM) must be the
// bare address, even if SMTP_FROM is in the format "Display Name <address>"
// (strict providers such as Brevo otherwise reject with 501). The From:
// header (cfg.From) remains unaffected.
func TestEnvelopeFrom(t *testing.T) {
	tests := []struct {
		name string
		from string
		want string
	}{
		{"Anzeigename-Format wird auf nackte Adresse reduziert", "Statushafen <no-reply@example.com>", "no-reply@example.com"},
		{"nackte Adresse bleibt unveraendert", "no-reply@example.com", "no-reply@example.com"},
		{"Anzeigename mit Komma wird korrekt geparst", `"Statushafen, Support" <no-reply@example.com>`, "no-reply@example.com"},
		{"Anzeigename mit Umlauten (UTF-8)", "Statushafen Störungsmeldung Ä Ö Ü ß <no-reply@example.com>", "no-reply@example.com"},
		{"quotierter Anzeigename mit Umlauten und Sonderzeichen", `"Müller & Söhne (Status); Ärger" <status@mueller.example>`, "status@mueller.example"},
		{"RFC-2047-kodierter Anzeigename", "=?UTF-8?Q?St=C3=B6rungsdienst?= <no-reply@example.com>", "no-reply@example.com"},
		{"nackte Adresse in spitzen Klammern", "<no-reply@example.com>", "no-reply@example.com"},
		{"nicht parsebarer Wert faellt auf Original zurueck", "kaputt ohne klammern", "kaputt ohne klammern"},
		{"Adresse ohne @ faellt auf Original zurueck", "Statushafen <kein-at-zeichen>", "Statushafen <kein-at-zeichen>"},
		{"leerer Wert bleibt leer", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := smtpConfig{From: tt.from}
			if got := cfg.envelopeFrom(); got != tt.want {
				t.Errorf("envelopeFrom() = %q, want %q", got, tt.want)
			}
			// cfg.From itself must never be modified (it stays for the From: header).
			if cfg.From != tt.from {
				t.Errorf("envelopeFrom() hat cfg.From verändert: %q (erwartet %q)", cfg.From, tt.from)
			}
		})
	}
}

// TestLoadSMTPConfig_PartialCredentialsMeanNoAuth: only SMTP_USER or only
// SMTP_PASSWORD set must not produce (incomplete) auth, otherwise
// smtp.SendMail would fail with a broken PlainAuth.
func TestLoadSMTPConfig_PartialCredentialsMeanNoAuth(t *testing.T) {
	t.Setenv("SMTP_HOST", "")
	t.Setenv("SMTP_PORT", "")
	t.Setenv("SMTP_USER", "only-user")
	t.Setenv("SMTP_PASSWORD", "")
	t.Setenv("SMTP_FROM", "")

	cfg := loadSMTPConfig()

	if cfg.auth() != nil {
		t.Errorf("expected no auth when only SMTP_USER is set, got %v", cfg.auth())
	}
}
