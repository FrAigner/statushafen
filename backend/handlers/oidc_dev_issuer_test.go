package handlers

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Local SSO tests against the mock OIDC service from
// docker-compose.yml. It runs without TLS on localhost, both of which the
// page SSO login otherwise rejects (https requirement, SSRF protection). Exceptions exist
// only for issuers explicitly listed in OIDC_DEV_INSECURE_ISSUERS and never
// with APP_ENV=production.

func TestOIDCDevIssuer_PlainHTTPLoginWorksWhenAllowlisted(t *testing.T) {
	idp := newPlainHTTPTestIdP(t)
	t.Setenv("APP_ENV", "test")
	t.Setenv("OIDC_DEV_INSECURE_ISSUERS", " http://andere.example.org , "+idp.srv.URL+" ,")
	f := newOIDCFixtureWith(t, idp)
	f.linkIdentity(t, f.user.ID)

	state, ck := f.startLogin(t)
	claims := jwtFromRedirect(t, f.callback(state, "good-code", ck))

	assert.Equal(t, f.user.ID, claims.UserID)
	assert.Equal(t, "oidc", claims.SSOProvider)
}

func TestOIDCDevIssuer_PlainHTTPRejectedWithoutAllowlist(t *testing.T) {
	idp := newPlainHTTPTestIdP(t)
	t.Setenv("OIDC_DEV_INSECURE_ISSUERS", "")

	_, err := oidcProviderFor(idp.srv.URL)

	assert.ErrorIs(t, err, errOIDCInsecureIssuer)
	assert.Zero(t, idp.hits())
}

func TestOIDCDevIssuer_IgnoredInProduction(t *testing.T) {
	idp := newPlainHTTPTestIdP(t)
	t.Setenv("OIDC_DEV_INSECURE_ISSUERS", idp.srv.URL)
	t.Setenv("APP_ENV", "production")

	_, err := oidcProviderFor(idp.srv.URL)

	assert.ErrorIs(t, err, errOIDCInsecureIssuer)
	assert.Zero(t, idp.hits())
}

// Fail closed: only an explicitly named development environment enables the
// exception. A forgotten or differently spelled APP_ENV ("prod",
// empty) must not open it, otherwise a page admin could reach
// internal services via the discovery URL.
func TestOIDCDevIssuer_IgnoredOutsideDevelopmentEnvironments(t *testing.T) {
	for _, env := range []string{"", "prod", "staging", "live", "devel"} {
		t.Run("APP_ENV="+env, func(t *testing.T) {
			idp := newPlainHTTPTestIdP(t)
			t.Setenv("OIDC_DEV_INSECURE_ISSUERS", idp.srv.URL)
			t.Setenv("APP_ENV", env)

			_, err := oidcProviderFor(idp.srv.URL)

			assert.ErrorIs(t, err, errOIDCInsecureIssuer)
			assert.Zero(t, idp.hits())
		})
	}
}

// Only the exact issuer counts, not a prefix (otherwise e.g. another
// port or path on the same host would slip through).
func TestOIDCDevIssuer_RequiresExactMatch(t *testing.T) {
	idp := newPlainHTTPTestIdP(t)
	t.Setenv("APP_ENV", "test")

	t.Setenv("OIDC_DEV_INSECURE_ISSUERS", idp.srv.URL+"/realm")
	_, err := oidcProviderFor(idp.srv.URL)
	assert.ErrorIs(t, err, errOIDCInsecureIssuer)

	t.Setenv("OIDC_DEV_INSECURE_ISSUERS", idp.srv.URL[:len(idp.srv.URL)-1])
	_, err = oidcProviderFor(idp.srv.URL)
	assert.ErrorIs(t, err, errOIDCInsecureIssuer)
	assert.Zero(t, idp.hits())
}

