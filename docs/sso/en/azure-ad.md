# Azure AD / Microsoft Entra ID as OIDC provider

> English version of the German guide [`../azure-ad.md`](../azure-ad.md).

Read the [general OIDC guide](oidc.md) first. This page contains only the Entra-specific steps.
Statushafen talks to Entra ID via OpenID Connect only; SAML is no longer supported.

## Steps

1. In Entra ID, go to "App registrations" → "New registration" and create an application.
   "Supported account types": **Accounts in this organizational directory only** (single tenant).
2. As redirect URI, choose the platform **Web** and enter the redirect URI of the status page,
   e.g.

   ```text
   https://status.example.org/api/auth/oidc/callback?page=<PAGE-ID>
   ```

3. Under "Certificates & secrets", create a **client secret** and copy its value (it is shown only
   once). The **Application (client) ID** is on the overview page.
4. Under "Token configuration", add the optional claim **email** to the ID token so that the link in
   the profile shows an address.
5. The issuer is `https://login.microsoftonline.com/<TENANT-ID>/v2.0`. Use the tenant ID, not
   `common` or `organizations`: only then does it match the `issuer` in the discovery document.
6. Enter it in Statushafen on the SSO page (or via the [API](api.md)):

   ```json
   {
     "oidc_enabled": true,
     "oidc_discovery_url": "https://login.microsoftonline.com/TENANT-ID/v2.0",
     "oidc_client_id": "CLIENT-ID",
     "oidc_client_secret": "CLIENT-SECRET",
     "oidc_scopes": "openid email profile"
   }
   ```

7. Start the test sign-in, switch OIDC on and link accounts, see
   [Accounts and linking](oidc.md#accounts-and-linking).

## As global provider

The same application (or a second one) can serve as a [global provider](providers.md) for signing in
to the whole instance. Redirect URI at the provider:
`https://status.example.org/api/auth/provider/entra/callback`.

```bash
OIDC_PROVIDERS=entra
OIDC_ENTRA_ISSUER=https://login.microsoftonline.com/TENANT-ID/v2.0
OIDC_ENTRA_CLIENT_ID=CLIENT-ID
OIDC_ENTRA_CLIENT_SECRET=CLIENT-SECRET
OIDC_ENTRA_NAME=Microsoft
```

Entra ID sends **no** `email_verified`. Without further settings no new accounts are created and
existing ones are not linked by e-mail. If your tenant only issues addresses of your verified
company domains, set `OIDC_ENTRA_EMAIL_TRUSTED=true`; the e-mail then counts as verified. Otherwise
existing accounts link the provider in the profile (for that, temporarily
`PASSWORD_LOGIN_ENABLED=true`). Always use the issuer with your
tenant ID, never `common`: with multi-tenant applications the `email` claim can be set by
outsiders.

Back to the [overview](README.md).
