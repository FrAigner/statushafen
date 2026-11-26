package handlers

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/logging"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OIDC login per status page (replaces the earlier scaffolding with
// fixed state and mock token). Flow:
//
//  1. GET /api/auth/oidc/login?page=N: store state, nonce and PKCE verifier in the
//     DB, additionally bind the state to the browser as an HttpOnly cookie
//     (against login CSRF), then redirect to the provider.
//  2. GET /api/auth/oidc/callback?page=N&state=…&code=…: redeem the state exactly
//     once, exchange the code with PKCE, verify the ID token via go-oidc
//     (signature via the JWKS, iss, aud, exp) and compare the nonce.
//  3. The account is found only via (page, issuer, subject), never via the
//     email (deliberate decision against account takeover). New accounts are not
//     created here. A logged-in user creates the link themselves
//     (POST /api/auth/oidc/link?page=N), after which the same callback runs.
//  4. Session: one-time code that the frontend exchanges for the JWT via
//     /api/auth/sso-exchange.
//
// Errors in the browser flow (login start and callback) redirect with
// a fixed code into the frontend instead of showing raw JSON, see
// ssoErrorURL. In addition there is the test login of a page admin
// (OIDCLoginState.TestLogin, StartSSOTestHandler).

const (
	oidcStateCookie     = "oidc_state"
	oidcStateCookiePath = "/api/auth/oidc"
	// oidcProviderTTL: how long a discovery result including the JWKS cache is valid
	// (go-oidc reloads the JWKS itself when an unknown kid appears).
	oidcProviderTTL = time.Hour
	// oidcFailureTTL: how long a failed discovery attempt counts as the
	// answer, so a dead provider does not block every call for up to 10 s
	// network time.
	oidcFailureTTL = time.Minute
)

// oidcError describes an error in the OIDC flow. status and
// msg go to JSON callers (starting a link and the test login), code is
// the fixed error code for the redirect into the frontend (translated there under
// auth.oidcErrors.<code>). Deliberately only this code goes into the URL: no
// message from the provider, no details from the log.
type oidcError struct {
	status int
	code   string
	msg    string
}

var (
	errOIDCBadPage             = &oidcError{http.StatusBadRequest, "bad_page", "Ungültige Seite"}
	errOIDCNotConfigured       = &oidcError{http.StatusNotFound, "not_configured", "SSO für diese Seite nicht eingerichtet"}
	errOIDCProviderUnreachable = &oidcError{http.StatusBadGateway, "provider_unreachable", "OIDC-Anbieter nicht erreichbar"}
	errOIDCStartFailed         = &oidcError{http.StatusInternalServerError, "internal", "Login konnte nicht gestartet werden"}
	errOIDCStateInvalid        = &oidcError{http.StatusBadRequest, "state_invalid", "Ungültiger oder abgelaufener Login-Versuch"}
	errOIDCProviderDenied      = &oidcError{http.StatusBadRequest, "provider_denied", "Anmeldung beim Anbieter abgebrochen oder abgelehnt"}
	errOIDCTokenExchange       = &oidcError{http.StatusBadGateway, "token_exchange", "Token-Austausch fehlgeschlagen"}
	errOIDCNoIDToken           = &oidcError{http.StatusBadGateway, "no_id_token", "Anbieter lieferte kein ID-Token"}
	errOIDCIDTokenInvalid      = &oidcError{http.StatusForbidden, "id_token_invalid", "ID-Token ungültig"}
	errOIDCNotLinked           = &oidcError{http.StatusForbidden, "not_linked", "Diese Anmeldung ist mit keinem Konto verknüpft"}
	errOIDCAlreadyLinked       = &oidcError{http.StatusConflict, "already_linked", "Diese Anmeldung ist bereits mit einem anderen Konto verknüpft"}
	errOIDCLinkFailed          = &oidcError{http.StatusInternalServerError, "internal", "Verknüpfung konnte nicht gespeichert werden"}
	errOIDCGlobalAdmin         = &oidcError{http.StatusForbidden, "global_admin", "Global Admins melden sich nicht über das Seiten-SSO an"}
	errOIDCOtherPageRole       = &oidcError{http.StatusForbidden, "other_page_role", "Konten mit Rolle auf einer anderen Seite nutzen dieses Seiten-SSO nicht"}
	errOIDCTestDisabled        = &oidcError{http.StatusForbidden, "test_disabled", "Testanmeldung ist für diese Seite abgeschaltet"}
	errOIDCTestIncomplete      = &oidcError{http.StatusBadRequest, "test_incomplete", "Für die Testanmeldung fehlen Discovery URL, Client ID oder Secret"}
)