// Other issuers keep the SSRF protection even if the list is set.
func TestOIDCDevIssuer_OtherIssuersKeepSSRFProtection(t *testing.T) {
	idp := newTestIdP(t)
	oidcHTTPClient = defaultOIDCHTTPClient()
	t.Setenv("APP_ENV", "test")
	t.Setenv("OIDC_DEV_INSECURE_ISSUERS", "http://localhost:8090/statushafen")

	_, err := oidcProviderFor(idp.srv.URL)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "interne Adresse")
	assert.Zero(t, idp.hits())
}

func TestOIDCDevHTTPClient_HasTimeout(t *testing.T) {
	assert.Equal(t, 10*time.Second, oidcDevHTTPClient().Timeout)
}

// Empty entries (e.g. a trailing comma) do not allow an empty issuer.
func TestOIDCDevIssuer_EmptyEntriesAllowNothing(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("OIDC_DEV_INSECURE_ISSUERS", "http://localhost:8090/statushafen, ,")
	assert.False(t, oidcDevIssuerAllowed(""))
	assert.True(t, oidcDevIssuerAllowed("http://localhost:8090/statushafen"))
}

// The addresses in the discovery document come from the provider that the page admin
// enters. An authorization_endpoint like javascript: or data: would go to
// the browser as a redirect target; only https is allowed (http only for
// the explicitly named development issuer).
func TestOIDCLogin_UnsichereEndpunkteImDiscoveryDokument(t *testing.T) {
	cases := map[string]struct{ auth, token string }{
		"javascript im authorization_endpoint": {auth: "javascript:alert(1)"},
		"data im authorization_endpoint":       {auth: "data:text/html,<script>alert(1)</script>"},
		"file im authorization_endpoint":       {auth: "file:///etc/passwd"},
		"http im authorization_endpoint":       {auth: "http://idp.example.org/authorize"},
		"ohne Schema":                          {auth: "//idp.example.org/authorize"},
		"ohne Host":                            {auth: "https:///authorize"},
		"javascript im token_endpoint":         {token: "javascript:alert(1)"},
		"http im token_endpoint":               {token: "http://idp.example.org/token"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newOIDCFixture(t)
			f.idp.authEndpoint, f.idp.tokenEndpoint = tc.auth, tc.token

			w := serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?"+f.pageQuery(), nil, 0)

			assertOIDCError(t, w, "/login", "provider_unreachable")
			assert.NotContains(t, w.Header().Get("Location"), "javascript:")
			assert.Empty(t, w.Result().Cookies(), "kein State-Cookie für einen abgelehnten Anbieter")
			var n int64
			f.db.Model(&models.OIDCLoginState{}).Count(&n)
			assert.Zero(t, n, "kein Login-State für einen abgelehnten Anbieter")
		})
	}

	t.Run("Gegenprobe: https-Endpunkte funktionieren", func(t *testing.T) {
		f := newOIDCFixture(t)
		w := serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?"+f.pageQuery(), nil, 0)
		require.Equal(t, http.StatusTemporaryRedirect, w.Code)
		assert.True(t, strings.HasPrefix(w.Header().Get("Location"), f.idp.srv.URL+"/authorize?"))
	})
}

// The development issuer may run with http endpoints, and only it.
func TestOIDCLogin_DevIssuerDarfHTTPEndpunkte(t *testing.T) {
	idp := newPlainHTTPTestIdP(t)
	t.Setenv("APP_ENV", "test")
	t.Setenv("OIDC_DEV_INSECURE_ISSUERS", idp.srv.URL)
	f := newOIDCFixtureWith(t, idp)

	w := serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?"+f.pageQuery(), nil, 0)

	require.Equal(t, http.StatusTemporaryRedirect, w.Code)
	assert.True(t, strings.HasPrefix(w.Header().Get("Location"), idp.srv.URL+"/authorize?"))

	// even in development mode javascript: stays forbidden
	resetOIDCProviderCache()
	idp.authEndpoint = "javascript:alert(1)"
	w = serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?"+f.pageQuery(), nil, 0)
	assertOIDCError(t, w, "/login", "provider_unreachable")
}
