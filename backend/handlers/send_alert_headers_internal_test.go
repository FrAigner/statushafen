package handlers

import (
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// TestSendAlertEmail_RejectsExtraHeaderWithNewline:
// die neuen Zusatz-Header (List-Unsubscribe) laufen durch denselben
// CR/LF-Schutz wie Subject/Body. Beobachtet über den Audit-Eintrag
// notification.email_failed: gegen einen nicht erreichbaren SMTP-Port wird
// er nur geschrieben, wenn sendAlertEmail tatsächlich zu senden versucht.
func TestSendAlertEmail_RejectsExtraHeaderWithNewline(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	cfg := smtpConfig{Host: "127.0.0.1", Port: "1", From: "alerts@statushafen.local"}
	failures := func() int64 {
		var n int64
		db.Model(&models.AuditLog{}).Where("action = ?", "notification.email_failed").Count(&n)
		return n
	}

	h.sendAlertEmail(cfg, "sub@example.com", "Betreff", "<p>x</p>", "test", 1, "t", "List-Unsubscribe: <https://a.example/x>\nBcc: evil@example.com")
	if got := failures(); got != 0 {
		t.Fatalf("expected no send attempt for a header with CR/LF, got %d failed attempts", got)
	}

	h.sendAlertEmail(cfg, "sub@example.com", "Betreff", "<p>x</p>", "test", 1, "t", "List-Unsubscribe: <https://a.example/x>")
	if got := failures(); got != 1 {
		t.Fatalf("expected exactly one (failed) send attempt for a clean header, got %d", got)
	}
}

// Fund 10: Zeilenumbrüche im Body werden gefaltet und verwerfen die
// Mail nicht; Betreff und Absender mit CR/LF bleiben abgelehnt.
func TestSendAlertEmail_FoldsBodyButRejectsSubjectAndFromNewlines(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	cfg := smtpConfig{Host: "127.0.0.1", Port: "1", From: "alerts@statushafen.local"}
	failures := func() int64 {
		var n int64
		db.Model(&models.AuditLog{}).Where("action = ?", "notification.email_failed").Count(&n)
		return n
	}

	h.sendAlertEmail(cfg, "sub@example.com", "Betreff\r\nBcc: evil@example.com", "<p>x</p>", "test", 1, "t")
	badFrom := cfg
	badFrom.From = "a@statushafen.local\nBcc: evil@example.com"
	h.sendAlertEmail(badFrom, "sub@example.com", "Betreff", "<p>x</p>", "test", 1, "t")
	if got := failures(); got != 0 {
		t.Fatalf("subject/sender with CR/LF must not be sent, got %d attempts", got)
	}

	h.sendAlertEmail(cfg, "sub@example.com", "Betreff", "<p>a</p>\r\n<p>b</p>\n<p>c</p>", "test", 1, "t")
	if got := failures(); got != 1 {
		t.Fatalf("a multi-line body must still be sent (folded), got %d attempts", got)
	}
}

func TestFoldBodyLines(t *testing.T) {
	if got := foldBodyLines("a\r\nb\nc\rd"); got != "a b c d" {
		t.Fatalf("got %q", got)
	}
}
