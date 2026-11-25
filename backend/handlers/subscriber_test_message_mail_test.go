package handlers_test

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// E-mail variant of the subscriber test message: sent right away (the admin
// waits for the result), in the admin UI language, without storing anything.

func sendTestMail(t *testing.T, h *handlers.Handler, page models.StatusPage, lang string) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/pages/:page_id/subscribers/test", h.SendSubscriberTestMessageHandler)
	raw, _ := json.Marshal(map[string]string{"Channel": "email", "Email": "ops@example.com", "Language": lang})
	req := httptest.NewRequest(http.MethodPost, "/api/pages/"+strconv.FormatUint(uint64(page.ID), 10)+"/subscribers/test", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func seedTestMailPage(t *testing.T) (*handlers.Handler, models.StatusPage) {
	t.Helper()
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "Acme Status", Slug: "acme-test-mail"}
	require.NoError(t, db.Create(&page).Error)
	return handlers.NewHandler(db), page
}

func TestSubscriberTestMessage_MailInAdminLanguage(t *testing.T) {
	for _, tc := range []struct{ lang, want, absent string }{
		{"en", "Test message", "Testnachricht"},
		{"de", "Testnachricht", "Test message"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			srv := startFakeSMTPServerWithEnv(t)
			defer srv.close()
			h, page := seedTestMailPage(t)

			status, body := sendTestMail(t, h, page, tc.lang)
			require.Equal(t, http.StatusOK, status, body)

			msg := awaitSMTPMessage(t, srv)
			assert.Equal(t, "ops@example.com", headerLine(msg, "To"))
			assert.Contains(t, headerLine(msg, "Subject"), tc.want)
			assert.Contains(t, headerLine(msg, "Subject"), "Acme Status")
			assert.NotContains(t, msg, tc.absent)

			var n int64
			h.DB.Model(&models.Subscriber{}).Count(&n)
			assert.Zero(t, n, "a test message never stores a subscriber")
		})
	}
}

func TestSubscriberTestMessage_MailServerFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, ln.Close()) // nothing listens there any more
	t.Setenv("SMTP_HOST", host)
	t.Setenv("SMTP_PORT", port)
	h, page := seedTestMailPage(t)

	status, body := sendTestMail(t, h, page, "en")
	assert.Equal(t, http.StatusBadGateway, status, body)
	assert.Equal(t, "mail_send_failed", body["code"])
}

func TestSubscriberTestMessage_MailDisabled(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	h, page := seedTestMailPage(t)
	disableEmail(t, h.DB)

	status, body := sendTestMail(t, h, page, "en")
	assert.Equal(t, http.StatusConflict, status, body)
	assert.Equal(t, "email_disabled", body["code"])
}

// A page name with CR/LF must not reach the Subject header.
func TestSubscriberTestMessage_HeaderInjectionIsRejected(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "Acme\r\nBcc: victim@example.com", Slug: "acme-inject"}
	require.NoError(t, db.Create(&page).Error)

	status, body := sendTestMail(t, handlers.NewHandler(db), page, "en")
	assert.Equal(t, http.StatusInternalServerError, status, body)
	assert.Equal(t, "server_error", body["code"])
}
