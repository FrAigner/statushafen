# SSO (OIDC)

Statushafen signs in via OpenID Connect (OIDC), in two ways:

- **Global sign-in providers** (e.g. GitLab or a company's identity service) are set up by the
  operator on the server. The login page shows one "Sign in with <name>" button per active
  sign-in provider.
- **A status page's SSO:** each status page can have its own identity provider (IdP). This is set
  up under **SSO Configuration** (permissions `sso:read`, `sso:update`, `sso:delete` on the page,
  or global admin).

Whether signing in with email and password is offered as well is also up to the operator. If it is
switched off, the login page shows no form. If no sign-in method is set up at all, the login page
says so.

## How sign-in works

1. Choose "Login" on the status page. The login page shows the buttons of the sign-in providers
   and "Sign in with SSO" when SSO is active for the page.
2. The browser goes to the IdP and, after sign-in, comes back to the page the sign-in started
   from.
3. A session via a status page's SSO is valid for this status page only.

**Important:** a status page's SSO does not create new accounts. You sign in once in another way
and click "Link with SSO" under "SSO sign-in" in your **Profile**. After that the account is
recognized by the IdP's identifier, never by email address. There you can also link an existing
account with a global sign-in provider ("Link with <name>").

Global admins and accounts with a role on a different status page cannot sign in via a status
page's SSO.

## Set up OIDC for a status page

| Field | Content |
|---|---|
| Enable OIDC sign-in | enables the SSO button |
| Discovery URL | Address of the IdP, e.g. `https://idp.example/realms/acme` |
| Client ID / Client Secret | Credentials of the app at the IdP; the secret is never shown again |
| Scopes | default `openid email profile` |
| Claim for the name / the email address | default `name` and `email` |
| ACS URL (read-only) | register as redirect URI at the IdP |

The redirect URI has the form `<address>/api/auth/oidc/callback?page=<page ID>`. The `?page=` is
part of it; many IdPs compare it character by character.

## Testing

"Start test sign-in" checks the **saved** configuration, even before SSO is enabled. The test
creates neither a session nor a link.

## Common errors

| Code | Meaning |
|---|---|
| `not_linked` | Account is not linked yet, see above |
| `already_linked` | This IdP identity is already linked to another account |
| `global_admin`, `other_page_role` | Account is excluded from the status page's SSO |
| `state_invalid` | Sign-in took longer than 10 minutes or the cookie is missing |
| `provider_unreachable` | IdP unreachable or issuer differs, often a trailing slash in the discovery URL |
| `provider_unreachable` (log: endpoint must be https) | The discovery document names a sign-in or token address without https |
| `token_exchange` | Client ID/secret wrong or redirect URI not registered exactly |
| `provider_unknown` | The global sign-in provider is unknown or no longer active |
| `password_login_disabled` | Signing in with email and password is switched off |

If the issuer changes, the status page's existing links are deleted and must be created again.

Detailed operator guides with examples for Authentik, Keycloak, Entra ID, Google and Okta are in
the repository under `docs/user-guide/en/sso-oidc.md` and `docs/sso/en/` (German originals:
`docs/user-guide/` and `docs/sso/`).