// ssoErrorURL is the frontend error target of the browser flows on
// FRONTEND_URL: test sign-in back to the SSO page, link back to the profile,
// otherwise the login page. It consists of FRONTEND_URL, a fixed path and the
// fixed code only (no open redirect). Only the error of a login start may go
// to a custom domain (loginStartErrorURL, login_origin.go).
func ssoErrorURL(testLogin bool, linkUserID uint, e *oidcError) string {
	if testLogin {
		return getFrontendURL() + "/admin/sso?oidc_test=" + e.code
	}
	if linkUserID != 0 {
		return getFrontendURL() + "/admin/profile?oidc_error=" + e.code
	}
	return getFrontendURL() + "/login?oidc_error=" + e.code
}

var errOIDCInsecureIssuer = errors.New("OIDC-Issuer muss https verwenden")

var errOIDCInsecureEndpoint = errors.New("OIDC-Endpunkt muss eine https-Adresse sein")

// oidcEndpointAllowed checks an address from the discovery document. It
// comes from the provider that the page admin enters: the browser is redirected to
// authorization_endpoint, the server calls token_endpoint.
// Only https with a host is allowed; http only for the explicitly named
// development issuer. javascript:, data: and the like are thereby excluded.
func oidcEndpointAllowed(issuer, raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return true
	case "http":
		return oidcDevIssuerAllowed(issuer)
	}
	return false
}

// defaultOIDCHTTPClient talks to discovery, JWKS and token endpoint. The
// discovery URL is maintained by the page admin, hence the same SSRF protection as for
// webhooks (safeDialContext): internal addresses are blocked, even
// after redirects and DNS rebinding.
func defaultOIDCHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{DialContext: safeDialContext},
	}
}

// oidcDevHTTPClient talks only to the issuers from
// OIDC_DEV_INSECURE_ISSUERS, i.e. the local mock OIDC service from
// docker-compose.yml. It runs on localhost, which safeDialContext blocks.
func oidcDevHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// oidcDevIssuerAllowed: for local development an
// issuer may run without TLS and on an internal address if it is listed exactly in the
// comma-separated list OIDC_DEV_INSECURE_ISSUERS. Deliberately exact
// comparison rather than prefix and only in an explicit development environment
// (as with seeding in database/db.go): otherwise a forgotten variable
// or a differing APP_ENV value on the server would suffice for a page admin to
// reach internal services via the discovery URL.
func oidcDevIssuerAllowed(issuer string) bool {
	if issuer == "" || !database.IsDevelopmentAppEnv() {
		return false
	}
	for _, allowed := range strings.Split(os.Getenv("OIDC_DEV_INSECURE_ISSUERS"), ",") {
		if strings.TrimSpace(allowed) == issuer {
			return true
		}
	}
	return false
}

var (
	oidcHTTPClient = defaultOIDCHTTPClient()
	// oidcTrustedHTTPClient talks to the global providers from
	// OIDC_PROVIDERS. Their issuers come from the operator, not from a page
	// admin, so a self-hosted IdP on an internal address (e.g. a GitLab in the
	// company network) is allowed; there is no SSRF dial guard.
	oidcTrustedHTTPClient = &http.Client{Timeout: 10 * time.Second}
	oidcNow               = time.Now

	// oidcProviderMu protects only the map accesses, never a network call.
	oidcProviderMu sync.Mutex
	oidcProviders  = map[string]oidcProviderEntry{}
	// oidcDiscoveries bundles concurrent discoveries per issuer.
	oidcDiscoveries singleflight.Group
)

