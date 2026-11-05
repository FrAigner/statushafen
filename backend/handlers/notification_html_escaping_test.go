package handlers_test

import (
	"bufio"
	"net"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// fakeSMTPServer is a minimal SMTP test double: just enough of the protocol
// for net/smtp.SendMail (used by sendAlertEmail/DispatchNotifications) to
// complete successfully, capturing the DATA section (the actual email body)
// for assertions. There is no real SMTP server available in the test
// environment, so this is the only way to observe the composed email body.
//
// Der Server ist bei MAIL FROM so strikt wie Brevo und lehnt alles
// außer der nackten Adresse in spitzen Klammern mit 501 ab. Damit prüft jeder
// Versandtest automatisch mit, dass der Envelope-Absender keinen Anzeigenamen
// trägt; die Argumente landen zusätzlich in mailFromCh.
type fakeSMTPServer struct {
	listener   net.Listener
	dataCh     chan string
	mailFromCh chan string
}

// strictMailFromRe entspricht der RFC-5321-Form "MAIL FROM:<adresse>" ohne
// Anzeigenamen, optional gefolgt von ESMTP-Parametern.
var strictMailFromRe = regexp.MustCompile(`^MAIL FROM:<([^<>"\s]*)>( .*)?$`)

func startFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start fake SMTP server: %v", err)
	}
	srv := &fakeSMTPServer{listener: ln, dataCh: make(chan string, 10), mailFromCh: make(chan string, 10)}
	go srv.acceptLoop()
	return srv
}

func (s *fakeSMTPServer) addr() string {
	return s.listener.Addr().String()
}

func (s *fakeSMTPServer) close() {
	_ = s.listener.Close()
}

func (s *fakeSMTPServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *fakeSMTPServer) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	writeLine := func(line string) {
		_, _ = w.WriteString(line + "\r\n")
		_ = w.Flush()
	}
	writeLine("220 fake.smtp ESMTP ready")

	var dataBuf strings.Builder
	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		if inData {
			if line == "." {
				inData = false
				s.dataCh <- dataBuf.String()
				writeLine("250 OK: queued")
				continue
			}
			dataBuf.WriteString(line + "\n")
			continue
		}

		switch upper := strings.ToUpper(line); {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			// Single-line response: advertises no extensions (no STARTTLS/AUTH),
			// which is fine since the test doesn't set SMTP_USER/SMTP_PASSWORD.
			writeLine("250 fake.smtp")
		case strings.HasPrefix(upper, "MAIL FROM"):
			m := strictMailFromRe.FindStringSubmatch(line)
			if m == nil {
				writeLine(`501 5.5.2 Was expecting MAIL arg syntax of FROM:<address>`)
				continue
			}
			select {
			case s.mailFromCh <- m[1]:
			default:
			}
			writeLine("250 OK")
		case strings.HasPrefix(upper, "RCPT TO"):
			writeLine("250 OK")
		case upper == "DATA":
			writeLine("354 Start mail input; end with <CRLF>.<CRLF>")
			inData = true
			dataBuf.Reset()
		case upper == "QUIT":
			writeLine("221 Bye")
			return
		default:
			writeLine("250 OK")
		}
	}
}

