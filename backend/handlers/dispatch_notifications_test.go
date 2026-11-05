package handlers_test

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"gorm.io/gorm"
)

// setupDispatchFixturesForPage baut das für alle DispatchNotifications-Tests
// identische Grundgerüst auf (DB, Handler, Component, Incident) für eine vom
// Aufrufer bereits vorbereitete StatusPage (z.B. mit IsPrivate oder
// NotificationFromAddress gesetzt) - die Tests unterscheiden sich sonst nur
// in den angehängten Subscribern.
func setupDispatchFixturesForPage(page models.StatusPage) (db *gorm.DB, h *handlers.Handler, comp models.Component, inc models.Incident) {
	db = testutils.SetupTestDB()
	h = handlers.NewHandler(db)

	db.Create(&page)

	comp = models.Component{StatusPageID: page.ID, Name: "API"}
	db.Create(&comp)

	inc = models.Incident{
		StatusPageID: page.ID,
		Title:        "API Outage",
		Message:      "Investigating elevated error rates.",
		Status:       "investigating",
	}
	db.Create(&inc)

	return db, h, comp, inc
}

// setupDispatchFixtures ist der Standardfall von setupDispatchFixturesForPage
// mit einer schlichten (weder privaten noch mandanten-spezifisch
// konfigurierten) StatusPage.
func setupDispatchFixtures(pageName, slug string) (db *gorm.DB, h *handlers.Handler, comp models.Component, inc models.Incident) {
	return setupDispatchFixturesForPage(models.StatusPage{Name: pageName, Slug: slug})
}

