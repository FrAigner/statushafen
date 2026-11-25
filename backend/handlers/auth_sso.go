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

// ssoCodeStore hält kurzlebige Einmal-Codes, gegen die das Frontend das fertige JWT
// eintauscht (S5 — Token wandert nicht mehr per Redirect-Query durch Browser-History/
// Referrer/Logs). Bewusst simpel gehalten (in-memory, Prozess-lokal), analog zum
// Visitor-Store in middleware.NewRateLimiter.
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

// StartSSOCleanupWorker startet den periodischen ssoCodeStore-Aufräum-Loop
// im Hintergrund und liefert die zugehörige CancelFunc zurück,
// die der Aufrufer per defer beim Application-Shutdown auslösen muss -
// vorher lief die Goroutine unkontrolliert aus init() los: sie startete
// auch in jedem Testprozess, der dieses Package importiert
// (Goroutine-Leak/Race-Risiko bei -race), und konnte nie
// sauber beendet werden. Erzeugt den Context selbst (statt ihn entgegen-
// zunehmen), damit main() nur eine einzige Zeile braucht
// (defer StartSSOCleanupWorker(...)()) - main() selbst lässt sich nicht
// sinnvoll unit-testen und würde sonst die SonarCloud-New-Code-Coverage
// unnötig belasten.
func StartSSOCleanupWorker(parent context.Context) context.CancelFunc {
	ctx, cancel := context.WithCancel(parent)
	go cleanupSSOCodeStore(ctx)
	return cancel
}

// cleanupSSOCodeStore entfernt abgelaufene, nie eingelöste SSO-Codes
// regelmäßig, analog zu rateLimiter.cleanupStaleVisitors
// (middleware/security.go). Ohne das wächst ssoCodeStore unbegrenzt, wenn
// ein Nutzer den Browser vor dem finalen Redirect schließt und
// consumeSSOCode dadurch nie aufgerufen wird. Beendet sich, sobald ctx
// gecancelt wird (Graceful Shutdown).
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

// sweepExpiredSSOCodes führt einen einzelnen Aufräumdurchlauf aus - aus der
// Sleep-Schleife ausgelagert, damit Tests ihn ohne Wartezeit direkt
// aufrufen können.
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

// isBootstrapAdminEmail prüft, ob die E-Mail in SSO_BOOTSTRAP_ADMIN_EMAILS
// (kommagetrennt, case-insensitive) hinterlegt ist.
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

// isEmailDomainAllowed prüft die Domain einer E-Mail gegen SSO_ALLOWED_DOMAINS
// (S8). Leere Env-Var bedeutet "keine Einschränkung".
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

// SSOExchangeHandler tauscht einen per SSO-Redirect ausgegebenen Einmal-Code gegen das
// eigentliche JWT ein. POST /api/auth/sso-exchange (öffentlich, rate-limitiert).
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
