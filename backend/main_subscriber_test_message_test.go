package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/middleware"

	"github.com/stretchr/testify/assert"
)

const testMessageBody = `{"Channel":"sms","Email":"a@example.com"}`

func postSubscriberTestMessage(w *permWorld, token, addr string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/pages/"+itoa(w.a.page.ID)+"/subscribers/test", strings.NewReader(testMessageBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", token)
	req.RemoteAddr = addr
	rec := httptest.NewRecorder()
	w.r.ServeHTTP(rec, req)
	return rec.Code
}

// Creating or editing subscribers is enough to send a test message; reading
// is not. The body is invalid on purpose: the guard runs first, nothing goes out.
func TestRouter_SubscriberTestMessage_NeedsCreateOrUpdate(t *testing.T) {
	w := newPermWorld(t)
	for perm, want := range map[string]int{
		"subscribers:create": http.StatusBadRequest,
		"subscribers:update": http.StatusBadRequest,
		"subscribers:read":   http.StatusForbidden,
		"subscribers:delete": http.StatusForbidden,
	} {
		assert.Equal(t, want, postSubscriberTestMessage(w, w.user(listOf(perm)), "203.0.113.20:1"), perm)
	}
}

// Tight limit per client: 5 test messages per minute, then 429 until the
// window has passed.
func TestRouter_SubscriberTestMessage_RateLimited(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	middleware.SetRateLimiterClockForTests(t.Cleanup, func() time.Time { return now })
	w := newPermWorld(t)
	tok := w.user(listOf("subscribers:create"))
	const addr = "203.0.113.21:1"
	for i := 0; i < 5; i++ {
		assert.Equal(t, http.StatusBadRequest, postSubscriberTestMessage(w, tok, addr), "request %d", i+1)
	}
	assert.Equal(t, http.StatusTooManyRequests, postSubscriberTestMessage(w, tok, addr))
	now = now.Add(61 * time.Second)
	assert.Equal(t, http.StatusBadRequest, postSubscriberTestMessage(w, tok, addr), "window passed")
}

func TestAnyActionGuard_RegistersAllActions(t *testing.T) {
	g := anyAction("subscribers", "create", "update")
	assert.Equal(t, modeAnyAction, g.Mode)
	assert.Equal(t, []string{"subscribers"}, g.Resources)
	assert.Equal(t, "create", g.Action, "first action counts for the HTTP method check")
	assert.Equal(t, []string{"create", "update"}, g.Actions)
	info, ok := guardedRoutesAfterRouter(t)["POST /api/pages/:page_id/subscribers/test"]
	assert.True(t, ok, "route registered via guarded()")
	assert.Equal(t, []string{"create", "update"}, info.Actions)
}

func guardedRoutesAfterRouter(t *testing.T) map[string]routeInfo {
	t.Helper()
	newPermTestRouter(t)
	return guardedRoutes()
}
