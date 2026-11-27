package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/fraigner/statushafen/models"
)

// Security audit, finding 2: POST /api/subscribe/test-webhook let every
// signed-in account, even without any role, have the server send a POST to
// an arbitrary https URL. The test button now only tests a webhook stored on
// the own page.

func storedWebhookPath(w *permWorld, page models.StatusPage, sub models.Subscriber) string {
	return "/api/pages/" + itoa(page.ID) + "/subscribers/" + itoa(sub.ID) + "/test-webhook"
}

// http:// fails the URL check in the handler: so a 400 proves that the
// guard let the request through without anything going out.
func seedStoredWebhook(w *permWorld, page models.StatusPage) models.Subscriber {
	sub := models.Subscriber{StatusPageID: page.ID, Channel: "google_chat", WebhookURL: "http://chat.example.com/hook"}
	w.db.Create(&sub)
	return sub
}

func TestRouter_UnscopedWebhookTestRemoved(t *testing.T) {
	w := newPermWorld(t)
	for _, ri := range w.r.Routes() {
		assert.NotEqual(t, "/api/subscribe/test-webhook", ri.Path, "route without page reference still registered")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/subscribe/test-webhook", strings.NewReader(`{"webhook_url":"https://example.com/"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", w.user(nil))
	rec := httptest.NewRecorder()
	w.r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// Readers may test stored webhooks (the button is in the list, not in
// the edit menu); without subscribers:read or without a role: 403.
func TestRouter_StoredWebhookTest_NeedsSubscribersRead(t *testing.T) {
	w := newPermWorld(t)
	path := storedWebhookPath(w, w.a.page, seedStoredWebhook(w, w.a.page))
	for name, tc := range map[string]struct {
		perms *string
		want  int
	}{
		"subscribers:read":   {listOf("subscribers:read"), http.StatusBadRequest},
		"subscribers:update": {listOf("subscribers:update"), http.StatusBadRequest},
		"components:read":    {listOf("components:read"), http.StatusForbidden},
		"ohne Rolle":         {nil, http.StatusForbidden},
	} {
		assert.Equal(t, tc.want, w.code(http.MethodPost, path, w.user(tc.perms)), name)
	}
}

// A subscriber of page B cannot be reached via page A, and rights on
// page A do not count for page B.
func TestRouter_StoredWebhookTest_TenantSeparation(t *testing.T) {
	w := newPermWorld(t)
	subB := seedStoredWebhook(w, w.b.page)
	tok := w.user(listOf("subscribers:read"))
	assert.Equal(t, http.StatusNotFound, w.code(http.MethodPost, storedWebhookPath(w, w.a.page, subB), tok), "B's subscriber via page A")
	assert.Equal(t, http.StatusForbidden, w.code(http.MethodPost, storedWebhookPath(w, w.b.page, subB), tok), "rights on A for page B")
}
