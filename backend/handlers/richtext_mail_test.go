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

// Formatierte Nachrichten in E-Mails. Geprüft wird der tatsächlich
// über SMTP gesendete Text (Fake-Server), also auch der CR/LF-Guard von
// sendAlertEmail: Eine mehrzeilige Nachricht darf die Mail nicht verwerfen.

const richMessage = "Wir untersuchen **Fehler**.\nZweite Zeile\n\n- eins\n- *zwei*\n\n[Status](https://status.example.com/p) <script>alert(1)</script> [böse](javascript:alert(2)) <img src=x onerror=alert(3)>"

// mailBody liefert den HTML-Teil einer gesendeten Mail (nach den Kopfzeilen).
func mailBody(t *testing.T, msg string) string {
	t.Helper()
	parts := strings.SplitN(msg, "\n\n", 2)
	require.Len(t, parts, 2, msg)
	return parts[1]
}

// requireNoActiveMarkup prüft mit dem HTML-Tokenizer, dass der Mailtext weder
// script/img/iframe-Tags noch Ereignis-Attribute noch Links mit anderem Schema
// als http(s) (javascript:, data:, ...) enthält.
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
				// Die festen Links der Mail (Statusseite, Abbestellen) dürfen http sein (lokale Umgebung).
				assert.Regexp(t, `^https?://`, string(v), "href %s", v)
			}
		}
	}
}

func TestDispatchNotifications_StandardMailZeigtFormatierteNachricht(t *testing.T) {
	srv, h, _, page, comp := setupNotificationDispatch(t, "rt-default", "rt-default@example.com")
	inc := seedIncident(t, h.DB, page.ID, "Titel", richMessage, "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	body := mailBody(t, waitForEmail(t, srv)) // angekommen = CR/LF-Guard bestanden
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

// Regression: Eine mehrzeilige Nachricht ließ bisher den CR/LF-Guard von
// sendAlertEmail anschlagen; die Mail wurde still verworfen.
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
	// {{.Message}}: unveränderter Klartext (Markdown-Quelltext), maskiert wie bisher.
	assert.Contains(t, body, `<section id="klar">Das ist **fett** &amp; &lt;b&gt;roh&lt;/b&gt; mit [Link](https://example.com/x)</section>`)
	// {{.MessageHTML}}: formatiert und bereinigt, nicht ein zweites Mal maskiert.
	assert.Contains(t, body, `<section id="html"><p style="margin:0 0 10px;">Das ist <strong>fett</strong> &amp; &lt;b&gt;roh&lt;/b&gt; mit <a href="https://example.com/x"`)
	assert.NotContains(t, body, "&lt;strong&gt;")
	assert.NotContains(t, body, "&amp;lt;")
	assert.Contains(t, msg, "Subject: S :: Titel")
}

// Eine Vorlage nur mit {{.MessageHTML}} übersteht auch eine mehrzeilige Nachricht
// (einzeilige Ausgabe), anders als {{.Message}} mit Zeilenumbruch.
func TestDispatchNotifications_EigeneVorlage_MessageHTMLPassiertCRLFGuard(t *testing.T) {
	srv, h, db, page, comp := setupNotificationDispatch(t, "rt-tpl-nl", "rt-tpl-nl@example.com")
	seedTemplate(t, db, page.ID, handlers.NotificationKindIncidentCreated, handlers.NotificationAudienceStaff, "en",
		"S", "<div>{{.MessageHTML}}</div>")
	inc := seedIncident(t, db, page.ID, "Titel", "eins\nzwei\r\ndrei", "investigating")

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	assert.Contains(t, mailBody(t, waitForEmail(t, srv)), "eins<br>zwei<br>drei")
}

// Zeilenumbruch in der Vorlage verwirft die Mail nicht mehr, der
// Body wird zu einer Zeile gefaltet. Der Guard für Betreff/Absender/Header
// bleibt (siehe send_alert_headers_internal_test.go).
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

// Chat/Webhook: Klartext statt rohem Markdown.
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
