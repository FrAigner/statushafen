package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/fraigner/statushafen/models"
)

func TestDispatchGlobalGoogleChatWebhook(t *testing.T) {
	SetWebhookLoopbackAllowedForTests(t.Cleanup)

	t.Run("posts to configured webhook URL", func(t *testing.T) {
		called := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		_ = os.Setenv("GOOGLE_CHAT_WEBHOOK_URL", server.URL)
		defer func() { _ = os.Unsetenv("GOOGLE_CHAT_WEBHOOK_URL") }()

		page := models.StatusPage{Name: "Öffentlich"}
		page.ID = 1
		ctx := notificationDispatchContext{
			inc:    models.Incident{Title: "Test Incident", Status: "investigating", Message: "checking"},
			action: "CREATED",
			page:   page,
		}
		dispatchGlobalGoogleChatWebhook(ctx)

		if !called {
			t.Fatal("expected global webhook to be called")
		}
	})

	t.Run("no-op when GOOGLE_CHAT_WEBHOOK_URL is unset", func(t *testing.T) {
		_ = os.Unsetenv("GOOGLE_CHAT_WEBHOOK_URL")
		// Must not panic or attempt any network call.
		dispatchGlobalGoogleChatWebhook(notificationDispatchContext{inc: models.Incident{Title: "x"}})
	})

	t.Run("logs but does not panic on request failure", func(t *testing.T) {
		_ = os.Setenv("GOOGLE_CHAT_WEBHOOK_URL", "https://127.0.0.1:1")
		defer func() { _ = os.Unsetenv("GOOGLE_CHAT_WEBHOOK_URL") }()
		page := models.StatusPage{Name: "Öffentlich"}
		page.ID = 1
		dispatchGlobalGoogleChatWebhook(notificationDispatchContext{inc: models.Incident{Title: "x"}, page: page})
	})
}
