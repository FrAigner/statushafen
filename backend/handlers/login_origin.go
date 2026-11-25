package handlers

import (
	"net/http"
	"net/url"

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
//     a same-origin request. Anything else ends on FRONTEND_URL as before.
//
// Whoever controls the DNS of a custom domain (the tenant) can serve own code
// on it and read what the domain's localStorage holds. A session handed to a
// custom domain is therefore always limited to the page of that domain
// (sso_page_id, never global admin rights), see completeProviderLogin.

// returnHostParam names the custom domain a login started on.
const returnHostParam = "return_host"

// loginOrigin is the verified custom domain a login returns to (host) and
// its page. The zero value stands for FRONTEND_URL.
type loginOrigin struct {
	host   string
	pageID uint
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
	return loginOrigin{host: page.CustomDomain, pageID: page.ID}
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
	base, err := url.Parse(getBaseURL())
	if err != nil || c.Request.Host == base.Host {
		return false
	}
	o := h.customDomainOrigin(c.Request.Host)
	if o.host == "" {
		return false
	}
	q := c.Request.URL.Query()
	q.Set(returnHostParam, o.host)
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

// callbackErrorURL is the error target of a callback: test sign-in and link
// as before (admin area on FRONTEND_URL), a login back to its login page.
func (h *Handler) callbackErrorURL(st models.OIDCLoginState, e *oidcError) string {
	if st.TestLogin || st.LinkUserID != 0 {
		return ssoErrorURL(st.TestLogin, st.LinkUserID, e)
	}
	return h.loginReturnOrigin(st).frontendURL() + "/login?oidc_error=" + e.code
}
