package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/testutils"
)

// TestMain switches the password login on for this package: many router
// tests sign in with a password as a fixture. The default (off) is tested
// explicitly below and in the handlers package.
func TestMain(m *testing.M) {
	_ = os.Setenv("PASSWORD_LOGIN_ENABLED", "true")
	os.Exit(m.Run())
}

// With the switch off, the real router answers the password endpoints with
// 403 password_login_disabled before any other check.
func TestRouter_PasswordEndpointsWhenPasswordLoginIsOff(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	t.Setenv("PASSWORD_LOGIN_ENABLED", "")
	r := newRouter(handlers.NewHandler(testutils.SetupTestDB()))
	for _, rq := range []struct{ path, body string }{
		{"/api/login", `{"email":"admin@example.org","password":"geheim-geheim"}`},
		{"/api/public/orders/set-password", `{"token":"x","password":"geheim-geheim"}`},
	} {
		req := httptest.NewRequest(http.MethodPost, rq.path, strings.NewReader(rq.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"code":"password_login_disabled"`) {
			t.Errorf("POST %s: %d %s, want 403 password_login_disabled", rq.path, w.Code, w.Body.String())
		}
	}
}