// oidcProviderEntry holds either a provider or the error of a
// failed attempt (negative cache), each until expires.
type oidcProviderEntry struct {
	provider *oidc.Provider
	err      error
	expires  time.Time
}

func resetOIDCProviderCache() {
	oidcProviderMu.Lock()
	defer oidcProviderMu.Unlock()
	oidcProviders = map[string]oidcProviderEntry{}
}

func cachedOIDCProvider(issuer string) (oidcProviderEntry, bool) {
	oidcProviderMu.Lock()
	defer oidcProviderMu.Unlock()
	e, ok := oidcProviders[issuer]
	return e, ok && oidcNow().Before(e.expires)
}

// oidcContext returns the context for all calls to an issuer (discovery,
// JWKS, token endpoint); go-oidc keeps the client for the JWKS from the
// discovery. trusted selects the client for operator-configured issuers.
func oidcContext(issuer string, trusted bool) context.Context {
	client := oidcHTTPClient
	if trusted {
		client = oidcTrustedHTTPClient
	}
	if oidcDevIssuerAllowed(issuer) {
		client = oidcDevHTTPClient()
	}
	ctx := oidc.ClientContext(context.Background(), client)
	return context.WithValue(ctx, oauth2.HTTPClient, client)
}

// oidcCacheKey separates the provider cache of trusted (global) and page
// issuers: a page admin entering the issuer of a global provider must not
// reuse the provider discovered with the trusted client.
func oidcCacheKey(issuer string, trusted bool) string {
	if trusted {
		return "trusted\x00" + issuer
	}
	return issuer
}

// oidcIssuer derives the issuer from the stored discovery URL. The
// admin UI accepts both: the issuer itself or the full
// .well-known URL. A trailing slash is kept because it
// is part of the issuer for some providers.
func oidcIssuer(discoveryURL string) string {
	return strings.TrimSuffix(strings.TrimSpace(discoveryURL), "/.well-known/openid-configuration")
}

// discoverOIDCProvider runs the discovery and caches the result (a failure
// too). A call that lands in singleflight only after a finished predecessor
// takes its cache entry.
func discoverOIDCProvider(issuer string, trusted bool) oidcProviderEntry {
	key := oidcCacheKey(issuer, trusted)
	if e, ok := cachedOIDCProvider(key); ok {
		return e
	}
	p, err := oidc.NewProvider(oidcContext(issuer, trusted), issuer)
	e := oidcProviderEntry{provider: p, err: err, expires: oidcNow().Add(oidcProviderTTL)}
	if err != nil {
		e.expires = oidcNow().Add(oidcFailureTTL)
	}
	oidcProviderMu.Lock()
	oidcProviders[key] = e
	oidcProviderMu.Unlock()
	return e
}

// oidcProviderFor returns the (cached) provider of a page SSO issuer.
func oidcProviderFor(issuer string) (*oidc.Provider, error) {
	return oidcProviderWith(issuer, false)
}

// oidcProviderWith returns the (cached) provider of an issuer; go-oidc checks
// that the discovery reports the same issuer.
func oidcProviderWith(issuer string, trusted bool) (*oidc.Provider, error) {
	if !strings.HasPrefix(issuer, "https://") && !oidcDevIssuerAllowed(issuer) {
		return nil, errOIDCInsecureIssuer
	}
	key := oidcCacheKey(issuer, trusted)
	e, ok := cachedOIDCProvider(key)
	if !ok {
		// The discovery runs without a lock; concurrent calls for the same
		// issuer share one network call.
		v, _, _ := oidcDiscoveries.Do(key, func() (interface{}, error) {
			return discoverOIDCProvider(issuer, trusted), nil
		})
		e = v.(oidcProviderEntry)
	}
	if e.err != nil {
		return nil, e.err
	}
	// Checked on every lookup (also from the cache) because it depends on
	// the development mode, which must not be cached.
	ep := e.provider.Endpoint()
	if !oidcEndpointAllowed(issuer, ep.AuthURL) || !oidcEndpointAllowed(issuer, ep.TokenURL) {
		return nil, errOIDCInsecureEndpoint
	}
	return e.provider, nil
}

