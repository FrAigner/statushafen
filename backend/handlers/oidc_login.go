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
//  4. Sitzung wie bei Google/GitHub: Einmal-Code, den das Frontend über
//     /api/auth/sso-exchange gegen das JWT tauscht.
//
// Fehler im Browser-Ablauf (Login-Start und Callback) leiten mit
// einem festen Code ins Frontend um statt rohes JSON zu zeigen, siehe
// oidcErrorURL. Dazu kommt die Testanmeldung eines Seiten-Admins
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

// oidcErrorURL ist das Frontend-Ziel eines Fehlers im
// Browser-Ablauf: Testanmeldung zurück auf die SSO-Seite, Verknüpfung zurück
// ins Profil, sonst die Login-Seite. Das Ziel setzt sich nur aus FRONTEND_URL,
// einem festen Pfad und dem festen Code zusammen (kein Open Redirect).
func oidcErrorURL(st models.OIDCLoginState, e *oidcError) string {
	return ssoErrorURL(st.TestLogin, st.LinkUserID, e)
}

// ssoErrorURL ist das gemeinsame Fehlerziel von OIDC und SAML.
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
	oidcNow        = time.Now

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

// oidcContext liefert den Kontext für alle Aufrufe an einen Issuer
// (Discovery, JWKS, Token-Endpunkt). go-oidc merkt sich den Client für das
// JWKS schon bei der Discovery.
func oidcContext(issuer string) context.Context {
	client := oidcHTTPClient
	if oidcDevIssuerAllowed(issuer) {
		client = oidcDevHTTPClient()
	}
	ctx := oidc.ClientContext(context.Background(), client)
	return context.WithValue(ctx, oauth2.HTTPClient, client)
}

// oidcIssuer leitet den Issuer aus dem gespeicherten Discovery-URL ab. Das
// Admin-UI nimmt beides an: den Issuer selbst oder den vollen
// .well-known-URL. Ein abschließender Schrägstrich bleibt stehen, weil er
// bei manchen Anbietern Teil des Issuers ist.
func oidcIssuer(discoveryURL string) string {
	return strings.TrimSuffix(strings.TrimSpace(discoveryURL), "/.well-known/openid-configuration")
}

// discoverOIDCProvider führt die Discovery aus und legt das Ergebnis (auch
// einen Fehlschlag) im Cache ab. Ein Aufruf, der erst nach einem fertigen
// Vorgänger in singleflight landet, nimmt dessen Cache-Eintrag.
func discoverOIDCProvider(issuer string) oidcProviderEntry {
	if e, ok := cachedOIDCProvider(issuer); ok {
		return e
	}
	p, err := oidc.NewProvider(oidcContext(issuer), issuer)
	e := oidcProviderEntry{provider: p, err: err, expires: oidcNow().Add(oidcProviderTTL)}
	if err != nil {
		e.expires = oidcNow().Add(oidcFailureTTL)
	}
	oidcProviderMu.Lock()
	oidcProviders[issuer] = e
	oidcProviderMu.Unlock()
	return e
}

// oidcProviderFor liefert den (gecachten) Provider eines Issuers. go-oidc
// prüft dabei, dass die Discovery denselben Issuer meldet.
func oidcProviderFor(issuer string) (*oidc.Provider, error) {
	if !strings.HasPrefix(issuer, "https://") && !oidcDevIssuerAllowed(issuer) {
		return nil, errOIDCInsecureIssuer
	}
	e, ok := cachedOIDCProvider(issuer)
	if !ok {
		// Die Discovery läuft ohne Sperre; gleichzeitige Aufrufe für denselben
		// Issuer teilen sich einen Netzwerkaufruf.
		v, _, _ := oidcDiscoveries.Do(issuer, func() (interface{}, error) {
			return discoverOIDCProvider(issuer), nil
		})
		e = v.(oidcProviderEntry)
	}
	if e.err != nil {
		return nil, e.err
	}
	// Die Prüfung läuft bei jedem Abruf (auch aus dem Cache), weil sie vom
	// Entwicklungsmodus abhängt und nicht mitgecacht werden soll.
	ep := e.provider.Endpoint()
	if !oidcEndpointAllowed(issuer, ep.AuthURL) || !oidcEndpointAllowed(issuer, ep.TokenURL) {
		return nil, errOIDCInsecureEndpoint
	}
	return e.provider, nil
}

func oidcRedirectURL(pageID uint) string {
	return getBaseURL() + "/api/auth/oidc/callback?page=" + strconv.FormatUint(uint64(pageID), 10)
}

