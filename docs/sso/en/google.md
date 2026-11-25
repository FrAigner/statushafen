# Google as OIDC provider

> English version of the German guide [`../google.md`](../google.md).

Google is connected through OpenID Connect like any other provider: as a
[global provider](providers.md) for signing in to the whole instance or as the
[page SSO](oidc.md) of a status page. The former built-in "Sign in with Google" login
(`GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `/api/auth/google`) is gone; for the switch see
[upgrading](providers.md#upgrading-from-older-versions).

## Create the client at Google

1. In the Google Cloud Console, choose or create a project and set up the OAuth consent screen
   (user type "Internal" if only your Google Workspace organization should sign in).
2. Under "APIs & Services → Credentials", create an **OAuth client ID** of type "Web application".
3. Enter as **authorized redirect URI**:
   - global provider: `https://status.example.org/api/auth/provider/google/callback`
   - page SSO: `https://status.example.org/api/auth/oidc/callback?page=<PAGE-ID>`
4. Copy the client ID and the client secret.

The issuer is always `https://accounts.google.com`. Google sends `email_verified`; existing accounts
with the same address are linked on the first login.

## As global provider

```bash
OIDC_PROVIDERS=google
OIDC_GOOGLE_ISSUER=https://accounts.google.com
OIDC_GOOGLE_CLIENT_ID=CLIENT-ID
OIDC_GOOGLE_CLIENT_SECRET=CLIENT-SECRET
OIDC_GOOGLE_NAME=Google
```

With user type "External" any Google account can sign in. Then restrict new accounts to your
domains with `SSO_ALLOWED_DOMAINS`.

## As page SSO

Enter it in Statushafen on the SSO page of the status page (or via the [API](api.md)):

```json
{
  "oidc_enabled": true,
  "oidc_discovery_url": "https://accounts.google.com",
  "oidc_client_id": "CLIENT-ID",
  "oidc_client_secret": "CLIENT-SECRET",
  "oidc_scopes": "openid email profile",
  "oidc_attribute_name": "name"
}
```

Then link an account and test, see [Accounts and linking](oidc.md#accounts-and-linking).

Back to the [overview](README.md).