func oidcRedirectURL(pageID uint) string {
	return getBaseURL() + "/api/auth/oidc/callback?page=" + strconv.FormatUint(uint64(pageID), 10)
}

// oidcClient is one OIDC relying-party configuration: the SSO of a status
// page or a global provider. All flows (login, link, test sign-in) run
// through it, so state, nonce, PKCE and ID token checks are shared.
type oidcClient struct {
	issuer       string
	clientID     string
	clientSecret string
	scopes       []string
	redirectURL  string
	nameClaim    string
	emailClaim   string
	// trusted: operator-configured issuer (see oidcTrustedHTTPClient).
	trusted bool
	// logKey/logValue identify the configuration in log lines.
	logKey   string
	logValue any
}

// pageOIDCClient builds the client of a page SSO configuration.
func pageOIDCClient(cfg models.StatusPageSSOConfig) oidcClient {
	scopes := strings.Fields(cfg.OIDCScopes)
	if !slices.Contains(scopes, oidc.ScopeOpenID) {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}
	return oidcClient{
		issuer:       oidcIssuer(cfg.OIDCDiscoveryURL),
		clientID:     cfg.OIDCClientID,
		clientSecret: cfg.OIDCClientSecret,
		scopes:       scopes,
		redirectURL:  oidcRedirectURL(cfg.StatusPageID),
		nameClaim:    orDefault(cfg.OIDCAttributeName, models.SSODefaultAttributeName),
		emailClaim:   orDefault(cfg.OIDCAttributeEmail, models.SSODefaultAttributeEmail),
		logKey:       "page",
		logValue:     cfg.StatusPageID,
	}
}

func (cl oidcClient) oauthConfig(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cl.clientID,
		ClientSecret: cl.clientSecret,
		RedirectURL:  cl.redirectURL,
		Scopes:       cl.scopes,
		Endpoint:     p.Endpoint(),
	}
}

// provider runs (or reuses) the discovery and logs a failure.
func (cl oidcClient) provider() (*oidc.Provider, bool) {
	p, err := oidcProviderWith(cl.issuer, cl.trusted)
	if err != nil {
		slog.Error("OIDC discovery failed", cl.logKey, cl.logValue, "error", logging.SanitizeErr(err))
		return nil, false
	}
	return p, true
}

func (h *Handler) activeOIDCConfig(pageID uint64) (models.StatusPageSSOConfig, bool) {
	var cfg models.StatusPageSSOConfig
	err := h.DB.Where("status_page_id = ?", pageID).
		Where(&models.StatusPageSSOConfig{OIDCEnabled: true}).
		First(&cfg).Error
	return cfg, err == nil
}

// beginOIDC creates the login state and returns the provider's
// authorize URL. linkUserID != 0 starts a link for this account.
func (h *Handler) beginOIDC(c *gin.Context, linkUserID uint) (string, *oidcError) {
	pageID, err := strconv.ParseUint(c.Query("page"), 10, 32)
	if err != nil {
		return "", errOIDCBadPage
	}
	cfg, ok := h.activeOIDCConfig(pageID)
	if !ok {
		return "", errOIDCNotConfigured
	}
	st := models.OIDCLoginState{StatusPageID: cfg.StatusPageID, LinkUserID: linkUserID}
	if linkUserID == 0 {
		st.ReturnTo = sanitizeReturnTo(c.Query("return_to"))
		st.ReturnHost = h.acceptReturnHost(c.Query(returnHostParam), cfg.StatusPageID).host
	}
	return h.startOIDCFlow(c, pageOIDCClient(cfg), st, oidcStateCookiePath)
}

