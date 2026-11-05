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

// Regressionstests für die Template-Engine.
//
// Testmatrix:
//   - Default-Renderer wird verwendet, wenn keine Statusseite ein eigenes
//     Template hat (bestehendes Verhalten unverändert).
//   - Custom-Template wird beim Versand genutzt, sobald es angelegt ist.
//   - Ein Custom-Template mit CR/LF im Subject wird von
//     ValidateNotificationTemplate abgelehnt.
// - Platzhalter werden weiter HTML-escaped (kein Injection-Regress).

// setupNotificationDispatch bündelt die vier Boilerplate-Schritte, die
// alle DispatchNotifications-Tests brauchen: Fake-SMTP-Server starten +
// SMTP_HOST/SMTP_PORT auf ihn zeigen lassen, Test-DB inkl. Statusseite,
// Komponente und Global-Admin-Empfänger anlegen, und den Handler mit
// dieser DB verdrahten. Existiert bewusst hier statt in testutils, weil
// nur der Fake-SMTP-Server aus dem handlers_test-Paket sichtbar ist (er
// wird von notification_html_escaping_test.go definiert).
// startFakeSMTPWithEnv startet den Fake-SMTP-Server, verdrahtet den
// t.Cleanup fürs Schließen und exportiert Host/Port als SMTP_HOST/
// SMTP_PORT-Env-Vars. Wird von Incident- UND Notice-Dispatch-Tests
// geteilt (- SonarCloud Duplication-Vermeidung).
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

// seedTemplate/seedIncident kapseln je einen db.Create-Aufruf plus
// t.Fatalf-Fehlerbehandlung. Ohne diese Helper wiederholte jeder Test die
// vier Zeilen aus Struct-Literal + Fehlercheck, was SonarCloud korrekt
// als new-code-Duplikation markierte.
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

// TestDispatchNotifications_CustomTemplate_IsUsed weist die Kernanforderung
// nach: liegt für (Statusseite, Kind, Sprache) ein
// NotificationTemplate vor, muss dessen gerenderter Subject/Body im
// tatsächlich versendeten Mail-Body auftauchen - nicht mehr der
// hartkodierte Default-Text.
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
		// Der Default-Body enthält "Statushafen" als Header - taucht er auf,
		// wurde nicht das Custom-Template gerendert.
		t.Fatalf("expected the default template NOT to be used when a custom template exists, got:\n%s", msg)
	}
}

// TestDispatchNotifications_NoCustomTemplate_FallsBackToDefault stellt
// sicher, dass Statusseiten ohne eigenes NotificationTemplate weiterhin
// exakt den Default-Body erhalten - der von der bestehenden Test-Suite
// bereits umfassend abgedeckt ist. Wenn dieser
// Test regressive fehlschlägt, ändert sich unbeabsichtigt das Verhalten
// aller Bestandskunden.
func TestDispatchNotifications_NoCustomTemplate_FallsBackToDefault(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "default-tpl-page", "default-tpl-admin@example.com")

	inc := seedIncident(t, db, page.ID, "Default path incident", "No template configured.", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := waitForEmail(t, srv)
	// Der Default-Body enthält den "Statushafen"-Header und den Standard-
	// Subject "[ALERT] ..." - beides muss weiterhin ankommen.
	if !strings.Contains(msg, "Subject: [ALERT] API: Default path incident (investigating)") {
		t.Fatalf("expected default subject, got:\n%s", msg)
	}
	if !strings.Contains(msg, "Statushafen") {
		t.Fatalf("expected default template Header 'Statushafen' in body, got:\n%s", msg)
	}
}

// TestValidateNotificationTemplate_RejectsHeaderInjection deckt die
// Sicherheits-Erweiterung ab: ein Template mit CR/LF im Subject
// muss abgelehnt werden, bevor es überhaupt in die DB kommt. Der Body darf
// CR/LF enthalten, wird aber zur Sendezeit weiter von sendAlertEmail
// geprüft (Defense-in-Depth).
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

// TestDispatchNotifications_CustomTemplate_EscapesPlaceholders sichert die
// Escaping-Garantie beim Custom-Template-Pfad ab: html/template muss
// nutzergesteuerte Feldwerte automatisch escapen - ein Incident-Titel mit
// <script> darf nach dem Rendern nicht als rohes Markup im Body auftauchen.
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
	// html/template ersetzt < durch &lt; usw. - eine der escapten Formen muss
	// im Body auftauchen, damit klar ist, dass der Titel überhaupt gerendert
	// wurde und nicht etwa komplett weggefiltert.
	if !strings.Contains(msg, "&lt;script&gt;") {
		t.Fatalf("expected the escaped form &lt;script&gt; in the body, got:\n%s", msg)
	}
}

// TestDispatchNotifications_CustomTemplate_ResolvedKindIsUsed prüft das
// Action->Kind-Mapping: ein UPDATED-Dispatch mit Status "resolved" muss
// das "incident_resolved"-Template ziehen, nicht das "incident_updated"-
// Template. Das ist die Voraussetzung für getrennt editierbare
// Auflösungs-Mails.
func TestDispatchNotifications_CustomTemplate_ResolvedKindIsUsed(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "resolved-tpl-page", "resolved-tpl-admin@example.com")

	// Beide Templates anlegen - der Test soll erweisen, dass genau das
	// resolved-Template greift, nicht das updated-Template.
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

// waitForEmail selektiert vom Fake-SMTP-Kanal mit Timeout, um die
// Boilerplate in den obigen Tests klein zu halten.
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

// Regressionstest Fund 10: ValidateNotificationTemplate erlaubt
// CR/LF im Body, sendAlertEmail verwarf aber jeden Body mit CR/LF: eine
// mehrzeilige Vorlage oder eine mehrzeilige {{.Message}} ließ alle Mails der
// Seite still ausfallen. Der Body wird jetzt zu einer Zeile gefaltet; der Guard
// für Betreff und Absender bleibt (siehe die Header-Injection-Tests).
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
