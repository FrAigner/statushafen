package handlers_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

// Formatted messages in emails. What is checked is the text actually
// sent via SMTP (fake server), so also the CR/LF guard of
// sendAlertEmail: a multi-line message must not cause the mail to be discarded.

const richMessage = "Wir untersuchen **Fehler**.\nZweite Zeile\n\n- eins\n- *zwei*\n\n[Status](https://status.example.com/p) <script>alert(1)</script> [böse](javascript:alert(2)) <img src=x onerror=alert(3)>"

// mailBody returns the HTML part of a sent mail (after the headers).
func mailBody(t *testing.T, msg string) string {
	t.Helper()
	parts := strings.SplitN(msg, "\n\n", 2)
	require.Len(t, parts, 2, msg)
	return parts[1]
}

// requireNoActiveMarkup uses the HTML tokenizer to check that the mail text contains neither
// script/img/iframe tags nor event attributes nor links with a scheme other
// than http(s) (javascript:, data:, ...).
func requireNoActiveMarkup(t *testing.T, body string) {
	t.Helper()
	z := html.NewTokenizer(strings.NewReader(body))
	for tt := z.Next(); tt != html.ErrorToken; tt = z.Next() {
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, hasAttr := z.TagName()
		assert.NotContains(t, []string{"script", "img", "iframe", "svg", "style", "object"}, string(name))
		for hasAttr {
			var k, v []byte
			k, v, hasAttr = z.TagAttr()
			assert.False(t, strings.HasPrefix(string(k), "on"), "Ereignis-Attribut %s", k)
			if string(k) == "href" {
				// The fixed links of the mail (status page, unsubscribe) may be http (local environment).
				assert.Regexp(t, `^https?://`, string(v), "href %s", v)
			}
		}
	}
}

func TestDispatchNotifications_StandardMailZeigtFormatierteNachricht(t *testing.T) {
	srv, h, _, page, comp := setupNotificationDispatch(t, "rt-default", "rt-default@example.com")
	inc := seedIncident(t, h.DB, page.ID, "Titel", richMessage, "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	body := mailBody(t, waitForEmail(t, srv)) // arrived = CR/LF guard passed
	assert.Contains(t, body, "<strong>Fehler</strong>")
	assert.Contains(t, body, "Zweite Zeile")
	assert.Contains(t, body, "<br>Zweite Zeile")
	assert.Contains(t, body, "<li style=\"margin:0 0 4px;\"><em>zwei</em></li>")
	assert.Contains(t, body, `<a href="https://status.example.com/p" target="_blank" rel="noopener noreferrer nofollow"`)
	assert.Contains(t, body, "&lt;script&gt;alert(1)&lt;/script&gt;")
	assert.Contains(t, body, "böse (javascript:alert(2))")
	assert.NotContains(t, body, "<script")
	requireNoActiveMarkup(t, body)
}

// Regression: A multi-line message used to trigger the CR/LF guard of
// sendAlertEmail; the mail was silently discarded.
func TestDispatchNotifications_MehrzeiligeAltNachrichtWirdZugestellt(t *testing.T) {
	srv, h, _, page, comp := setupNotificationDispatch(t, "rt-legacy", "rt-legacy@example.com")
	legacy := "Wir untersuchen das Problem.\r\nBitte um Geduld.\n\n* Punkt\n_ Unterstrich\n# Raute\n- Strich\nsnake_case_name"
	inc := seedIncident(t, h.DB, page.ID, "Alt", legacy, "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	body := mailBody(t, waitForEmail(t, srv))
	for _, want := range []string{"Wir untersuchen das Problem.<br>Bitte um Geduld.", "Punkt", "# Raute", "Strich", "snake_case_name"} {
		assert.Contains(t, body, want)
	}
	assert.NotContains(t, body, "\r")
}

func TestDispatchNotifications_EigeneVorlage_MessageBleibtKlartextMessageHTMLFormatiert(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "rt-tpl", "rt-tpl@example.com")
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceStaff, "en",
		"S :: {{.Title}}", "<section id=\"klar\">{{.Message}}</section><section id=\"html\">{{.MessageHTML}}</section>")
	inc := seedIncident(t, db, page.ID, "Titel", "Das ist **fett** & <b>roh</b> mit [Link](https://example.com/x)", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msg := waitForEmail(t, srv)
	body := mailBody(t, msg)
	// {{.Message}}: unchanged plain text (Markdown source), escaped as before.
	assert.Contains(t, body, `<section id="klar">Das ist **fett** &amp; &lt;b&gt;roh&lt;/b&gt; mit [Link](https://example.com/x)</section>`)
	// {{.MessageHTML}}: formatted and sanitized, not escaped a second time.
	assert.Contains(t, body, `<section id="html"><p style="margin:0 0 10px;">Das ist <strong>fett</strong> &amp; &lt;b&gt;roh&lt;/b&gt; mit <a href="https://example.com/x"`)
	assert.NotContains(t, body, "&lt;strong&gt;")
	assert.NotContains(t, body, "&amp;lt;")
	assert.Contains(t, msg, "Subject: S :: Titel")
}

// A template with only {{.MessageHTML}} also survives a multi-line message
// (single-line output), unlike {{.Message}} with a line break.
func TestDispatchNotifications_EigeneVorlage_MessageHTMLPassiertCRLFGuard(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "rt-tpl-nl", "rt-tpl-nl@example.com")
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceStaff, "en",
		"S", "<div>{{.MessageHTML}}</div>")
	inc := seedIncident(t, db, page.ID, "Titel", "eins\nzwei\r\ndrei", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	assert.Contains(t, mailBody(t, waitForEmail(t, srv)), "eins<br>zwei<br>drei")
}

