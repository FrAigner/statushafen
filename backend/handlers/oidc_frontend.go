package handlers

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Endpoints for the frontend entry into page SSO:
//   - GET /api/public/pages/:slug/sso only reports whether OIDC is active
//     (the "Sign in with SSO" button on the login and status page),
//   - GET/DELETE /api/me/sso shows and removes the user's own links,
//   - POST /api/admin/pages/:page_id/sso/test starts the test login
//     (test_login_enabled).

// GetPublicOIDCHandler — GET /api/public/pages/:slug/sso (public).
// Deliberately returns only oidc_enabled and the page ID for the
// login link, no discovery URL, client ID or other configuration.
// A private page reveals page ID and SSO kind only to authorized users (role or
// global admin); everyone else gets the same 404 response as for a
// missing page, otherwise one could anonymously tell which slugs are private pages
// and which SSO they use. The entry point for visitors of a private
// page is GET /api/auth/sso/login?slug=… (SSOLoginBySlugHandler).
func (h *Handler) GetPublicOIDCHandler(c *gin.Context) {
	var page models.StatusPage
	if err := h.DB.Where(slugWhereClause, c.Param("slug")).First(&page).Error; err != nil {
		respondPageNotVisible(c)
		return
	}
	// Without the auto-join side effect of checkPrivatePageAccess: a pure
	// status query must not assign a role.
	if page.IsPrivate && !h.userHasRoleOnPrivatePage(c, page.ID) {
		respondPageNotVisible(c)
		return
	}
	_, active := h.activeOIDCConfig(uint64(page.ID))
	c.JSON(http.StatusOK, gin.H{"oidc_enabled": active, "page_id": page.ID})
}

// SSOLoginBySlugHandler — GET /api/auth/sso/login?slug=… (public,
// rate-limited). Entry into page SSO for visitors to whom the page does not reveal its
// page ID and SSO kind (private page without authorization, see
// GetPublicOIDCHandler). An unknown page, a page without SSO and an empty slug parameter
// all end in the same error redirect (not_configured). Anyone who
// calls the address for a slug and is redirected to the provider thereby learns
// only that the page uses SSO; that is the minimum a login
// needs.
func (h *Handler) SSOLoginBySlugHandler(c *gin.Context) {
	if h.moveLoginToBaseHost(c) {
		return
	}
	target, e := h.beginSSOBySlug(c)
	if e != nil {
		target = h.loginStartErrorURL(c, e)
	}
	c.Redirect(http.StatusTemporaryRedirect, target)
}

func (h *Handler) beginSSOBySlug(c *gin.Context) (string, *oidcError) {
	var page models.StatusPage
	if err := h.DB.Where(slugWhereClause, c.Query("slug")).First(&page).Error; err != nil {
		return "", errOIDCNotConfigured
	}
	if cfg, ok := h.activeOIDCConfig(uint64(page.ID)); ok {
		st := models.OIDCLoginState{
			StatusPageID: cfg.StatusPageID,
			ReturnTo:     sanitizeReturnTo(c.Query("return_to")),
			ReturnHost:   h.acceptReturnHost(c.Query(returnHostParam), cfg.StatusPageID).host,
		}
		return h.startOIDCFlow(c, pageOIDCClient(cfg), st, oidcStateCookiePath)
	}
	return "", errOIDCNotConfigured
}

// oidcConfigComplete: without discovery URL, client ID and secret no
// flow can be started at the provider.
func oidcConfigComplete(cfg models.StatusPageSSOConfig) bool {
	return cfg.OIDCDiscoveryURL != "" && cfg.OIDCClientID != "" && cfg.OIDCClientSecret != ""
}

// testOIDCConfig loads the configuration for a test login. Unlike the
// login it does not need oidc_enabled, so an admin can check the setup before
// enabling it; instead test_login_enabled must be set (field
// "Allow test login", can be switched off in live operation).
func (h *Handler) testOIDCConfig(pageID uint64) (models.StatusPageSSOConfig, *oidcError) {
	var cfg models.StatusPageSSOConfig
	if err := h.DB.Where("status_page_id = ?", pageID).First(&cfg).Error; err != nil {
		return cfg, errOIDCNotConfigured
	}
	if !cfg.TestLoginEnabled {
		return cfg, errOIDCTestDisabled
	}
	if !oidcConfigComplete(cfg) {
		return cfg, errOIDCTestIncomplete
	}
	return cfg, nil
}

