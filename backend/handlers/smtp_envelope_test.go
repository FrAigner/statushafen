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

// Ende-zu-Ende-Tests für alle drei smtp.SendMail-Aufrufstellen
// (Bestell-Bestätigung, Abo-Bestätigung, Incident-/Notice-Benachrichtigung
// über sendAlertEmail). Der Fake-SMTP-Server lehnt wie Brevo jedes MAIL FROM
// mit Anzeigenamen per 501 ab; kommt die Mail an, war der Envelope-Absender
// also die nackte Adresse. Zusätzlich muss der From:-Header den Anzeigenamen
// unverändert behalten.

// displayNameFrom enthält Umlaute, ein Komma und ein Ampersand im
// Anzeigenamen, damit auch die quotierte Form durch mail.ParseAddress läuft.
const (
	displayNameFrom   = `"Statushafen Störungsdienst, Müller & Co" <no-reply@example.com>`
	displayNameBareTo = "no-reply@example.com"
)

// awaitMailFrom liefert das vom Fake-Server akzeptierte MAIL-FROM-Argument.
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

// assertEnvelopeAndHeader prüft Envelope-Absender (nackt) und From:-Header
// (mit Anzeigename) einer zugestellten Mail.
func assertEnvelopeAndHeader(t *testing.T, srv *fakeSMTPServer, msg, wantHeaderFrom, wantEnvelope string) {
	t.Helper()
	if got := awaitMailFrom(t, srv); got != wantEnvelope {
		t.Errorf("MAIL FROM = %q, want bare address %q", got, wantEnvelope)
	}
	if !strings.Contains(msg, "From: "+wantHeaderFrom+"\n") {
		t.Errorf("expected From header %q to keep the display name, got:\n%s", wantHeaderFrom, msg)
	}
}

// assertSenderIsOperator prüft für Mails einer Statusseite: Envelope-Absender
// und Adresse im From-Header sind die nackte Betreiberadresse, der Seitenname
// steht als (gegebenenfalls kodierter) Anzeigename im Header.
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
	// Seitenname mit Umlauten: wird als Anzeigename RFC-2047-kodiert.
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

// TestSMTPEnvelope_BareAddressInputStaysBare: Ist SMTP_FROM schon die nackte
// Adresse (aktueller Prod-Workaround), bleiben Envelope und Header gleich.
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

// TestSMTPEnvelope_InvalidFromIsRejectedAndAudited (Fehlerpfad): Ein nicht
// parsebarer Absender fällt unverändert durch, der strikte Server lehnt ihn
// mit 501 ab und der Fehler landet wie jeder andere Versandfehler im Audit-Log
// statt still zu verschwinden.
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
