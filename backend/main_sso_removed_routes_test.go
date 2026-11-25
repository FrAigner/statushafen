package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/testutils"
)

// SAML has been removed: no route mentions it any more, and the former
// addresses answer 404 like any unknown API route.
func TestRouter_SAMLRoutesRemoved(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	r := newRouter(handlers.NewHandler(testutils.SetupTestDB()))
	for _, ri := range r.Routes() {
		if strings.Contains(strings.ToLower(ri.Path), "saml") {
			t.Errorf("route %s %s still mentions SAML", ri.Method, ri.Path)
		}
	}
	for _, rq := range [][2]string{
		{http.MethodGet, "/api/auth/saml/login?page=1"},
		{http.MethodGet, "/api/auth/saml/1/metadata"},
		{http.MethodPost, "/api/auth/saml/1/acs"},
		{http.MethodPost, "/api/auth/saml/link?page=1"},
		{http.MethodPost, "/api/admin/pages/1/sso/saml-test"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(rq[0], rq[1], nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s: status %d, want 404", rq[0], rq[1], w.Code)
		}
	}
}

// The built-in Google and GitHub OAuth logins are gone; Google now works as a
// generic OIDC provider. The former addresses answer 404.
func TestRouter_GoogleAndGitHubRoutesRemoved(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	r := newRouter(handlers.NewHandler(testutils.SetupTestDB()))
	for _, ri := range r.Routes() {
		if strings.Contains(ri.Path, ":provider") {
			t.Errorf("route %s %s still uses the old :provider pattern", ri.Method, ri.Path)
		}
	}
	for _, path := range []string{
		"/api/auth/google", "/api/auth/google/callback?code=x&state=y",
		"/api/auth/github", "/api/auth/github/callback?code=x&state=y",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", path, w.Code)
		}
	}
}
