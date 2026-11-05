# SSO (OIDC and SAML)

Each status page can have its own identity provider (IdP): via OpenID Connect (OIDC), via SAML
2.0, or both in parallel. This is set up under **SSO Configuration** (permissions `sso:read`,
`sso:update`, `sso:delete` on the page, or global admin).

## How sign-in works

1. Choose "Login" on the status page. The login page shows an SSO button (OIDC or SAML) when SSO
   is active for the page.
2. The browser goes to the IdP and comes back after sign-in.
3. The session is valid for this status page only.

**Important:** SSO does not create new accounts. You sign in once as usual (password, Google or
GitHub) and click "Link with SSO" under "SSO sign-in" in your **Profile**. After that the account
is recognized by the IdP's identifier, never by email address.

Global admins and accounts with a role on a different status page cannot sign in via the page
SSO.

## Set up OIDC

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

## Set up SAML

| Field | Content |
|---|---|
| Enable SAML sign-in | enables the SAML button |
| IdP metadata URL **or** IdP metadata (XML) | one of the two; URL `https` only |
| Attribute for the email address / name | attribute names from the SAML response |
| Allow NameID format "unspecified" | off by default |
| SP entity ID, SP ACS URL, SP certificate (read-only) | register at the IdP |

The status page's metadata is at `<address>/api/auth/saml/<page ID>/metadata`, the ACS at
`<address>/api/auth/saml/<page ID>/acs` (HTTP-POST). Key and certificate are created on the first
save. The expected NameID format is `persistent`.

## Testing

"Start test sign-in" checks the **saved** configuration, even before SSO is enabled. The test
creates neither a session nor a link.

## Common errors

| Code | Meaning |
|---|---|
| `not_linked` | Account is not linked yet, see above |
| `already_linked` | This IdP identity is already linked to another account |
| `global_admin`, `other_page_role` | Account is excluded from the page SSO |
| `state_invalid` | Sign-in took longer than 10 minutes or the cookie is missing |
| `provider_unreachable` | IdP unreachable or issuer differs, often a trailing slash in the discovery URL |
| `provider_unreachable` (log: endpoint must be https) | The discovery document names a sign-in or token address without https |
| `token_exchange` | OIDC: client ID/secret wrong or redirect URI not registered exactly |
| `saml_nameid_format` | SAML: NameID format not accepted |

If the issuer or the IdP metadata changes, existing links are deleted and must be created again.

Detailed operator guides with examples for Authentik, Keycloak, Entra ID, Google and Okta are in
the repository under `docs/user-guide/en/sso-oidc.md`, `docs/user-guide/en/sso-saml.md` and
`docs/sso/en/` (German originals: `docs/user-guide/` and `docs/sso/`).
