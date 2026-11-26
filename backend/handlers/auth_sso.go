package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

func randomToken(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// ssoCodeStore holds short-lived one-time codes against which the frontend exchanges the finished JWT
// (S5 — the token no longer travels through browser history/
// referrer/logs via the redirect query). Deliberately kept simple (in-memory, process-local), analogous to the
// visitor store in middleware.NewRateLimiter.
//
// returnTo is the validated frontend path the login started from (see
// sanitizeReturnTo), "" if none.
// host is the custom domain the code was sent to ("" for FRONTEND_URL); the
// code is redeemed only from there (see SSOExchangeHandler).
// binding is the hash of the browser binding cookie the exchange needs
// (handlers/login_binding.go); "" is never redeemed.
type ssoCodeEntry struct {
	token    string
	returnTo string
	host     string
	binding  string
	expires  time.Time
}

var (
	ssoCodeMu    sync.Mutex
	ssoCodeStore = map[string]ssoCodeEntry{}
)

func storeSSOCode(token, returnTo, host, binding string) string {
	code := randomToken(32)
	ssoCodeMu.Lock()
	ssoCodeStore[code] = ssoCodeEntry{token: token, returnTo: returnTo, host: host, binding: binding, expires: time.Now().Add(ssoCodeExpiry())}
	ssoCodeMu.Unlock()
	return code
}

// consumeSSOCode redeems a one-time code exactly once (also when expired).
func consumeSSOCode(code string) (ssoCodeEntry, bool) {
	ssoCodeMu.Lock()
	defer ssoCodeMu.Unlock()
	entry, ok := ssoCodeStore[code]
	delete(ssoCodeStore, code)
	if !ok || time.Now().After(entry.expires) {
		return ssoCodeEntry{}, false
	}
	return entry, true
}

// StartSSOCleanupWorker starts the periodic ssoCodeStore cleanup loop
// in the background and returns the corresponding CancelFunc,
// which the caller must trigger via defer at application shutdown -
// previously the goroutine ran off uncontrolled from init(): it also started
// in every test process that imports this package
// (goroutine leak/race risk with -race), and could never
// be stopped cleanly. Creates the context itself (instead of accepting
// one) so that main() needs only a single line
// (defer StartSSOCleanupWorker(...)()) - main() itself cannot be
// meaningfully unit-tested and would otherwise burden the SonarCloud new-code coverage
// unnecessarily.
func StartSSOCleanupWorker(parent context.Context) context.CancelFunc {
	ctx, cancel := context.WithCancel(parent)
	go cleanupSSOCodeStore(ctx)
	return cancel
}

// cleanupSSOCodeStore regularly removes expired SSO codes that were never
// redeemed, analogous to rateLimiter.cleanupStaleVisitors
// (middleware/security.go). Without it ssoCodeStore grows without bound if
// a user closes the browser before the final redirect and
// consumeSSOCode is therefore never called. Exits as soon as ctx
// is cancelled (graceful shutdown).
func cleanupSSOCodeStore(ctx context.Context) {
	ticker := time.NewTicker(ssoCleanupInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepExpiredSSOCodes()
		}
	}
}

// sweepExpiredSSOCodes performs a single cleanup pass - extracted from the
// sleep loop so that tests can call it directly without
// waiting.
func sweepExpiredSSOCodes() {
	ssoCodeMu.Lock()
	defer ssoCodeMu.Unlock()
	now := time.Now()
	for code, entry := range ssoCodeStore {
		if now.After(entry.expires) {
			delete(ssoCodeStore, code)
		}
	}
}

func getBaseURL() string {
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	return baseURL
}

func getFrontendURL() string {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}
	return frontendURL
}

// isBootstrapAdminEmail checks whether the email is listed in SSO_BOOTSTRAP_ADMIN_EMAILS
// (comma-separated, case-insensitive).
func isBootstrapAdminEmail(email string) bool {
	list := os.Getenv("SSO_BOOTSTRAP_ADMIN_EMAILS")
	if list == "" {
		return false
	}
	for _, e := range strings.Split(list, ",") {
		if strings.EqualFold(strings.TrimSpace(e), email) {
			return true
		}
	}
	return false
}

// isEmailDomainAllowed checks the domain of an email against SSO_ALLOWED_DOMAINS
// (S8). An empty env var means "no restriction".
func isEmailDomainAllowed(email string) bool {
	allowed := os.Getenv("SSO_ALLOWED_DOMAINS")
	if allowed == "" {
		return true
	}
	domain := ""
	if at := strings.LastIndex(email, "@"); at != -1 {
		domain = strings.ToLower(email[at+1:])
	}
	for _, d := range strings.Split(allowed, ",") {
		if strings.EqualFold(strings.TrimSpace(d), domain) {
			return true
		}
	}
	return false
}

// SSOExchangeHandler exchanges a one-time code issued via SSO redirect for the
// actual JWT. POST /api/auth/sso-exchange (public, rate-limited).
func (h *Handler) SSOExchangeHandler(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Code erforderlich", "code": "login_code_required"})
		return
	}

	// The code is redeemed once, also when the origin does not match: a code
	// for a custom domain only there, a code for FRONTEND_URL never on one.
	// No boundary against a server-side attacker (any header can be sent);
	// that is what the page limit and the role check are for.
	// The browser must also be the one that started the login (binding
	// cookie, login CSRF); a mismatch spends the code as well.
	entry, ok := consumeSSOCode(req.Code)
	if !ok || h.browserOrigin(c).host != entry.host || !loginBindingMatches(c, entry.binding) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Ungültiger oder abgelaufener Code", "code": "login_code_invalid"})
		return
	}
	clearLoginBinding(c)

	// return_to: where the frontend continues after the login ("" = its
	// default); always present.
	c.JSON(http.StatusOK, gin.H{"token": entry.token, "return_to": entry.returnTo})
}
