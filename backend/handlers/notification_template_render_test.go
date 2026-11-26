package handlers_test

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"gorm.io/gorm"
)

// Regression tests for the template engine.
//
// Testmatrix:
//   - The default renderer is used when no status page has its own
//     template (existing behavior unchanged).
//   - A custom template is used when sending as soon as it has been created.
//   - A custom template with CR/LF in the subject is
//     rejected by ValidateNotificationTemplate.
// - Placeholders continue to be HTML-escaped (no injection regression).

// setupNotificationDispatch bundles the four boilerplate steps that
// all DispatchNotifications tests need: start a fake SMTP server and
// point SMTP_HOST/SMTP_PORT at it, create a test DB including a status page,
// component and global admin recipient, and wire the handler to
// this DB. It deliberately lives here instead of in testutils because
// only the fake SMTP server from the handlers_test package is visible (it
// is defined by notification_html_escaping_test.go).
// startFakeSMTPWithEnv starts the fake SMTP server, wires up
// t.Cleanup for closing it and exports host/port as the SMTP_HOST/
// SMTP_PORT env vars. Shared by incident AND notice dispatch tests
// (avoids SonarCloud duplication findings).
func startFakeSMTPWithEnv(t *testing.T) *fakeSMTPServer {
	t.Helper()
	srv := startFakeSMTPServer(t)
	t.Cleanup(srv.close)
	host, port, err := net.SplitHostPort(srv.addr())
	if err != nil {
		t.Fatalf("failed to split fake SMTP server address: %v", err)
	}
	t.Setenv("SMTP_HOST", host)
	t.Setenv("SMTP_PORT", port)
	return srv
}

func setupNotificationDispatch(t *testing.T, slug, adminEmail string) (*fakeSMTPServer, *handlers.Handler, *gorm.DB, models.StatusPage, models.Component) {
	t.Helper()
	srv := startFakeSMTPWithEnv(t)

	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "Tpl " + slug, Slug: slug}
	if err := db.Create(&page).Error; err != nil {
		t.Fatalf("create page: %v", err)
	}
	comp := models.Component{StatusPageID: page.ID, Name: "API"}
	if err := db.Create(&comp).Error; err != nil {
		t.Fatalf("create component: %v", err)
	}
	admin := models.User{Email: adminEmail, IsGlobalAdmin: true}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}

	return srv, handlers.NewHandler(db), db, page, comp
}

// seedTemplate/seedIncident each wrap one db.Create call plus
// t.Fatalf error handling. Without these helpers every test repeated the
// four lines of struct literal plus error check, which SonarCloud correctly
// flagged as new-code duplication.
func seedTemplate(t *testing.T, db *gorm.DB, pageID uint, kind, audience, lang, subject, body string) {
	t.Helper()
	tmpl := models.NotificationTemplate{StatusPageID: pageID, Kind: kind, Audience: audience, Language: lang, Subject: subject, BodyHTML: body}
	if err := db.Create(&tmpl).Error; err != nil {
		t.Fatalf("create template (%s/%s/%s): %v", kind, audience, lang, err)
	}
}

func seedIncident(t *testing.T, db *gorm.DB, pageID uint, title, message, status string) models.Incident {
	t.Helper()
	inc := models.Incident{StatusPageID: pageID, Title: title, Message: message, Status: status}
	if err := db.Create(&inc).Error; err != nil {
		t.Fatalf("create incident %q: %v", title, err)
	}
	return inc
}