// startOIDCFlow adds state, nonce and PKCE verifier to st, stores it and sets
// the state cookie on cookiePath (the path of the matching callback). The
// caller sets the target of st (page or provider). Shared by login, link
// and test sign-in of page SSO and global providers.
func (h *Handler) startOIDCFlow(c *gin.Context, cl oidcClient, st models.OIDCLoginState, cookiePath string) (string, *oidcError) {
	p, ok := cl.provider()
	if !ok {
		return "", errOIDCProviderUnreachable
	}

	// Remove never-redeemed states of expired logins on the way.
	h.DB.Where("expires_at < ?", time.Now()).Delete(&models.OIDCLoginState{})
	st.State = randomToken(32)
	st.Nonce = randomToken(32)
	st.CodeVerifier = oauth2.GenerateVerifier()
	st.ExpiresAt = time.Now().Add(oidcStateLifetime())
	bindLoginState(c, &st)
	if err := h.DB.Create(&st).Error; err != nil {
		return "", errOIDCStartFailed
	}
	c.SetCookie(oidcStateCookie, st.State, int(oidcStateLifetime().Seconds()), cookiePath, "", true, true)
	return cl.oauthConfig(p).AuthCodeURL(st.State, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.CodeVerifier)), nil
}

// oidcStateLifetime: how long a login state is valid. A function because gremlins
// mutants in package constants are not assigned to any coverage block (NOT COVERED).
func oidcStateLifetime() time.Duration { return 10 * time.Minute }

// OIDCLoginHandler starts the login via a status page's SSO.
// GET /api/auth/oidc/login?page=N (public, rate-limited). The browser
// navigates here ("Sign in with SSO" button), so errors go to the login page as a code,
// as in the callback.
func (h *Handler) OIDCLoginHandler(c *gin.Context) {
	if h.moveLoginToBaseHost(c) {
		return
	}
	target, e := h.beginOIDC(c, 0)
	if e != nil {
		target = h.loginStartErrorURL(c, e)
	}
	c.Redirect(http.StatusTemporaryRedirect, target)
}

