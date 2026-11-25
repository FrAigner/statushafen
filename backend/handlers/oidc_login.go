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

// OIDC-Anmeldung je Statusseite (ersetzt das frühere Gerüst mit
// festem State und Mock-Token). Ablauf:
//
//  1. GET /api/auth/oidc/login?page=N: State, Nonce und PKCE-Verifier in der
//     DB ablegen, State zusätzlich als HttpOnly-Cookie an den Browser binden
//     (gegen Login-CSRF), dann zum Anbieter umleiten.
//  2. GET /api/auth/oidc/callback?page=N&state=…&code=…: State genau einmal
//     einlösen, Code mit PKCE tauschen, ID-Token per go-oidc prüfen
//     (Signatur über das JWKS, iss, aud, exp) und die Nonce vergleichen.
//  3. Das Konto wird nur über (Seite, Issuer, Subject) gefunden, nie über die
// E-Mail (bewusste Entscheidung gegen Konto-Übernahme). Neue Konten entstehen hier
//     nicht. Die Verknüpfung legt ein angemeldeter Nutzer selbst an
//     (POST /api/auth/oidc/link?page=N), danach läuft derselbe Callback.
//  4. Sitzung: Einmal-Code, den das Frontend über
//     /api/auth/sso-exchange gegen das JWT tauscht.
//
// Fehler im Browser-Ablauf (Login-Start und Callback) leiten mit
// einem festen Code ins Frontend um statt rohes JSON zu zeigen, siehe
// ssoErrorURL. Dazu kommt die Testanmeldung eines Seiten-Admins
// (OIDCLoginState.TestLogin, StartSSOTestHandler).

const (
	oidcStateCookie     = "oidc_state"
	oidcStateCookiePath = "/api/auth/oidc"
	// oidcProviderTTL: So lange gilt ein Discovery-Ergebnis samt JWKS-Cache
	// (go-oidc lädt das JWKS selbst nach, wenn eine unbekannte kid auftaucht).
	oidcProviderTTL = time.Hour
	// oidcFailureTTL: So lange gilt ein fehlgeschlagener Discovery-Versuch als
	// Antwort, damit ein toter Anbieter nicht bei jedem Aufruf bis zu 10 s
	// Netzwerkzeit kostet.
	oidcFailureTTL = time.Minute
)

// oidcError beschreibt einen Fehler im OIDC-Ablauf. status und
// msg gehen an JSON-Aufrufer (Verknüpfen und Testanmeldung starten), code ist
// der feste Fehlercode für den Redirect ins Frontend (dort übersetzt unter
// auth.oidcErrors.<code>). In die URL kommt bewusst nur dieser Code: keine
// Meldung des Anbieters, keine Details aus dem Log.
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

// oidcEndpointAllowed prüft eine Adresse aus dem Discovery-Dokument. Sie
// stammt vom Anbieter, den der Seiten-Admin einträgt: Der Browser wird auf
// authorization_endpoint weitergeleitet, der Server ruft token_endpoint auf.
// Erlaubt ist nur https mit Host; http nur für den ausdrücklich benannten
// Entwicklungs-Issuer. javascript:, data: und Ähnliches fallen so heraus.
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

// defaultOIDCHTTPClient spricht Discovery, JWKS und Token-Endpunkt an. Den
// Discovery-URL pflegt der Seiten-Admin, deshalb derselbe SSRF-Schutz wie bei
// Webhooks (safeDialContext): interne Adressen sind gesperrt, auch
// nach Redirects und DNS-Rebinding.
func defaultOIDCHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{DialContext: safeDialContext},
	}
}

// oidcDevHTTPClient spricht nur die Issuer aus
// OIDC_DEV_INSECURE_ISSUERS an, also den lokalen Mock-OIDC-Dienst aus
// docker-compose.yml. Der läuft auf localhost, den safeDialContext sperrt.
func oidcDevHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// oidcDevIssuerAllowed: Für die lokale Entwicklung darf ein
// Issuer ohne TLS und auf einer internen Adresse laufen, wenn er exakt in der
// kommagetrennten Liste OIDC_DEV_INSECURE_ISSUERS steht. Bewusst exakter
// Vergleich statt Präfix und nur in einer ausdrücklichen Entwicklungsumgebung
// (wie beim Seeding in database/db.go): Sonst reichte eine vergessene Variable
// oder ein abweichender APP_ENV-Wert auf dem Server, damit ein Seiten-Admin per
// Discovery-URL interne Dienste erreicht.
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

	// oidcProviderMu schützt nur die Map-Zugriffe, nie einen Netzwerkaufruf.
	oidcProviderMu sync.Mutex
	oidcProviders  = map[string]oidcProviderEntry{}
	// oidcDiscoveries bündelt gleichzeitige Discoveries je Issuer.
	oidcDiscoveries singleflight.Group
)

// oidcProviderEntry hält entweder einen Provider oder den Fehler eines
// fehlgeschlagenen Versuchs (negativer Cache), jeweils bis expires.
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

