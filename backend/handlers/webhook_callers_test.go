package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/fraigner/statushafen/models"
)

// Die vier Aufrufer von postWebhook (Test-Endpunkt, Subscriber-
// Webhook, globaler Webhook, externe Vorfälle) reagieren auf 429/503/4xx wie
// beschrieben. Vor dem Fix beantwortete der Test-Endpunkt jeden Fehler mit
// 500 ohne Code, und die Benachrichtigungen versuchten es nie ein zweites Mal.

// useTLSWebhookClient lässt webhookTestClient dem TLS-Zertifikat des
// httptest-Servers vertrauen. Der Dialer bleibt safeDialContext (SSRF-Schutz).
func useTLSWebhookClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := webhookTestClient
	tr := srv.Client().Transport.(*http.Transport).Clone()
	tr.DialContext = safeDialContext
	webhookTestClient = &http.Client{Timeout: orig.Timeout, CheckRedirect: orig.CheckRedirect, Transport: tr}
	t.Cleanup(func() { webhookTestClient = orig })
}

func callTestWebhook(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/subscribe/test-webhook", (&Handler{}).TestWebhookHandler)
	body, _ := json.Marshal(map[string]string{"webhook_url": url})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/subscribe/test-webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestTestWebhookHandler_AntwortenJeFehlerklasse(t *testing.T) {
	cases := []struct {
		name       string
		resp       fakeResp
		wantStatus int
		wantCode   string
		wantRetry  float64
	}{
		{"Erfolg", fakeResp{200, ""}, 200, "", 0},
		{"429 mit Retry-After", fakeResp{429, "12"}, 429, "webhook_rate_limited", 12},
		{"429 ohne Retry-After", fakeResp{429, ""}, 429, "webhook_rate_limited", 1},
		{"503", fakeResp{503, ""}, 502, "webhook_unreachable", 0},
		{"400", fakeResp{400, ""}, 502, "webhook_rejected", 0},
		{"403", fakeResp{403, ""}, 502, "webhook_rejected", 0},
		{"404", fakeResp{404, ""}, 502, "webhook_rejected", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newWebhookEnv(t)
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.resp.retryAfter != "" {
					w.Header().Set("Retry-After", tc.resp.retryAfter)
				}
				w.WriteHeader(tc.resp.status)
			}))
			t.Cleanup(srv.Close)
			useTLSWebhookClient(t, srv)
			// Die Test-URL hat https; 127.0.0.1 ist per Test-Ausnahme erlaubt.
			status, body := callTestWebhook(t, srv.URL+"/hook")
			if status != tc.wantStatus {
				t.Fatalf("Status = %d (%v), erwartet %d", status, body, tc.wantStatus)
			}
			if tc.wantCode != "" && body["code"] != tc.wantCode {
				t.Errorf("code = %v, erwartet %s", body["code"], tc.wantCode)
			}
			if tc.wantRetry != 0 && body["retry_after"] != tc.wantRetry {
				t.Errorf("retry_after = %v, erwartet %v", body["retry_after"], tc.wantRetry)
			}
			if tc.wantStatus != 429 {
				if _, has := body["retry_after"]; has {
					t.Errorf("retry_after nur bei 429, Antwort: %v", body)
				}
			}
			if len(env.sleeps) != 0 {
				t.Errorf("der Test-Endpunkt darf nie warten: %v", env.sleeps)
			}
			if raw, _ := json.Marshal(body); strings.Contains(string(raw), "127.0.0.1") || strings.Contains(string(raw), "Status:") {
				t.Errorf("Antwort enthält URL oder rohen Servertext: %s", raw)
			}
		})
	}
}

func TestTestWebhookHandler_Netzfehler(t *testing.T) {
	newWebhookEnv(t)
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	useTLSWebhookClient(t, srv)
	url := srv.URL
	srv.Close()
	status, body := callTestWebhook(t, url)
	if status != 502 || body["code"] != "webhook_unreachable" {
		t.Errorf("Status %d, Antwort %v; erwartet 502 webhook_unreachable", status, body)
	}
}

func notifyCtx() notificationDispatchContext {
	page := models.StatusPage{Name: "Öffentlich"}
	page.ID = 1
	return notificationDispatchContext{inc: models.Incident{Title: "T", Status: "investigating", Message: "M"}, action: "created", page: page}
}

func TestBenachrichtigungen_WiederholenBei429(t *testing.T) {
	type caller struct {
		name string
		call func(t *testing.T, url string) error
	}
	callers := []caller{
		{"Subscriber-Webhook", func(t *testing.T, url string) error {
			(&Handler{}).notifySubscriberGoogleChat(notifyCtx(), models.Subscriber{WebhookURL: url})
			return nil
		}},
		{"globaler Webhook", func(t *testing.T, url string) error {
			t.Setenv("GOOGLE_CHAT_WEBHOOK_URL", url)
			dispatchGlobalGoogleChatWebhook(notifyCtx())
			return nil
		}},
		{"externer Vorfall", func(t *testing.T, url string) error {
			m := &extIncidentMail{}
			m.ev.Incident.Title = "Ext"
			return (&Handler{}).sendExternalIncidentWebhook(m, models.Subscriber{WebhookURL: url})
		}},
	}
	for _, c := range callers {
		t.Run(c.name+": 429 dann OK", func(t *testing.T) {
			env := newWebhookEnv(t)
			srv, calls := sequenceServer(t, []fakeResp{{429, "2"}, {200, ""}})
			if err := c.call(t, srv.URL); err != nil {
				t.Fatalf("Fehler: %v", err)
			}
			if *calls != 2 || len(env.sleeps) != 1 {
				t.Errorf("Aufrufe=%d Wartezeiten=%v, erwartet 2 Aufrufe nach einer Wartezeit", *calls, env.sleeps)
			}
		})
		t.Run(c.name+": 403 wird nicht wiederholt", func(t *testing.T) {
			env := newWebhookEnv(t)
			srv, calls := sequenceServer(t, []fakeResp{{403, ""}, {200, ""}})
			_ = c.call(t, srv.URL)
			if *calls != 1 || len(env.sleeps) != 0 {
				t.Errorf("Aufrufe=%d Wartezeiten=%v, erwartet 1 Aufruf", *calls, env.sleeps)
			}
		})
	}
}

func TestExternerVorfallWebhook_FehlerOhneURL(t *testing.T) {
	newWebhookEnv(t)
	srv, _ := sequenceServer(t, []fakeResp{{403, ""}})
	m := &extIncidentMail{}
	err := (&Handler{}).sendExternalIncidentWebhook(m, models.Subscriber{WebhookURL: srv.URL + "/geheim?key=GEHEIM"})
	if err == nil || err.Error() != "webhook status 403" {
		t.Fatalf("Fehler = %v, erwartet \"webhook status 403\"", err)
	}
	srv.Close()
	err = (&Handler{}).sendExternalIncidentWebhook(m, models.Subscriber{WebhookURL: srv.URL + "/geheim?key=GEHEIM"})
	if err == nil || strings.Contains(err.Error(), "GEHEIM") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("Netzfehler darf die URL nicht preisgeben: %v", err)
	}
	if err := (&Handler{}).sendExternalIncidentWebhook(m, models.Subscriber{}); err == nil {
		t.Error("ohne URL muss ein Fehler kommen")
	}
}
