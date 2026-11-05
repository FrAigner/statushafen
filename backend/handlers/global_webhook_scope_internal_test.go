package handlers

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func countingWebhookServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// Der Betreiber-Webhook bekommt nur Vorfälle öffentlicher Seiten. Vorfälle
// privater Seiten gehören dem jeweiligen Mandanten und gehen nicht in einen
// Chat, den der Betreiber instanzweit liest; ist die Seite nicht geladen
// (Datenbankfehler), gilt das ebenso (fail closed).
func TestDispatchGlobalGoogleChatWebhook_NurOeffentlicheSeiten(t *testing.T) {
	newWebhookEnv(t)
	srv, calls := countingWebhookServer(t)
	t.Setenv("GOOGLE_CHAT_WEBHOOK_URL", srv.URL)
	inc := models.Incident{Title: "Ausfall", Status: "investigating", Message: "m"}

	dispatchGlobalGoogleChatWebhook(notificationDispatchContext{inc: inc, action: "created",
		page: models.StatusPage{IsPrivate: true}})
	assert.Zero(t, calls.Load(), "private Seite: nichts weitergeben")
	dispatchGlobalGoogleChatWebhook(notificationDispatchContext{inc: inc, action: "created"})
	assert.Zero(t, calls.Load(), "Seite nicht geladen: nichts weitergeben")

	public := models.StatusPage{Name: "Öffentlich"}
	public.ID = 7
	dispatchGlobalGoogleChatWebhook(notificationDispatchContext{inc: inc, action: "created", page: public})
	assert.EqualValues(t, 1, calls.Load(), "Gegenprobe: öffentliche Seite wird weitergegeben")
}

// Ende-zu-Ende über DispatchNotifications: Der Vorfall einer privaten Seite
// erreicht den Attrappen-Listener nicht, der einer öffentlichen Seite schon.
func TestDispatchNotifications_BetreiberWebhookAusgenommenPrivateSeite(t *testing.T) {
	newWebhookEnv(t)
	srv, calls := countingWebhookServer(t)
	t.Setenv("GOOGLE_CHAT_WEBHOOK_URL", srv.URL)
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	// Der Versand läuft in Goroutinen und liest Variablen, die newWebhookEnv
	// beim Aufräumen zurücksetzt; vorher abwarten (LIFO: läuft zuerst).
	t.Cleanup(h.WaitNotifications)
	private := models.StatusPage{Name: "Intern", Slug: "intern-wh", IsPrivate: true}
	public := models.StatusPage{Name: "Offen", Slug: "offen-wh"}
	require.NoError(t, db.Create(&private).Error)
	require.NoError(t, db.Create(&public).Error)

	run := func(page models.StatusPage) {
		inc := models.Incident{StatusPageID: page.ID, Title: "Ausfall", Message: "m", Status: "investigating"}
		require.NoError(t, db.Create(&inc).Error)
		h.DispatchNotifications(inc, nil, "created")
	}
	run(private)
	time.Sleep(300 * time.Millisecond) // der Versand läuft in einer Goroutine
	assert.Zero(t, calls.Load(), "Vorfall der privaten Seite bleibt beim Mandanten")

	run(public)
	require.Eventually(t, func() bool { return calls.Load() == 1 }, 3*time.Second, 20*time.Millisecond, "öffentliche Seite kommt an")
}
