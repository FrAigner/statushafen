package handlers

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
)

// Sign-in from a status page on a verified custom domain.
//
// The login flows run on the BASE_URL host: the state cookie set at the
// login start has to reach the callback there (the redirect URI registered at
// the provider). The session, however, belongs into the localStorage of the
// origin the user started from. So:
//
//  1. A login start that arrives on a verified custom domain (the frontend
//     there uses relative URLs) is moved to the BASE_URL host first, with the
//     domain as return_host (moveLoginToBaseHost). The Host header wins over
//     a return_host in the query.
//  2. The login start keeps return_host in the state only if it is the exact
//     hostname of a verified or active custom domain (acceptReturnHost); for
//     a page SSO it must be the domain of that same page.
//  3. The callback checks the domain again and redirects the one-time code to
//     https://<domain>/auth/sso-callback; the frontend there redeems it with
//     a same-origin request (the code is bound to that origin). Anything else
//     ends on FRONTEND_URL as before.
//
// Whoever controls the DNS of a custom domain (the tenant) can serve own code
// on it and read what the domain's localStorage holds, and can send anyone a
// login link with their domain as return_host. Therefore:
//
//   - The code goes to a custom domain only for an account that agreed to a
//     role on the domain's page (loginDelivery, consentedMember); anyone else
//     signs in on FRONTEND_URL. Callback errors always go to FRONTEND_URL.
//   - A session handed to a custom domain (OIDC or password) is limited to the
//     domain's page (sso_page_id, never global admin rights) and marked
//     (custom_domain claim): it never acts on the account itself, see
//     middleware.RejectCustomDomainSession.
//   - Host and Origin of a request are normalised (case, port, trailing dot)
//     before deciding whether it comes from a custom domain (browserOrigin),
//     so a variant never yields a full session there.

// returnHostParam names the custom domain a login started on.
const returnHostParam = "return_host"

// loginOrigin is the verified custom domain a login returns to (host) and
// its page. The zero value stands for FRONTEND_URL.
type loginOrigin struct {
	host   string
	pageID uint
	slug   string
}

// frontendURL is the frontend base URL of the origin. Custom domains are
// always served with TLS (on-demand certificates of the proxy).
func (o loginOrigin) frontendURL() string {
	if o.host == "" {
		return getFrontendURL()
	}
	return "https://" + o.host
}

// customDomainOrigin finds the page whose verified or active custom domain is
// exactly host. Only a plain, lower-case hostname counts (normalizeDomain
// must leave it unchanged): no scheme, port, path, userinfo, trailing dot,
// whitespace or upper case. The target always comes from the database.
func (h *Handler) customDomainOrigin(host string) loginOrigin {
	if d, ok := normalizeDomain(host); !ok || d == "" || d != host {
		return loginOrigin{}
	}
	var page models.StatusPage
	if h.DB.Where("custom_domain = ? AND custom_domain_status IN ('verified','active')", host).First(&page).Error != nil {
		return loginOrigin{}
	}
	return loginOrigin{host: page.CustomDomain, pageID: page.ID, slug: page.Slug}
}

// requestHostname normalises a Host header value: lower case, without port
// and trailing dot. Only for deciding whether a request comes from a custom
// domain, never as a redirect target.
func requestHostname(raw string) string {
	host := strings.ToLower(strings.TrimSpace(raw))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.TrimRight(host, ".")
}

// browserOrigin is the custom domain a request comes from: the browser's
// Origin header (sent on every POST, also same-origin, and left alone by
// proxies) or else the Host header, both normalised. Either one on a custom
// domain counts; the zero value means none.
func (h *Handler) browserOrigin(c *gin.Context) loginOrigin {
	if u, err := url.Parse(c.GetHeader("Origin")); err == nil && u.Host != "" {
		if o := h.customDomainOrigin(requestHostname(u.Host)); o.host != "" {
			return o
		}
	}
	return h.customDomainOrigin(requestHostname(c.Request.Host))
}