// TestDispatchNotifications_EmailBodyEscapesHTMLInjection:
// Incident Title/Message/Country and component names were previously inserted
// into the text/html email body via fmt.Sprintf without HTML-escaping, so an
// editor with incidents:write could inject arbitrary HTML (links, tracking
// pixels, script-looking markup) into notification emails. This test creates
// an incident with a script-tag title and asserts the actual composed email
// body (captured via a fake SMTP server) contains the escaped form and not
// the raw, executable markup.
func TestDispatchNotifications_EmailBodyEscapesHTMLInjection(t *testing.T) {
	srv := startFakeSMTPServer(t)
	defer srv.close()

	host, port, err := net.SplitHostPort(srv.addr())
	if err != nil {
		t.Fatalf("failed to split fake SMTP server address: %v", err)
	}
	t.Setenv("SMTP_HOST", host)
	t.Setenv("SMTP_PORT", port)

	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)

	page := models.StatusPage{Name: "HTML Escaping Page", Slug: "html-escaping-page"}
	db.Create(&page)

	comp := models.Component{StatusPageID: page.ID, Name: "<b>API</b>"}
	db.Create(&comp)

	maliciousTitle := "<script>alert(1)</script>"
	inc := models.Incident{
		StatusPageID: page.ID,
		Title:        maliciousTitle,
		Message:      "Investigating <img src=x onerror=alert(2)>.",
		Status:       "investigating",
	}
	db.Create(&inc)

	// Global Admin so the tenant-scoped user query still selects
	// this recipient regardless of role assignment on this specific page.
	admin := models.User{Email: "html-escape-admin@example.com", IsGlobalAdmin: true}
	db.Create(&admin)

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	select {
	case msg := <-srv.dataCh:
		// The raw title legitimately appears in the Subject header (a plain-text
		// SMTP header, not HTML - CR/LF-guarded separately via
		// hasHeaderInjectionChars), so only the HTML body (after the blank line
		// separating headers from body) must be free of unescaped markup.
		headerBodySplit := strings.SplitN(msg, "\n\n", 2)
		if len(headerBodySplit) != 2 {
			t.Fatalf("expected the composed email to have a header/body separator, got:\n%s", msg)
		}
		htmlBody := headerBodySplit[1]

		if strings.Contains(htmlBody, "<script>alert(1)</script>") {
			t.Fatalf("expected the incident title to be HTML-escaped in the email body, but found raw <script> markup:\n%s", htmlBody)
		}
		if !strings.Contains(htmlBody, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Fatalf("expected the escaped form &lt;script&gt;...&lt;/script&gt; in the email body, got:\n%s", htmlBody)
		}
		if strings.Contains(htmlBody, "<img src=x onerror=alert(2)>") {
			t.Fatalf("expected the incident message to be HTML-escaped in the email body, but found raw <img> markup:\n%s", htmlBody)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected the fake SMTP server to receive an email within the timeout")
	}
}

// TestDispatchNotifications_FromHeaderIsValid covers the bug where sendAlertEmail
// wrapped the already-fully-formed SMTP_FROM value (e.g. "Statushafen
// <notifications@example.com>") in an additional hardcoded "Legacy Sender <...>"
// display name, producing a doubly-nested, RFC 5322-invalid From header like
// "From: Legacy Sender <Statushafen <notifications@example.com>>". Several
// receiving mail providers (observed with Outlook.com) silently discarded
// messages with this malformed header instead of bouncing them, so the SMTP
// transaction looked successful in logs while no email ever arrived.
func TestDispatchNotifications_FromHeaderIsValid(t *testing.T) {
	srv := startFakeSMTPServer(t)
	defer srv.close()

	host, port, err := net.SplitHostPort(srv.addr())
	if err != nil {
		t.Fatalf("failed to split fake SMTP server address: %v", err)
	}
	t.Setenv("SMTP_HOST", host)
	t.Setenv("SMTP_PORT", port)
	t.Setenv("SMTP_FROM", "Global Default <alerts@statushafen.local>")

	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)

	// Die Seitenadresse wirkt nur als Reply-To; der From-Header besteht aus dem
	// Seitennamen und der Adresse aus SMTP_FROM. Dieser Test sichert, dass dabei
	// keine verschachtelten spitzen Klammern entstehen.
	page := models.StatusPage{
		Name:                    "From Header Page",
		Slug:                    "from-header-page",
		NotificationFromAddress: "Statushafen <notifications@example.com>",
	}
	db.Create(&page)

	comp := models.Component{StatusPageID: page.ID, Name: "API"}
	db.Create(&comp)

	inc := models.Incident{
		StatusPageID: page.ID,
		Title:        "Test Incident",
		Message:      "Investigating.",
		Status:       "investigating",
	}
	db.Create(&inc)

	admin := models.User{Email: "from-header-admin@example.com", IsGlobalAdmin: true}
	db.Create(&admin)

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	select {
	case msg := <-srv.dataCh:
		wantFromLine := `From: "From Header Page" <alerts@statushafen.local>`
		if !strings.Contains(msg, wantFromLine) {
			t.Fatalf("expected From header %q, got composed message:\n%s", wantFromLine, msg)
		}
		if strings.Contains(msg, "Legacy Sender") {
			t.Fatalf("expected no leftover hardcoded display name in From header, got:\n%s", msg)
		}
		if strings.Contains(msg, "<Statushafen <notifications@example.com>>") {
			t.Fatalf("expected From header without nested angle brackets, got:\n%s", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected the fake SMTP server to receive an email within the timeout")
	}
}
