package handlers

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// Browser binding of the SSO one-time code (login CSRF). Without it, an
// attacker could finish a login in their own browser and send the victim the
// frontend link with the attacker's code; redeeming it would sign the victim
// in to the attacker's account.
//
// Every login or link start sets a random nonce in an HttpOnly, Secure,
// SameSite=Lax cookie on the host it runs on, scoped to the exchange path.
// The login state and then the one-time code keep only its SHA-256 hash, and
// POST /api/auth/sso-exchange redeems the code only with the matching cookie
// (cleared afterwards). The cookie lives on the API host the frontend calls
// for the exchange; that is the host of the login start (BASE_URL, where the
// state cookie must reach the callback as well).
//
// A login started on a custom domain moves to the BASE_URL host first
// (moveLoginToBaseHost). The code may then go to the custom domain or, for an
// account without consent there, to FRONTEND_URL, so the move sets a second
// cookie on the custom domain and passes only its hash on (loginBindingParam).
// A return_host without a valid such hash is dropped: a code for the domain
// could never be redeemed.
const (
	loginBindingCookie     = "sso_binding"
	loginBindingCookiePath = "/api/auth/sso-exchange"
	loginBindingParam      = "login_binding"
)

// hashLoginBinding is the stored form of a binding nonce (hex SHA-256).
func hashLoginBinding(nonce string) string {
	sum := sha256.Sum256([]byte(nonce))
	return hex.EncodeToString(sum[:])
}

// validLoginBindingHash accepts only the exact form hashLoginBinding returns
// (64 lowercase hex digits).
func validLoginBindingHash(s string) bool {
	return len(s) == sha256.Size*2 && strings.Trim(s, "0123456789abcdef") == ""
}

// setLoginBinding sets a fresh binding cookie on the request's host and
// returns its hash. It lives as long as a login state plus a one-time code.
func setLoginBinding(c *gin.Context) string {
	nonce := randomToken(32)
	writeLoginBindingCookie(c, nonce, int((oidcStateLifetime() + ssoCodeExpiry()).Seconds()))
	return hashLoginBinding(nonce)
}

func clearLoginBinding(c *gin.Context) {
	writeLoginBindingCookie(c, "", -1)
}

func writeLoginBindingCookie(c *gin.Context, value string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     loginBindingCookie,
		Value:    value,
		Path:     loginBindingCookiePath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// loginBindingMatches reports whether the request carries the binding cookie
// whose hash is want. An unbound code (want "") never matches.
func loginBindingMatches(c *gin.Context, want string) bool {
	nonce, err := c.Cookie(loginBindingCookie)
	if want == "" || err != nil || nonce == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hashLoginBinding(nonce)), []byte(want)) == 1
}

// bindLoginState sets the browser binding of a login state at its start: the
// cookie on this host, and for a custom domain return the hash of the
// domain's cookie from the move (else the return_host is dropped).
func bindLoginState(c *gin.Context, st *models.OIDCLoginState) {
	st.BrowserBinding = setLoginBinding(c)
	if st.ReturnHost == "" {
		return
	}
	if b := c.Query(loginBindingParam); validLoginBindingHash(b) {
		st.DomainBinding = b
		return
	}
	st.ReturnHost = ""
}

// codeBinding is the binding a one-time code for origin gets: the domain's
// cookie for a custom domain, else the one of the login start.
func codeBinding(st models.OIDCLoginState, origin loginOrigin) string {
	if origin.host != "" {
		return st.DomainBinding
	}
	return st.BrowserBinding
}
