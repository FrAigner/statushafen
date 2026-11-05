package handlers

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fehlschläge beim Anbieter loggen den Fehlertext des HTTP-Clients, und der
// enthält die abgerufene URL samt Query. Der Discovery-URL pflegt der Seiten-
// Admin und der Token-Endpunkt kommt vom Anbieter; ein Geheimnis darin darf
// selbst bei einem Standard-Logger ohne zentrale Bereinigung nicht im Log landen.
func TestOIDCLogin_ProviderFailuresKeepSecretsOutOfTheLog(t *testing.T) {
	const secret = "probe-abruf-marker"
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	t.Run("Discovery nicht erreichbar", func(t *testing.T) {
		f := newOIDCFixture(t)
		base := f.idp.srv.URL
		f.idp.srv.Close()
		require.NoError(t, f.db.Model(&models.StatusPageSSOConfig{}).Where("status_page_id = ?", f.page.ID).
			Update("o_id_c_discovery_url", base+"/realm?token="+secret).Error)
		resetOIDCProviderCache()

		w := serveOIDC(f.h.OIDCLoginHandler, "GET", "/api/auth/oidc/login?"+f.pageQuery(), nil, 0)
		assertOIDCError(t, w, "/login", "provider_unreachable")
	})
	t.Run("Token-Austausch scheitert im Netz", func(t *testing.T) {
		f := newOIDCFixture(t)
		f.idp.tokenEndpoint = "https://127.0.0.1:1/token?client_secret=" + secret
		resetOIDCProviderCache()
		state, ck := f.startLogin(t)

		w := f.callback(state, "good-code", ck)
		assertOIDCError(t, w, "/login", "token_exchange")
	})

	assert.Contains(t, buf.String(), "OIDC discovery failed", "Gegenprobe: der Fehlschlag wird geloggt")
	assert.Contains(t, buf.String(), "OIDC token exchange failed", "Gegenprobe: der Fehlschlag wird geloggt")
	assert.NotContains(t, buf.String(), secret)
}