func oidcOAuthConfig(cfg models.StatusPageSSOConfig, p *oidc.Provider) *oauth2.Config {
	scopes := strings.Fields(cfg.OIDCScopes)
	if !slices.Contains(scopes, oidc.ScopeOpenID) {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}
	return &oauth2.Config{
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		RedirectURL:  oidcRedirectURL(cfg.StatusPageID),
		Scopes:       scopes,
		Endpoint:     p.Endpoint(),
	}
}

func (h *Handler) activeOIDCConfig(pageID uint64) (models.StatusPageSSOConfig, bool) {
	var cfg models.StatusPageSSOConfig
	err := h.DB.Where("status_page_id = ?", pageID).
		Where(&models.StatusPageSSOConfig{OIDCEnabled: true}).
		First(&cfg).Error
	return cfg, err == nil
}

func (h *Handler) oidcProviderForConfig(cfg models.StatusPageSSOConfig) (*oidc.Provider, bool) {
	p, err := oidcProviderFor(oidcIssuer(cfg.OIDCDiscoveryURL))
	if err != nil {
		slog.Error("OIDC discovery failed", "page", cfg.StatusPageID, "error", logging.SanitizeErr(err))
		return nil, false
	}
	return p, true
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
	return h.startOIDCFlow(c, cfg, models.OIDCLoginState{LinkUserID: linkUserID})
}

// startOIDCFlow ergänzt st um State, Nonce und PKCE-Verifier, speichert ihn und
// setzt das State-Cookie. Gemeinsam für Login, Verknüpfung und Testanmeldung.
func (h *Handler) startOIDCFlow(c *gin.Context, cfg models.StatusPageSSOConfig, st models.OIDCLoginState) (string, *oidcError) {
	p, ok := h.oidcProviderForConfig(cfg)
	if !ok {
		return "", errOIDCProviderUnreachable
	}

	// Nie eingelöste States abgelaufener Logins gleich mit wegräumen.
	h.DB.Where("expires_at < ?", time.Now()).Delete(&models.OIDCLoginState{})
	st.State = randomToken(32)
	st.Nonce = randomToken(32)
	st.CodeVerifier = oauth2.GenerateVerifier()
	st.StatusPageID = cfg.StatusPageID
	st.ExpiresAt = time.Now().Add(oidcStateLifetime())
	if err := h.DB.Create(&st).Error; err != nil {
		return "", errOIDCStartFailed
	}
	c.SetCookie(oidcStateCookie, st.State, int(oidcStateLifetime().Seconds()), oidcStateCookiePath, "", true, true)
	return oidcOAuthConfig(cfg, p).AuthCodeURL(st.State, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.CodeVerifier)), nil
}

// oidcStateLifetime: So lange gilt ein Login-State. Als Funktion, weil gremlins
// Mutanten in Paketkonstanten keinem Coverage-Block zuordnet (NOT COVERED).
func oidcStateLifetime() time.Duration { return 10 * time.Minute }

