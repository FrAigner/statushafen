package handlers_test

import (
	"net/http"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// End-to-end tests for all three smtp.SendMail call sites
// (order confirmation, subscription confirmation, incident/notice notification
// via sendAlertEmail). Like Brevo, the fake SMTP server rejects every MAIL FROM
// with a display name via 501; if the mail arrives, the envelope sender
// was therefore the bare address. In addition, the From: header must keep the display name
// unchanged.

// displayNameFrom contains umlauts, a comma and an ampersand in the
// display name, so that the quoted form also goes through mail.ParseAddress.
const (
	displayNameFrom   = `"Statushafen Störungsdienst, Müller & Co" <no-reply@example.com>`
	displayNameBareTo = "no-reply@example.com"
)

// awaitMailFrom returns the MAIL FROM argument accepted by the fake server.
func awaitMailFrom(t *testing.T, srv *fakeSMTPServer) string {
	t.Helper()
	select {
	case from := <-srv.mailFromCh:
		return from
	case <-time.After(3 * time.Second):
		t.Fatal("expected the fake SMTP server to accept a MAIL FROM within the timeout")
		return ""
	}
}

// assertEnvelopeAndHeader checks the envelope sender (bare) and From: header
// (with display name) of a delivered mail.
func assertEnvelopeAndHeader(t *testing.T, srv *fakeSMTPServer, msg, wantHeaderFrom, wantEnvelope string) {
	t.Helper()
	if got := awaitMailFrom(t, srv); got != wantEnvelope {
		t.Errorf("MAIL FROM = %q, want bare address %q", got, wantEnvelope)
	}
	if !strings.Contains(msg, "From: "+wantHeaderFrom+"\n") {
		t.Errorf("expected From header %q to keep the display name, got:\n%s", wantHeaderFrom, msg)
	}
}

// assertSenderIsOperator checks for mails of a status page: the envelope sender
// and the address in the From header are the bare operator address, the page name
// appears as the (possibly encoded) display name in the header.
func assertSenderIsOperator(t *testing.T, srv *fakeSMTPServer, msg, wantName, wantAddress string) {
	t.Helper()
	if got := awaitMailFrom(t, srv); got != wantAddress {
		t.Errorf("MAIL FROM = %q, want bare operator address %q", got, wantAddress)
	}
	from, err := mail.ParseAddress(headerLine(msg, "From"))
	if err != nil {
		t.Fatalf("From header not parsable: %v\n%s", err, msg)
	}
	if from.Address != wantAddress || from.Name != wantName {
		t.Errorf("From = %q <%s>, want %q <%s>", from.Name, from.Address, wantName, wantAddress)
	}
}

func TestSMTPEnvelope_OrderConfirmationUsesBareAddress(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("SMTP_FROM", displayNameFrom)

	h := handlers.NewHandler(testutils.SetupTestDB())
	r := setupOrderRouter(h)

	w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
		"company_name": "Envelope GmbH",
		"email":        "envelope-order@example.com",
	})
	requireStatusCode(t, w, 201)

	msg := awaitSMTPMessage(t, srv)
	assertEnvelopeAndHeader(t, srv, msg, displayNameFrom, displayNameBareTo)
}

func TestSMTPEnvelope_SubscriptionConfirmationUsesBareAddress(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("SMTP_FROM", displayNameFrom)
	db, r, page := setupDOIRouter(t)
	// Page name with umlauts: is RFC 2047-encoded as the display name.
	db.Model(&page).Update("name", "Störungs-Info Ä Ö Ü")

	w := subscribeEmail(r, "envelope-sub@example.com", nil)
	requireStatusCode(t, w, http.StatusOK)

	msg := awaitSMTPMessage(t, srv)
	assertSenderIsOperator(t, srv, msg, "Störungs-Info Ä Ö Ü", displayNameBareTo)
}

func TestSMTPEnvelope_IncidentNotificationUsesBareAddress(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()

	t.Setenv("SMTP_FROM", displayNameFrom)
	db, h, comp, inc := setupDispatchFixturesForPage(models.StatusPage{
		Name: "Envelope Page", Slug: "envelope-page", NotificationFromAddress: "Acme Status <status@acme.example>",
	})
	admin := models.User{Email: "envelope-admin@example.com", IsGlobalAdmin: true}
	db.Create(&admin)

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessage(t, srv)
	assertSenderIsOperator(t, srv, msg, "Envelope Page", displayNameBareTo)
}

// TestSMTPEnvelope_BareAddressInputStaysBare: if SMTP_FROM is already the bare
// address (current production workaround), envelope and header stay the same.
func TestSMTPEnvelope_BareAddressInputStaysBare(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("SMTP_FROM", displayNameBareTo)

	h := handlers.NewHandler(testutils.SetupTestDB())
	r := setupOrderRouter(h)

	w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
		"company_name": "Bare GmbH",
		"email":        "envelope-bare@example.com",
	})
	requireStatusCode(t, w, 201)

	msg := awaitSMTPMessage(t, srv)
	assertEnvelopeAndHeader(t, srv, msg, displayNameBareTo, displayNameBareTo)
}

// TestSMTPEnvelope_InvalidFromIsRejectedAndAudited (error path): an
// unparsable sender passes through unchanged, the strict server rejects it
// with 501, and the error ends up in the audit log like any other send error
// instead of silently disappearing.
func TestSMTPEnvelope_InvalidFromIsRejectedAndAudited(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("SMTP_FROM", "Statushafen <kein-at-zeichen>")

	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupOrderRouter(h)

	w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
		"company_name": "Invalid From GmbH",
		"email":        "envelope-invalid@example.com",
	})
	requireStatusCode(t, w, 201)

	expectNoSMTPMessage(t, srv)
	deadline := time.Now().Add(3 * time.Second)
	for auditCountFor(db, "pageorder.email_failed") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("expected a pageorder.email_failed audit entry for the rejected MAIL FROM")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var entry models.AuditLog
	db.Where("action = ?", "pageorder.email_failed").First(&entry)
	if !strings.Contains(entry.Details, "501") {
		t.Errorf("expected the audit entry to carry the 501 error, got %q", entry.Details)
	}
}
