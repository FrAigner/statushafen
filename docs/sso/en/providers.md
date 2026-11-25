# Sign-in through global OIDC providers

> English version of the German guide [`../providers.md`](../providers.md).

Statushafen signs users in through OpenID Connect (OIDC). The **global providers** are set up by the
operator for the whole instance through environment variables; the login page shows one button per
provider. This is independent of the [page SSO](oidc.md) that a page admin enters per status page.
Guides per provider: [GitLab](gitlab.md), [Google](google.md), [Keycloak](keycloak.md),
[Okta](okta.md), [Azure AD](azure-ad.md). Endpoints: [API](api.md), flow and threat model:
[architecture](architecture.md).

## Configuration

| Variable | Meaning |
| --- | --- |
| `OIDC_PROVIDERS` | Comma-separated provider IDs, e.g. `gitlab,google`. Allowed are `a-z`, `0-9` and `-`, 1 to 32 characters. The order is the order of the buttons. |
| `OIDC_<ID>_ISSUER` | Issuer of the provider (or the full `.well-known/openid-configuration` address). `https://` only. |
| `OIDC_<ID>_CLIENT_ID` | Client ID of the application registered at the provider. |
| `OIDC_<ID>_CLIENT_SECRET` | Client secret. Never returned or logged. |
| `OIDC_<ID>_NAME` | Button label, e.g. `GitLab`. Default: the ID. |
| `OIDC_<ID>_SCOPES` | Scopes, default `openid email profile`; `openid` is always added. |
| `OIDC_<ID>_EMAIL_TRUSTED` | `true` = a missing `email_verified` counts as verified. Only for providers where users cannot choose their address (e.g. Entra ID with verified company domains). Default: off. |
| `PASSWORD_LOGIN_ENABLED` | `true`, `1` or `yes` (any case) switches the password login on. Anything else and unset: **off**. |
| `SSO_ALLOWED_DOMAINS` | Comma-separated e-mail domains for which a **new** account may be created on the first login. Empty: no restriction. |
| `SSO_BOOTSTRAP_ADMIN_EMAILS` | E-mail addresses whose account becomes global admin on every login through a global provider (only with a verified e-mail, see below). |

`<ID>` is the provider ID in upper case with `-` replaced by `_`: for `google-work` the variables
are `OIDC_GOOGLE_WORK_ISSUER` and so on.

Example:

```bash
OIDC_PROVIDERS=gitlab,google
OIDC_GITLAB_ISSUER=https://gitlab.com
OIDC_GITLAB_CLIENT_ID=CLIENT-ID
OIDC_GITLAB_CLIENT_SECRET=CLIENT-SECRET
OIDC_GITLAB_NAME=GitLab
OIDC_GOOGLE_ISSUER=https://accounts.google.com
OIDC_GOOGLE_CLIENT_ID=CLIENT-ID
OIDC_GOOGLE_CLIENT_SECRET=CLIENT-SECRET
OIDC_GOOGLE_NAME=Google
```

**Redirect URI** to register at the provider:

```text
<BASE_URL>/api/auth/provider/<id>/callback
```

e.g. `https://status.example.org/api/auth/provider/gitlab/callback`.

An incompletely or wrongly configured provider (missing variable, invalid ID, duplicate ID, issuer
without `https://`) is disabled at start-up with a warning in the log; the server starts anyway. If no
sign-in method is active at all (no valid provider and password login off), the start-up warns
clearly: nobody can sign in then, only the `create-admin` command can still create accounts. The
discovery at the provider runs on the first login, not at start-up.

The addresses of global providers come from the operator. Unlike the page SSO they may therefore
live in the internal network (e.g. a self-hosted GitLab). `http://` only works during development via
`OIDC_DEV_INSECURE_ISSUERS`, see [local tests](local-testing.md).

## Accounts

The account is found through the issuer and the `sub` of the ID token. On the **first** login without
a link, the e-mail address from the `email` claim decides:

| Situation | Result |
| --- | --- |
| An account with this e-mail exists, `email_verified` is `true` | The account is linked and signed in (also an account with a password or a global admin). |
| Account exists, `email_verified` missing or `false` | Refused with `email_unverified`. The account can link the provider itself in the profile. |
| No account, `email_verified` is `true` | New account with the role "Viewer", no tags and no password, if the domain is in `SSO_ALLOWED_DOMAINS` (otherwise `domain_not_allowed`). |
| No account, `email_verified` missing or `false` | Refused with `email_unverified`. |
| No `email` claim | Refused with `email_missing`. |