// TestDispatchNotifications_GoogleChatSubscriberReceivesWebhook:
// a Subscriber with Channel="google_chat" must be notified via HTTP POST to its
// own WebhookURL, not (only) via email. Previously DispatchNotifications ignored
// Channel/WebhookURL entirely and always tried the SMTP email path.
func TestDispatchNotifications_GoogleChatSubscriberReceivesWebhook(t *testing.T) {
	handlers.SetWebhookLoopbackAllowedForTests(t.Cleanup)
	var mu sync.Mutex
	var receivedBodies [][]byte
	receivedCh := make(chan struct{}, 10)

	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST to subscriber webhook, got %s", r.Method)
		}
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)

		mu.Lock()
		receivedBodies = append(receivedBodies, body)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
		receivedCh <- struct{}{}
	}))
	defer webhookServer.Close()

	db, h, comp, inc := setupDispatchFixtures("Webhook Dispatch Page", "webhook-dispatch-page")

	// Subscriber configured for the google_chat channel: notification MUST go to
	// WebhookURL via HTTP POST.
	chatSub := models.Subscriber{
		StatusPageID:           comp.StatusPageID,
		Channel:                "google_chat",
		WebhookURL:             webhookServer.URL,
		SubscribedComponentIDs: "all",
	}
	db.Create(&chatSub)

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	select {
	case <-receivedCh:
		// good, webhook was called
	case <-time.After(3 * time.Second):
		t.Fatal("expected subscriber webhook to receive a POST request, but it did not within the timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(receivedBodies) != 1 {
		t.Fatalf("expected exactly 1 webhook call, got %d", len(receivedBodies))
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(receivedBodies[0], &payload); err != nil {
		t.Fatalf("failed to decode webhook payload: %v", err)
	}
	text, ok := payload["text"].(string)
	if !ok || text == "" {
		t.Fatalf("expected webhook payload to contain a non-empty 'text' field, got: %#v", payload)
	}
}

// TestDispatchNotifications_EmailSubscriberDoesNotTriggerWebhook is the regression
// guard for the existing email path: a Channel="email" subscriber must NOT cause
// any HTTP POST to be made (i.e. it must not fall into the webhook branch), it
// should only go through the SMTP email path as before.
func TestDispatchNotifications_EmailSubscriberDoesNotTriggerWebhook(t *testing.T) {
	requestReceived := make(chan struct{}, 10)
	sentinelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer sentinelServer.Close()

	db, h, comp, inc := setupDispatchFixtures("Email Dispatch Page", "email-dispatch-page")

	// Email subscriber has no WebhookURL at all - if the dispatcher incorrectly
	// took the webhook branch for it, sub.WebhookURL would be empty and no
	// request would ever reach sentinelServer, so this alone doesn't prove much.
	// The real regression guard is combined with the google_chat test above:
	// this test just asserts the email subscriber path completes without
	// attempting any HTTP POST.
	emailSub := models.Subscriber{
		StatusPageID:           comp.StatusPageID,
		Channel:                "email",
		Email:                  "subscriber@example.com",
		SubscribedComponentIDs: "all",
	}
	db.Create(&emailSub)

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	select {
	case <-requestReceived:
		t.Fatal("email-channel subscriber unexpectedly triggered an HTTP POST (webhook path)")
	case <-time.After(500 * time.Millisecond):
		// expected: no HTTP call was made for the email channel
	}
}

// TestDispatchNotifications_MixedChannelsRouteIndependently ensures that when
// both an email and a google_chat subscriber exist for the same incident, only
// the google_chat subscriber's WebhookURL receives the HTTP POST.
func TestDispatchNotifications_MixedChannelsRouteIndependently(t *testing.T) {
	handlers.SetWebhookLoopbackAllowedForTests(t.Cleanup)
	receivedCh := make(chan *http.Request, 10)
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCh <- r
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	db, h, comp, inc := setupDispatchFixtures("Mixed Dispatch Page", "mixed-dispatch-page")

	emailSub := models.Subscriber{
		StatusPageID:           comp.StatusPageID,
		Channel:                "email",
		Email:                  "subscriber@example.com",
		SubscribedComponentIDs: "all",
	}
	db.Create(&emailSub)

	chatSub := models.Subscriber{
		StatusPageID:           comp.StatusPageID,
		Channel:                "google_chat",
		WebhookURL:             webhookServer.URL,
		SubscribedComponentIDs: "all",
	}
	db.Create(&chatSub)

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	select {
	case <-receivedCh:
		// expected exactly one webhook call from the google_chat subscriber
	case <-time.After(3 * time.Second):
		t.Fatal("expected the google_chat subscriber's webhook to be called")
	}

	// Make sure only one call arrived (not one per subscriber).
	select {
	case extra := <-receivedCh:
		t.Fatalf("expected exactly one webhook call, got an extra one: %+v", extra)
	case <-time.After(300 * time.Millisecond):
		// good, no extra calls
	}
}

// TestDispatchNotifications_SubscriberIsolatedByStatusPage:
// a Subscriber with SubscribedComponentIDs="all" on Status Page A must NOT be
// notified about an incident on Status Page B, even though the old
// "all"-check (SubscriberHasAnyComponent) had no page-scoping of its own.
func TestDispatchNotifications_SubscriberIsolatedByStatusPage(t *testing.T) {
	receivedCh := make(chan struct{}, 10)
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCh <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)

	pageA := models.StatusPage{Name: "Page A", Slug: "page-a-sub-isolation"}
	db.Create(&pageA)
	pageB := models.StatusPage{Name: "Page B", Slug: "page-b-sub-isolation"}
	db.Create(&pageB)

	// Subscriber lives on Page A with "all" components subscribed.
	subA := models.Subscriber{
		StatusPageID:           pageA.ID,
		Channel:                "google_chat",
		WebhookURL:             webhookServer.URL,
		SubscribedComponentIDs: "all",
	}
	db.Create(&subA)

	// Incident happens on Page B - subA must NOT be notified.
	compB := models.Component{StatusPageID: pageB.ID, Name: "API"}
	db.Create(&compB)
	incB := models.Incident{
		StatusPageID: pageB.ID,
		Title:        "Page B Outage",
		Message:      "Confidential to Page B.",
		Status:       "investigating",
	}
	db.Create(&incB)

	h.DispatchNotifications(incB, []models.Component{compB}, "CREATED")

	select {
	case <-receivedCh:
		t.Fatal("subscriber on Page A must not receive a notification for an incident on Page B")
	case <-time.After(500 * time.Millisecond):
		// expected: no cross-tenant webhook call
	}
}

// startFakeSMTPServerWithEnv starts a fake SMTP server and points
// SMTP_HOST/SMTP_PORT at it via t.Setenv, returning the server so callers can
// assert on its captured dataCh. Shared by the subscriber-link and
// NotificationFromAddress tests below to avoid duplicating the
// host/port-splitting boilerplate.
func startFakeSMTPServerWithEnv(t *testing.T) *fakeSMTPServer {
	t.Helper()
	srv := startFakeSMTPServer(t)
	host, port, err := net.SplitHostPort(srv.addr())
	if err != nil {
		srv.close()
		t.Fatalf("failed to split fake SMTP server address: %v", err)
	}
	t.Setenv("SMTP_HOST", host)
	t.Setenv("SMTP_PORT", port)
	return srv
}

