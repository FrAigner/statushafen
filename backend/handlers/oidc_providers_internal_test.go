package handlers

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setProvider sets the four mandatory variables of one global provider.
func setProvider(t *testing.T, envID, issuer string) {
	t.Helper()
	t.Setenv("OIDC_"+envID+"_ISSUER", issuer)
	t.Setenv("OIDC_"+envID+"_CLIENT_ID", "client-"+envID)
	t.Setenv("OIDC_"+envID+"_CLIENT_SECRET", "secret-"+envID)
}

func TestParseOIDCProviders_ReadsProvidersInListOrder(t *testing.T) {
	t.Setenv("OIDC_PROVIDERS", " gitlab , google-work ")
	setProvider(t, "GITLAB", "https://gitlab.example.org")
	t.Setenv("OIDC_GITLAB_NAME", "GitLab")
	t.Setenv("OIDC_GITLAB_SCOPES", "email openid read_user")
	setProvider(t, "GOOGLE_WORK", "https://accounts.example.org/.well-known/openid-configuration")

	got, warnings := parseOIDCProviders()

	assert.Empty(t, warnings)
	require.Len(t, got, 2)
	assert.Equal(t, globalOIDCProvider{
		ID: "gitlab", Name: "GitLab", Issuer: "https://gitlab.example.org",
		ClientID: "client-GITLAB", ClientSecret: "secret-GITLAB",
		Scopes: []string{"email", "openid", "read_user"},
	}, got[0])
	// Defaults: name = ID, scopes = openid email profile; a full discovery
	// URL is reduced to the issuer.
	assert.Equal(t, "google-work", got[1].Name)
	assert.Equal(t, []string{"openid", "email", "profile"}, got[1].Scopes)
	assert.Equal(t, "https://accounts.example.org", got[1].Issuer)
}

func TestParseOIDCProviders_ScopesAlwaysContainOpenID(t *testing.T) {
	t.Setenv("OIDC_PROVIDERS", "kc")
	setProvider(t, "KC", "https://kc.example.org/realms/a")
	t.Setenv("OIDC_KC_SCOPES", "email profile")

	got, _ := parseOIDCProviders()

	require.Len(t, got, 1)
	assert.Equal(t, []string{"openid", "email", "profile"}, got[0].Scopes)
}

func TestParseOIDCProviders_InvalidEntriesAreDisabledWithAWarning(t *testing.T) {
	t.Setenv("OIDC_PROVIDERS", "ok,Bad_ID,,no-secret,plain-http,ok,way-too-long-provider-identifier-xx")
	setProvider(t, "OK", "https://ok.example.org")
	setProvider(t, "NO_SECRET", "https://n.example.org")
	t.Setenv("OIDC_NO_SECRET_CLIENT_SECRET", "")
	setProvider(t, "PLAIN_HTTP", "http://plain.example.org")
	t.Setenv("APP_ENV", "production")

	got, warnings := parseOIDCProviders()

	require.Len(t, got, 1, "only the valid provider stays active")
	assert.Equal(t, "ok", got[0].ID)
	joined := ""
	for _, w := range warnings {
		joined += w + "\n"
	}
	for _, want := range []string{"Bad_ID", "no-secret", "OIDC_NO_SECRET_CLIENT_SECRET", "plain-http", "duplicate", "way-too-long-provider-identifier-xx"} {
		assert.Contains(t, joined, want)
	}
	assert.NotContains(t, joined, "secret-OK", "warnings never contain secrets")
}

func TestParseOIDCProviders_DevInsecureIssuerIsAccepted(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("OIDC_DEV_INSECURE_ISSUERS", "http://localhost:8090/statushafen")
	t.Setenv("OIDC_PROVIDERS", "mock")
	setProvider(t, "MOCK", "http://localhost:8090/statushafen")

	got, warnings := parseOIDCProviders()

	assert.Empty(t, warnings)
	require.Len(t, got, 1)
}

func TestParseOIDCProviders_NoneConfigured(t *testing.T) {
	t.Setenv("OIDC_PROVIDERS", "")
	got, warnings := parseOIDCProviders()
	assert.Empty(t, got)
	assert.Empty(t, warnings)
}

func TestGlobalOIDCProviderByID(t *testing.T) {
	t.Setenv("OIDC_PROVIDERS", "gitlab")
	setProvider(t, "GITLAB", "https://gitlab.example.org")

	p, ok := globalOIDCProviderByID("gitlab")
	assert.True(t, ok)
	assert.Equal(t, "gitlab", p.ID)
	_, ok = globalOIDCProviderByID("github")
	assert.False(t, ok)
	_, ok = globalOIDCProviderByID("")
	assert.False(t, ok)
}

func captureDefaultLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestWarnAuthConfig(t *testing.T) {
	t.Run("invalid provider is reported without its secret", func(t *testing.T) {
		t.Setenv("OIDC_PROVIDERS", "half")
		t.Setenv("OIDC_HALF_ISSUER", "https://half.example.org")
		t.Setenv("OIDC_HALF_CLIENT_ID", "")
		t.Setenv("OIDC_HALF_CLIENT_SECRET", "s3cr3t-value-of-half")
		buf := captureDefaultLog(t)
		WarnAuthConfig()
		assert.Contains(t, buf.String(), "half")
		assert.Contains(t, buf.String(), "OIDC_HALF_CLIENT_ID")
		assert.NotContains(t, buf.String(), "s3cr3t-value-of-half")
	})
	t.Run("valid provider logs nothing", func(t *testing.T) {
		t.Setenv("OIDC_PROVIDERS", "gitlab")
		setProvider(t, "GITLAB", "https://gitlab.example.org")
		buf := captureDefaultLog(t)
		WarnAuthConfig()
		assert.Empty(t, buf.String())
	})
}