// OIDCLoginHandler startet den Login über das SSO einer Statusseite.
// GET /api/auth/oidc/login?page=N (öffentlich, rate-limitiert). Der Browser
// navigiert hierher (Knopf „Mit SSO anmelden“), Fehler gehen deshalb wie im
// Callback als Code auf die Login-Seite.
func (h *Handler) OIDCLoginHandler(c *gin.Context) {
	target, e := h.beginOIDC(c, 0)
	if e != nil {
		target = oidcErrorURL(models.OIDCLoginState{}, e)
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

// ssoLinkUser (gemeinsam für OIDC und SAML) lädt das angemeldete
// Konto für eine Verknüpfung mit dem SSO der Seite ?page=N und wendet die
// Regeln aus OIDCLinkHandler an.
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

// consumeOIDCState prüft den State gegen das Cookie und löst ihn atomar ein
// (DELETE … RETURNING): Zwei gleichzeitige Callbacks mit demselben State
// können so nicht beide durchkommen. Fehlt der Code (der Anbieter
// meldet error=…, etwa nach einem Abbruch), wird der State trotzdem
// eingelöst, damit der Fehler-Redirect weiß, ob Login, Verknüpfung oder
// Testanmeldung lief.
func (h *Handler) consumeOIDCState(c *gin.Context) (models.OIDCLoginState, *oidcError) {
	var st models.OIDCLoginState
	state := c.Query("state")
	cookie, _ := c.Cookie(oidcStateCookie)
	c.SetCookie(oidcStateCookie, "", -1, oidcStateCookiePath, "", true, true)
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

// oidcClaims sind die Werte der konfigurierten Claims (oidc_attribute_name
// und, oidc_attribute_email) aus dem geprüften ID-Token.
type oidcClaims struct {
	name  string
	email string
}

// claimString liest einen String-Claim; fehlt er oder ist er kein String,
// ist das Ergebnis leer.
func claimString(claims map[string]interface{}, key string) string {
	v, _ := claims[key].(string)
	return v
}

// verifyOIDCCode tauscht den Code (mit PKCE-Verifier) und prüft das
// ID-Token samt Nonce. Liefert Token und die konfigurierten Claims; ein leerer
// Claim-Name gilt als Standard (name bzw. email).
func verifyOIDCCode(code string, cfg models.StatusPageSSOConfig, p *oidc.Provider, st models.OIDCLoginState) (*oidc.IDToken, oidcClaims, *oidcError) {
	ctx := oidcContext(oidcIssuer(cfg.OIDCDiscoveryURL))
	tok, err := oidcOAuthConfig(cfg, p).Exchange(ctx, code, oauth2.VerifierOption(st.CodeVerifier))
	if err != nil {
		slog.Error("OIDC token exchange failed", "page", cfg.StatusPageID, "error", logging.SanitizeErr(err))
		return nil, oidcClaims{}, errOIDCTokenExchange
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return nil, oidcClaims{}, errOIDCNoIDToken
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID}).Verify(ctx, raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(st.Nonce)) != 1 {
		slog.Warn("OIDC ID token rejected", "page", cfg.StatusPageID, "error", err)
		return nil, oidcClaims{}, errOIDCIDTokenInvalid
	}
	claims := map[string]interface{}{}
	_ = idt.Claims(&claims)
	return idt, oidcClaims{
		name:  claimString(claims, orDefault(cfg.OIDCAttributeName, models.SSODefaultAttributeName)),
		email: claimString(claims, orDefault(cfg.OIDCAttributeEmail, models.SSODefaultAttributeEmail)),
	}, nil
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

// resolveSSOUser findet (bzw. verknüpft) das Konto zur geprüften Identität
// key (Seite, Protokoll, Issuer, Subject; gemeinsam für OIDC und
// SAML). Die E-Mail aus dem Claim bzw. Attribut landet nur an der Verknüpfung
// (Anzeige im Profil), gesucht wird allein über key.
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

// hasSSOIdentityOnOtherPage: Seiten-SSO-Verknüpfung (OIDC/SAML) auf einer
// anderen Seite; bei DB-Fehler fail closed.
func (h *Handler) hasSSOIdentityOnOtherPage(userID, pageID uint) bool {
	var n int64
	if err := h.DB.Model(&models.UserSSOIdentity{}).Where("user_id = ? AND status_page_id <> ?", userID, pageID).Count(&n).Error; err != nil {
		return true
	}
	return n > 0
}

// OIDCCallbackHandler schließt Login, Verknüpfung oder Testanmeldung ab.
// GET /api/auth/oidc/callback?page=N&state=…&code=… (öffentlich). Jeder
// Fehler endet als Redirect mit festem Code ins Frontend.
func (h *Handler) OIDCCallbackHandler(c *gin.Context) {
	st, e := h.consumeOIDCState(c)
	if e == nil {
		e = h.completeOIDC(c, st)
	}
	if e != nil {
		c.Redirect(http.StatusTemporaryRedirect, oidcErrorURL(st, e))
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
	p, ok := h.oidcProviderForConfig(cfg)
	if !ok {
		return errOIDCProviderUnreachable
	}
	idt, claims, e := verifyOIDCCode(c.Query("code"), cfg, p, st)
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
	h.finishSSOLogin(c, user, claims.name, st.StatusPageID, st.LinkUserID, models.SSOProtocolOIDC)
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

// finishSSOLogin schreibt den Audit-Eintrag und gibt wie beim
// Google/GitHub-Login einen Einmal-Code statt des JWT im Redirect aus (S5).
// Nach einer Verknüpfung geht es zurück ins Profil (next=profile;
// das Frontend kennt nur diesen festen Wert). protocol (oidc oder
// saml) landet im Audit-Eintrag und im sso-Claim des JWT.
func (h *Handler) finishSSOLogin(c *gin.Context, user models.User, name string, pageID, linkUserID uint, protocol string) {
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
	token, _ := middleware.GeneratePageSSOToken(user, name, protocol, pageID)
	ssoRedirect(c, getFrontendURL()+"/auth/sso-callback?code="+storeSSOCode(token)+next)
}

// ssoRedirect leitet nach einem GET (OIDC-Callback) wie bisher
// mit 307 um, nach einem POST (SAML-ACS) mit 303: Ein 307 ließe den Browser
// den POST samt SAMLResponse am Frontend wiederholen.
func ssoRedirect(c *gin.Context, target string) {
	code := http.StatusTemporaryRedirect
	if c.Request.Method == http.MethodPost {
		code = http.StatusSeeOther
	}
	c.Redirect(code, target)
}