// awaitSMTPMessage waits for the fake SMTP server to receive a composed
// message body, failing the test if none arrives within the timeout.
func awaitSMTPMessage(t *testing.T, srv *fakeSMTPServer) string {
	t.Helper()
	select {
	case msg := <-srv.dataCh:
		return msg
	case <-time.After(3 * time.Second):
		t.Fatal("expected the fake SMTP server to receive an email within the timeout")
		return ""
	}
}

// TestDispatchNotifications_SubscriberEmailContainsUnsubscribeAndAccessLinks
// covers a public-subscriber notification email must contain a
// working unsubscribe link, and for a PRIVATE status page the "view status
// page" link must carry a subscriber access token (otherwise a subscriber
// without a user account could never open the linked page, since private
// pages require login).
func TestDispatchNotifications_SubscriberEmailContainsUnsubscribeAndAccessLinks(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("FRONTEND_URL", "https://status.example.test")

	db, h, comp, inc := setupDispatchFixturesForPage(models.StatusPage{Name: "Private Sub Page", Slug: "private-sub-page", IsPrivate: true})

	// Seit bekommen Abos privater Seiten nur Mails,
	// wenn ihr Ersteller die Seite sehen darf. Der Global Admin erhält
	// zusätzlich die interne Mail, daher nach Empfänger filtern.
	creator := models.User{Email: "creator@example.com", IsGlobalAdmin: true}
	db.Create(&creator)
	sub := models.Subscriber{StatusPageID: comp.StatusPageID, Channel: "email", Email: "sub@example.com", SubscribedComponentIDs: "all", CreatedByUserID: &creator.ID}
	db.Create(&sub)

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessageTo(t, srv, "sub@example.com")
	if !strings.Contains(msg, "https://status.example.test/unsubscribe?token=") {
		t.Fatalf("expected an unsubscribe link with a token in the email, got:\n%s", msg)
	}
	if !strings.Contains(msg, "https://status.example.test/status/private-sub-page?token=") {
		t.Fatalf("expected the 'view status page' link to point at the slug with an access token (private page), got:\n%s", msg)
	}
}

// TestDispatchNotifications_PublicPageSubscriberLinkHasNoToken is the
// regression guard for a PUBLIC status page's "view status page"
// link must stay exactly as before (no token needed, since the page is
// already accessible without login) - only the new unsubscribe link is added.
func TestDispatchNotifications_PublicPageSubscriberLinkHasNoToken(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("FRONTEND_URL", "https://status.example.test")

	db, h, comp, inc := setupDispatchFixtures("Public Sub Page", "public-sub-page")

	sub := models.Subscriber{StatusPageID: comp.StatusPageID, Channel: "email", Email: "sub@example.com", SubscribedComponentIDs: "all"}
	db.Create(&sub)

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessage(t, srv)
	if !strings.Contains(msg, "https://status.example.test/unsubscribe?token=") {
		t.Fatalf("expected an unsubscribe link with a token in the email, got:\n%s", msg)
	}
	if !strings.Contains(msg, `href="https://status.example.test"`) {
		t.Fatalf("expected the unchanged plain frontend link (no slug, no token) for a public page, got:\n%s", msg)
	}
	if strings.Contains(msg, "/status/public-sub-page") {
		t.Fatalf("expected no slug-scoped access link for a public page, got:\n%s", msg)
	}
}

// TestDispatchNotifications_PageNotificationFromAddressIsReplyToOnly:
// die Adresse einer Statusseite (NotificationFromAddress) wirkt nur als
// Reply-To. Absenderadresse im From-Header ist die des Betreibers (SMTP_FROM),
// der Seitenname dient als Anzeigename. Früher ersetzte die Seitenadresse den
// Absender, womit Seiten-Admins Absender fälschen konnten.
func TestDispatchNotifications_PageNotificationFromAddressIsReplyToOnly(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("SMTP_FROM", "Global Default <alerts@statushafen.local>")

	db, h, comp, inc := setupDispatchFixturesForPage(models.StatusPage{
		Name:                    "Per-Page From Page",
		Slug:                    "per-page-from-page",
		NotificationFromAddress: "Acme Status <status@acme.example>",
	})

	admin := models.User{Email: "per-page-from-admin@example.com", IsGlobalAdmin: true}
	db.Create(&admin)

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessage(t, srv)
	from, err := mail.ParseAddress(headerLine(msg, "From"))
	if err != nil || from.Address != "alerts@statushafen.local" || from.Name != "Per-Page From Page" {
		t.Fatalf("expected operator address with page name as sender, got %q (%v) in:\n%s", headerLine(msg, "From"), err, msg)
	}
	assertReplyTo(t, msg, "status@acme.example")
}

