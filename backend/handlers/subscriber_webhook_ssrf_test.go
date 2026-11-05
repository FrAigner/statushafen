package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// TestIsBlockedWebhookIP prüft die Grundbausteine der SSRF-Filterung, die
// sowohl validateWebhookURL als auch safeDialContext verwenden.
func TestIsBlockedWebhookIP(t *testing.T) {
	blocked := []string{"127.0.0.1", "10.0.0.5", "192.168.1.1", "169.254.1.1", "0.0.0.0", "::1"}
	for _, s := range blocked {
		if !isBlockedWebhookIP(net.ParseIP(s)) {
			t.Errorf("expected %s to be blocked", s)
		}
	}

	allowed := []string{"93.184.216.34", "8.8.8.8"}
	for _, s := range allowed {
		if isBlockedWebhookIP(net.ParseIP(s)) {
			t.Errorf("expected %s to be allowed", s)
		}
	}
}

// TestIsBlockedWebhookIP_SpecialPurpose (Security-Review): Die
// IANA-Special-Purpose-Bereiche sind gesperrt (gilt für Webhooks und die
// SAML-Metadaten gleichermaßen), jeweils erste und letzte Adresse, daneben
// liegende öffentliche Adressen nicht.
func TestIsBlockedWebhookIP_SpecialPurpose(t *testing.T) {
	for _, tc := range []struct {
		ip      string
		blocked bool
	}{
		{"100.64.0.0", true}, {"100.127.255.255", true}, {"100.128.0.0", false}, {"100.63.255.255", false},
		{"198.18.0.0", true}, {"198.19.255.255", true}, {"198.20.0.0", false},
		{"192.0.0.1", true}, {"192.0.1.1", false},
		{"192.0.2.10", true}, {"198.51.100.7", true}, {"203.0.113.200", true}, {"203.0.114.1", false},
		{"240.0.0.1", true}, {"255.255.255.255", true},
		{"224.0.0.1", true}, {"239.255.255.255", true}, {"223.255.255.255", false},
		{"ff02::1", true}, {"64:ff9b::808:808", true}, {"2002:c000:204::1", true},
		{"2001:db8::1", true}, {"2001:4860:4860::8888", false},
		// IPv4 in IPv6 gemappt zählt wie IPv4.
		{"::ffff:100.64.0.1", true}, {"::ffff:8.8.8.8", false},
	} {
		if got := isBlockedWebhookIP(net.ParseIP(tc.ip)); got != tc.blocked {
			t.Errorf("%s: blocked=%v, expected %v", tc.ip, got, tc.blocked)
		}
	}
	if !isBlockedWebhookIP(nil) {
		t.Error("ungültige IP muss gesperrt sein")
	}
}

// TestSafeDialContext_RejectsDNSRebindingBetweenValidationAndDial covers
// validateWebhookURL performs a DNS lookup once, before
// webhookTestClient.Post() is called; the underlying dialer used to perform
// its OWN, independent second lookup at actual connect time. If an attacker
// controls the DNS answer and changes it between the two lookups (DNS
// rebinding), the first (validation) lookup can return a public IP while the
// second (connect) lookup returns an internal one - a classic TOCTOU SSRF
// bypass.
//
// safeDialContext closes this window by being the ONLY place that resolves
// the host for the actual connection - there is no second, independent
// lookup left that could disagree with what was just checked. This test
// simulates rebinding by making webhookLookupIP return a public IP on the
// first call (as used by validateWebhookURL) and an internal IP on the
// second call (as used by safeDialContext at dial time), and asserts that
// safeDialContext still refuses to connect.
func TestSafeDialContext_RejectsDNSRebindingBetweenValidationAndDial(t *testing.T) {
	orig := webhookLookupIP
	defer func() { webhookLookupIP = orig }()

	callCount := 0
	webhookLookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		callCount++
		if callCount == 1 {
			// First lookup (validateWebhookURL): looks like a legitimate public host.
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		}
		// Second lookup (safeDialContext, at actual connect time): DNS has been
		// rebound to an internal address by the attacker.
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}

	if err := validateWebhookURL("https://rebinding.example.test/webhook"); err != nil {
		t.Fatalf("expected validateWebhookURL to pass on the first (public) DNS answer, got error: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected exactly 1 lookup during validation, got %d", callCount)
	}

	conn, err := safeDialContext(context.Background(), "tcp", "rebinding.example.test:443")
	if err == nil {
		if conn != nil {
			_ = conn.Close()
		}
		t.Fatal("expected safeDialContext to reject the connection after DNS rebound to an internal address, but it succeeded")
	}
}

// TestSafeDialContext_RejectsLiteralInternalIP covers the simpler, non-DNS
// case: a webhook target given directly as a blocked IP literal (no
// rebinding needed) must also be rejected at dial time.
func TestSafeDialContext_RejectsLiteralInternalIP(t *testing.T) {
	_, err := safeDialContext(context.Background(), "tcp", "127.0.0.1:8080")
	if err == nil {
		t.Fatal("expected safeDialContext to reject a literal loopback address")
	}
}

// TestSubscribeHandler_RejectsInternalWebhookURL covers the gap where
// validateWebhookURL was wired only into TestWebhookHandler: SubscribeHandler
// itself accepted any webhook_url unchecked, so an attacker could subscribe
// an internal URL that would only be dialed later, unguarded, when a real
// incident fired (see the plain http.Post calls in incident.go). This
// asserts SubscribeHandler now rejects an internal target up front.
func TestSubscribeHandler_RejectsInternalWebhookURL(t *testing.T) {
	orig := webhookLookupIP
	defer func() { webhookLookupIP = orig }()
	webhookLookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}

	db := testutils.SetupTestDB()
	h := NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/subscribe", h.SubscribeHandler)

	page := models.StatusPage{Name: "SSRF Page", Slug: "ssrf-page"}
	db.Create(&page)

	payload := map[string]interface{}{
		"channel":       "google_chat",
		"webhook_url":   "https://internal.example.test/hook",
		"component_ids": "all",
		"slug":          "ssrf-page",
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "/api/subscribe", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for internal webhook URL, got %d. Body: %s", w.Code, w.Body.String())
	}

	var count int64
	db.Model(&models.Subscriber{}).Where("status_page_id = ?", page.ID).Count(&count)
	if count != 0 {
		t.Fatalf("expected no subscriber to be created for a rejected internal webhook URL, found %d", count)
	}
}
