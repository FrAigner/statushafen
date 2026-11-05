package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Der Admin-Pfad (CreateSubscriberAdminHandler /
// UpdateSubscriberHandler) übernahm Webhook-URLs ungeprüft, während der
// öffentliche SubscribeHandler validateWebhookURL aufruft. Ein
// Seiten-Admin konnte so interne Ziele (Loopback, private Netze) als
// Google-Chat-Webhook hinterlegen.

func stubWebhookLookup(t *testing.T) {
	t.Helper()
	orig := webhookLookupIP
	t.Cleanup(func() { webhookLookupIP = orig })
	webhookLookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		if strings.HasPrefix(host, "internal.") {
			return []net.IP{net.ParseIP("10.0.0.5")}, nil
		}
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
}

func adminSubscriberRequest(t *testing.T, r http.Handler, method, path string, payload map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(method, path, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCreateSubscriberAdminHandler_ValidatesWebhookURL(t *testing.T) {
	stubWebhookLookup(t)
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/pages/:page_id/subscribers", h.CreateSubscriberAdminHandler)

	page := models.StatusPage{Name: "Admin SSRF", Slug: "admin-ssrf"}
	db.Create(&page)
	path := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/subscribers"

	for _, bad := range []string{"https://internal.example.test/hook", "http://chat.example.com/hook"} {
		w := adminSubscriberRequest(t, r, http.MethodPost, path, map[string]interface{}{
			"Channel": "google_chat", "WebhookURL": bad, "SubscribedComponentIDs": "all",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for webhook URL %q, got %d: %s", bad, w.Code, w.Body.String())
		}
	}
	var count int64
	db.Model(&models.Subscriber{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected no subscriber for rejected webhook URLs, found %d", count)
	}

	w := adminSubscriberRequest(t, r, http.MethodPost, path, map[string]interface{}{
		"Channel": "google_chat", "WebhookURL": "https://chat.example.com/hook", "SubscribedComponentIDs": "all",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for a public https webhook URL, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSubscriberHandler_ValidatesWebhookURL(t *testing.T) {
	stubWebhookLookup(t)
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := testutils.SetupRouter()
	r.PUT("/api/pages/:page_id/subscribers/:id", h.UpdateSubscriberHandler)

	page := models.StatusPage{Name: "Admin SSRF Update", Slug: "admin-ssrf-update"}
	db.Create(&page)
	// Bestandsabo, dessen gespeicherte URL inzwischen intern auflöst: eine
	// reine Komponenten-Änderung soll weiter möglich sein (Bestandsdaten
	// nicht aussperren); beim Versand schützt safeDialContext.
	legacy := models.Subscriber{StatusPageID: page.ID, Channel: "google_chat", WebhookURL: "https://internal.example.test/legacy", SubscribedComponentIDs: "all"}
	email := models.Subscriber{StatusPageID: page.ID, Channel: "email", Email: "sub@example.com", SubscribedComponentIDs: "all"}
	db.Create(&legacy)
	db.Create(&email)
	path := func(s models.Subscriber) string {
		return "/api/pages/" + strconv.Itoa(int(page.ID)) + "/subscribers/" + strconv.Itoa(int(s.ID))
	}
	storedURL := func(s models.Subscriber) string {
		var got models.Subscriber
		db.First(&got, s.ID)
		return got.WebhookURL
	}

	cases := []struct {
		desc    string
		sub     models.Subscriber
		payload map[string]interface{}
		want    int
	}{
		{"neue interne URL", legacy, map[string]interface{}{"Channel": "google_chat", "WebhookURL": "https://internal.example.test/new", "SubscribedComponentIDs": "all"}, http.StatusBadRequest},
		{"neue http-URL", legacy, map[string]interface{}{"Channel": "google_chat", "WebhookURL": "http://chat.example.com/hook", "SubscribedComponentIDs": "all"}, http.StatusBadRequest},
		{"Kanalwechsel auf google_chat ohne URL", email, map[string]interface{}{"Channel": "google_chat", "WebhookURL": "", "SubscribedComponentIDs": "all"}, http.StatusBadRequest},
		{"Kanalwechsel auf google_chat mit interner URL", email, map[string]interface{}{"Channel": "google_chat", "WebhookURL": "https://internal.example.test/x", "SubscribedComponentIDs": "all"}, http.StatusBadRequest},
		{"unveränderte Bestands-URL, nur Komponenten", legacy, map[string]interface{}{"Channel": "google_chat", "WebhookURL": "https://internal.example.test/legacy", "SubscribedComponentIDs": "1"}, http.StatusOK},
	}
	for _, c := range cases {
		w := adminSubscriberRequest(t, r, http.MethodPut, path(c.sub), c.payload)
		if w.Code != c.want {
			t.Errorf("%s: got %d, want %d: %s", c.desc, w.Code, c.want, w.Body.String())
		}
	}
	if got := storedURL(legacy); got != "https://internal.example.test/legacy" {
		t.Errorf("rejected updates must not change the stored webhook URL, got %q", got)
	}
	if got := storedURL(email); got != "" {
		t.Errorf("rejected channel switch must not store a webhook URL, got %q", got)
	}

	w := adminSubscriberRequest(t, r, http.MethodPut, path(legacy), map[string]interface{}{
		"Channel": "google_chat", "WebhookURL": "https://chat.example.com/new", "SubscribedComponentIDs": "all",
	})
	if w.Code != http.StatusOK || storedURL(legacy) != "https://chat.example.com/new" {
		t.Fatalf("expected a new public https webhook URL to be accepted, got %d: %s", w.Code, w.Body.String())
	}
}