// loginDelivery decides where the one-time code of a finished login goes and
// with which return path: to the custom domain of the state only for an
// account that agreed to a role on its page (consentedMember). Anyone else
// signs in on FRONTEND_URL and lands on the landing page there (empty return
// path): the private page itself would auto-join the account. A global admin
// never auto-joins and goes back to the same page there (/status/<slug>).
func (h *Handler) loginDelivery(st models.OIDCLoginState, user models.User) (loginOrigin, string) {
	o := h.loginReturnOrigin(st)
	if o.pageID == 0 || h.consentedMember(user.ID, o.pageID) {
		return o, st.ReturnTo
	}
	if user.IsGlobalAdmin {
		return loginOrigin{}, sanitizeReturnTo("/status/" + o.slug)
	}
	return loginOrigin{}, ""
}

// consentedMember reports whether the account has a membership on the page
// it agreed to (invitation, admin, order). Only an explicit auto-join does not
// count: the page's admins choose its e-mail domains. A membership from before
// joined_via (legacy, or a row still empty) counts unless the audit log, which
// has always recorded every auto-join, shows an auto-join of this account on
// this page. False on a database error (fail closed: the login then ends on
// FRONTEND_URL).
func (h *Handler) consentedMember(userID, pageID uint) bool {
	var m models.UserStatusPageRole
	if h.DB.Where(whereUserIDAndStatusPageID, userID, pageID).First(&m).Error != nil {
		return false
	}
	switch m.JoinedVia {
	case models.JoinedViaAutoJoin:
		return false
	case models.JoinedViaLegacy, "":
		return !h.autoJoinAudited(userID, pageID)
	}
	return true
}

// autoJoinAudited reports whether the audit log records an auto-join of the
// account on the page (true on a database error: fail closed).
func (h *Handler) autoJoinAudited(userID, pageID uint) bool {
	var n int64
	err := h.DB.Model(&models.AuditLog{}).
		Where("action = ? AND actor_id = ? AND target_id = ?", "statuspage.user_role_auto_assigned", userID, strconv.FormatUint(uint64(pageID), 10)).
		Count(&n).Error
	return err != nil || n > 0
}

// acceptReturnHost validates a return_host. pageID != 0 (page SSO) accepts
// only the domain of that page: a tenant's IdP never hands a session to the
// domain of another tenant.
func (h *Handler) acceptReturnHost(raw string, pageID uint) loginOrigin {
	o := h.customDomainOrigin(raw)
	if pageID != 0 && o.pageID != pageID {
		return loginOrigin{}
	}
	return o
}

// loginReturnOrigin is the origin a finished login (its state) returns to,
// checked again: the domain may have been removed during the login.
func (h *Handler) loginReturnOrigin(st models.OIDCLoginState) loginOrigin {
	return h.acceptReturnHost(st.ReturnHost, st.StatusPageID)
}

// moveLoginToBaseHost redirects a login start that arrived on a verified
// custom domain to the same path on the BASE_URL host, with the domain as
// return_host, and reports true. Any other host (unknown, unverified, the
// BASE_URL host itself) is left alone.
func (h *Handler) moveLoginToBaseHost(c *gin.Context) bool {
	host := requestHostname(c.Request.Host)
	base, err := url.Parse(getBaseURL())
	if err != nil || host == requestHostname(base.Host) {
		return false
	}
	o := h.customDomainOrigin(host)
	if o.host == "" {
		return false
	}
	q := c.Request.URL.Query()
	q.Set(returnHostParam, o.host)
	// The code may come back to this domain: bind it to this browser here
	// (only the hash goes along, see bindLoginState).
	q.Set(loginBindingParam, setLoginBinding(c))
	c.Redirect(http.StatusTemporaryRedirect, getBaseURL()+c.Request.URL.EscapedPath()+"?"+q.Encode())
	return true
}

// loginStartErrorURL is the error target of a login start: the login page of
// the custom domain in return_host if it is verified, else of FRONTEND_URL.
// Only the fixed error code goes along, so a verified domain of any page is
// fine here.
func (h *Handler) loginStartErrorURL(c *gin.Context, e *oidcError) string {
	return h.acceptReturnHost(c.Query(returnHostParam), 0).frontendURL() + "/login?oidc_error=" + e.code
}
