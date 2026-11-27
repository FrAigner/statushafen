package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// The webhook test in the subscriber list only tests a webhook that is
// already stored on the page. Before, POST /api/subscribe/test-webhook let
// every signed-in account have the server send a POST to any https URL
// (security audit, finding 2): no page reference, free target.

type storedWebhookEnv struct {
	r      *gin.Engine
	db     *gorm.DB
	pageA  models.StatusPage
	pageB  models.StatusPage
	hits   *atomic.Int32
	target string
}

func newStoredWebhookEnv(t *testing.T) *storedWebhookEnv {
	t.Helper()
	newWebhookEnv(t)
	gin.SetMode(gin.TestMode)
	db := testutils.SetupTestDB()
	env := &storedWebhookEnv{db: db, hits: &atomic.Int32{}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		env.hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	useTLSWebhookClient(t, srv)
	env.target = srv.URL + "/hook"
	env.pageA = models.StatusPage{Name: "A", Slug: "stored-a"}
	env.pageB = models.StatusPage{Name: "B", Slug: "stored-b"}
	db.Create(&env.pageA)
	db.Create(&env.pageB)
	env.r = gin.New()
	env.r.POST("/api/pages/:page_id/subscribers/:id/test-webhook", NewHandler(db).TestStoredSubscriberWebhookHandler)
	return env
}

func (env *storedWebhookEnv) subscriber(page models.StatusPage, channel, webhookURL string) models.Subscriber {
	sub := models.Subscriber{StatusPageID: page.ID, Channel: channel, WebhookURL: webhookURL}
	if channel == "email" {
		sub.Email = "abo@example.com"
	}
	env.db.Create(&sub)
	return sub
}

func (env *storedWebhookEnv) call(pageID uint, subID string) (int, map[string]any) {
	w := httptest.NewRecorder()
	url := "/api/pages/" + strconv.FormatUint(uint64(pageID), 10) + "/subscribers/" + subID + "/test-webhook"
	env.r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, url, strings.NewReader("{}")))
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func idOf(sub models.Subscriber) string { return strconv.FormatUint(uint64(sub.ID), 10) }

func TestStoredWebhookTest_SendetAnGespeicherteURL(t *testing.T) {
	env := newStoredWebhookEnv(t)
	sub := env.subscriber(env.pageA, "google_chat", env.target)

	status, out := env.call(env.pageA.ID, idOf(sub))

	if status != http.StatusOK {
		t.Fatalf("Status %d (%v), erwartet 200", status, out)
	}
	if env.hits.Load() != 1 {
		t.Errorf("Webhook-Ziel %d-mal aufgerufen, erwartet 1", env.hits.Load())
	}
}

// Mandantentrennung: Ein Abonnent einer anderen Seite ist über Seite A nicht
// erreichbar – dieselbe 404 wie bei einer unbekannten ID, nichts geht raus.
func TestStoredWebhookTest_AbonnentAndererSeite404(t *testing.T) {
	env := newStoredWebhookEnv(t)
	other := env.subscriber(env.pageB, "google_chat", env.target)

	for name, id := range map[string]string{"andere Seite": idOf(other), "unbekannt": "999"} {
		status, out := env.call(env.pageA.ID, id)
		if status != http.StatusNotFound || out["code"] != "subscriber_not_found" {
			t.Errorf("%s: Status %d (%v), erwartet 404 subscriber_not_found", name, status, out)
		}
	}
	if env.hits.Load() != 0 {
		t.Errorf("Webhook-Ziel %d-mal aufgerufen, erwartet 0", env.hits.Load())
	}
}

func TestStoredWebhookTest_EmailAbonnentHatKeinenWebhook(t *testing.T) {
	env := newStoredWebhookEnv(t)
	sub := env.subscriber(env.pageA, "email", "")

	status, out := env.call(env.pageA.ID, idOf(sub))

	if status != http.StatusBadRequest || out["code"] != "channel_invalid" {
		t.Errorf("Status %d (%v), erwartet 400 channel_invalid", status, out)
	}
}

// Die gespeicherte URL wird vor dem Senden erneut geprüft (Altbestand von
// vor der https-Pflicht, inzwischen interne DNS-Antwort).
func TestStoredWebhookTest_PrueftGespeicherteURLErneut(t *testing.T) {
	env := newStoredWebhookEnv(t)
	sub := env.subscriber(env.pageA, "google_chat", "http://chat.example.com/alt")

	status, out := env.call(env.pageA.ID, idOf(sub))

	if status != http.StatusBadRequest || out["code"] != "webhook_https_only" {
		t.Errorf("Status %d (%v), erwartet 400 webhook_https_only", status, out)
	}
	if env.hits.Load() != 0 {
		t.Errorf("Webhook-Ziel %d-mal aufgerufen, erwartet 0", env.hits.Load())
	}
}

func TestStoredWebhookTest_UngueltigeIDs(t *testing.T) {
	env := newStoredWebhookEnv(t)
	w := httptest.NewRecorder()
	env.r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/pages/x/subscribers/1/test-webhook", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("page_id x: Status %d, erwartet 400", w.Code)
	}
	status, out := env.call(env.pageA.ID, "abc")
	if status != http.StatusBadRequest || out["code"] != "invalid_id" {
		t.Errorf("id abc: Status %d (%v), erwartet 400 invalid_id", status, out)
	}
}
