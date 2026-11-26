package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Sign-in through a global OIDC provider (see oidc_providers.go):
//
//  1. GET /api/auth/provider/:id/login: state, nonce and PKCE verifier as
//     for the page SSO (startOIDCFlow), with OIDCLoginState.Provider = :id.
//  2. GET /api/auth/provider/:id/callback: redeem the state (only a state of
//     this provider), exchange the code, verify the ID token.
//  3. The account is found through the identity (StatusPageID 0, issuer,
//     subject). On the first login without an identity, the e-mail claim
//     decides, and only a verified e-mail counts (email_verified=true, or a
//     missing claim at a provider with OIDC_<ID>_EMAIL_TRUSTED): it links an
//     existing account or creates a new one (SSO_ALLOWED_DOMAINS applies).
//     An unverified e-mail never creates an account: otherwise anyone could
//     pre-create an account for someone else's address, which that person's
//     later verified login (or SSO_BOOTSTRAP_ADMIN_EMAILS) would link to.
//  4. Session as for the page SSO: one-time code, redeemed via
//     POST /api/auth/sso-exchange. The JWT has no sso_page_id.
//
// POST /api/auth/provider/:id/link links the signed-in account; it is the
// only way to attach an identity whose e-mail does not (verifiably) match.

const providerStateCookiePath = "/api/auth/provider"

var (
	errOIDCProviderUnknown  = &oidcError{http.StatusNotFound, "provider_unknown", "Unbekannter Anmeldeanbieter"}
	errOIDCEmailMissing     = &oidcError{http.StatusForbidden, "email_missing", "Der Anbieter liefert keine E-Mail-Adresse"}
	errOIDCEmailUnverified  = &oidcError{http.StatusForbidden, "email_unverified", "Die E-Mail-Adresse ist beim Anbieter nicht bestätigt"}
	errOIDCDomainNotAllowed = &oidcError{http.StatusForbidden, "domain_not_allowed", "E-Mail-Domain nicht für SSO freigegeben"}
	errOIDCPageSession      = &oidcError{http.StatusForbidden, "page_session", "Eine Seiten-SSO-Sitzung kann keinen globalen Anbieter verknüpfen"}
)

// providerRedirectURL is the redirect URI to register at the provider.
func providerRedirectURL(id string) string {
	return getBaseURL() + "/api/auth/provider/" + id + "/callback"
}

// client builds the relying-party configuration of a global provider.
func (p globalOIDCProvider) client() oidcClient {
	return oidcClient{
		issuer:       p.Issuer,
		clientID:     p.ClientID,
		clientSecret: p.ClientSecret,
		scopes:       p.Scopes,
		redirectURL:  providerRedirectURL(p.ID),
		nameClaim:    models.SSODefaultAttributeName,
		emailClaim:   models.SSODefaultAttributeEmail,
		trusted:      true,
		logKey:       "provider",
		logValue:     p.ID,
	}
}

// authConfigProvider is one sign-in button of the login page.
type authConfigProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetAuthConfigHandler — GET /api/public/auth-config (public, rate-limited).
// Lists the sign-in methods for the login page: whether the password login is
// on and the active global providers in OIDC_PROVIDERS order (ID and name
// only, no issuer or client data).
func (h *Handler) GetAuthConfigHandler(c *gin.Context) {
	providers := []authConfigProvider{}
	for _, p := range globalOIDCProviders() {
		providers = append(providers, authConfigProvider{ID: p.ID, Name: p.Name})
	}
	c.JSON(http.StatusOK, gin.H{"password_login": passwordLoginEnabled(), "providers": providers})
}

func (h *Handler) beginProviderFlow(c *gin.Context, linkUserID uint) (string, *oidcError) {
	p, ok := globalOIDCProviderByID(c.Param("id"))
	if !ok {
		return "", errOIDCProviderUnknown
	}
	st := models.OIDCLoginState{Provider: p.ID, LinkUserID: linkUserID}
	if linkUserID == 0 {
		st.ReturnTo = sanitizeReturnTo(c.Query("return_to"))
		st.ReturnHost = h.acceptReturnHost(c.Query(returnHostParam), 0).host
	}
	return h.startOIDCFlow(c, p.client(), st, providerStateCookiePath)
}

