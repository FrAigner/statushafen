package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// mailboxKey is the key for deduplication and the per-mailbox limit:
// case and a "+tag" in the local part do not make a different mailbox.
func TestMailboxKey(t *testing.T) {
	cases := map[string]string{
		"opfer@example.com":              "opfer@example.com",
		"Opfer@Example.COM":              "opfer@example.com",
		"  OPFER@example.com ":           "opfer@example.com",
		"opfer+1@example.com":            "opfer@example.com",
		"Opfer+Newsletter+x@EXAMPLE.com": "opfer@example.com",
		"opfer+@example.com":             "opfer@example.com",
		"a.b@example.com":                "a.b@example.com", // Dots are provider-dependent, stay
		"+x@example.com":                 "+x@example.com",  // empty remainder: do not truncate
		"ohne-at":                        "ohne-at",
	}
	for in, want := range cases {
		assert.Equal(t, want, mailboxKey(in), in)
	}
}

// The public signup accepts as Google Chat target only webhooks from
// chat.googleapis.com (port 443, path /v1/spaces/…). No DNS resolution
// takes place and every rejection returns the same code, so internal
// hostnames cannot be detected and the server does not serve as a proxy for arbitrary targets.
func TestValidateSubscribeRequest_WebhookAllowlist(t *testing.T) {
	orig := webhookLookupIP
	t.Cleanup(func() { webhookLookupIP = orig })
	webhookLookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		t.Errorf("unerwartete DNS-Auflösung für %q", host)
		return nil, errors.New("kein DNS erwartet")
	}

	check := func(url string) error {
		return validateSubscribeRequest(SubscribeRequest{Channel: "google_chat", WebhookURL: url, ComponentIDs: "all", Slug: "s"})
	}
	codeOf := func(err error) string {
		var ce *codedError
		require.True(t, errors.As(err, &ce), "codedError erwartet, bekam %v", err)
		return ce.code
	}

	allowed := []string{
		"https://chat.googleapis.com/v1/spaces/AAAA1234/messages?key=k&token=t",
		"https://chat.googleapis.com:443/v1/spaces/AAAA1234/messages?key=k&token=t",
		"https://CHAT.googleapis.com/v1/spaces/AAAA1234/messages?key=k&token=t",
	}
	for _, u := range allowed {
		assert.NoError(t, check(u), u)
	}

	rejected := []string{
		"https://victim-api.example.net:8443/admin/api/v1/hooks?x=1", // foreign host and port
		"https://victim-api.example.net/hook",                        // foreign host
		"https://chat.googleapis.com:8443/v1/spaces/AAAA/messages",   // wrong port
		"https://chat.googleapis.com/admin",                          // wrong path
		"https://chat.googleapis.com.evil.example/v1/spaces/A/messages",
		"https://evilchat.googleapis.com/v1/spaces/A/messages",
		"https://user:pw@chat.googleapis.com/v1/spaces/A/messages", // credentials
		"https://chat.googleapis.com@evil.example/v1/spaces/A/messages",
		"http://chat.googleapis.com/v1/spaces/A/messages", // no https
		"https://127.0.0.1/hook",                          // internal
		"https://intern.example.test/hook",                // would resolve internally
		"https://gibt-es-nicht.invalid/hook",              // not resolvable
		"ftp://chat.googleapis.com/v1/spaces/A",
		"nicht-eine-url",
	}
	var codes []string
	for _, u := range rejected {
		err := check(u)
		require.Error(t, err, u)
		codes = append(codes, codeOf(err))
	}
	for _, c := range codes {
		assert.Equal(t, "webhook_url_invalid", c, "alle Ablehnungen müssen gleich aussehen")
	}

	// The required field remains its own error; the email check is unaffected.
	assert.Equal(t, "webhook_url_required", codeOf(check("")))
}

// HTTP level: an internally resolving and an unresolvable host give the same
// response (no DNS oracle), no subscription is created.
func TestSubscribeHandler_NoDNSOracleForWebhookHosts(t *testing.T) {
	orig := webhookLookupIP
	t.Cleanup(func() { webhookLookupIP = orig })
	webhookLookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		if host == "intern.example.test" {
			return []net.IP{net.ParseIP("10.0.0.5")}, nil
		}
		return nil, errors.New("nicht auflösbar")
	}

	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/subscribe", h.SubscribeHandler)
	require.NoError(t, db.Create(&models.StatusPage{Name: "P", Slug: "dns-orakel"}).Error)

	post := func(url string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"channel": "google_chat", "webhook_url": url, "component_ids": "all", "slug": "dns-orakel"})
		req, _ := http.NewRequest("POST", "/api/subscribe", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	internal := post("https://intern.example.test/hook")
	unresolvable := post("https://gibt-es-nicht.invalid/hook")
	assert.Equal(t, http.StatusBadRequest, internal.Code)
	assert.Equal(t, internal.Code, unresolvable.Code)
	assert.JSONEq(t, internal.Body.String(), unresolvable.Body.String())
	assert.Contains(t, internal.Body.String(), `"code":"webhook_url_invalid"`)

	var n int64
	db.Model(&models.Subscriber{}).Count(&n)
	assert.EqualValues(t, 0, n)

	// Control case: a real Google Chat target is accepted without DNS resolution.
	ok := post("https://chat.googleapis.com/v1/spaces/AAAA1234/messages?key=k&token=t")
	assert.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
}

// If counting fails, the quota counts as exhausted (fail closed): better
// no confirmation mail than an unlimited number to a foreign mailbox.
func TestConfirmationMailBudgetExhausted_DBErrorFailsClosed(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	assert.False(t, h.confirmationMailBudgetExhausted("leser@example.org"), "ohne Abos und ohne Fehler ist Platz")

	injectDBError(t, db, "query", "subscribers", 0)

	assert.True(t, h.confirmationMailBudgetExhausted("leser@example.org"))
}
