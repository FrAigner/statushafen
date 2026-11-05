# Setting up SAML sign-in per status page

> English version of the German guide [`../sso-saml.md`](../sso-saml.md). If the two
> differ, the code and the German original are authoritative; please fix both together.

Besides OIDC ([`sso-oidc.md`](./sso-oidc.md)), each status page can have its own SAML 2.0 identity
provider (IdP), for example Microsoft Entra ID, Keycloak, Authentik, ADFS or Okta.
Both can be switched on in parallel on the same page. The same rules apply as for OIDC: no
creation of new accounts, linking by the signed-in user themselves, no global admins, no accounts
with a role on another page, no role mapping from IdP attributes. There is no Single Logout (SLO).
The session is valid for this page only and never with global admin permissions (see
[`sso-oidc.md`](./sso-oidc.md#the-session-is-valid-for-this-page-only)).

State of the backend: `backend/handlers/saml_login.go`, `saml_metadata.go`, `saml_keys.go`,
`statuspage_sso.go`. Library: [`github.com/crewjam/saml`](https://github.com/crewjam/saml)
(pure Go, XML signatures via `russellhaering/goxmldsig`, no `xmlsec1` in the image).

## Overview: how sign-in works

1. On the login page (or in the notice of a private page), "Sign in with SSO (SAML)" appears as
   soon as SAML is active for the page (`GET /api/public/pages/<slug>/sso` reports
   `saml_enabled`). The browser calls `GET /api/auth/saml/login?page=<page ID>`. For a **private page**
   the information names the page ID and the SSO kind only to authorized callers; visitors see "Sign in
   with SSO" in the notice and the browser calls `GET /api/auth/sso/login?slug=<slug>`, after which
   the server picks the method itself (OIDC if active, otherwise SAML).
2. The backend loads the IdP metadata, creates an AuthnRequest signed with the page's SP key
   (HTTP-Redirect binding, RSA-SHA256), remembers its ID for 10 minutes (`SAMLLoginState`) and
   redirects to the IdP. A random state is sent along as `RelayState` and is additionally stored
   in the cookie `saml_state` (`Secure`, `HttpOnly`, `SameSite=None`, because the IdP returns via
   a cross-site POST).
3. The IdP sends the response by HTTP-POST to `<BASE_URL>/api/auth/saml/<page ID>/acs`.
4. The backend redeems the state exactly once and validates the response:
   - A signature on the response **or** the assertion is mandatory, checked against the
     certificates from the IdP metadata. The response must contain exactly one assertion; the ACS
     rejects multiple assertions (protection against signature wrapping) and encrypted
     assertions (`EncryptedAssertion`). The SP metadata therefore offers no encryption
     certificate; the connection to the ACS is protected by https anyway.
   - Issuer = entity ID of the IdP, `Destination` and `Recipient` = ACS URL, `Audience` = entity
     ID of the SP (an `AudienceRestriction` is mandatory), `InResponseTo` = ID of the own
     AuthnRequest (IdP-initiated sign-in is disabled), `NotBefore`/`NotOnOrAfter` and issue time
     (180 s clock tolerance, at most 90 s old).
   - At least one `SubjectConfirmation` and a non-empty NameID in the **persistent** format (see
     [NameID format](#nameid-format)).
   - The assertion ID is stored until the assertion expires (`SAMLAssertionUse`); a second use
     is rejected (replay protection).
5. The account is found only through *(status page, protocol SAML, entity ID of the IdP, NameID
   including format)*, never through the email address.
6. As with OIDC, the one-time code follows to `<FRONTEND_URL>/auth/sso-callback`, this time via
   `303 See Other` so that the browser loads the frontend with a GET.

Errors redirect with a fixed code to `<FRONTEND_URL>/login?oidc_error=<code>` (after linking to
the profile, after the test sign-in to the SSO page). For historical reasons the parameters are
named `oidc_*` and carry the shared codes of both protocols. Audit entries as for OIDC with
`provider=saml page=<ID>`.

## Values for the IdP

The SSO page in the admin area shows in the section "SAML 2.0" what to enter at the IdP (all
values depend on `BASE_URL`):

| Value | Example |
|------|----------|
| SP entity ID (= metadata URL) | `https://status.example.com/api/auth/saml/3/metadata` |
| ACS URL (reply URL), binding HTTP-POST | `https://status.example.com/api/auth/saml/3/acs` |
| SP metadata | `GET <BASE_URL>/api/auth/saml/<page ID>/metadata` (public, also for private pages so that the IdP can load it; 404 for deleted pages) |
| SP certificate | in the form and in the metadata |

The backend generates the key and a self-signed certificate (RSA 2048, valid for 10 years) on the
**first save** of the page's SSO configuration, so even before SAML is switched on; until then the
metadata endpoint returns 404. The pair is kept on every further save. After "Delete SSO
configuration", a new pair is created on the next save; then re-import the SP metadata at the IdP.
Like the OIDC client secret, the private key exists only in the database and appears in no API
response.

The SP metadata offers only the HTTP-POST binding for the ACS (no artifact binding), requires
signed assertions (`WantAssertionsSigned`) and announces signed AuthnRequests. The NameID format
is `persistent` (see below).

## NameID format

By default the AuthnRequest asks for a persistent NameID
(`urn:oasis:names:tc:SAML:2.0:nameid-format:persistent`), and the ACS accepts only that. The
following are rejected with `saml_nameid_format`:

- `emailAddress`: always. An email address is reissued by the IdP (renaming, successor on the
  mailbox); the new person would then sign in with the linked account.
- `transient`: changes with every sign-in, a link would hold exactly once.
- any other format (`kerberos`, `X509SubjectName` …).
- `unspecified` (also a missing format attribute), as long as the switch **"Allow NameID format
  'unspecified'"** (`saml_allow_unspecified_nameid`, default off) is not set. With the switch on,
  the AuthnRequest no longer asks for a format, so that IdPs without a persistent NameID also
  answer. Only turn it on if the identifier is never reassigned at the IdP (the interface warns
  about this).

The format belongs to the link: the same NameID in a different format does not match an existing
link.

## Changing the IdP

If, on saving, the source of the IdP metadata changes (different URL, switch between URL and XML,
metadata removed) or, with pasted XML, the entity ID or a signing certificate of the IdP, the
backend deletes all SAML links of the page and records this in the audit log
(`sso.saml_identities_reset`, details `count=<number>`). The users then link again in their
profile. The SSO page warns before saving as soon as the metadata URL or XML changes. A key
rollover that arrives through an unchanged metadata URL deletes nothing.

## Configuration fields

| Field in the interface | JSON field | Meaning |
|------------------------|-----------|---------|
| Enable SAML sign-in | `saml_enabled` | Turns on SAML login. Requires IdP metadata (URL or XML), otherwise 400. |
| IdP metadata URL | `saml_idp_metadata_url` | `https://` only. Fetched with SSRF protection as for OIDC discovery (no internal addresses, not even after redirects), at most 1 MB, result cached for 1 hour, a failure for 1 minute. Key rollovers at the IdP arrive automatically this way. |
| IdP metadata (XML) | `saml_idp_metadata_xml` | Alternatively paste the metadata XML (at most 128 KB); it is validated on saving (400 for invalid content) and cached parsed per page. Paste it again after a key rollover. URL and XML together result in 400. |
| Attribute for the email address | `saml_attribute_email` | Name **or** FriendlyName of the attribute (empty = `email`). For display with the link only. |
| Attribute for the name | `saml_attribute_name` | Display name of the session (empty = `name`). |
| Allow NameID format "unspecified" | `saml_allow_unspecified_nameid` | Default off, see [NameID format](#nameid-format). |
| SP entity ID, SP ACS URL, SP certificate | `saml_entity_id`, `saml_acs_url`, `saml_metadata_url`, `saml_sp_certificate` | Read-only, see above. |

The metadata must contain an entity ID, a `SingleSignOnService` with HTTP-Redirect binding and at
least one signing certificate. For an `EntitiesDescriptor`, the first entry with an IdP role
counts. `validUntil` of the IdP metadata is not evaluated.

## Test sign-in

With "Allow test login", "Start SAML test sign-in" appears
(`POST /api/admin/pages/<page ID>/sso/saml-test`). It uses the saved configuration, works even
before SAML is switched on, and runs through all checks of the ACS, without a session and without
a link. The SSO page then reports success and names missing attributes for email or name. If the
metadata is missing, the start answers with 400 (`saml_test_incomplete`).

## Account linking

As for OIDC (see
[`sso-oidc.md`](./sso-oidc.md#account-linking-who-may-use-the-page-sso)): in the profile, "SSO
sign-in" shows the linkable pages per protocol ("OIDC" or "SAML"); "Link with SSO" calls
`POST /api/auth/saml/link?page=<page ID>`. An OIDC link on the same page does not count for SAML
and vice versa. The link depends on entity ID and NameID: if the IdP changes either of them, the
accounts must be linked again.

## Error cases

| Code | Cause and remedy |
|------|------------------|
| `saml_response_invalid` | Signature missing or not matching the certificate from the metadata, wrong audience/recipient/destination, expired (check the server clock), foreign `InResponseTo`, missing NameID or SubjectConfirmation, artifact binding instead of POST. Details in the backend log (`SAML response rejected`). |
| `saml_replay` | The assertion ID has already been redeemed once. |
| `saml_nameid_format` | The NameID is not persistent (see [NameID format](#nameid-format)); set the format `persistent` at the IdP. |
| `metadata_invalid` | IdP metadata unreadable or without redirect SSO or signing certificate (log `SAML metadata invalid`). |
| `provider_unreachable` | Metadata URL not reachable over https, internal address, or HTTP error (log `SAML metadata fetch failed`). |
| `provider_denied` | The IdP reports a status other than Success (for example sign-in cancelled). |
| `state_invalid` | Login older than 10 minutes, ACS called twice, cookie missing (different browser or a proxy that strips `SameSite=None`), or the page ID in the ACS path does not match. |
| `not_linked`, `already_linked`, `global_admin`, `other_page_role`, `not_configured`, `test_disabled`, `internal` | As for OIDC. |

Rate limits: login, ACS and test sign-in 20 calls per minute each, metadata 60 per minute.

## IdP examples

### Microsoft Entra ID

*Enterprise application* → *Single sign-on* → *SAML*: identifier (entity ID) and reply URL (ACS)
as above. Use the *App Federation Metadata URL* as the metadata URL. By default the email is sent
as `http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress` and the name as
`http://schemas.microsoft.com/identity/claims/displayname`; enter these names in the attribute
fields. By default Entra sends the email as NameID; under *Attributes & Claims* → *Unique User
Identifier* choose the format **Persistent** (for example `user.objectid`).

### Keycloak

Client of type *SAML*, client ID = SP entity ID, *Valid redirect URIs* and *Assertion Consumer
Service POST Binding URL* = ACS URL. Set *Name ID format* to `persistent`. Metadata:
`https://<host>/realms/<realm>/protocol/saml/descriptor`.

### Authentik

Create a *SAML provider*, ACS URL as above, *Issuer* freely selectable, *Service Provider Binding*
Post. The SP metadata can be imported directly. Use the provider's metadata URL.

## Local testing with the mock

README, section ["Lokale SSO-Tests mit Mock-SAML"](../../../README.md#lokale-sso-tests-mit-mock-saml)
(local SSO tests with mock SAML; the README is in German).
