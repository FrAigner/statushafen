package handlers_test

import (
	"net/mail"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Der Absender einer Mail (Header und SMTP-Envelope) gehört dem Betreiber des
// Servers. Eine Statusseite steuert höchstens den Anzeigenamen und eine
// Antwortadresse (Reply-To) bei; sonst ließe sich der Server als Relais mit
// gefälschtem Absender missbrauchen.

const operatorFrom = "Statushafen <alerts@mail.example.com>"

// headerLine liefert den Wert der ersten Kopfzeile name der Mail ("" ohne).
func headerLine(msg, name string) string {
	head, _, _ := strings.Cut(msg, "\n\n")
	for _, l := range strings.Split(head, "\n") {
		if v, ok := strings.CutPrefix(l, name+": "); ok {
			return v
		}
	}
	return ""
}

func assertReplyTo(t *testing.T, msg, wantAddress string) {
	t.Helper()
	rt, err := mail.ParseAddress(headerLine(msg, "Reply-To"))
	require.NoError(t, err, msg)
	assert.Equal(t, wantAddress, rt.Address)
}

func TestMailSender_PageAddressIsOnlyReplyTo(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("SMTP_FROM", operatorFrom)

	db, h, comp, inc := setupDispatchFixturesForPage(models.StatusPage{
		Name: "Acme Status", Slug: "acme-sender", NotificationFromAddress: "Bank Security <security@bank.example>",
	})
	require.NoError(t, db.Create(&models.User{Email: "sender-admin@example.com", IsGlobalAdmin: true}).Error)

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessage(t, srv)
	assert.Equal(t, "alerts@mail.example.com", awaitMailFrom(t, srv), "Envelope-Absender gehört dem Betreiber")
	from, err := mail.ParseAddress(headerLine(msg, "From"))
	require.NoError(t, err, msg)
	assert.Equal(t, "alerts@mail.example.com", from.Address)
	assert.Equal(t, "Acme Status", from.Name, "Seitenname als Anzeigename")
	assertReplyTo(t, msg, "security@bank.example")
	assert.NotContains(t, headerLine(msg, "From"), "bank.example")
}

func TestMailSender_WithoutPageAddressNoReplyTo(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("SMTP_FROM", operatorFrom)

	db, h, comp, inc := setupDispatchFixturesForPage(models.StatusPage{Name: "Plain", Slug: "plain-sender"})
	require.NoError(t, db.Create(&models.User{Email: "plain-admin@example.com", IsGlobalAdmin: true}).Error)

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessage(t, srv)
	from, err := mail.ParseAddress(headerLine(msg, "From"))
	require.NoError(t, err, msg)
	assert.Equal(t, "alerts@mail.example.com", from.Address)
	assert.Empty(t, headerLine(msg, "Reply-To"))
}

func TestMailSender_DisplayNameCannotInjectHeaders(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("SMTP_FROM", operatorFrom)

	db, h, comp, inc := setupDispatchFixturesForPage(models.StatusPage{
		Name: "Evil\r\nBcc: victim@example.org <x@evil.example>", Slug: "evil-name",
	})
	require.NoError(t, db.Create(&models.User{Email: "evil-admin@example.com", IsGlobalAdmin: true}).Error)

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := awaitSMTPMessage(t, srv)
	assert.Empty(t, headerLine(msg, "Bcc"))
	from, err := mail.ParseAddress(headerLine(msg, "From"))
	require.NoError(t, err, msg)
	assert.Equal(t, "alerts@mail.example.com", from.Address)
}

// Admin-Weg: Seitenabsender, Vorlage mit fremdem Link und mehrfaches Anlegen
// derselben Fremdadresse.
func TestMailSender_AdminSubscriberFlowCannotRelay(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("SMTP_FROM", operatorFrom)
	h, r, adminTok, _, page, _ := newCodeEnvRouter(t)
	require.NoError(t, h.DB.Model(&page).Update("notification_from_address", "Bank Security <security@bank.example>").Error)
	pid := strconv.Itoa(int(page.ID))

	status, out := doJSON(t, r, "PUT", "/api/pages/"+pid+"/notification-templates/subscription_confirm/subscriber/de", adminTok,
		map[string]any{"Subject": "Konto gesperrt", "BodyHTML": `<a href="https://login.evil.example.net/">Anmelden</a> {{.ConfirmURL}}`})
	require.Equal(t, 200, status, "%v", out)

	status, out = doJSON(t, r, "POST", "/api/pages/"+pid+"/subscribers", adminTok,
		map[string]any{"Channel": "email", "Email": "opfer@example.org", "SubscribedComponentIDs": "all"})
	require.Equal(t, 201, status, "%v", out)

	msg := awaitSMTPMessage(t, srv)
	assert.Equal(t, "alerts@mail.example.com", awaitMailFrom(t, srv))
	assert.NotContains(t, headerLine(msg, "From"), "bank.example")
	assertReplyTo(t, msg, "security@bank.example")
	// Fester Rahmen: Hinweis für Empfänger, die sich nicht angemeldet haben.
	assert.Contains(t, msg, "ignoriere diese E-Mail")
}

func TestNotificationTemplate_SubscriptionConfirmRequiresConfirmURL(t *testing.T) {
	assert.Equal(t, "template_confirm_url_missing",
		handlers.TemplateErrorCode(handlers.ValidateNotificationTemplateForKind(handlers.NotificationKindSubscriptionConfirm, "S", "<p>Hallo</p>")))
	assert.Equal(t, "template_confirm_url_missing",
		handlers.TemplateErrorCode(handlers.ValidateNotificationTemplateForKind(handlers.NotificationKindSubscriptionConfirm, "S", "{{if .Level}}{{.ConfirmURL}}{{end}}x{{.Title}}")), "ConfirmURL nur in Bedingung zählt nicht...")
}

func TestNotificationTemplate_SubscriptionConfirmWithConfirmURLAndOtherKinds(t *testing.T) {
	assert.NoError(t, handlers.ValidateNotificationTemplateForKind(handlers.NotificationKindSubscriptionConfirm, "S", `<a href="{{.ConfirmURL}}">Bestätigen</a>`))
	// Gegenprobe: andere Arten brauchen keinen Bestätigungslink.
	assert.NoError(t, handlers.ValidateNotificationTemplateForKind(handlers.NotificationKindIncidentCreated, "S", "<p>Hallo</p>"))
}

func TestFehlercodes_VorlageBestaetigungOhneLink(t *testing.T) {
	_, r, adminTok, _, page, _ := newCodeEnvRouter(t)
	url := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/notification-templates/subscription_confirm/subscriber/de"
	runCodeCases(t, r, []codeCase{
		{"Speichern ohne Bestätigungslink", "PUT", url, adminTok, map[string]any{"Subject": "S", "BodyHTML": "<p>x</p>"}, 400, "template_confirm_url_missing"},
		{"Vorschau ohne Bestätigungslink", "POST", url + "/preview", adminTok, map[string]any{"Subject": "S", "BodyHTML": "<p>x</p>"}, 400, "template_confirm_url_missing"},
		{"Speichern mit Bestätigungslink", "PUT", url, adminTok, map[string]any{"Subject": "S", "BodyHTML": `<a href="{{.ConfirmURL}}">x</a>`}, 200, ""},
	})
}

// Eine früher gespeicherte Bestätigungsvorlage ohne Link würde nie ein Abo
// freischalten; beim Versand gilt dann der Standardtext mit Link.
func TestMailSender_StoredConfirmTemplateWithoutLinkFallsBackToDefault(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("SMTP_FROM", operatorFrom)
	db, r, page := setupDOIRouter(t)
	require.NoError(t, db.Create(&models.NotificationTemplate{
		StatusPageID: page.ID, Kind: handlers.NotificationKindSubscriptionConfirm, Audience: handlers.NotificationAudienceSubscriber, Language: "de",
		Subject: "Gesperrt", BodyHTML: `<a href="https://login.evil.example.net/">Anmelden</a>`,
	}).Error)

	w := subscribeEmail(r, "alt-vorlage@example.com", nil)
	requireStatusCode(t, w, 200)

	msg := awaitSMTPMessage(t, srv)
	assert.NotContains(t, msg, "evil.example.net")
	assert.Contains(t, msg, "/subscribe/confirm?token=")
}

func TestReservedSlugs_MailLocalPartsAreBlocked(t *testing.T) {
	db, r := newInputValidationRouter(t)
	for _, slug := range []string{"support", "security", "billing", "postmaster", "abuse", "noreply", "no-reply", "info", "root", "hostmaster", "webmaster", "edge"} {
		w := doStatusPageRequest(r, "POST", "/api/admin/pages", map[string]any{"Name": "Reserviert", "Slug": slug})
		require.Equal(t, 400, w.Code, slug+": "+w.Body.String())
		requireErrorCode(t, w.Body.Bytes(), "slug_reserved")
	}
	assert.Zero(t, count(db, &models.StatusPage{}, "1 = 1"))
}

func TestAdminSubscriberCreate_DuplicateIsRejected(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	_, r, adminTok, _, page, _ := newCodeEnvRouter(t)
	url := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/subscribers"
	body := map[string]any{"Channel": "email", "Email": "doppelt@example.org", "SubscribedComponentIDs": "all"}

	status, out := doJSON(t, r, "POST", url, adminTok, body)
	require.Equal(t, 201, status, "%v", out)
	awaitSMTPMessage(t, srv)

	status, out = doJSON(t, r, "POST", url, adminTok, body)
	assert.Equal(t, 409, status, "%v", out)
	assert.Equal(t, "subscription_exists_email", out["code"])
}

func TestAdminSubscriberCreate_PerRecipientLimitAcrossPages(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	h, r, adminTok, _, _, _ := newCodeEnvRouter(t)
	var codes []int
	for i := 0; i < 5; i++ {
		p := models.StatusPage{Name: "Mailbomb " + strconv.Itoa(i), Slug: "mailbomb-" + strconv.Itoa(i)}
		require.NoError(t, h.DB.Create(&p).Error)
		status, out := doJSON(t, r, "POST", "/api/pages/"+strconv.Itoa(int(p.ID))+"/subscribers", adminTok,
			map[string]any{"Channel": "email", "Email": "opfer@example.org", "SubscribedComponentIDs": "all"})
		codes = append(codes, status)
		if status == 429 {
			assert.Equal(t, "subscriber_rate_limited", out["code"])
		}
	}
	assert.Equal(t, []int{201, 201, 201, 429, 429}, codes)
}

// drainSMTP nimmt zugestellte Mails und Envelope-Absender entgegen, damit der
// Fake-Server bei vielen Mails nicht an vollen Kanälen blockiert.
func drainSMTP(srv *fakeSMTPServer) {
	go func() {
		for range srv.dataCh {
		}
	}()
	go func() {
		for range srv.mailFromCh {
		}
	}()
}

func TestAdminSubscriberCreate_PerPageLimit(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	drainSMTP(srv)
	_, r, adminTok, _, page, _ := newCodeEnvRouter(t)
	url := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/subscribers"
	var last int
	for i := 0; i <= handlers.MaxAdminSubscriberCreatesPerHour; i++ {
		last, _ = doJSON(t, r, "POST", url, adminTok,
			map[string]any{"Channel": "email", "Email": "limit" + strconv.Itoa(i) + "@example.org", "SubscribedComponentIDs": "all"})
		if i < handlers.MaxAdminSubscriberCreatesPerHour {
			require.Equal(t, 201, last, "Anfrage %d", i)
		}
	}
	assert.Equal(t, 429, last)

}
