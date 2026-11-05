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

// Lokale SSO-Tests gegen den Mock-OIDC-Dienst aus
// docker-compose.yml. Der läuft ohne TLS auf localhost, beides lehnt der
// Seiten-SSO-Login sonst ab (https-Pflicht, SSRF-Schutz). Ausnahmen gibt es
// nur für ausdrücklich in OIDC_DEV_INSECURE_ISSUERS genannte Issuer und nie
// mit APP_ENV=production.

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

// Fail closed: Nur eine ausdrücklich benannte Entwicklungsumgebung schaltet die
// Ausnahme frei. Eine vergessene oder abweichend geschriebene APP_ENV ("prod",
// leer) darf sie nicht öffnen, sonst erreichte ein Seiten-Admin per
// Discovery-URL interne Dienste.
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

// Nur der exakte Issuer zählt, kein Präfix (sonst käme z. B. ein anderer
// Port oder Pfad auf demselben Host mit durch).
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

// Andere Issuer behalten den SSRF-Schutz, auch wenn die Liste gesetzt ist.
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

// Leere Einträge (z. B. abschließendes Komma) erlauben keinen leeren Issuer.
func TestOIDCDevIssuer_EmptyEntriesAllowNothing(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("OIDC_DEV_INSECURE_ISSUERS", "http://localhost:8090/statushafen, ,")
	assert.False(t, oidcDevIssuerAllowed(""))
	assert.True(t, oidcDevIssuerAllowed("http://localhost:8090/statushafen"))
}

// Die Adressen im Discovery-Dokument stammen vom Anbieter, den der Seiten-Admin
// einträgt. Ein authorization_endpoint wie javascript: oder data: würde als
// Weiterleitungsziel an den Browser gehen; nur https ist erlaubt (http nur für
// den ausdrücklich benannten Entwicklungs-Issuer).
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

// Der Entwicklungs-Issuer darf mit http-Endpunkten laufen, nur er.
func TestOIDCLogin_DevIssuerDarfHTTPEndpunkte(t *testing.T) {
	idp := newPlainHTTPTestIdP(t)
	t.Setenv("APP_ENV", "test")
	t.Setenv("OIDC_DEV_INSECURE_ISSUERS", idp.srv.URL)
	f := newOIDCFixtureWith(t, idp)

	w := serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?"+f.pageQuery(), nil, 0)

	require.Equal(t, http.StatusTemporaryRedirect, w.Code)
	assert.True(t, strings.HasPrefix(w.Header().Get("Location"), idp.srv.URL+"/authorize?"))

	// auch im Entwicklungsmodus bleibt javascript: verboten
	resetOIDCProviderCache()
	idp.authEndpoint = "javascript:alert(1)"
	w = serveOIDC(f.h.OIDCLoginHandler, http.MethodGet, "/api/auth/oidc/login?"+f.pageQuery(), nil, 0)
	assertOIDCError(t, w, "/login", "provider_unreachable")
}
