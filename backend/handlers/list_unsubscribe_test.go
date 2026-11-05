package handlers_test

import (
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"
)

// API-Befund / RFC 8058: Benachrichtigungsmails an Abonnenten
// tragen List-Unsubscribe (POST-fähige URL mit Token) und
// List-Unsubscribe-Post, damit Mail-Clients ein One-Click-Abbestellen
// anbieten, ohne dass ein bloßer GET (Link-Scanner) etwas löscht. Interne
// Mails an Team-Mitglieder haben kein Abo und daher keinen solchen Header.

const oneClickPostHeader = "List-Unsubscribe-Post: List-Unsubscribe=One-Click"
const listUnsubscribePrefix = "List-Unsubscribe: <https://status.example.test/api/public/subscribers/unsubscribe?token="

// mailHeaders liefert den Header-Block einer vom Fake-SMTP-Server
// mitgeschnittenen Nachricht (alles vor der ersten Leerzeile).
func mailHeaders(msg string) string {
	head, _, _ := strings.Cut(msg, "\n\n")
	return head
}

// collectSMTPMessages sammelt n Nachrichten und ordnet sie nach Empfänger.
func collectSMTPMessages(t *testing.T, srv *fakeSMTPServer, n int) map[string]string {
	t.Helper()
	out := map[string]string{}
	deadline := time.After(3 * time.Second)
	for len(out) < n {
		select {
		case msg := <-srv.dataCh:
			for _, line := range strings.Split(mailHeaders(msg), "\n") {
				if to, ok := strings.CutPrefix(line, "To: "); ok {
					out[to] = msg
				}
			}
		case <-deadline:
			t.Fatalf("expected %d emails within the timeout, got %d", n, len(out))
		}
	}
	return out
}

func TestDispatchNotifications_SubscriberMailHasOneClickUnsubscribeHeaders(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	t.Setenv("FRONTEND_URL", "https://status.example.test")

	db, h, comp, inc := setupDispatchFixtures("One-Click Page", "one-click-page")
	db.Create(&models.User{Email: "team@example.com", IsGlobalAdmin: true})
	db.Create(&models.Subscriber{StatusPageID: comp.StatusPageID, Channel: "email", Email: "sub@example.com", SubscribedComponentIDs: "all"})

	h.DispatchNotifications(inc, []models.Component{comp}, "CREATED")

	msgs := collectSMTPMessages(t, srv, 2)
	subHeaders := mailHeaders(msgs["sub@example.com"])
	if !strings.Contains(subHeaders, listUnsubscribePrefix) {
		t.Fatalf("expected List-Unsubscribe header with POST URL in subscriber mail headers, got:\n%s", subHeaders)
	}
	if !strings.Contains(subHeaders, oneClickPostHeader) {
		t.Fatalf("expected %q in subscriber mail headers, got:\n%s", oneClickPostHeader, subHeaders)
	}
	if teamHeaders := mailHeaders(msgs["team@example.com"]); strings.Contains(teamHeaders, "List-Unsubscribe") {
		t.Fatalf("internal team mail must not carry List-Unsubscribe headers, got:\n%s", teamHeaders)
	}
}

func TestDispatchNoticeNotifications_SubscriberMailHasOneClickUnsubscribeHeaders(t *testing.T) {
	srv, h, page, _ := setupNoticeDispatch(t, "one-click-notice")
	t.Setenv("FRONTEND_URL", "https://status.example.test")
	h.DispatchNoticeNotifications(models.GeneralNotice{
		StatusPageID: page.ID, Title: "Hinweis", Message: "m", Level: "info", IsActive: true, NotifySubscribers: true,
	}, "created")

	headers := mailHeaders(waitForEmail(t, srv))
	if !strings.Contains(headers, listUnsubscribePrefix) || !strings.Contains(headers, oneClickPostHeader) {
		t.Fatalf("expected One-Click List-Unsubscribe headers in notice mail, got:\n%s", headers)
	}
}
