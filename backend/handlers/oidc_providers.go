package handlers

import (
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Global OIDC providers are configured by the operator through environment
// variables and offer sign-in for the whole instance (unlike the page SSO,
// whose IdP a page admin maintains):
//
//	OIDC_PROVIDERS=gitlab,google
//	OIDC_<ID>_ISSUER, OIDC_<ID>_CLIENT_ID, OIDC_<ID>_CLIENT_SECRET (required)
//	OIDC_<ID>_NAME (button label, default: the ID)
//	OIDC_<ID>_SCOPES (default: "openid email profile")
//	OIDC_<ID>_EMAIL_TRUSTED (true/1/yes: a missing email_verified claim counts
//	as verified; only for providers that issue e-mail addresses users cannot
//	choose, e.g. Entra ID with verified company domains. Default: off)
//
// <ID> is the provider ID in upper case with "-" replaced by "_". The
// configuration is read on every use (cheap) so that tests can set it per
// test; WarnAuthConfig reports invalid entries once at start-up. An invalid or
// incomplete provider is disabled, never fatal.

// globalOIDCProvider is one valid, enabled entry of OIDC_PROVIDERS.
type globalOIDCProvider struct {
	ID           string
	Name         string
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	// EmailTrusted: the operator vouches for the provider's e-mail claim
	// (OIDC_<ID>_EMAIL_TRUSTED), so a missing email_verified counts as verified.
	EmailTrusted bool
}

var oidcProviderIDPattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// oidcProviderEnvPrefix maps a provider ID to its variable prefix, e.g.
// "google-work" -> "OIDC_GOOGLE_WORK_".
func oidcProviderEnvPrefix(id string) string {
	return "OIDC_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_"
}

// parseOIDCProviders reads OIDC_PROVIDERS and returns the valid providers in
// list order plus one warning per disabled entry. Warnings name the provider
// and the variables involved, never their values.
func parseOIDCProviders() ([]globalOIDCProvider, []string) {
	var providers []globalOIDCProvider
	var warnings []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(os.Getenv("OIDC_PROVIDERS"), ",") {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if !oidcProviderIDPattern.MatchString(id) {
			warnings = append(warnings, "OIDC provider "+id+" disabled: the ID must match [a-z0-9-]{1,32}")
			continue
		}
		if seen[id] {
			warnings = append(warnings, "OIDC provider "+id+" disabled: duplicate entry in OIDC_PROVIDERS")
			continue
		}
		seen[id] = true
		p, problem := readOIDCProvider(id)
		if problem != "" {
			warnings = append(warnings, "OIDC provider "+id+" disabled: "+problem)
			continue
		}
		providers = append(providers, p)
	}
	return providers, warnings
}

// readOIDCProvider reads the variables of one provider and returns a problem
// description when it cannot be used.
func readOIDCProvider(id string) (globalOIDCProvider, string) {
	prefix := oidcProviderEnvPrefix(id)
	get := func(name string) string { return strings.TrimSpace(os.Getenv(prefix + name)) }
	p := globalOIDCProvider{
		ID:           id,
		Name:         orDefault(get("NAME"), id),
		Issuer:       oidcIssuer(get("ISSUER")),
		ClientID:     get("CLIENT_ID"),
		ClientSecret: get("CLIENT_SECRET"),
		Scopes:       strings.Fields(orDefault(get("SCOPES"), "openid email profile")),
		EmailTrusted: envFlag(prefix + "EMAIL_TRUSTED"),
	}
	var missing []string
	for _, v := range []struct{ name, value string }{
		{"ISSUER", p.Issuer}, {"CLIENT_ID", p.ClientID}, {"CLIENT_SECRET", p.ClientSecret},
	} {
		if v.value == "" {
			missing = append(missing, prefix+v.name)
		}
	}
	if len(missing) > 0 {
		return p, "missing " + strings.Join(missing, ", ")
	}
	if !strings.HasPrefix(p.Issuer, "https://") && !oidcDevIssuerAllowed(p.Issuer) {
		return p, prefix + "ISSUER must use https (http only for issuers listed in OIDC_DEV_INSECURE_ISSUERS during development)"
	}
	if !slices.Contains(p.Scopes, oidc.ScopeOpenID) {
		p.Scopes = append([]string{oidc.ScopeOpenID}, p.Scopes...)
	}
	return p, ""
}

// globalOIDCProviders returns the enabled providers in OIDC_PROVIDERS order.
func globalOIDCProviders() []globalOIDCProvider {
	p, _ := parseOIDCProviders()
	return p
}

// globalOIDCProviderByID returns the enabled provider with this ID.
func globalOIDCProviderByID(id string) (globalOIDCProvider, bool) {
	for _, p := range globalOIDCProviders() {
		if p.ID == id {
			return p, true
		}
	}
	return globalOIDCProvider{}, false
}

// WarnAuthConfig logs every disabled OIDC provider once at start-up and a
// clear warning when no sign-in method is active at all.
func WarnAuthConfig() {
	providers, warnings := parseOIDCProviders()
	for _, w := range warnings {
		slog.Warn(w)
	}
	if len(providers) == 0 && !passwordLoginEnabled() {
		slog.Warn("No sign-in method is active: no valid OIDC provider in OIDC_PROVIDERS and PASSWORD_LOGIN_ENABLED is off. Only the create-admin CLI can create an account; nobody can sign in.")
	}
}