// oidcIssuer leitet den Issuer aus dem gespeicherten Discovery-URL ab. Das
// Admin-UI nimmt beides an: den Issuer selbst oder den vollen
// .well-known-URL. Ein abschließender Schrägstrich bleibt stehen, weil er
// bei manchen Anbietern Teil des Issuers ist.
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

// beginOIDC legt den Login-State an und liefert die Authorize-URL des
// Anbieters. linkUserID != 0 startet eine Verknüpfung für dieses Konto.
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
	if err := h.DB.Create(&st).Error; err != nil {
		return "", errOIDCStartFailed
	}
	c.SetCookie(oidcStateCookie, st.State, int(oidcStateLifetime().Seconds()), cookiePath, "", true, true)
	return cl.oauthConfig(p).AuthCodeURL(st.State, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.CodeVerifier)), nil
}

// oidcStateLifetime: So lange gilt ein Login-State. Als Funktion, weil gremlins
// Mutanten in Paketkonstanten keinem Coverage-Block zuordnet (NOT COVERED).
func oidcStateLifetime() time.Duration { return 10 * time.Minute }

// OIDCLoginHandler startet den Login über das SSO einer Statusseite.
// GET /api/auth/oidc/login?page=N (öffentlich, rate-limitiert). Der Browser
// navigiert hierher (Knopf „Mit SSO anmelden“), Fehler gehen deshalb wie im
// Callback als Code auf die Login-Seite.
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

// OIDCLinkHandler startet die Verknüpfung des angemeldeten Kontos mit dem
// SSO einer Statusseite. Liefert die Authorize-URL als JSON, weil ein
// Browser-Redirect den Authorization-Header nicht mitschickt.
// POST /api/auth/oidc/link?page=N (angemeldet)
//
// Global Admins sind ausgenommen: Den IdP einer Seite kontrolliert deren
// Admin, eine Verknüpfung gäbe ihm sonst einen Weg zu globalen Rechten. Das
// gilt auch für Konten mit Rolle auf einer anderen Seite (Rechte dort).
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

// claimString liest einen String-Claim; fehlt er oder ist er kein String,
// ist das Ergebnis leer.
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

// linkSSOIdentity legt die Verknüpfung für linkUserID an. Gehört die
// Identität schon einem anderen Konto, bleibt es bei diesem (409).
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
	// Ein Schlüsselteil, der leer ist, wäre "beliebig": nie suchen, nie anlegen.
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

// ssoIdentityScope wählt genau die Identität mit allen fünf Schlüsselspalten.
// GORMs Struktur-Where (Where(&key)) ließe Nullwerte weg und träfe dann
// beliebige Zeilen; hier steht jede Spalte ausdrücklich in der Bedingung
// (auch ein leeres Subject-Format).
func ssoIdentityScope(db *gorm.DB, key models.UserSSOIdentity) *gorm.DB {
	return db.Where("status_page_id = ? AND protocol = ? AND issuer = ? AND subject = ? AND subject_format = ?",
		key.StatusPageID, key.Protocol, key.Issuer, key.Subject, key.SubjectFormat)
}

// hasRoleOnOtherPage: Wer auf einer anderen Seite eine Rolle hat, darf nicht
// über dieses SSO hinein (Rechteausweitung durch den Admin des fremden IdP).
// Seit der Umstellung ist die Sitzung zusätzlich per sso_page_id auf die Seite
// beschränkt; das deckt Rollen ab, die der Nutzer erst nach der Anmeldung
// erhält. Die Prüfung hier bleibt als frühe, verständliche Ablehnung.
func (h *Handler) hasRoleOnOtherPage(userID, pageID uint) bool {
	var n int64
	// Fail closed: Bei einem DB-Fehler gilt die Sperre (Anmeldung abgelehnt,
	// Konto nicht anhängbar), nie "keine Rolle gefunden".
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

// oidcConfigForState: Login und Verknüpfung brauchen ein aktives Seiten-SSO,
// die Testanmeldung nur eine vollständige Konfiguration mit
// test_login_enabled (sie soll vor dem Einschalten prüfbar sein).
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

// oidcTestResultURL meldet den Erfolg der Testanmeldung an die SSO-Seite,
// dazu welche der konfigurierten Claims im ID-Token fehlten (nur die festen
// Namen email/name, nie Werte).
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

	// HS256 mit gesetztem JWT_SECRET kann nicht fehlschlagen; ohne Secret
	// beendet getJWTSecret den Prozess schon vorher. Das Token
	// gilt nur für pageID (sso_page_id-Claim).
	origin, returnTo := h.loginDelivery(st, user.ID)
	token, _ := middleware.GeneratePageSSOToken(user, name, protocol, pageID, middleware.WithCustomDomain(origin.host))
	c.Redirect(http.StatusTemporaryRedirect, origin.frontendURL()+"/auth/sso-callback?code="+storeSSOCode(token, returnTo, origin.host)+next)
}
