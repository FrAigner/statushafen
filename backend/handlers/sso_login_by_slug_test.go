package handlers

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /api/auth/sso/login?slug=… is the entry point into page SSO for visitors
// of a private page. The lookup GET /api/public/pages/:slug/sso no longer tells
// them the page ID and SSO type; this route does not need them, the
// server picks the method itself.

const notConfiguredRedirect = "http://localhost:5173/login?oidc_error=not_configured"

func ssoBySlug(f *oidcFixture, slug string) (int, string) {
	target := "/api/auth/sso/login"
	if slug != "" {
		target += "?slug=" + url.QueryEscape(slug)
	}
	w := serveOIDC(f.h.SSOLoginBySlugHandler, http.MethodGet, target, nil, 0)
	return w.Code, w.Header().Get("Location")
}

func TestSSOLoginBySlug_StartsOIDCForPublicAndPrivatePage(t *testing.T) {
	f := newOIDCFixture(t)
	for _, private := range []bool{false, true} {
		require.NoError(t, f.db.Model(&models.StatusPage{}).Where("id = ?", f.page.ID).Update("is_private", private).Error)
		code, loc := ssoBySlug(f, f.page.Slug)
		require.Equal(t, http.StatusTemporaryRedirect, code)
		u, err := url.Parse(loc)
		require.NoError(t, err)
		assert.Equal(t, f.idp.srv.URL+"/authorize", u.Scheme+"://"+u.Host+u.Path, "private=%v", private)
		assert.Equal(t, "S256", u.Query().Get("code_challenge_method"))

		var st models.OIDCLoginState
		require.NoError(t, f.db.Where("state = ?", u.Query().Get("state")).First(&st).Error)
		assert.Equal(t, f.page.ID, st.StatusPageID)
		assert.Zero(t, st.LinkUserID)
		assert.False(t, st.TestLogin)
	}
}

// A leftover from the SAML era (saml_enabled in the DB) starts nothing: without
// active OIDC the page counts as having no SSO.
func TestSSOLoginBySlug_LegacySAMLColumnStartsNothing(t *testing.T) {
	f := newOIDCFixture(t)
	require.NoError(t, f.db.Exec("UPDATE status_page_sso_configs SET o_id_c_enabled = ? WHERE status_page_id = ?", false, f.page.ID).Error)
	require.NoError(t, f.db.Exec("ALTER TABLE status_page_sso_configs ADD COLUMN saml_enabled numeric DEFAULT false").Error)
	require.NoError(t, f.db.Exec("UPDATE status_page_sso_configs SET saml_enabled = ? WHERE status_page_id = ?", true, f.page.ID).Error)
	code, loc := ssoBySlug(f, f.page.Slug)
	assert.Equal(t, http.StatusTemporaryRedirect, code)
	assert.Equal(t, notConfiguredRedirect, loc)
}

// Unknown slug, page without SSO and page with SSO disabled end up at the
// same destination: the error path reveals nothing about the page.
func TestSSOLoginBySlug_UnknownAndUnconfiguredPagesGetTheSameAnswer(t *testing.T) {
	f := newOIDCFixture(t)
	plain := models.StatusPage{Name: "Ohne SSO", Slug: "ohne-sso", IsPrivate: true}
	require.NoError(t, f.db.Create(&plain).Error)
	off := models.StatusPage{Name: "SSO aus", Slug: "sso-aus"}
	require.NoError(t, f.db.Create(&off).Error)
	require.NoError(t, f.db.Create(&models.StatusPageSSOConfig{StatusPageID: off.ID, OIDCEnabled: false, OIDCDiscoveryURL: "https://idp.example.com", OIDCClientID: "x", OIDCClientSecret: "y"}).Error)

	for _, slug := range []string{"gibt-es-nicht", plain.Slug, off.Slug, ""} {
		code, loc := ssoBySlug(f, slug)
		assert.Equal(t, http.StatusTemporaryRedirect, code, slug)
		assert.Equal(t, notConfiguredRedirect, loc, slug)
	}
}

func TestSSOLoginBySlug_ProviderUnreachableReportsStartError(t *testing.T) {
	f := newOIDCFixture(t)
	require.NoError(t, f.db.Model(&models.StatusPageSSOConfig{}).Where("status_page_id = ?", f.page.ID).
		Update("o_id_c_discovery_url", "https://127.0.0.1:1/.well-known/openid-configuration").Error)
	code, loc := ssoBySlug(f, f.page.Slug)
	assert.Equal(t, http.StatusTemporaryRedirect, code)
	assert.Contains(t, loc, "/login?oidc_error=")
	assert.NotEqual(t, notConfiguredRedirect, loc)
}