// ProviderLoginHandler starts the sign-in with a global provider.
// GET /api/auth/provider/:id/login (public, rate-limited). The browser
// navigates here, so errors end as a redirect to the login page.
func (h *Handler) ProviderLoginHandler(c *gin.Context) {
	if h.moveLoginToBaseHost(c) {
		return
	}
	target, e := h.beginProviderFlow(c, 0)
	if e != nil {
		target = h.loginStartErrorURL(c, e)
	}
	c.Redirect(http.StatusTemporaryRedirect, target)
}

// ProviderLinkHandler starts linking the signed-in account with a global
// provider and returns the authorize URL as JSON (like OIDCLinkHandler).
// POST /api/auth/provider/:id/link (signed in). Global admins may link; a
// session from a page SSO may not (it would become a full session).
func (h *Handler) ProviderLinkHandler(c *gin.Context) {
	var user models.User
	if err := h.DB.First(&user, getUserID(c)).Error; err != nil {
		writeOIDCError(c, errSSONotLoggedIn)
		return
	}
	if middleware.SSOPageID(c) != 0 {
		writeOIDCError(c, errOIDCPageSession)
		return
	}
	target, e := h.beginProviderFlow(c, user.ID)
	if e != nil {
		writeOIDCError(c, e)
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": target})
}

// ProviderCallbackHandler completes login or link of a global provider.
// GET /api/auth/provider/:id/callback (public, rate-limited).
func (h *Handler) ProviderCallbackHandler(c *gin.Context) {
	var st models.OIDCLoginState
	p, ok := globalOIDCProviderByID(c.Param("id"))
	e := errOIDCProviderUnknown
	if ok {
		st, e = h.consumeOIDCState(c, providerStateCookiePath)
	}
	// Only a state started for exactly this provider counts (not one of a
	// page SSO or of another provider).
	if e == nil && st.Provider != p.ID {
		e = errOIDCStateInvalid
	}
	if e == nil {
		e = h.completeProviderLogin(c, p, st)
	}
	if e != nil {
		c.Redirect(http.StatusTemporaryRedirect, ssoErrorURL(st.TestLogin, st.LinkUserID, e))
	}
}

func (h *Handler) completeProviderLogin(c *gin.Context, p globalOIDCProvider, st models.OIDCLoginState) *oidcError {
	cl := p.client()
	op, ok := cl.provider()
	if !ok {
		return errOIDCProviderUnreachable
	}
	idt, claims, e := verifyOIDCCode(c.Query("code"), cl, op, st)
	if e != nil {
		return e
	}
	user, e := h.resolveProviderUser(st.LinkUserID, idt, claims, p.EmailTrusted)
	if e != nil {
		return e
	}
	action, next := "auth.sso_login", ""
	if st.LinkUserID != 0 {
		action, next = "auth.sso_linked", "&next=profile"
	}
	origin, returnTo := h.loginDelivery(st, user)
	detail := "provider=" + p.ID
	if origin.pageID != 0 {
		detail += " page=" + strconv.FormatUint(uint64(origin.pageID), 10)
	}
	c.Set("email", user.Email)
	c.Set("userID", user.ID)
	c.Set("role", user.Role.Name)
	h.RecordAudit(c, action, "auth", "User", strconv.FormatUint(uint64(user.ID), 10), user.Email, detail)
	// HS256 with a configured JWT_SECRET cannot fail (see finishSSOLogin).
	// On a custom domain the session is limited to its page (see login_origin.go).
	token, _ := middleware.GenerateTokenFull(user, user.Role.Permissions, claims.name, models.SSOProtocolOIDC)
	if origin.pageID != 0 {
		token, _ = middleware.GeneratePageSSOToken(user, claims.name, models.SSOProtocolOIDC, origin.pageID, middleware.WithCustomDomain(origin.host))
	}
	c.Redirect(http.StatusTemporaryRedirect, origin.frontendURL()+"/auth/sso-callback?code="+storeSSOCode(token, returnTo, origin.host, codeBinding(st, origin))+next)
	return nil
}

// providerIdentityScope selects the global identity (StatusPageID 0) of an
// issuer and subject.
func providerIdentityScope(db *gorm.DB, issuer, subject string) *gorm.DB {
	return db.Where("status_page_id = ? AND protocol = ? AND issuer = ? AND subject = ? AND subject_format = ?",
		0, models.SSOProtocolOIDC, issuer, subject, "")
}

// resolveProviderUser finds, links or creates the account of a verified ID
// token. linkUserID != 0 links that signed-in account.
func (h *Handler) resolveProviderUser(linkUserID uint, idt *oidc.IDToken, claims oidcClaims, emailTrusted bool) (models.User, *oidcError) {
	var user models.User
	var ident models.UserSSOIdentity
	found := providerIdentityScope(h.DB, idt.Issuer, idt.Subject).First(&ident).Error == nil
	verified := emailVerified(claims.emailVerified, emailTrusted)
	if linkUserID != 0 {
		key := models.UserSSOIdentity{Protocol: models.SSOProtocolOIDC, Issuer: idt.Issuer, Subject: idt.Subject, Email: claims.email}
		if found {
			key = ident
		}
		userID, e := h.linkSSOIdentity(linkUserID, key, found)
		if e != nil {
			return user, e
		}
		ident.UserID = userID
	} else if !found {
		var e *oidcError
		if ident, e = h.firstProviderLogin(claims, verified, idt); e != nil {
			return user, e
		}
	}
	if h.DB.Preload("Role").First(&user, ident.UserID).Error != nil {
		return user, errOIDCNotLinked
	}
	providerIdentityScope(h.DB.Model(&models.UserSSOIdentity{}), idt.Issuer, idt.Subject).Update("email", claims.email)
	if verified && strings.EqualFold(claims.email, user.Email) && isBootstrapAdminEmail(user.Email) && !user.IsGlobalAdmin {
		user = h.raiseToGlobalAdmin(user)
	}
	return user, nil
}

// emailVerified reports whether an e-mail claim counts as verified: an explicit
// email_verified=true, or a missing claim at a provider the operator trusts.
// An explicit false never counts.
func emailVerified(claim *bool, trusted bool) bool {
	if claim == nil {
		return trusted
	}
	return *claim
}

// firstProviderLogin handles a login without an identity: link an existing
// account by its verified e-mail, or create a new account.
func (h *Handler) firstProviderLogin(claims oidcClaims, verified bool, idt *oidc.IDToken) (models.UserSSOIdentity, *oidcError) {
	ident := models.UserSSOIdentity{Protocol: models.SSOProtocolOIDC, Issuer: idt.Issuer, Subject: idt.Subject, Email: claims.email}
	email := strings.TrimSpace(claims.email)
	if email == "" {
		return ident, errOIDCEmailMissing
	}
	ident.Email = email
	var existing models.User
	err := h.DB.Where("email = ?", email).First(&existing).Error
	if err == nil {
		// Only a verified e-mail links an existing account.
		if !verified {
			return ident, errOIDCEmailUnverified
		}
		ident.UserID = existing.ID
		if h.DB.Create(&ident).Error != nil {
			return ident, errOIDCLinkFailed
		}
		return ident, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return ident, errOIDCStartFailed
	}
	if !verified {
		return ident, errOIDCEmailUnverified
	}
	if !isEmailDomainAllowed(email) {
		return ident, errOIDCDomainNotAllowed
	}
	return ident, h.provisionProviderUser(email, verified && isBootstrapAdminEmail(email), &ident)
}

// provisionProviderUser creates the account and its identity in one
// transaction. New accounts get the Viewer role and no tags (fail closed);
// bootstrap admins become global admin.
func (h *Handler) provisionProviderUser(email string, bootstrapAdmin bool, ident *models.UserSSOIdentity) *oidcError {
	roleName := "Viewer"
	if bootstrapAdmin {
		roleName = models.GlobalAdminRoleName
	}
	var role models.Role
	h.DB.Where("name = ?", roleName).First(&role)
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		user := models.User{Email: email, RoleID: role.ID, IsGlobalAdmin: bootstrapAdmin}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		ident.UserID = user.ID
		return tx.Create(ident).Error
	})
	if err != nil {
		return errOIDCLinkFailed
	}
	return nil
}

// raiseToGlobalAdmin makes a bootstrap admin (SSO_BOOTSTRAP_ADMIN_EMAILS) a
// global admin on login.
func (h *Handler) raiseToGlobalAdmin(user models.User) models.User {
	updates := map[string]interface{}{"is_global_admin": true}
	var role models.Role
	if h.DB.Where("name = ?", models.GlobalAdminRoleName).First(&role).Error == nil {
		updates["role_id"] = role.ID
	}
	h.DB.Model(&user).Updates(updates)
	h.DB.Preload("Role").First(&user, user.ID)
	return user
}