// OIDCLinkHandler starts linking the logged-in account with the
// SSO of a status page. Returns the authorize URL as JSON because a
// browser redirect does not send the Authorization header.
// POST /api/auth/oidc/link?page=N (logged in)
//
// Global admins are excluded: a page's IdP is controlled by its
// admin, and a link would otherwise give them a path to global permissions. This
// also applies to accounts with a role on another page (permissions there).
func (h *Handler) OIDCLinkHandler(c *gin.Context) {
	user, e := h.ssoLinkUser(c)
	if e != nil {
		writeOIDCError(c, e)
		return
	}
	target, e := h.beginOIDC(c, user.ID)
	if e != nil {
		writeOIDCError(c, e)
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": target})
}

var errSSONotLoggedIn = &oidcError{http.StatusUnauthorized, "internal", "Nicht angemeldet"}

// ssoLinkUser loads the signed-in account for a link with the SSO of page
// ?page=N and applies the rules described at OIDCLinkHandler.
func (h *Handler) ssoLinkUser(c *gin.Context) (models.User, *oidcError) {
	var user models.User
	if err := h.DB.First(&user, getUserID(c)).Error; err != nil {
		return user, errSSONotLoggedIn
	}
	if user.IsGlobalAdmin {
		return user, errOIDCGlobalAdmin
	}
	if pageID, err := strconv.ParseUint(c.Query("page"), 10, 32); err == nil && h.hasRoleOnOtherPage(user.ID, uint(pageID)) {
		return user, errOIDCOtherPageRole
	}
	return user, nil
}

func writeOIDCError(c *gin.Context, e *oidcError) {
	c.JSON(e.status, gin.H{"error": e.msg, "code": codeForStatus(e.status)})
}

// consumeOIDCState checks the state against the cookie on cookiePath and
// redeems it atomically (DELETE … RETURNING): two concurrent callbacks with
// the same state cannot both pass. If the code is missing (the provider
// reports error=…, e.g. after an abort), the state is still redeemed so that
// the error redirect knows whether a login, a link or a test sign-in ran.
func (h *Handler) consumeOIDCState(c *gin.Context, cookiePath string) (models.OIDCLoginState, *oidcError) {
	var st models.OIDCLoginState
	state := c.Query("state")
	cookie, _ := c.Cookie(oidcStateCookie)
	c.SetCookie(oidcStateCookie, "", -1, cookiePath, "", true, true)
	if state == "" || subtle.ConstantTimeCompare([]byte(cookie), []byte(state)) != 1 {
		return st, errOIDCStateInvalid
	}
	res := h.DB.Clauses(clause.Returning{}).Where("state = ?", state).Delete(&st)
	if res.Error != nil || res.RowsAffected == 0 || time.Now().After(st.ExpiresAt) {
		return st, errOIDCStateInvalid
	}
	if c.Query("code") == "" {
		return st, errOIDCProviderDenied
	}
	return st, nil
}

// oidcClaims are the values of the configured claims (name and e-mail) from
// the verified ID token. emailVerified is nil when the token carries no
// email_verified claim.
type oidcClaims struct {
	name          string
	email         string
	emailVerified *bool
}

// claimString reads a string claim; if it is missing or not a string,
// the result is empty.
func claimString(claims map[string]interface{}, key string) string {
	v, _ := claims[key].(string)
	return v
}

// verifyOIDCCode exchanges the code (with the PKCE verifier) and verifies the
// ID token: signature via the JWKS, issuer, audience (client ID), expiry and
// the nonce of the state. An empty subject is rejected as well.
func verifyOIDCCode(code string, cl oidcClient, p *oidc.Provider, st models.OIDCLoginState) (*oidc.IDToken, oidcClaims, *oidcError) {
	ctx := oidcContext(cl.issuer, cl.trusted)
	tok, err := cl.oauthConfig(p).Exchange(ctx, code, oauth2.VerifierOption(st.CodeVerifier))
	if err != nil {
		slog.Error("OIDC token exchange failed", cl.logKey, cl.logValue, "error", logging.SanitizeErr(err))
		return nil, oidcClaims{}, errOIDCTokenExchange
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return nil, oidcClaims{}, errOIDCNoIDToken
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: cl.clientID}).Verify(ctx, raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(st.Nonce)) != 1 || idt.Subject == "" {
		slog.Warn("OIDC ID token rejected", cl.logKey, cl.logValue, "error", err)
		return nil, oidcClaims{}, errOIDCIDTokenInvalid
	}
	claims := map[string]interface{}{}
	_ = idt.Claims(&claims)
	return idt, oidcClaims{
		name:          claimString(claims, cl.nameClaim),
		email:         claimString(claims, cl.emailClaim),
		emailVerified: claimBool(claims, "email_verified"),
	}, nil
}

// claimBool reads a boolean claim; some IdPs send "true"/"false" as strings.
// A missing or unreadable claim is nil.
func claimBool(claims map[string]interface{}, key string) *bool {
	var v bool
	switch raw := claims[key].(type) {
	case bool:
		v = raw
	case string:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil
		}
		v = b
	default:
		return nil
	}
	return &v
}

// linkSSOIdentity creates the link for linkUserID. If the
// identity already belongs to another account, it stays with that one (409).
func (h *Handler) linkSSOIdentity(linkUserID uint, ident models.UserSSOIdentity, found bool) (uint, *oidcError) {
	if found {
		if ident.UserID != linkUserID {
			return 0, errOIDCAlreadyLinked
		}
		return ident.UserID, nil
	}
	ident.UserID = linkUserID
	if err := h.DB.Create(&ident).Error; err != nil {
		return 0, errOIDCLinkFailed
	}
	return ident.UserID, nil
}

