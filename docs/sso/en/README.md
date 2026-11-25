# SSO setup (administrators)

> English version of the German overview [`../README.md`](../README.md). The German
> files are the original; keep both versions in sync.

Statushafen signs users in through OpenID Connect (OIDC). There are two levels:

- **Global providers** (e.g. GitLab, Google, Keycloak) are set up by the operator through
  environment variables for the whole instance. They are the normal sign-in; the login page shows
  one button per provider. The password login is off by default (`PASSWORD_LOGIN_ENABLED`).
- **Page SSO**: each status page can additionally get its own OIDC provider, entered by the page
  admin. The session from it is valid for that page only.

These guides explain the setup without looking at the code.

## Status

| Method | Status | Guides |
| --- | --- | --- |
| Global OIDC providers | available | [Global providers](providers.md), [GitLab](gitlab.md), [Google](google.md), [Keycloak](keycloak.md), [Okta](okta.md), [Azure AD](azure-ad.md) |
| OIDC (per status page) | available | [OIDC general](oidc.md) and the same provider guides |
| Google/GitHub login (built in) | removed | Google via [OIDC](google.md) |
| SAML 2.0 | removed | – |

The page SSO creates no new accounts; an existing account is linked once. Global admins and accounts
with a role on another page are excluded there. A global provider creates new accounts (role
"Viewer") and links existing ones through a verified e-mail.

## Upgrade note

Google/GitHub login and SAML are gone, the password login is off by default. Before the update, set
up at least one global provider or set `PASSWORD_LOGIN_ENABLED=true`. The steps are under
[Upgrading from older versions](providers.md#upgrading-from-older-versions).

## Contents

- [Global OIDC providers](providers.md): environment variables, redirect URI, account rules,
  password login, returning after the login, upgrade.
- [OIDC general](oidc.md): page SSO with issuer, client ID/secret, redirect URI, scopes, test
  sign-in, linking an account.
- [GitLab](gitlab.md): gitlab.com and self-hosted GitLab.
- [Google](google.md): OIDC with Google as provider.
- [Keycloak](keycloak.md): OIDC with Keycloak as provider.
- [Okta](okta.md): OIDC with Okta as provider.
- [Azure AD](azure-ad.md): OIDC with Microsoft Entra ID as provider.
- [Local tests with a mock provider](local-testing.md): OIDC mock for development.
- [API](api.md): endpoints, parameters and error codes.
- [Architecture](architecture.md): flows as diagrams and threat model.

All fields, rules and error cases of the page SSO in detail are in the admin manual:
[OIDC sign-in per status page](../../user-guide/en/sso-oidc.md).

Back to the [admin guide](../../user-guide/admin.md) (German).
