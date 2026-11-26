package handlers_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var orderConfirmLinkRe = regexp.MustCompile(`/order/confirm\?token=([0-9a-f]+)`)

func extractOrderConfirmToken(t *testing.T, msg string) string {
	t.Helper()
	m := orderConfirmLinkRe.FindStringSubmatch(msg)
	require.NotNil(t, m, "Bestätigungslink fehlt in der Mail: %s", msg)
	return m[1]
}

// rawColumn reads a column without a model so that the test sees the stored
// value and not the interpretation by the field.
func rawColumn(t *testing.T, h *handlers.Handler, query string, args ...any) string {
	t.Helper()
	var v string
	require.NoError(t, h.DB.Raw(query, args...).Scan(&v).Error)
	return v
}

// The webhook token is stored in the database only as SHA-256: the plaintext is
// delivered once at creation, so a database or backup leak yields
// no usable token. The stored hash itself is worthless as a token
// (control check), the plaintext works, and a new token invalidates the old one.
func TestWebhookToken_NurAlsHashGespeichert(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/admin/pages/:page_id/webhook-token", h.GenerateWebhookTokenHandler)
	r.POST("/api/webhooks/component-slugs/:slug/status", h.ComponentStatusWebhookBySlugHandler)
	page := models.StatusPage{Name: "Hash Seite", Slug: "hash-seite"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&models.Component{StatusPageID: page.ID, Name: "API", Slug: "api", Status: "operational"}).Error)

	w := doWebhookTokenRequest(r, http.MethodPost, itoa(page.ID))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token, _ := responseField(t, w, "token").(string)
	require.Len(t, token, 64)

	stored := rawColumn(t, h, "SELECT webhook_token FROM status_pages WHERE id = ?", page.ID)
	assert.NotEmpty(t, stored)
	assert.NotContains(t, stored, token, "der Klartext steht nicht in der Datenbank")
	assert.True(t, strings.HasPrefix(stored, "sha256:"), "gespeichert wird der Hash: %q", stored)

	assert.Equal(t, http.StatusOK, slugWebhookPost(r, "api", "major_outage", token).Code, "Klartext-Token funktioniert")
	assert.Equal(t, http.StatusUnauthorized, slugWebhookPost(r, "api", "operational", stored).Code, "der Hash ist als Token wertlos")
	assert.Equal(t, http.StatusUnauthorized, slugWebhookPost(r, "api", "operational", strings.TrimPrefix(stored, "sha256:")).Code, "auch der Hash ohne Präfix")

	w = doWebhookTokenRequest(r, http.MethodPost, itoa(page.ID))
	require.Equal(t, http.StatusOK, w.Code)
	neu, _ := responseField(t, w, "token").(string)
	assert.NotEqual(t, token, neu)
	assert.Equal(t, http.StatusUnauthorized, slugWebhookPost(r, "api", "operational", token).Code, "der alte Token ist nach dem Erneuern ungültig")
	assert.Equal(t, http.StatusOK, slugWebhookPost(r, "api", "operational", neu).Code)
}

// The confirmation and password tokens of an order are likewise stored
// only as a hash in the database; the mail carries the plaintext, redemption
// compares the hash.
func TestOrderTokens_NurAlsHashGespeichert(t *testing.T) {
	srv := startFakeSMTPServerWithEnv(t)
	defer srv.close()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := setupOrderRouter(h)
	seedPageAdminRole(t, db)

	w := doStatusPageRequest(r, "POST", "/api/public/orders", map[string]interface{}{
		"company_name": "Hash GmbH", "email": "hash-bestellung@example.com",
	})
	requireStatusCode(t, w, 201)
	token := extractOrderConfirmToken(t, awaitSMTPMessage(t, srv))
	h.WaitMails()

	stored := rawColumn(t, h, "SELECT confirm_token FROM page_orders WHERE email = ?", "hash-bestellung@example.com")
	assert.NotContains(t, stored, token, "der Klartext steht nicht in der Datenbank")
	assert.True(t, strings.HasPrefix(stored, "sha256:"), "gespeichert wird der Hash: %q", stored)

	requireStatusCode(t, doStatusPageRequest(r, "POST", "/api/public/orders/confirm", map[string]interface{}{"token": stored}), http.StatusBadRequest)
	w = doStatusPageRequest(r, "POST", "/api/public/orders/confirm", map[string]interface{}{"token": token})
	requireStatusCode(t, w, http.StatusOK)
	pwToken, _ := responseField(t, w, "set_password_token").(string)
	require.Len(t, pwToken, 64)

	storedPw := rawColumn(t, h, "SELECT set_password_token FROM page_orders WHERE email = ?", "hash-bestellung@example.com")
	assert.NotContains(t, storedPw, pwToken)
	assert.True(t, strings.HasPrefix(storedPw, "sha256:"), "gespeichert wird der Hash: %q", storedPw)
	assert.Empty(t, rawColumn(t, h, "SELECT confirm_token FROM page_orders WHERE email = ?", "hash-bestellung@example.com"), "verbraucht")

	requireStatusCode(t, doStatusPageRequest(r, "POST", "/api/public/orders/set-password", map[string]interface{}{"token": storedPw, "password": orderTestPass}), http.StatusBadRequest)
	requireStatusCode(t, doStatusPageRequest(r, "POST", "/api/public/orders/set-password", map[string]interface{}{"token": pwToken, "password": orderTestPass}), http.StatusOK)
}

// orderTestPass: made-up test password for setting the order password.
const orderTestPass = "Sehr-gutes-Passwort-42"