// resolveSSOUser finds (or links) the account of the verified identity key
// (page, protocol, issuer, subject). The e-mail from the claim is only stored
// on the link (shown in the profile); the lookup uses key alone.
func (h *Handler) resolveSSOUser(linkUserID uint, key models.UserSSOIdentity, email string) (models.User, *oidcError) {
	var user models.User
	// A key part that is empty would mean "anything": never search, never create.
	if key.StatusPageID == 0 || key.Protocol == "" || key.Issuer == "" || key.Subject == "" {
		return user, errOIDCIDTokenInvalid
	}
	ident := key
	found := ssoIdentityScope(h.DB, key).First(&ident).Error == nil
	userID := ident.UserID
	if linkUserID != 0 {
		var e *oidcError
		if userID, e = h.linkSSOIdentity(linkUserID, ident, found); e != nil {
			return user, e
		}
	}
	if userID == 0 || h.DB.Preload("Role").First(&user, userID).Error != nil {
		return user, errOIDCNotLinked
	}
	if user.IsGlobalAdmin {
		return user, errOIDCGlobalAdmin
	}
	if h.hasRoleOnOtherPage(user.ID, key.StatusPageID) {
		return user, errOIDCOtherPageRole
	}
	ssoIdentityScope(h.DB.Model(&models.UserSSOIdentity{}), key).Update("email", email)
	return user, nil
}

// ssoIdentityScope selects exactly the identity with all five key columns.
// GORM's struct Where (Where(&key)) would drop zero values and then match
// arbitrary rows; here every column is explicitly in the condition
// (even an empty subject format).
func ssoIdentityScope(db *gorm.DB, key models.UserSSOIdentity) *gorm.DB {
	return db.Where("status_page_id = ? AND protocol = ? AND issuer = ? AND subject = ? AND subject_format = ?",
		key.StatusPageID, key.Protocol, key.Issuer, key.Subject, key.SubjectFormat)
}

// hasRoleOnOtherPage: anyone with a role on another page must not get in
// via this SSO (privilege escalation by the admin of the foreign IdP).
// Since the changeover the session is additionally restricted to the page via sso_page_id;
// that covers roles the user only receives after logging in.
// The check here remains as an early, understandable rejection.
func (h *Handler) hasRoleOnOtherPage(userID, pageID uint) bool {
	var n int64
	// Fail closed: on a DB error the block applies (login rejected,
	// account cannot be attached), never "no role found".
	if err := h.DB.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id <> ?", userID, pageID).Count(&n).Error; err != nil {
		return true
	}
	return n > 0
}

// hasSSOIdentityOnOtherPage: page SSO link on another page; fails closed on a
// DB error.
func (h *Handler) hasSSOIdentityOnOtherPage(userID, pageID uint) bool {
	var n int64
	if err := pageSSOIdentitiesElsewhere(h.DB, userID, pageID).Count(&n).Error; err != nil {
		return true
	}
	return n > 0
}

// pageSSOIdentitiesElsewhere selects the account's effective page SSO links on
// pages other than pageID. Rows from the SAML era (protocol "saml") stay in
// the table but no longer count; identities of global providers
// (status_page_id 0) are no page links at all.
func pageSSOIdentitiesElsewhere(db *gorm.DB, userID, pageID uint) *gorm.DB {
	return db.Model(&models.UserSSOIdentity{}).
		Where("user_id = ? AND status_page_id <> ? AND status_page_id <> 0 AND protocol = ?", userID, pageID, models.SSOProtocolOIDC)
}

// OIDCCallbackHandler completes login, link or test sign-in of a page SSO.
// GET /api/auth/oidc/callback?page=N&state=…&code=… (public). Every error
// ends as a redirect with a fixed code into the frontend. A state of a global
// provider login (Provider set) is never accepted here.
func (h *Handler) OIDCCallbackHandler(c *gin.Context) {
	st, e := h.consumeOIDCState(c, oidcStateCookiePath)
	if e == nil && st.Provider != "" {
		e = errOIDCStateInvalid
	}
	if e == nil {
		e = h.completeOIDC(c, st)
	}
	if e != nil {
		c.Redirect(http.StatusTemporaryRedirect, ssoErrorURL(st.TestLogin, st.LinkUserID, e))
	}
}