// TestDispatchNotifications_UsesOperatorAddressWhenNoPageAddress:
// eine Statusseite ohne eigene Adresse sendet mit der Betreiberadresse
// (SMTP_FROM) und dem Seitennamen als Anzeigenamen, ohne Reply-To.
func TestDispatchNotifications_UsesOperatorAddressWhenNoPageAddress(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("SMTP_FROM", "Global Default <alerts@statushafen.local>")

	db, h, comp, inc := setupDispatchFixtures("No Override Page", "no-override-page")

	admin := models.User{Email: "no-override-admin@example.com", IsGlobalAdmin: true}
	db.Create(&admin)

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessage(t, srv)
	from, err := mail.ParseAddress(headerLine(msg, "From"))
	if err != nil || from.Address != "alerts@statushafen.local" || from.Name != "No Override Page" {
		t.Fatalf("expected operator address with page name as sender, got %q (%v) in:\n%s", headerLine(msg, "From"), err, msg)
	}
	if headerLine(msg, "Reply-To") != "" {
		t.Fatalf("expected no Reply-To without a page address, got:\n%s", msg)
	}
}

// TestDispatchNotifications_UserIsolatedByStatusPage: an
// internal user without any role on the affected Status Page (and without
// Global Admin) must not be sent an alert email, even if their
// AllowedCountries string happens to match the incident's Country. Since the
// test environment has no real SMTP server, sendAlertEmail's dial failure is
// recorded via RecordSystemAudit ("notification.email_failed") - this test
// uses that audit trail as the observable signal for "was this recipient
// even attempted".
func TestDispatchNotifications_UserIsolatedByStatusPage(t *testing.T) {
	// Force a fast, deterministic SMTP dial failure instead of depending on
	// whatever (if anything) is listening on the Mailpit default.
	t.Setenv("SMTP_HOST", "127.0.0.1")
	t.Setenv("SMTP_PORT", "1")

	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)

	role := models.Role{Name: "isolation-test-role", Permissions: "incidents:write"}
	db.Create(&role)

	pageA := models.StatusPage{Name: "Page A Users", Slug: "page-a-user-isolation"}
	db.Create(&pageA)
	pageB := models.StatusPage{Name: "Page B Users", Slug: "page-b-user-isolation"}
	db.Create(&pageB)

	// outsider has no role on Page B at all, but AllowedCountries matches the
	// incident's Country - under the old code this alone was enough to
	// receive the notification.
	outsider := models.User{Email: "outsider@example.com", RoleID: role.ID}
	db.Create(&outsider)

	// insider has an explicit role on Page B and must be notified.
	insider := models.User{Email: "insider@example.com", RoleID: role.ID}
	db.Create(&insider)
	db.Create(&models.UserStatusPageRole{UserID: insider.ID, StatusPageID: pageB.ID, RoleID: role.ID})

	compB := models.Component{StatusPageID: pageB.ID, Name: "API"}
	db.Create(&compB)
	incB := models.Incident{
		StatusPageID: pageB.ID,
		Title:        "Page B Outage",
		Message:      "Confidential to Page B.",
		Status:       "investigating",
	}
	db.Create(&incB)

	h.DispatchNotifications(incB, []models.Component{compB}, "CREATED")

	// Poll the audit log (async dispatch) until the insider's attempted send
	// shows up, then assert the outsider's email never appears anywhere.
	deadline := time.Now().Add(3 * time.Second)
	var logs []models.AuditLog
	for time.Now().Before(deadline) {
		db.Where("action = ?", "notification.email_failed").Find(&logs)
		found := false
		for _, l := range logs {
			if strings.Contains(l.Details, insider.Email) {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	sawInsider := false
	for _, l := range logs {
		if strings.Contains(l.Details, insider.Email) {
			sawInsider = true
		}
		if strings.Contains(l.Details, outsider.Email) {
			t.Fatalf("user without any role on Page B must not have been attempted, but found audit entry: %+v", l)
		}
	}
	if !sawInsider {
		t.Fatal("expected the insider user (with a role on Page B) to have been attempted, but no matching audit entry was found")
	}
}
