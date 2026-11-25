# SSO setup (administrators)

> English version of the German overview [`../README.md`](../README.md). The German
> files are the original; keep both versions in sync.

Each status page can get its own single sign-on (SSO) via OpenID Connect (OIDC). SAML is no longer
supported. These guides explain how to set it up without looking at the code.

## Status

| Method | Status | Guides |
| --- | --- | --- |
| OIDC (per status page) | available | [OIDC general](oidc.md), [Google](google.md), [Keycloak](keycloak.md), [Okta](okta.md), [Azure AD](azure-ad.md) |
| SAML 2.0 | removed | – |

The page SSO creates no new accounts; an existing account is linked once. Global admins
and accounts with a role on another page are excluded, and the session is valid for this page only
().

## Contents

- [OIDC general](oidc.md): issuer, client ID/secret, redirect URI, scopes, test sign-in, linking an
  account.
- [Google](google.md): OIDC with Google as provider.
- [Keycloak](keycloak.md): OIDC with Keycloak as provider.
- [Okta](okta.md): OIDC with Okta as provider.
- [Azure AD](azure-ad.md): OIDC with Microsoft Entra ID as provider.
- [Local tests with a mock provider](local-testing.md): OIDC mock for development.
- [API](api.md): endpoints, parameters and error codes.
- [Architecture](architecture.md): flow of login and linking as diagrams.

All fields, rules and error cases in detail are in the admin manual:
[OIDC sign-in per status page](../../user-guide/en/sso-oidc.md).

Back to the [admin guide](../../user-guide/admin.md) (German).