// oidcConfigForState: login and linking need an active page SSO,
// the test login only a complete configuration with
// test_login_enabled (it should be checkable before enabling).
func (h *Handler) oidcConfigForState(st models.OIDCLoginState) (models.StatusPageSSOConfig, *oidcError) {
	if st.TestLogin {
		return h.testOIDCConfig(uint64(st.StatusPageID))
	}
	cfg, ok := h.activeOIDCConfig(uint64(st.StatusPageID))
	if !ok {
		return cfg, errOIDCNotConfigured
	}
	return cfg, nil
}

func (h *Handler) completeOIDC(c *gin.Context, st models.OIDCLoginState) *oidcError {
	cfg, e := h.oidcConfigForState(st)
	if e != nil {
		return e
	}
	cl := pageOIDCClient(cfg)
	p, ok := cl.provider()
	if !ok {
		return errOIDCProviderUnreachable
	}
	idt, claims, e := verifyOIDCCode(c.Query("code"), cl, p, st)
	if e != nil {
		return e
	}
	if st.TestLogin {
		c.Redirect(http.StatusTemporaryRedirect, oidcTestResultURL(claims))
		return nil
	}
	key := models.UserSSOIdentity{StatusPageID: st.StatusPageID, Protocol: models.SSOProtocolOIDC, Issuer: idt.Issuer, Subject: idt.Subject}
	user, e := h.resolveSSOUser(st.LinkUserID, key, claims.email)
	if e != nil {
		return e
	}
	h.finishSSOLogin(c, user, claims.name, st, models.SSOProtocolOIDC)
	return nil
}

// oidcTestResultURL reports the success of the test login to the SSO page,
// including which of the configured claims were missing in the ID token (only the fixed
// names email/name, never values).
func oidcTestResultURL(claims oidcClaims) string {
	var missing []string
	if claims.email == "" {
		missing = append(missing, "email")
	}
	if claims.name == "" {
		missing = append(missing, "name")
	}
	target := getFrontendURL() + "/admin/sso?oidc_test=ok"
	if len(missing) > 0 {
		target += "&missing=" + strings.Join(missing, ",")
	}
	return target
}

// finishSSOLogin writes the audit entry and hands out a one-time code instead
// of the JWT in the redirect (S5). After a link the user returns to the
// profile (next=profile; the frontend only knows this fixed value). protocol
// ends up in the audit entry and in the sso claim of the JWT.
func (h *Handler) finishSSOLogin(c *gin.Context, user models.User, name string, st models.OIDCLoginState, protocol string) {
	pageID, linkUserID := st.StatusPageID, st.LinkUserID
	action, next := "auth.sso_login", ""
	if linkUserID != 0 {
		action, next = "auth.sso_linked", "&next=profile"
	}
	c.Set("email", user.Email)
	c.Set("userID", user.ID)
	c.Set("role", user.Role.Name)
	h.RecordAudit(c, action, "auth", "User", strconv.FormatUint(uint64(user.ID), 10), user.Email,
		"provider="+protocol+" page="+strconv.FormatUint(uint64(pageID), 10))

	// HS256 with JWT_SECRET set cannot fail; without a secret
	// getJWTSecret ends the process beforehand. The token
	// is valid only for pageID (sso_page_id claim).
	origin, returnTo := h.loginDelivery(st, user)
	token, _ := middleware.GeneratePageSSOToken(user, name, protocol, pageID, middleware.WithCustomDomain(origin.host))
	c.Redirect(http.StatusTemporaryRedirect, origin.frontendURL()+"/auth/sso-callback?code="+storeSSOCode(token, returnTo, origin.host, codeBinding(st, origin))+next)
}
