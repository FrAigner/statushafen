# Setting up OIDC (general)

> English version of the German guide [`../oidc.md`](../oidc.md).

With OIDC, users sign in through the identity provider (IdP) of your organization. The
configuration applies per status page. The flow is described in the [architecture](architecture.md),
the endpoints in the [API](api.md), all fields and error cases in the
[admin manual](../../user-guide/en/sso-oidc.md).

## Prerequisites

- The IdP supports OpenID Connect with discovery (`/.well-known/openid-configuration`) and the
  authorization code flow. PKCE (S256) is always used.
- The **issuer** starts with `https://`. Statushafen rejects it with `http://`.
- The IdP is reachable from the server's network. Internal addresses (for example private IP
  ranges) are blocked (SSRF protection as for webhooks).
- You have the permission `sso:update` on this status page (or are a global admin).

## Steps

1. **Create the client at the IdP** (type "Web"/"confidential", authorization code flow). Enter as
   redirect URI:

   ```text
   https://status.example.org/api/auth/oidc/callback?page=<PAGE-ID>
   ```

   `https://status.example.org` is the `BASE_URL` of the server (placeholder), `<PAGE-ID>` the ID
   of the status page. The address must be registered at the IdP exactly like this, including
   `?page=`. The SSO page in the admin area shows it to you (field `oidc_acs_url`).

2. **Note the client ID and client secret** from the IdP.

3. **Enter them in Statushafen:** admin area → group "System" → "SSO Configuration", section OIDC.
   Via the API the same works with `PUT /api/admin/pages/<PAGE-ID>/sso` (see [API](api.md)):

   ```json
   {
     "oidc_enabled": true,
     "oidc_discovery_url": "https://idp.example.org/realms/demo",
     "oidc_client_id": "CLIENT-ID",
     "oidc_client_secret": "CLIENT-SECRET",
     "oidc_scopes": "openid email profile",
     "oidc_attribute_name": "name",
     "test_login_enabled": true
   }
   ```

   `oidc_discovery_url` may be the issuer itself or the full `.well-known/openid-configuration`
   address. **Scopes:** the default is `openid email profile`; Statushafen adds `openid` itself.
   With `oidc_enabled: true`, discovery URL, client ID and a client secret are mandatory. The
   secret is never returned again; an empty field on the next save keeps the stored one.

4. **Test:** With "Allow test login", "Start test sign-in" starts a trial run with the saved
   configuration, even before OIDC is switched on. The SSO page then reports success or the error
   code. Turn the test login off again in production.

5. **Sign in:** On the status page "Login" → "Sign in with SSO". The button appears as soon as
   OIDC is active; behind it is `GET /api/auth/oidc/login?page=<PAGE-ID>`.

## Accounts and linking

The account is found only through the triple (status page, issuer, subject), **never through the
email address**. No new accounts are created at OIDC login. An existing account must be linked to
the identity once:

1. The user signs in normally (password, Google, GitHub).
2. In the profile, "SSO sign-in" shows the pages they may link. "Link with SSO" calls
   `POST /api/auth/oidc/link?page=<PAGE-ID>` and redirects to the IdP.
3. After signing in at the IdP, the account is linked and the user is signed in.

A link can also be removed again in the profile. Excluded are global admins (they do not use the
page SSO) and accounts with a role on another status page. If the identity already belongs to
another account, linking aborts with `already_linked`. If the issuer changes, saving deletes the
OIDC links of the page; the accounts must be linked again.

## Environment variables

These apply to the server, not per page:

| Variable | Effect |
| --- | --- |
| `BASE_URL` | Public address of the server; basis of the redirect URI. Without a value: `http://localhost:8080`. |
| `FRONTEND_URL` | The server redirects here after login (`/auth/sso-callback?code=…`) and on errors. Without a value: `http://localhost:5173`. |
| `JWT_SECRET` | Mandatory for the session after login. |

`SSO_ALLOWED_DOMAINS` and `SSO_BOOTSTRAP_ADMIN_EMAILS` take effect **only** for the Google/GitHub
login (`/api/auth/google`, `/api/auth/github`), not for page OIDC: there is no automatic account
creation and no email matching that would need restricting.

## Troubleshooting

Errors at login and callback end up as a code in the URL of the login page (`?oidc_error=<code>`),
for the test sign-in on the SSO page (`?oidc_test=<code>`). The most common ones:

| Code | Cause |
| --- | --- |
| `not_configured` | OIDC is off for the page or the page ID is wrong. |
| `provider_unreachable` | Discovery fails: issuer without `https://`, internal address, unreachable, or a different `issuer` in the discovery document (often a trailing slash). Failures are remembered for one minute. |
| `state_invalid` | Login took longer than 10 minutes, was completed in a different browser, or the state was already used. |
| `token_exchange` | Client ID/secret or redirect URI do not match at the IdP. |
| `id_token_invalid` | Signature, issuer, audience (`aud`), expiry or nonce do not match. Check the server clock. |
| `not_linked` | Link the account first, see above. |

All codes: [error cases in the admin manual](../../user-guide/en/sso-oidc.md#error-cases).