// TestDispatchNotifications_CustomTemplate_IsUsed proves the core requirement:
// if a NotificationTemplate exists for (status page, kind, language),
// its rendered subject/body must appear in the mail body that is
// actually sent - no longer the
// hard-coded default text.
func TestDispatchNotifications_CustomTemplate_IsUsed(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "custom-tpl-page", "custom-tpl-admin@example.com")

	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceStaff, "en",
		"CUSTOM SUBJECT :: {{.IncidentTitle}}",
		`<p>CUSTOM BODY :: {{.IncidentTitle}} :: {{.Status}} :: {{.ComponentNames}} :: {{.Message}}</p>`)
	inc := seedIncident(t, db, page.ID, "Latency spike", "Investigating right now.", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := waitForEmail(t, srv)
	if !strings.Contains(msg, "Subject: CUSTOM SUBJECT :: Latency spike") {
		t.Fatalf("expected custom subject in message, got:\n%s", msg)
	}
	if !strings.Contains(msg, "CUSTOM BODY :: Latency spike :: investigating :: API :: Investigating right now.") {
		t.Fatalf("expected custom body with placeholders substituted, got:\n%s", msg)
	}
	if strings.Contains(msg, "Statushafen") {
		// The default body contains "Statushafen" as a header - if it appears,
		// the custom template was not rendered.
		t.Fatalf("expected the default template NOT to be used when a custom template exists, got:\n%s", msg)
	}
}

// TestDispatchNotifications_NoCustomTemplate_FallsBackToDefault
// ensures that status pages without their own NotificationTemplate still
// receive exactly the default body - which is already
// comprehensively covered by the existing test suite. If this
// test fails as a regression, the behavior is unintentionally changing
// for all existing customers.
func TestDispatchNotifications_NoCustomTemplate_FallsBackToDefault(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "default-tpl-page", "default-tpl-admin@example.com")

	inc := seedIncident(t, db, page.ID, "Default path incident", "No template configured.", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := waitForEmail(t, srv)
	// The default body contains the "Statushafen" header and the default
	// subject "[ALERT] ..." - both must still arrive.
	if !strings.Contains(msg, "Subject: [ALERT] API: Default path incident (investigating)") {
		t.Fatalf("expected default subject, got:\n%s", msg)
	}
	if !strings.Contains(msg, "Statushafen") {
		t.Fatalf("expected default template Header 'Statushafen' in body, got:\n%s", msg)
	}
}

// TestValidateNotificationTemplate_RejectsHeaderInjection covers the
// security extension: a template with CR/LF in the subject
// must be rejected before it even gets into the DB. The body may
// contain CR/LF, but is still checked by sendAlertEmail at send time
// (defense in depth).
func TestValidateNotificationTemplate_RejectsHeaderInjection(t *testing.T) {
	if err := handlers.ValidateNotificationTemplate("Hello\r\nBcc: attacker@example.com", "<p>Body</p>"); err == nil {
		t.Fatal("expected validation to reject a subject containing CR/LF, got nil error")
	}
	if err := handlers.ValidateNotificationTemplate("Hello\nBcc: attacker@example.com", "<p>Body</p>"); err == nil {
		t.Fatal("expected validation to reject a subject containing LF, got nil error")
	}
	if err := handlers.ValidateNotificationTemplate("Valid subject", "<p>Body</p>"); err != nil {
		t.Fatalf("expected valid template to pass, got: %v", err)
	}
	if err := handlers.ValidateNotificationTemplate("Subject with {{.Broken", "<p>Body</p>"); err == nil {
		t.Fatal("expected an unbalanced action expression to be rejected, got nil error")
	}
	if err := handlers.ValidateNotificationTemplate("", "<p>Body</p>"); err == nil {
		t.Fatal("expected empty subject to be rejected, got nil error")
	}
	if err := handlers.ValidateNotificationTemplate("Subject", ""); err == nil {
		t.Fatal("expected empty body to be rejected, got nil error")
	}
}

// TestDispatchNotifications_CustomTemplate_EscapesPlaceholders secures the
// escaping guarantee on the custom template path: html/template must
// automatically escape user-controlled field values - an incident title with
// <script> must not appear as raw markup in the body after rendering.
func TestDispatchNotifications_CustomTemplate_EscapesPlaceholders(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "escape-tpl-page", "escape-tpl-admin@example.com")

	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceStaff, "en",
		"Subject",
		`<p>Title: {{.IncidentTitle}} - Message: {{.Message}}</p>`)
	inc := seedIncident(t, db, page.ID, "<script>alert(1)</script>", "<img src=x onerror=alert(2)>", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := waitForEmail(t, srv)
	if strings.Contains(msg, "<script>alert(1)</script>") {
		t.Fatalf("expected html/template to escape <script>, got:\n%s", msg)
	}
	if strings.Contains(msg, "<img src=x onerror=alert(2)>") {
		t.Fatalf("expected html/template to escape <img>, got:\n%s", msg)
	}
	// html/template replaces < with &lt; etc. - one of the escaped forms must
	// appear in the body, to make clear that the title was rendered at all
	// and not filtered out completely.
	if !strings.Contains(msg, "&lt;script&gt;") {
		t.Fatalf("expected the escaped form &lt;script&gt; in the body, got:\n%s", msg)
	}
}

