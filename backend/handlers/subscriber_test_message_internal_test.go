package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// The test message from the admin subscriber form goes to the entered target
// (Google Chat webhook or e-mail) in the admin UI language, before saving, and
// never stores a subscriber.

func newTestMessageRouter(t *testing.T) (*Handler, *gin.Engine, models.StatusPage) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "Acme Status", Slug: "acme-test-msg"}
	require.NoError(t, db.Create(&page).Error)
	h := NewHandler(db)
	r := gin.New()
	r.POST("/api/pages/:page_id/subscribers/test", h.SendSubscriberTestMessageHandler)
	return h, r, page
}

func postTestMessage(t *testing.T, r *gin.Engine, pageID string, body any) (int, map[string]any) {
	t.Helper()
	var raw []byte
	if s, ok := body.(string); ok {
		raw = []byte(s)
	} else {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/pages/"+pageID+"/subscribers/test", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func captureTLSWebhook(t *testing.T, status int) (*httptest.Server, func() string) {
	t.Helper()
	var mu sync.Mutex
	var text string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]string
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		text = body["text"]
		mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	useTLSWebhookClient(t, srv)
	return srv, func() string {
		mu.Lock()
		defer mu.Unlock()
		return text
	}
}

func testMsgPageID(p models.StatusPage) string { return strconv.FormatUint(uint64(p.ID), 10) }

func TestSubscriberTestMessage_WebhookInAdminLanguage(t *testing.T) {
	cases := []struct {
		lang     string
		contains string
		absent   string
	}{
		{"en", "Test message", "Testnachricht"},
		{"de", "Testnachricht", "Test message"},
		{"en-US", "Test message", "Testnachricht"},
		{"", "Testnachricht", "Test message"},
	}
	for _, tc := range cases {
		t.Run("lang="+tc.lang, func(t *testing.T) {
			newWebhookEnv(t)
			h, r, page := newTestMessageRouter(t)
			srv, got := captureTLSWebhook(t, http.StatusOK)

			status, body := postTestMessage(t, r, testMsgPageID(page), map[string]string{
				"Channel": "google_chat", "WebhookURL": srv.URL + "/hook", "Language": tc.lang,
			})
			require.Equal(t, http.StatusOK, status, body)
			assert.Contains(t, got(), tc.contains)
			assert.NotContains(t, got(), tc.absent)
			assert.Contains(t, got(), "Acme Status", "page name in the message")

			var n int64
			h.DB.Model(&models.Subscriber{}).Count(&n)
			assert.Zero(t, n, "a test message never stores a subscriber")
		})
	}
}

func TestSubscriberTestMessage_WebhookFailures(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		wantStatus int
		wantCode   string
	}{
		{"rejected", http.StatusNotFound, http.StatusBadGateway, "webhook_rejected"},
		{"unreachable", http.StatusServiceUnavailable, http.StatusBadGateway, "webhook_unreachable"},
		{"throttled", http.StatusTooManyRequests, http.StatusTooManyRequests, "webhook_rate_limited"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newWebhookEnv(t)
			_, r, page := newTestMessageRouter(t)
			srv, _ := captureTLSWebhook(t, tc.status)
			status, body := postTestMessage(t, r, testMsgPageID(page), map[string]string{
				"Channel": "google_chat", "WebhookURL": srv.URL + "/hook", "Language": "en",
			})
			assert.Equal(t, tc.wantStatus, status, body)
			assert.Equal(t, tc.wantCode, body["code"])
		})
	}
}

func TestSubscriberTestMessage_InvalidInput(t *testing.T) {
	_, r, page := newTestMessageRouter(t)
	cases := []struct {
		name     string
		pageID   string
		body     any
		wantCode string
	}{
		{"broken body", testMsgPageID(page), "{", "invalid_request"},
		{"invalid page id", "abc", map[string]string{"Channel": "email", "Email": "a@example.com"}, "invalid_id"},
		{"unknown channel", testMsgPageID(page), map[string]string{"Channel": "sms", "Email": "a@example.com"}, "channel_invalid"},
		{"invalid email", testMsgPageID(page), map[string]string{"Channel": "email", "Email": "Foo <a@example.com>"}, "email_invalid"},
		{"empty email", testMsgPageID(page), map[string]string{"Channel": "email"}, "email_invalid"},
		{"empty webhook", testMsgPageID(page), map[string]string{"Channel": "google_chat"}, "webhook_url_required"},
		{"http webhook", testMsgPageID(page), map[string]string{"Channel": "google_chat", "WebhookURL": "http://chat.example.com/hook"}, "webhook_https_only"},
		{"internal webhook", testMsgPageID(page), map[string]string{"Channel": "google_chat", "WebhookURL": "https://127.0.0.1/hook"}, "webhook_internal_address"},
		{"overlong language", testMsgPageID(page), map[string]string{"Channel": "email", "Email": "a@example.com", "Language": string(bytes.Repeat([]byte("x"), 200))}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := postTestMessage(t, r, tc.pageID, tc.body)
			assert.Equal(t, http.StatusBadRequest, status, body)
			if tc.wantCode != "" {
				assert.Equal(t, tc.wantCode, body["code"])
			}
		})
	}
}

func TestSubscriberTestMessage_UnknownPage(t *testing.T) {
	_, r, _ := newTestMessageRouter(t)
	status, body := postTestMessage(t, r, "99999", map[string]string{"Channel": "email", "Email": "a@example.com"})
	assert.Equal(t, http.StatusNotFound, status, body)
	assert.Equal(t, "page_not_found", body["code"])
}

func TestSubscriberTestMessageTexts(t *testing.T) {
	for _, lang := range []string{"de", "en"} {
		subject, chat, mail := subscriberTestMessageTexts(lang, `<Acme & "Co">`)
		assert.NotEmpty(t, subject)
		assert.Contains(t, subject, `<Acme & "Co">`)
		assert.Contains(t, chat, `<Acme & "Co">`)
		assert.Contains(t, mail, "&lt;Acme &amp; &#34;Co&#34;&gt;", "page name escaped in the mail body")
		assert.False(t, hasHeaderInjectionChars(subject) || hasHeaderInjectionChars(mail), "single-line subject and body")
	}
	_, chatEN, _ := subscriberTestMessageTexts("en", "X")
	_, chatDE, _ := subscriberTestMessageTexts("de", "X")
	assert.NotEqual(t, chatEN, chatDE)
}
