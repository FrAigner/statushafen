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

// The operator webhook only receives incidents of public sites. Incidents
// of private sites belong to the respective tenant and do not go into a
// chat the operator reads instance-wide; if the site is not loaded
// (database error), the same applies (fail closed).
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

// End to end via DispatchNotifications: The incident of a private site
// does not reach the dummy listener, that of a public site does.
func TestDispatchNotifications_BetreiberWebhookAusgenommenPrivateSeite(t *testing.T) {
	newWebhookEnv(t)
	srv, calls := countingWebhookServer(t)
	t.Setenv("GOOGLE_CHAT_WEBHOOK_URL", srv.URL)
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	// Delivery runs in goroutines and reads variables that newWebhookEnv
	// resets on cleanup; wait first (LIFO: runs first).
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
	time.Sleep(300 * time.Millisecond) // delivery runs in a goroutine
	assert.Zero(t, calls.Load(), "Vorfall der privaten Seite bleibt beim Mandanten")

	run(public)
	require.Eventually(t, func() bool { return calls.Load() == 1 }, 3*time.Second, 20*time.Millisecond, "öffentliche Seite kommt an")
}
