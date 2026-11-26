package handlers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// safeDialContext never connects unchecked: invalid addresses, errors of the
// own resolution and empty results end as errors, without a
// connection attempt taking place.
func TestSafeDialContext_ErrorPaths(t *testing.T) {
	orig := webhookLookupIP
	t.Cleanup(func() { webhookLookupIP = orig })

	t.Run("Adresse ohne Port", func(t *testing.T) {
		_, err := safeDialContext(context.Background(), "tcp", "kein-port")
		require.Error(t, err)
	})
	t.Run("Auflösung scheitert", func(t *testing.T) {
		webhookLookupIP = func(context.Context, string) ([]net.IP, error) { return nil, errors.New("kein DNS") }
		_, err := safeDialContext(context.Background(), "tcp", "hook.example.com:443")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "kein DNS")
	})
	t.Run("Auflösung liefert keine Adresse", func(t *testing.T) {
		webhookLookupIP = func(context.Context, string) ([]net.IP, error) { return nil, nil }
		_, err := safeDialContext(context.Background(), "tcp", "hook.example.com:443")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "keine gültige Adresse")
	})
	t.Run("nur interne Adressen", func(t *testing.T) {
		webhookLookupIP = func(context.Context, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("10.0.0.5"), net.ParseIP("127.0.0.1")}, nil
		}
		_, err := safeDialContext(context.Background(), "tcp", "hook.example.com:443")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nicht erlaubt")
	})
}

// The webhook test follows no redirects: a 302 to an internal
// address would otherwise bypass the blocklist.
func TestWebhookTestClient_DoesNotFollowRedirects(t *testing.T) {
	err := webhookTestClient.CheckRedirect(&http.Request{}, nil)
	assert.ErrorIs(t, err, http.ErrUseLastResponse)
}

// An unchecked language choice does not fail silently: a database error while
// checking the language aborts creation, and for Google Chat subscriptions
// the email of the signed-in user takes over the assignment.
func TestCreateSubscriberFromRequest_LanguageErrorAndChatEmailFallback(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "Abo", Slug: "abo-intern"}
	require.NoError(t, db.Create(&page).Error)

	t.Run("Sprachprüfung scheitert", func(t *testing.T) {
		remove := injectDBError(t, db, "query", "status_page_languages", 0)
		defer remove()
		c, _ := pageTestContext(http.MethodPost, "", nil)
		_, _, err := h.createSubscriberFromRequest(c, SubscribeRequest{Channel: "email", Email: "leser@example.org", ComponentIDs: "all", Language: "de"}, page.ID)
		require.Error(t, err)
		var n int64
		db.Model(&models.Subscriber{}).Count(&n)
		assert.Zero(t, n, "nichts gespeichert")
	})

	t.Run("Chat-Abo übernimmt die E-Mail des angemeldeten Nutzers", func(t *testing.T) {
		c, _ := pageTestContext(http.MethodPost, "", nil)
		c.Set("email", "angemeldet@example.com")
		sub, token, err := h.createSubscriberFromRequest(c, SubscribeRequest{Channel: "google_chat", WebhookURL: "https://chat.googleapis.com/v1/spaces/x/messages?key=k&token=t", ComponentIDs: "all"}, page.ID)
		require.NoError(t, err)
		assert.Equal(t, "angemeldet@example.com", sub.Email)
		assert.Empty(t, token, "Webhook-Kanäle brauchen keine Bestätigung")
		assert.Equal(t, models.SubscriberStatusActive, sub.Status)
	})
}

func TestSubscriberConflictMessage_ByChannel(t *testing.T) {
	assert.Contains(t, subscriberConflictMessage("email"), "E-Mail")
	assert.Contains(t, subscriberConflictMessage("google_chat"), "Webhook")
}
