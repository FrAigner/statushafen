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

// Endpunkte für den Frontend-Einstieg ins Seiten-SSO:
//   - GET /api/public/pages/:slug/sso meldet nur, ob OIDC aktiv ist
//     (Knopf „Mit SSO anmelden“ auf Login- und Statusseite),
//   - GET/DELETE /api/me/sso zeigt und löst die eigenen Verknüpfungen,
//   - POST /api/admin/pages/:page_id/sso/test startet die Testanmeldung
//     (test_login_enabled).

// GetPublicOIDCHandler — GET /api/public/pages/:slug/sso (öffentlich).
// Liefert bewusst nur oidc_enabled und die Seiten-ID für den
// Login-Link, keine Discovery-URL, Client-ID oder sonstige Konfiguration.
// Eine private Seite nennt Seiten-ID und SSO-Art nur Berechtigten (Rolle oder
// Global Admin); alle anderen bekommen dieselbe 404-Antwort wie bei einer
// fehlenden Seite, sonst ließe sich anonym erkennen, welche Slugs private Seiten
// sind und welches SSO sie nutzen. Den Einstieg für Besucher einer privaten
// Seite bietet GET /api/auth/sso/login?slug=… (SSOLoginBySlugHandler).
func (h *Handler) GetPublicOIDCHandler(c *gin.Context) {
	var page models.StatusPage
	if err := h.DB.Where(slugWhereClause, c.Param("slug")).First(&page).Error; err != nil {
		respondPageNotVisible(c)
		return
	}
	// Ohne den Auto-Join-Nebeneffekt von checkPrivatePageAccess: Eine reine
	// Auskunft soll keine Rolle vergeben.
	if page.IsPrivate && !h.userHasRoleOnPrivatePage(c, page.ID) {
		respondPageNotVisible(c)
		return
	}
	_, active := h.activeOIDCConfig(uint64(page.ID))
	c.JSON(http.StatusOK, gin.H{"oidc_enabled": active, "page_id": page.ID})
}

// SSOLoginBySlugHandler — GET /api/auth/sso/login?slug=… (öffentlich,
// rate-limitiert). Einstieg ins Seiten-SSO für Besucher, denen die Seite ihre
// Seiten-ID und SSO-Art nicht nennt (private Seite ohne Berechtigung, siehe
// GetPublicOIDCHandler). Eine unbekannte Seite, eine Seite ohne SSO und ein Slug-Parameter
// ohne Wert enden alle in derselben Fehlerweiterleitung (not_configured). Wer die
// Adresse für einen Slug aufruft und zum Anbieter geleitet wird, erfährt damit
// nur, dass die Seite SSO nutzt; das ist der Mindestumfang, den eine Anmeldung
// braucht.
func (h *Handler) SSOLoginBySlugHandler(c *gin.Context) {
	target, e := h.beginSSOBySlug(c)
	if e != nil {
		target = ssoErrorURL(false, 0, e)
	}
	c.Redirect(http.StatusTemporaryRedirect, target)
}

func (h *Handler) beginSSOBySlug(c *gin.Context) (string, *oidcError) {
	var page models.StatusPage
	if err := h.DB.Where(slugWhereClause, c.Query("slug")).First(&page).Error; err != nil {
		return "", errOIDCNotConfigured
	}
	if cfg, ok := h.activeOIDCConfig(uint64(page.ID)); ok {
		return h.startOIDCFlow(c, pageOIDCClient(cfg), models.OIDCLoginState{StatusPageID: cfg.StatusPageID}, oidcStateCookiePath)
	}
	return "", errOIDCNotConfigured
}

// oidcConfigComplete: Ohne Discovery-URL, Client-ID und Secret kann kein
// Ablauf beim Anbieter starten.
func oidcConfigComplete(cfg models.StatusPageSSOConfig) bool {
	return cfg.OIDCDiscoveryURL != "" && cfg.OIDCClientID != "" && cfg.OIDCClientSecret != ""
}

// testOIDCConfig lädt die Konfiguration für eine Testanmeldung. Anders als der
// Login braucht sie kein oidc_enabled, damit ein Admin die Einrichtung vor dem
// Einschalten prüfen kann; dafür muss test_login_enabled gesetzt sein (Feld
// „Test-Login erlauben“, im Live-Betrieb abschaltbar).
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
// auf der Seite). Liefert die Authorize-URL als JSON wie OIDCLinkHandler. Der
// Callback prüft Code, ID-Token und Nonce wie beim Login, legt aber weder eine
// Sitzung noch eine Verknüpfung an und meldet das Ergebnis an die SSO-Seite.
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

// GetMySSOHandler — GET /api/me/sso. Eigene Verknüpfungen und die Seiten, die
// das Konto noch verknüpfen darf.
func (h *Handler) GetMySSOHandler(c *gin.Context) {
	var user models.User
	if err := h.DB.First(&user, getUserID(c)).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Nicht angemeldet", "code": "unauthorized"})
		return
	}
	resp := mySSOResponse{Identities: []mySSOIdentity{}, Linkable: []mySSOLinkable{}}
	// LEFT JOIN: Auch die Verknüpfung einer inzwischen gelöschten Seite bleibt
	// sichtbar und lösbar.
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

// linkablePageScope bildet die Regeln aus OIDCLinkHandler ab (Global Admins
// fallen schon vorher raus): Mit Rolle auf genau einer Seite nur diese, mit
// Rollen auf mehreren keine (hasRoleOnOtherPage träfe immer). Ohne Rolle nur
// öffentliche, gelistete Seiten, damit die Liste keine privaten oder
// ungelisteten Seitennamen verrät.
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

// DeleteMySSOHandler — DELETE /api/me/sso/:id. Löst eine eigene Verknüpfung;
// fremde und unbekannte IDs sind gleichermaßen 404.
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