// A line break in the template no longer discards the mail, the
// body is folded into one line. The guard for subject/sender/header
// remains (see send_alert_headers_internal_test.go).
func TestDispatchNotifications_MehrzeiligeVorlageWirdGefaltetGesendet(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "rt-guard", "rt-guard@example.com")
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceStaff, "en",
		"S", "<div>{{.MessageHTML}}</div>\n<div>zweite Zeile der Vorlage</div>")
	inc := seedIncident(t, db, page.ID, "Titel", "x", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	body := mailBody(t, waitForEmail(t, srv))
	assert.Contains(t, body, "</div> <div>zweite Zeile der Vorlage</div>")
	assert.NotContains(t, strings.TrimSpace(body), "\n")
}

func TestDispatchNoticeNotifications_StandardMailZeigtFormatierteNachricht(t *testing.T) {
	srv, h, page, _ := setupNoticeDispatch(t, "rt-notice")
	notice := models.GeneralNotice{
		StatusPageID: page.ID, Title: "Hinweis", Message: richMessage, Level: "info", IsActive: true, NotifySubscribers: true,
	}
	h.DispatchNoticeNotifications(notice, "created")

	body := mailBody(t, waitForEmail(t, srv))
	assert.Contains(t, body, "<strong>Fehler</strong>")
	assert.Contains(t, body, "<br>Zweite Zeile")
	assert.Contains(t, body, "<ul ")
	assert.NotContains(t, body, "<script")
	requireNoActiveMarkup(t, body)
}

func TestDispatchNoticeNotifications_EigeneVorlage_MessageUndMessageHTML(t *testing.T) {
	srv, h, page, _ := setupNoticeDispatch(t, "rt-notice-tpl")
	seedTemplate(t, h.DB, page.ID, handlers.NotificationKindNoticeCreated, handlers.NotificationAudienceSubscriber, "de",
		"N", "<i>{{.Message}}</i>|<b>{{.MessageHTML}}</b>")
	notice := models.GeneralNotice{StatusPageID: page.ID, Title: "H", Message: "**fett** <u>x</u>", Level: "info", IsActive: true, NotifySubscribers: true}

	h.DispatchNoticeNotifications(notice, "created")

	body := mailBody(t, waitForEmail(t, srv))
	assert.Contains(t, body, "<i>**fett** &lt;u&gt;x&lt;/u&gt;</i>|<b><p ")
	assert.Contains(t, body, "<strong>fett</strong> &lt;u&gt;x&lt;/u&gt;")
}

func TestPreview_MessageHTMLPlatzhalterIstBelegt(t *testing.T) {
	_, r, adminTok, _, page, _ := newCodeEnvRouter(t)
	status, out := doJSON(t, r, "POST", "/api/pages/"+strconv.Itoa(int(page.ID))+"/notification-templates/incident_created/staff/en/preview", adminTok,
		map[string]any{"Subject": "S", "BodyHTML": "<div>{{.MessageHTML}}</div>"})
	assert.Equal(t, http.StatusOK, status, "%v", out)
	assert.Contains(t, out["bodyHTML"], `<div><p style="margin:0 0 10px;">We are investigating elevated error rates on the payment API.</p></div>`)
}

// Chat/webhook: plain text instead of raw Markdown.
func TestGoogleChatPayload_NachrichtAlsKlartext(t *testing.T) {
	payload := handlers.IncidentGoogleChatPayloadForTest(models.Incident{
		Title: "T", Status: "investigating",
		Message: "Das ist **fett**\n- eins\n- zwei\n\n[Status](https://status.example.com/p) <script>x</script>",
	})
	text, _ := payload["text"].(string)
	assert.Contains(t, text, "**Details:** Das ist fett\n\n- eins\n- zwei\n\nStatus (https://status.example.com/p) <script>x</script>")
	assert.NotContains(t, text, "**fett**")
}

func TestExternalIncidentMail_MessageHTMLBleibtKlartextMaskiert(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	s := newIncidentSetup(t)
	s.subscriber("alle@example.com", "all")
	require.NoError(t, s.db.Create(&models.NotificationTemplate{
		StatusPageID: s.page.ID, Kind: "external_incident_created", Audience: "subscriber", Language: "de",
		Subject: "S", BodyHTML: `<p>{{.MessageHTML}}</p>`,
	}).Error)

	s.setFeed(models.ExternalIncidentKindIncident, extInc("i1", "investigating", "**roh** <b>x</b>"))
	s.poll()
	m := drainMails(t, srv, 1)[0]
	assert.Contains(t, m.body, "<p>**roh** &lt;b&gt;x&lt;/b&gt;</p>")
}
