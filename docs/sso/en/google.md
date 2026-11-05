# Google as OIDC provider

> English version of the German guide [`../google.md`](../google.md).

Read the [general OIDC guide](oidc.md) first. This page contains only the Google-specific steps.

> This is the **page SSO** of a status page. The server's global "Sign in with Google" login
> (`GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`) is a different function.

## Steps

1. In the Google Cloud Console, choose or create a project and set up the OAuth consent screen
   (user type "Internal" if only your organization should sign in).
2. Under "APIs & Services → Credentials", create an **OAuth client ID** of type "Web application".
3. Enter as **authorized redirect URI**:

   ```text
   https://status.example.org/api/auth/oidc/callback?page=<PAGE-ID>
   ```

4. Copy the client ID and client secret.
5. Enter them in Statushafen on the SSO page (or via the [API](api.md)):

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

6. Link an account and test, see [Accounts and linking](oidc.md#accounts-and-linking).