Only a verified e-mail links or creates an account. Otherwise someone could use a provider without
verification to pre-create an account for another person's address, which that person's later,
verified sign-in (or `SSO_BOOTSTRAP_ADMIN_EMAILS`) would take over. With
`OIDC_<ID>_EMAIL_TRUSTED=true` a missing `email_verified` counts as verified for that provider; an
explicit `false` never does. If
the verified e-mail of the account is in `SSO_BOOTSTRAP_ADMIN_EMAILS`, it becomes global admin. Later
logins find the account through `sub`, even if the e-mail changes at the provider.

Signed-in users link further providers in the profile ("SSO sign-in",
`POST /api/auth/provider/<id>/link`); this also works for global admins, but not from a session that
was created through the page SSO. The session of a global provider applies to the whole account,
not just to one page.

An account accepts invitations to a status page after signing in through a global provider with the
invited e-mail address.

## Password login

By default the password login is **off**. Then these answer `403` with the code
`password_login_disabled`:

- `POST /api/login`,
- `POST /api/public/orders/set-password`,
- `POST /api/users` with a password and `PUT /api/users/:id` with a new password.

Accounts can be created without a password (`POST /api/users` without `password`); the self-service
order is active right after the e-mail is confirmed, and the customer signs in through a global
provider with the same address. The command `./server create-admin` remains the operator's emergency
access; the account it creates signs in with `PASSWORD_LOGIN_ENABLED=true` or through a global
provider with the same, verified e-mail.

## Returning after the login

All login starts (`/api/auth/provider/<id>/login`, `/api/auth/oidc/login`, `/api/auth/sso/login`)
accept `return_to`, the frontend path the login started from. The server checks it (one leading `/`,
no `//`, no control characters or whitespace, no backslash, not under `/api`, at most 2048 bytes) and
silently drops it otherwise. `POST /api/auth/sso-exchange` returns it as `return_to`. After linking,
the user goes on to the profile.

## Troubleshooting

Errors in the browser flow end up as a code on the login page (`?oidc_error=<code>`), when linking in
the profile:

| Code | Cause |
| --- | --- |
| `provider_unknown` | The ID is not (validly) in `OIDC_PROVIDERS`; check the warning in the start-up log. |
| `provider_unreachable` | Discovery fails (wrong issuer, not reachable, `issuer` in the discovery document differs). |
| `state_invalid` | Login older than 10 minutes, another browser or state already used. |
| `token_exchange` | Client ID/secret or redirect URI do not match at the provider. |
| `id_token_invalid` | Signature, issuer, audience, expiry or nonce do not match; check the clock. |
| `email_missing` | The provider sends no `email` claim (scope `email` missing?). |
| `email_unverified` | See the table "Accounts". |
| `domain_not_allowed` | The domain is not in `SSO_ALLOWED_DOMAINS`. |
| `already_linked` | The identity already belongs to another account. |

## Upgrading from older versions

- **Google and GitHub login are gone** (`GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
  `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, routes `/api/auth/google`, `/api/auth/github`). Set up
  Google as a global provider ([guide](google.md)); the redirect URI changes to
  `/api/auth/provider/google/callback`. Existing accounts are linked on the first login with a
  verified e-mail. GitHub offers no OIDC for signing users in; use another provider.
- **SAML is gone** (routes `/api/auth/saml/*`, SAML fields of the SSO configuration). Database
  columns and tables stay, existing SAML links and configurations have no effect. Move pages with
  SAML to the OIDC page SSO ([Okta](okta.md), [Azure AD](azure-ad.md)).
- **The password login is off by default.** Before the update, set up at least one global provider
  or set `PASSWORD_LOGIN_ENABLED=true`, otherwise nobody can sign in.
- Migration steps: 1. Register the provider at the IdP (redirect URI above). 2. Set `OIDC_PROVIDERS`
  and `OIDC_<ID>_*`, remove `GOOGLE_*`/`GITHUB_*`. 3. Keep `PASSWORD_LOGIN_ENABLED=true` for a while
  until all accounts have signed in through the provider (providers without `email_verified`: set
  `OIDC_<ID>_EMAIL_TRUSTED=true` if it only issues your company addresses, otherwise link in the
  profile). 4. Switch the password login off.

Back to the [overview](README.md).
