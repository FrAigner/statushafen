package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A session handed out on a custom domain can be read by whoever controls the
// domain's DNS (the tenant). It must never reach the account itself: delete
// it, export its data from all pages, unlink a sign-in or link a new one.
// A page SSO session on the main domain keeps these routes (GDPR rights of
// accounts that only sign in through a page SSO).
var customDomainBlockedRoutes = []struct{ method, path, body string }{
	{http.MethodDelete, "/api/me", `{"confirm_email":"v@example.org"}`},
	{http.MethodGet, "/api/me/export", ""},
	{http.MethodDelete, "/api/me/sso/1", ""},
	{http.MethodPost, "/api/auth/oidc/link?page=1", ""},
	{http.MethodPost, "/api/auth/provider/corp/link", ""},
}

func serveWithToken(r http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestRouter_CustomDomainSessionCannotTouchTheAccount(t *testing.T) {
	db := testutils.SetupTestDB()
	u := models.User{Email: "v@example.org"}
	require.NoError(t, db.Create(&u).Error)
	page := models.StatusPage{Name: "Acme", Slug: "acme", CustomDomain: "status.acme.example", CustomDomainStatus: "active"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&models.UserSSOIdentity{UserID: u.ID, StatusPageID: 0, Protocol: models.SSOProtocolOIDC, Issuer: "https://idp.example", Subject: "s"}).Error)
	r := newRouter(handlers.NewHandler(db))

	onDomain, err := middleware.GeneratePageSSOToken(u, "V", models.SSOProtocolOIDC, page.ID, middleware.WithCustomDomain(page.CustomDomain))
	require.NoError(t, err)
	password, err := middleware.GenerateToken(u.ID, u.Email, "", "", false, middleware.WithSSOPageID(page.ID), middleware.WithCustomDomain(page.CustomDomain))
	require.NoError(t, err)

	for _, token := range []string{onDomain, password} {
		for _, rt := range customDomainBlockedRoutes {
			rec := serveWithToken(r, rt.method, rt.path, rt.body, token)
			require.Equal(t, http.StatusForbidden, rec.Code, "%s %s: %s", rt.method, rt.path, rec.Body.String())
			var body map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, "custom_domain_session", body["code"], "%s %s", rt.method, rt.path)
		}
	}
	var n int64
	db.Model(&models.User{}).Where("id = ?", u.ID).Count(&n)
	assert.Equal(t, int64(1), n, "the account still exists")
	db.Model(&models.UserSSOIdentity{}).Where("user_id = ?", u.ID).Count(&n)
	assert.Equal(t, int64(1), n, "the sign-in is still linked")

	// The list of linked sign-ins (provider, e-mail there) stays
	// empty on a custom domain: linking and unlinking are refused there anyway.
	for _, token := range []string{onDomain, password} {
		rec := serveWithToken(r, http.MethodGet, "/api/me/sso", "", token)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"identities":[],"linkable":[]}`, rec.Body.String())
	}

	// The same page session issued on the main domain keeps the routes.
	mainDomain, err := middleware.GeneratePageSSOToken(u, "V", models.SSOProtocolOIDC, page.ID)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, serveWithToken(r, http.MethodGet, "/api/me/export", "", mainDomain).Code)
	assert.Contains(t, serveWithToken(r, http.MethodGet, "/api/me/sso", "", mainDomain).Body.String(), `"protocol":"oidc"`)
}
