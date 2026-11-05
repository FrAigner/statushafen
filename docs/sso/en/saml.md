# Setting up SAML (general)

> English version of the German guide [`../saml.md`](../saml.md).

With SAML 2.0, users sign in through the identity provider (IdP) of your organization, for
example Microsoft Entra ID, Okta, Keycloak, Authentik or ADFS. The configuration
applies per status page and can be switched on in parallel to [OIDC](oidc.md). The flow is
described in the [architecture](architecture.md), the endpoints in the [API](api.md), all checks
and error cases in the [admin manual](../../user-guide/en/sso-saml.md).

## Prerequisites

- The IdP offers a `SingleSignOnService` with **HTTP-Redirect binding** and sends the response by
  **HTTP-POST** to the ACS.
- The IdP signs the response or the assertion. Encrypted assertions are not supported.
- The IdP delivers a NameID in the **persistent** format (see [NameID format](#nameid-format)).
- `BASE_URL` points to the public address of the server; all SP values depend on it.
- You have the permission `sso:update` on this status page (or are a global admin).

## Steps

1. **Save the SSO configuration once.** Admin area → group "System" → "SSO Configuration". On the
   first save, Statushafen generates the page's SP key pair; only then do SP metadata exist.
2. **Enter the SP values at the IdP.** The SSO page shows them in the section "SAML 2.0":

   | Value | Example |
   | --- | --- |
   | SP entity ID (= metadata URL) | `https://status.example.org/api/auth/saml/<PAGE-ID>/metadata` |
   | ACS URL (reply URL), binding HTTP-POST | `https://status.example.org/api/auth/saml/<PAGE-ID>/acs` |
   | SP certificate | in the form and in the SP metadata |

   If the IdP can import SP metadata, the metadata URL is enough.
3. **Store the IdP metadata in Statushafen:** either the IdP's metadata URL (`https://` only,
   reloaded hourly) or paste the metadata XML (at most 128 KB, paste it again after a key rollover
   at the IdP). Both at the same time is not possible.
4. **Map attributes:** name or FriendlyName of the attributes for email (empty = `email`, for
   display with the link only) and display name (empty = `name`).
5. **Test:** With "Allow test login", "Start SAML test sign-in" starts a trial run with the saved
   configuration, even before SAML is switched on. The SSO page then reports success and names
   missing attributes.
6. **Switch SAML on** and save. On the status page, "Sign in with SSO (SAML)" appears.

## NameID format

The account is bound to *(status page, entity ID of the IdP, NameID including format)*, never to
the email address. Statushafen therefore requires a permanent NameID in the format
`urn:oasis:names:tc:SAML:2.0:nameid-format:persistent`. An email address as NameID can change or
be reissued. If the IdP delivers only "unspecified", this can be allowed explicitly in the
configuration; the NameID must then still be stable.

## Accounts and linking

As for [OIDC](oidc.md#accounts-and-linking): no new accounts are created. The signed-in user links
their account in the profile ("Link with SSO", protocol "SAML"). An OIDC link on the same page
does not count for SAML and vice versa. If the IdP changes its entity ID or the stored metadata
changes, saving deletes the SAML links of the page.

After "Delete SSO configuration", a new SP key pair is created on the next save; then re-import
the SP metadata at the IdP.

## Troubleshooting

Errors end up as a code in the URL of the login page (`?oidc_error=<code>`; the name is historical
and applies to both protocols). The most common ones:

| Code | Cause |
| --- | --- |
| `saml_response_invalid` | Signature missing or not matching, wrong audience/recipient/destination, expired (check the server clock), or IdP-initiated sign-in. |
| `saml_nameid_format` | NameID not persistent, see [NameID format](#nameid-format). |
| `metadata_invalid` | IdP metadata unreadable or without redirect SSO or signing certificate. |
| `provider_unreachable` | Metadata URL not reachable over https, or internal address. |
| `state_invalid` | Login older than 10 minutes or the cookie is missing (different browser, proxy strips `SameSite=None`). |

All codes: [error cases in the admin manual](../../user-guide/en/sso-saml.md#error-cases).

## Providers

- [Okta](okta.md)
- [Azure AD](azure-ad.md)

Back to the [overview](README.md).