// StartSSOTestHandler — POST /api/admin/pages/:page_id/sso/test (pages:write
// on the page). Returns the authorize URL as JSON like OIDCLinkHandler. The
// callback checks code, ID token and nonce as in the login, but creates neither a
// session nor a link and reports the result to the SSO page.
func (h *Handler) StartSSOTestHandler(c *gin.Context) {
	pageID, err := strconv.ParseUint(c.Param("page_id"), 10, 32)
	if err != nil {
		writeOIDCError(c, errOIDCBadPage)
		return
	}
	cfg, e := h.testOIDCConfig(pageID)
	if e != nil {
		writeOIDCError(c, e)
		return
	}
	target, e := h.startOIDCFlow(c, pageOIDCClient(cfg), models.OIDCLoginState{StatusPageID: cfg.StatusPageID, TestLogin: true}, oidcStateCookiePath)
	if e != nil {
		writeOIDCError(c, e)
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": target})
}

type mySSOIdentity struct {
	ID           uint      `json:"id"`
	StatusPageID uint      `json:"status_page_id"`
	PageName     string    `json:"page_name"`
	Protocol     string    `json:"protocol"`
	Email        string    `json:"email"`
	CreatedAt    time.Time `json:"created_at"`
	// Provider: ID of the global provider of an identity with
	// StatusPageID 0 (derived from the issuer; "" if no longer configured),
	// "" for page SSO identities.
	Provider string `json:"provider"`
	Issuer   string `json:"-"`
}

// mySSOLinkable: one entry per page with active OIDC (link endpoint
// /api/auth/oidc/link?page=N) or per global provider (StatusPageID 0,
// Provider set, PageName = provider name, link endpoint
// /api/auth/provider/<Provider>/link). Protocol is always "oidc".
type mySSOLinkable struct {
	StatusPageID uint   `json:"status_page_id"`
	PageName     string `json:"page_name"`
	Protocol     string `json:"protocol"`
	Provider     string `json:"provider"`
}

type mySSOResponse struct {
	Identities []mySSOIdentity `json:"identities"`
	Linkable   []mySSOLinkable `json:"linkable"`
}

// GetMySSOHandler — GET /api/me/sso. The user's own links and the pages that
// the account may still link.
func (h *Handler) GetMySSOHandler(c *gin.Context) {
	var user models.User
	if err := h.DB.First(&user, getUserID(c)).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Nicht angemeldet", "code": "unauthorized"})
		return
	}
	resp := mySSOResponse{Identities: []mySSOIdentity{}, Linkable: []mySSOLinkable{}}
	// On a custom domain the tenant could read the list (providers, e-mail
	// there); linking and unlinking are refused there anyway.
	if middleware.CustomDomainSession(c) {
		c.JSON(http.StatusOK, resp)
		return
	}
	// LEFT JOIN: even the link of a since-deleted page remains
	// visible and removable.
	h.DB.Table("user_sso_identities AS i").
		Select("i.id, i.status_page_id, p.name AS page_name, i.protocol, i.email, i.created_at, i.issuer").
		Joins("LEFT JOIN status_pages p ON p.id = i.status_page_id AND p.deleted_at IS NULL").
		// OIDC only: links from the SAML era stay in the table but have no
		// effect and are no longer listed.
		Where("i.user_id = ? AND i.protocol = ?", user.ID, models.SSOProtocolOIDC).Order("i.id").Scan(&resp.Identities)
	providers := globalOIDCProviders()
	linkedIssuers := map[string]bool{}
	for i, ident := range resp.Identities {
		if ident.StatusPageID != 0 {
			continue
		}
		linkedIssuers[ident.Issuer] = true
		if p, ok := providerForIssuer(providers, ident.Issuer); ok {
			resp.Identities[i].Provider, resp.Identities[i].PageName = p.ID, p.Name
		}
	}
	// Global providers first (OIDC_PROVIDERS order), also for global admins;
	// a page SSO session may not link them (see ProviderLinkHandler).
	if middleware.SSOPageID(c) == 0 {
		for _, p := range providers {
			if !linkedIssuers[p.Issuer] {
				resp.Linkable = append(resp.Linkable, mySSOLinkable{PageName: p.Name, Protocol: models.SSOProtocolOIDC, Provider: p.ID})
			}
		}
	}
	if !user.IsGlobalAdmin {
		resp.Linkable = append(resp.Linkable, h.linkableSSOPages(user.ID)...)
	}
	c.JSON(http.StatusOK, resp)
}

