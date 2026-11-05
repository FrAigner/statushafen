package handlers

import (
	"strings"
	"testing"
)

// TestBuildNotificationEmailHTML_NoLiteralNewlines guards against a
// regression where notificationEmailTemplate's own static markup contains
// real newline characters (e.g. someone reintroducing a raw multi-line
// backtick string without compacting it first). sendAlertEmail's
// hasHeaderInjectionChars guard rejects the entire composed body if it
// contains ANY \r/\n, so a template with its own embedded newlines would
// make DispatchNotifications silently drop every single notification email -
// exactly the bug fixed alongside the malformed From header (see
// TestDispatchNotifications_FromHeaderIsValid).
func TestBuildNotificationEmailHTML_NoLiteralNewlines(t *testing.T) {
	html := buildNotificationEmailHTML(notificationEmailParams{
		lang:        "en",
		actionVerb:  "created",
		compNames:   "API",
		title:       "Elevated error rate",
		status:      "investigating",
		country:     "global",
		message:     "Investigating elevated error rates.",
		frontendURL: "https://example.com",
	})

	if strings.ContainsAny(html, "\r\n") {
		t.Fatalf("expected the composed email body to contain no literal CR/LF characters, got:\n%s", html)
	}
}

// TestBuildNotificationEmailHTML_KnownStatusUsesLabelAndColor covers the
// STATUS_META-mirroring badge (see incidentStatusMeta): a known incident
// status must render its localized label and brand color, not the raw status
// string, so the email visually matches the public status page.
func TestBuildNotificationEmailHTML_KnownStatusUsesLabelAndColor(t *testing.T) {
	html := buildNotificationEmailHTML(notificationEmailParams{
		lang:        "de",
		actionVerb:  "erstellt",
		compNames:   "API",
		title:       "Test",
		status:      "resolved",
		country:     "global",
		message:     "Behoben.",
		frontendURL: "https://example.com",
	})

	if !strings.Contains(html, ">Behoben</span>") {
		t.Fatalf("expected the localized German label 'Behoben' for status 'resolved', got:\n%s", html)
	}
	if !strings.Contains(html, "#2c9c6a") {
		t.Fatalf("expected the resolved-status brand color #2c9c6a in the badge, got:\n%s", html)
	}
}

// TestBuildNotificationEmailHTML_UnknownStatusFallsBackToEscapedRawValue
// covers statuses that aren't in incidentStatusMeta (e.g. a future status
// value not yet mapped) - it must still render (escaped) rather than crash or
// silently omit the status.
func TestBuildNotificationEmailHTML_UnknownStatusFallsBackToEscapedRawValue(t *testing.T) {
	html := buildNotificationEmailHTML(notificationEmailParams{
		lang:        "en",
		actionVerb:  "created",
		compNames:   "API",
		title:       "Test",
		status:      "some-future-status",
		country:     "global",
		message:     "Test.",
		frontendURL: "https://example.com",
	})

	if !strings.Contains(html, "some-future-status") {
		t.Fatalf("expected the raw status value as a fallback label, got:\n%s", html)
	}
}
