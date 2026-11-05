package handlers

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /api/auth/sso/login?slug=… ist der Einstieg ins Seiten-SSO für Besucher
// einer privaten Seite. Die Auskunft GET /api/public/pages/:slug/sso nennt
// ihnen Seiten-ID und SSO-Art nicht mehr; dieser Weg braucht sie nicht, der
// Server wählt das Verfahren selbst.

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

func TestSSOLoginBySlug_StartsSAMLWhenOnlySAMLIsActive(t *testing.T) {
	f := newSAMLFixture(t)
	w := serveOIDC(f.h.SSOLoginBySlugHandler, http.MethodGet, "/api/auth/sso/login?slug="+f.page.Slug, nil, 0)
	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.NotEmpty(t, loc.Query().Get("SAMLRequest"))
	assert.NotNil(t, samlStateCookie(w))
}

func TestSSOLoginBySlug_OIDCWinsWhenBothAreActive(t *testing.T) {
	f := newOIDCFixture(t)
	require.NoError(t, f.db.Model(&models.StatusPageSSOConfig{}).Where("status_page_id = ?", f.page.ID).Update("saml_enabled", true).Error)
	_, loc := ssoBySlug(f, f.page.Slug)
	assert.Contains(t, loc, f.idp.srv.URL+"/authorize")
}

// Unbekannter Slug, Seite ohne SSO und Seite mit ausgeschaltetem SSO enden am
// selben Ziel: Der Fehlerweg verrät nichts über die Seite.
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