// TestDispatchNotifications_CustomTemplate_ResolvedKindIsUsed checks the
// action->kind mapping: an UPDATED dispatch with status "resolved" must
// pick up the "incident_resolved" template, not the "incident_updated"
// template. This is the prerequisite for separately editable
// resolution mails.
func TestDispatchNotifications_CustomTemplate_ResolvedKindIsUsed(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "resolved-tpl-page", "resolved-tpl-admin@example.com")

	// Create both templates - the test must show that exactly the
	// resolved template applies, not the updated template.
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentUpdated, handlers.NotificationAudienceStaff, "en", "UPDATED-KIND", "<p>UPDATED-KIND-BODY</p>")
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentResolved, handlers.NotificationAudienceStaff, "en", "RESOLVED-KIND", "<p>RESOLVED-KIND-BODY</p>")
	inc := seedIncident(t, db, page.ID, "Incident to resolve", "Fixed.", "resolved")

	h.DispatchNotifications(inc, []models.Component{comp}, "UPDATED")

	msg := waitForEmail(t, srv)
	if !strings.Contains(msg, "RESOLVED-KIND-BODY") {
		t.Fatalf("expected the resolved-kind template body in message, got:\n%s", msg)
	}
	if strings.Contains(msg, "UPDATED-KIND-BODY") {
		t.Fatalf("expected the updated-kind template NOT to be used for a resolved status, got:\n%s", msg)
	}
}

// waitForEmail selects on the fake SMTP channel with a timeout, to keep the
// boilerplate in the tests above small.
func waitForEmail(t *testing.T, srv *fakeSMTPServer) string {
	t.Helper()
	select {
	case msg := <-srv.dataCh:
		return msg
	case <-time.After(3 * time.Second):
		t.Fatal("expected the fake SMTP server to receive an email within the timeout")
		return ""
	}
}

// Regression test finding 10: ValidateNotificationTemplate allows
// CR/LF in the body, but sendAlertEmail discarded every body with CR/LF: a
// multi-line template or a multi-line {{.Message}} silently dropped all of the
// page's mails. The body is now folded into one line; the guard
// for subject and sender remains (see the header injection tests).
func TestDispatchNotifications_MultilineTemplateAndMessageStillSent(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "multiline-tpl-page", "multiline-tpl-admin@example.com")

	body := "<p>BODY ::\r\n{{.IncidentTitle}}</p>\n<p>{{.Message}}</p>"
	if err := handlers.ValidateNotificationTemplate("S :: {{.IncidentTitle}}", body); err != nil {
		t.Fatalf("a multi-line body must remain valid: %v", err)
	}
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceStaff, "en",
		"S :: {{.IncidentTitle}}", body)
	inc := seedIncident(t, db, page.ID, "Latency spike", "Zeile eins\nZeile zwei\r\nZeile drei", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := waitForEmail(t, srv)
	_, mailBody, found := strings.Cut(strings.ReplaceAll(msg, "\r\n", "\n"), "\n\n")
	if !found {
		t.Fatalf("no body separator in message:\n%s", msg)
	}
	mailBody = strings.TrimSpace(mailBody)
	if !strings.Contains(mailBody, "Zeile eins") || !strings.Contains(mailBody, "Zeile drei") || !strings.Contains(mailBody, "BODY ::") {
		t.Fatalf("expected the multi-line template and message to be delivered, got:\n%s", msg)
	}
	if strings.ContainsAny(mailBody, "\r\n") {
		t.Fatalf("expected the body folded to one line, got:\n%q", mailBody)
	}
}
