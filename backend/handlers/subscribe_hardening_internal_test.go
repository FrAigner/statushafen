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

// mailboxKey ist der Schlüssel für Deduplizierung und das Limit je Postfach:
// Groß-/Kleinschreibung und ein "+tag" im lokalen Teil sind kein anderes Postfach.
func TestMailboxKey(t *testing.T) {
	cases := map[string]string{
		"opfer@example.com":              "opfer@example.com",
		"Opfer@Example.COM":              "opfer@example.com",
		"  OPFER@example.com ":           "opfer@example.com",
		"opfer+1@example.com":            "opfer@example.com",
		"Opfer+Newsletter+x@EXAMPLE.com": "opfer@example.com",
		"opfer+@example.com":             "opfer@example.com",
		"a.b@example.com":                "a.b@example.com", // Punkte sind providerabhängig, bleiben
		"+x@example.com":                 "+x@example.com",  // leerer Rest: nicht kürzen
		"ohne-at":                        "ohne-at",
	}
	for in, want := range cases {
		assert.Equal(t, want, mailboxKey(in), in)
	}
}

// Die öffentliche Anmeldung nimmt als Google-Chat-Ziel nur Webhooks von
// chat.googleapis.com an (Port 443, Pfad /v1/spaces/…). Es gibt dafür keine
// DNS-Auflösung und für jede Ablehnung denselben Code, damit weder interne
// Hostnamen erkennbar sind noch der Server als Proxy für beliebige Ziele dient.
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
		"https://victim-api.example.net:8443/admin/api/v1/hooks?x=1", // fremder Host und Port
		"https://victim-api.example.net/hook",                        // fremder Host
		"https://chat.googleapis.com:8443/v1/spaces/AAAA/messages",   // falscher Port
		"https://chat.googleapis.com/admin",                          // falscher Pfad
		"https://chat.googleapis.com.evil.example/v1/spaces/A/messages",
		"https://evilchat.googleapis.com/v1/spaces/A/messages",
		"https://user:pw@chat.googleapis.com/v1/spaces/A/messages", // Zugangsdaten
		"https://chat.googleapis.com@evil.example/v1/spaces/A/messages",
		"http://chat.googleapis.com/v1/spaces/A/messages", // kein https
		"https://127.0.0.1/hook",                          // intern
		"https://intern.example.test/hook",                // würde intern auflösen
		"https://gibt-es-nicht.invalid/hook",              // nicht auflösbar
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

	// Pflichtfeld bleibt ein eigener Fehler, die E-Mail-Prüfung ist unberührt.
	assert.Equal(t, "webhook_url_required", codeOf(check("")))
}

// HTTP-Ebene: intern auflösender und nicht auflösbarer Host ergeben dieselbe
// Antwort (kein DNS-Orakel), es wird kein Abo angelegt.
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

	// Gegenprobe: ein echtes Google-Chat-Ziel wird ohne DNS-Auflösung akzeptiert.
	ok := post("https://chat.googleapis.com/v1/spaces/AAAA1234/messages?key=k&token=t")
	assert.Equal(t, http.StatusOK, ok.Code, ok.Body.String())
}

// Scheitert die Zählung, gilt das Kontingent als erschöpft (fail closed): lieber
// keine Bestätigungsmail als unbegrenzt viele an eine fremde Mailbox.
func TestConfirmationMailBudgetExhausted_DBErrorFailsClosed(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	assert.False(t, h.confirmationMailBudgetExhausted("leser@example.org"), "ohne Abos und ohne Fehler ist Platz")

	injectDBError(t, db, "query", "subscribers", 0)

	assert.True(t, h.confirmationMailBudgetExhausted("leser@example.org"))
}