// providerForIssuer returns the first configured provider with this issuer.
func providerForIssuer(providers []globalOIDCProvider, issuer string) (globalOIDCProvider, bool) {
	for _, p := range providers {
		if p.Issuer == issuer {
			return p, true
		}
	}
	return globalOIDCProvider{}, false
}

// linkableSSOPages: pages with active OIDC that the account has not linked
// yet, sorted by page name.
func (h *Handler) linkableSSOPages(userID uint) []mySSOLinkable {
	out := []mySSOLinkable{}
	h.DB.Table("status_pages AS p").
		Select("p.id AS status_page_id, p.name AS page_name, ? AS protocol", models.SSOProtocolOIDC).
		Joins("JOIN status_page_sso_configs s ON s.status_page_id = p.id AND s.deleted_at IS NULL").
		Where("p.deleted_at IS NULL AND s.o_id_c_enabled = ?", true).
		Where("p.id NOT IN (?)", h.DB.Model(&models.UserSSOIdentity{}).Select("status_page_id").
			Where("user_id = ? AND protocol = ?", userID, models.SSOProtocolOIDC)).
		Scopes(h.linkablePageScope(userID)).
		Scan(&out)
	sort.SliceStable(out, func(i, j int) bool { return out[i].PageName < out[j].PageName })
	return out
}

// linkablePageScope mirrors the rules from OIDCLinkHandler (global admins
// are already excluded earlier): with a role on exactly one page only that one, with
// roles on several none (hasRoleOnOtherPage would always match). Without a role only
// public, listed pages, so the list does not reveal private or
// unlisted page names.
func (h *Handler) linkablePageScope(userID uint) func(*gorm.DB) *gorm.DB {
	var rolePages []uint
	h.DB.Model(&models.UserStatusPageRole{}).Where("user_id = ?", userID).Distinct().Pluck("status_page_id", &rolePages)
	return func(q *gorm.DB) *gorm.DB {
		if len(rolePages) > 1 {
			return q.Where("1 = 0")
		}
		if len(rolePages) == 1 {
			return q.Where("p.id = ?", rolePages[0])
		}
		return q.Where("p.is_private = ? AND p.is_public_indexed = ?", false, true)
	}
}

// DeleteMySSOHandler — DELETE /api/me/sso/:id. Removes one of the user's own links;
// foreign and unknown IDs alike yield 404.
func (h *Handler) DeleteMySSOHandler(c *gin.Context) {
	var user models.User
	if err := h.DB.First(&user, getUserID(c)).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Nicht angemeldet", "code": "unauthorized"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ungültige ID", "code": "invalid_id"})
		return
	}
	var ident models.UserSSOIdentity
	res := h.DB.Clauses(clause.Returning{}).Where("id = ? AND user_id = ?", id, user.ID).Delete(&ident)
	if res.Error != nil || res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Verknüpfung nicht gefunden", "code": "identity_not_found"})
		return
	}
	h.RecordAudit(c, "auth.sso_unlinked", "auth", "User", strconv.FormatUint(uint64(user.ID), 10), user.Email,
		"provider="+ident.Protocol+" page="+strconv.FormatUint(uint64(ident.StatusPageID), 10))
	c.JSON(http.StatusNoContent, nil)
}
