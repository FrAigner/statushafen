# Okta as OIDC provider

> English version of the German guide [`../okta.md`](../okta.md).

Read the [general OIDC guide](oidc.md) first. This page contains only the Okta-specific steps.
Statushafen talks to Okta via OpenID Connect only; SAML is no longer supported.

## Steps

1. In the Okta admin console, go to "Applications" → "Create App Integration" and choose:
   - Sign-in method: **OIDC – OpenID Connect**
   - Application type: **Web Application**
2. "Grant type": **Authorization Code** (Statushafen always adds PKCE).
3. "Sign-in redirect URIs": the redirect URI of the status page, e.g.

   ```text
   https://status.example.org/api/auth/oidc/callback?page=<PAGE-ID>
   ```

4. Under "Assignments", assign the users or groups that may sign in.
5. Copy the **Client ID** and **Client secret** from the "General" tab.
6. Determine the issuer. With the default authorization server it is
   `https://<OKTA-DOMAIN>/oauth2/default`, without a custom authorization server `https://<OKTA-DOMAIN>`.
   It must match the `issuer` in `/.well-known/openid-configuration`.
7. Enter it in Statushafen on the SSO page (or via the [API](api.md)):

   ```json
   {
     "oidc_enabled": true,
     "oidc_discovery_url": "https://okta.example.org/oauth2/default",
     "oidc_client_id": "CLIENT-ID",
     "oidc_client_secret": "CLIENT-SECRET",
     "oidc_scopes": "openid email profile"
   }
   ```

8. Start the test sign-in, switch OIDC on and link accounts, see
   [Accounts and linking](oidc.md#accounts-and-linking).

## As global provider

The same application (or a second one) can serve as a [global provider](providers.md) for signing in
to the whole instance. Redirect URI at the provider:
`https://status.example.org/api/auth/provider/okta/callback`.

```bash
OIDC_PROVIDERS=okta
OIDC_OKTA_ISSUER=https://okta.example.org/oauth2/default
OIDC_OKTA_CLIENT_ID=CLIENT-ID
OIDC_OKTA_CLIENT_SECRET=CLIENT-SECRET
OIDC_OKTA_NAME=Okta
```

Okta sends `email_verified`.

Back to the [overview](README.md).
