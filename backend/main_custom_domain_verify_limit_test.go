package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The domain check triggers a DNS lookup by the server on every call.
// It is limited per user and per page (10 per minute), so that a page
// admin cannot use the server as a tool for arbitrarily many lookups. The
// pages here have no domain entered: the handler then answers with 400
// (custom_domain_missing) and queries no DNS; the test only checks the limiting.
func TestRouter_CustomDomainVerifyIsLimitedPerUserAndPage(t *testing.T) {
	db := testutils.SetupTestDB()
	r := newRouter(handlers.NewHandler(db))
	pageA := models.StatusPage{Name: "Prüfung A", Slug: "pruefung-a"}
	pageB := models.StatusPage{Name: "Prüfung B", Slug: "pruefung-b"}
	require.NoError(t, db.Create(&pageA).Error)
	require.NoError(t, db.Create(&pageB).Error)
	token := func(email string) string {
		u := models.User{Email: email, IsGlobalAdmin: true}
		require.NoError(t, db.Create(&u).Error)
		tok, err := middleware.GenerateToken(u.ID, u.Email, "", "", true)
		require.NoError(t, err)
		return tok
	}
	alice, bob := token("alice-verify@example.com"), token("bob-verify@example.com")

	verify := func(tok string, page models.StatusPage) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/pages/"+strconv.Itoa(int(page.ID))+"/custom-domain/verify", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.RemoteAddr = "192.0.2.44:6000"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// Control check: the first ten calls run through to the handler.
	for i := 1; i <= 10; i++ {
		w := verify(alice, pageA)
		require.Equal(t, http.StatusBadRequest, w.Code, "Aufruf %d: %s", i, w.Body.String())
		require.Contains(t, w.Body.String(), "custom_domain_missing")
	}

	w := verify(alice, pageA)
	assert.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"code":"rate_limited"`)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))

	assert.Equal(t, http.StatusTooManyRequests, verify(bob, pageA).Code, "die Seite ist für alle Nutzer erschöpft")
	assert.Equal(t, http.StatusTooManyRequests, verify(alice, pageB).Code, "der Nutzer ist für alle Seiten erschöpft")
	assert.Equal(t, http.StatusBadRequest, verify(bob, pageB).Code, "anderer Nutzer auf anderer Seite bleibt unberührt")
}
