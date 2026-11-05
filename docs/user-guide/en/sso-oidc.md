# Setting up OIDC sign-in per status page

> English version of the German guide [`../sso-oidc.md`](../sso-oidc.md). If the two
> differ, the code and the German original are authoritative; please fix both together.

Each status page can have its own OpenID Connect identity provider (IdP), for example Authentik,
Keycloak, Microsoft Entra ID or Google. Accounts linked to this provider sign in through it
without needing a password of their own (documentation). SAML
is described in [`sso-saml.md`](./sso-saml.md).

This guide describes the state of the backend (`backend/handlers/oidc_login.go`,
`backend/handlers/oidc_frontend.go`, `backend/handlers/statuspage_sso.go`).

## Overview: how sign-in works

1. The user clicks "Login" at the top of the status page and then "Sign in with SSO" on the login
   page (on a private page the button also appears directly in the "Private status page" notice).
   The button only appears when the page SSO is active; the frontend asks
   `GET /api/public/pages/<slug>/sso`, which returns only `oidc_enabled` and the page ID, no
   configuration. The browser then calls `GET /api/auth/oidc/login?page=<page ID>`.
2. The backend fetches the provider's discovery document, creates state, nonce and PKCE verifier
   (valid for 10 minutes) and redirects to the provider.
3. After sign-in the provider redirects back to
   `<BASE_URL>/api/auth/oidc/callback?page=<page ID>`.
4. The backend exchanges the code (with PKCE) for tokens and validates the ID token: signature via
   the provider's JWKS, `iss`, `aud` (= client ID), expiry and nonce.
5. The account is found exclusively through the triple *(status page, issuer, `sub`)*, never
   through the email address. No new accounts are created in this step.
6. As with the Google/GitHub login, the backend redirects with a one-time code to
   `<FRONTEND_URL>/auth/sso-callback`; the frontend exchanges it for the session.

If a step fails, the backend redirects to `<FRONTEND_URL>/login?oidc_error=<code>` (after linking
to the profile, after a test sign-in to the SSO page). The page shows a translated message; the
URL contains only the fixed code, never a message from the provider. Codes and causes are listed
under [Error cases](#error-cases).

Every sign-in, every link and every removal of a link is written to the audit log
(`auth.sso_login`, `auth.sso_linked` and `auth.sso_unlinked`, details `provider=oidc page=<ID>`).

## Prerequisites

- **Permission:** The SSO configuration of a page can be written by whoever has the permission
  `sso:update` on that page, or by a global admin (`GET/PUT/DELETE
  /api/admin/pages/<page ID>/sso`). In the admin area the menu item "SSO Configuration" (group
  "System") appears accordingly for everyone with `sso:update` on the status page selected at the
  top.
- **`BASE_URL`** must point to the publicly reachable address of the backend (for example
  `https://status.example.com`). The redirect URL is derived from it. Without the variable,
  `http://localhost:8080` applies.
- **`FRONTEND_URL`** must point to the frontend (default `http://localhost:5173`); this is where
  the user lands after a successful sign-in.
- **Provider over https** with a publicly reachable address (see [Error cases](#error-cases)).
- **Page ID:** the numeric ID of the status page (found in the admin URL, among other places, or
  returned by `GET /api/admin/pages`).

## Redirect/callback URL

Register exactly this URL at the provider as an allowed redirect URI:

```
<BASE_URL>/api/auth/oidc/callback?page=<page ID>
```

Example: `https://status.example.com/api/auth/oidc/callback?page=3`

The backend shows the value as `oidc_acs_url` (`GET /api/admin/pages/<ID>/sso`, even before a
configuration has been saved; in the admin interface as the field "ACS URL"). It is computed from
the current `BASE_URL` on every request, exactly as at login. After changing `BASE_URL`, adjust
the redirect URI at the provider; there is no need to save again. The query parameter `?page=…`
is part of the URL; most providers compare redirect URIs character by character.

## Configuration fields

The admin page "SSO Configuration" edits the configuration of the status page selected at the top
of the admin area. Its fields correspond to the JSON fields of
`PUT /api/admin/pages/<page ID>/sso`:

| Field in the interface | JSON field | Meaning |
|------------------------|-----------|---------|
| Enable OIDC sign-in | `oidc_enabled` | Turns the page SSO on. If it is off, login and callback answer with 404 "SSO für diese Seite nicht eingerichtet" (SSO is not set up for this page). |
| Discovery URL | `oidc_discovery_url` | Issuer of the provider **or** the full discovery URL (`…/.well-known/openid-configuration`). The backend cuts off the `.well-known` part and uses the rest as the issuer. A trailing slash is kept. |
| Client ID | `oidc_client_id` | Client ID of the application created at the provider. Must appear in the `aud` of the ID token. |
| Client Secret | `oidc_client_secret` | Client secret. Never returned; GET only reports `oidc_client_secret_set` (`true` if one is stored). If one is stored, the interface shows "Stored – leave empty to keep it": an empty field (or an empty or missing value in the PUT) keeps the stored secret, a new value replaces it. |
| Scopes | `oidc_scopes` | Separated by spaces; if saved empty, the default `openid email profile` applies. The backend adds `openid` itself if it is missing. |
| Claim for the name | `oidc_attribute_name` | Claim from which the display name of the session is taken (empty = default `name`). |
| Claim for the email address | `oidc_attribute_email` | Claim whose value is stored with the link and shown in the profile ("linked as …", updated at every sign-in; empty = default `email`). Accounts are **never** found through the email address. |
| – | `oidc_attribute_role` | **Not supported** and not in the interface. A role from a claim would come from the IdP, which the page admin controls, and bypass role management; that would be a privilege escalation. A value saved via the API has no effect, and the interface clears it on save. |
| Allow test login | `test_login_enabled` | Allows the [test sign-in](#test-sign-in) from the SSO page, even before `oidc_enabled` is switched on (default for a new configuration: on). Turn it off in production. |
| ACS URL | `oidc_acs_url` | Read-only: the currently valid redirect URL (see above). |

With `oidc_enabled = true` the backend requires a discovery URL and a client ID in the request as
well as a client secret, either in the request or already stored; otherwise it answers 400 "OIDC
enabled erfordert Discovery URL, Client ID und Secret" (OIDC enabled requires discovery URL,
client ID and secret). The interface then marks these fields as required; without the permission
it shows "Keine Berechtigung für diesen Bereich" (no permission for this area).

`DELETE /api/admin/pages/<ID>/sso` (button "Delete SSO configuration") removes the configuration
including the secret; saving again later recreates it and then needs a secret again.

## Test sign-in

With "Allow test login" (`test_login_enabled`) the button "Start test sign-in" appears on the SSO
page (`POST /api/admin/pages/<page ID>/sso/test`, same permission as the configuration, 20 calls
per minute). It checks the **saved** configuration (so save first) and works even before
`oidc_enabled` is switched on:

1. Save discovery URL, client ID and secret, and tick "Allow test login".
2. Click "Start test sign-in" and sign in at the provider with any account.
3. Back on the SSO page, it reports "Test sign-in successful" (discovery, client credentials,
   redirect URL, signature, `aud` and nonce match) and names claims for name or email that were
   missing from the ID token. On failure it shows the message for the code (see
   [Error cases](#error-cases)).

The test sign-in creates neither a session nor a link and writes no audit entry for a sign-in. If
discovery URL, client ID or secret are missing, the start answers with 400; if "Allow test login"
is off, with 403.

Afterwards, check the real login with a linked account in a private browser window via the
status page ("Login" → "Sign in with SSO"). Details on errors are in the backend log
(`OIDC discovery failed`, `OIDC token exchange failed`, `OIDC ID token rejected`).

## Account linking: who may use the page SSO

The page SSO creates **no** new accounts. An account must already exist and is linked once:

1. The user signs in the usual way (password, Google, GitHub).
2. In the profile (`/admin/profile`), the section "SSO sign-in" shows the pages they may link, and
   they choose "Link with SSO" there. Behind it is `POST /api/auth/oidc/link?page=<page ID>` (login
   token in the header `X-Auth-Token`); the response contains `{"url": "…"}`, the
   provider's authorize URL.
3. They sign in at the provider. The callback stores the link (page, issuer, `sub`, plus the email
   claim for display), signs them in and leads back to the profile.

After that, "Login" → "Sign in with SSO" on the status page is enough.

The profile lists existing links ("linked as <email claim>") and removes them with "Unlink"
(`GET /api/me/sso`, `DELETE /api/me/sso/<ID>`, own links only). As linkable it only shows pages
with active OIDC that the rules below allow: without a role on any page, only public, listed
pages (so the list does not reveal private page names); with a role on exactly one page, only that
page; with roles on several pages, and for global admins, none. A private page without a role of
one's own can still be linked via the API.

Excluded, both when linking and at every sign-in (403), are:

- **Global admins**: "Global Admins melden sich nicht über das Seiten-SSO an" (global admins do
  not sign in via the page SSO). The IdP of a page is controlled by its admin; a link would
  otherwise give that admin a path to global permissions.
- **Accounts with a role on another status page**: "Konten mit Rolle auf einer anderen Seite
  nutzen dieses Seiten-SSO nicht" (accounts with a role on another page do not use this page SSO).
  Otherwise the admin of the foreign IdP could gain permissions on other pages. A role on *this*
  page is allowed.

### The session is valid for this page only

The session after a page SSO login (OIDC and SAML) is restricted to the page whose IdP confirmed
the sign-in (claim `sso_page_id` in the JWT). This also applies to permissions the
account only receives after signing in: if it gets a role on another page or becomes a global
admin during the session, this has no effect in that session.

- Admin routes of other pages and all global admin routes (users, roles, tags, creating pages,
  audit log …) answer with 403. Private other pages stay hidden.
- The page selector in the admin area shows only this page.
- The user's own profile (`/api/me…`, data export, delete account, SSO links, sign out) works as
  usual.

Anyone who needs more signs in with a password.

Further rules:

- A provider identity belongs to exactly one account. If it is already linked to another account,
  the callback answers with 409 "Diese Anmeldung ist bereits mit einem anderen Konto verknüpft"
  (this sign-in is already linked to another account).
- A sign-in without a link ends with 403 "Diese Anmeldung ist mit keinem Konto verknüpft" (this
  sign-in is not linked to any account).
- The link is bound to the issuer. If the issuer changes (different realm, different tenant,
  trailing slash), saving the configuration deletes the OIDC links of the page (audit entry
  `sso.oidc_identities_reset`, result of a security review); the SSO page warns beforehand. The
  accounts must be linked again.

## Error cases

Login and callback show errors on the login page (or in the profile or on the SSO page) as a
translated message for the code in `oidc_error` or `oidc_test`. The JSON endpoints (linking, starting
the test sign-in) still answer with a status and `{"error": "…"}`. The JSON messages are
returned in German by the backend; they are quoted here as they appear, with an English
translation in parentheses.

| Code | Message / status (JSON) | Cause and remedy |
|------|-------------------------|------------------|
| `provider_denied` | – | The provider redirected back without a code (`error=…`), for example because the user cancelled the sign-in or the provider refused it. |
| `not_linked` | 403 "Diese Anmeldung ist mit keinem Konto verknüpft" (this sign-in is not linked to any account) | See [Account linking](#account-linking-who-may-use-the-page-sso). |
| `already_linked` | 409 "Diese Anmeldung ist bereits mit einem anderen Konto verknüpft" (this sign-in is already linked to another account) | See [Account linking](#account-linking-who-may-use-the-page-sso). |
| `global_admin` / `other_page_role` | 403 | See [Account linking](#account-linking-who-may-use-the-page-sso). |
| `test_disabled` / `test_incomplete` | 403 / 400 | See [Test sign-in](#test-sign-in). |
| `bad_page` | 400 "Ungültige Seite" (invalid page) | The page ID in the login link is not a number. |
| `internal` | 500 | The login state or link could not be stored (database). |

The remaining codes:

| Message / status | Cause and remedy |
|------------------|------------------|
| `provider_unreachable`: 502 "OIDC-Anbieter nicht erreichbar" (OIDC provider unreachable), log `OIDC-Issuer muss https verwenden` (OIDC issuer must use https) | The issuer does not start with `https://`. https is mandatory in production. The only exception is local development, see [Local testing with the mock](#local-testing-with-the-mock). |
| `provider_unreachable`, log `interne Adresse nicht erlaubt` (internal address not allowed) | SSRF protection: discovery, JWKS and token endpoint must not point to loopback, private (10/8, 172.16/12, 192.168/16, fc00::/7), link-local or unspecified addresses, not even after redirects or DNS rebinding. An IdP in the internal network must be reachable via a public address. |
| `provider_unreachable`, log `OIDC-Endpunkt muss eine https-Adresse sein` (OIDC endpoint must be an https address) | The discovery document names an address without https for `authorization_endpoint` or `token_endpoint` (for example `javascript:`, `data:` or `http://`). Only https is allowed; `http://` is accepted solely for the development issuer, see [Local testing with the mock](#local-testing-with-the-mock). |
| `provider_unreachable`, log `issuer URL provided to client (…) did not match the issuer URL returned by provider (…)` | Issuer mismatch: the issuer from `oidc_discovery_url` does not match the `issuer` in the discovery document character by character. The most common cause is a missing or extra trailing slash. Copy the value from the `issuer` field of the discovery document. |
| `provider_unreachable` (otherwise) | Provider unreachable or discovery URL wrong. A failed attempt is cached for 1 minute, a successful one for 1 hour, so wait briefly after a correction if necessary. |
| `token_exchange` (502 "Token-Austausch fehlgeschlagen", token exchange failed) | Client ID/secret wrong, redirect URI not registered exactly at the provider, or the code has expired. |
| `no_id_token` (502 "Anbieter lieferte kein ID-Token", provider returned no ID token) | The scope `openid` is missing at the provider, or the application is not an OIDC client. |
| `id_token_invalid` (403 "ID-Token ungültig", ID token invalid) | Signature, `aud` (client ID), expiry or nonce do not match. Check the server clock. |
| `state_invalid` (400 "Ungültiger oder abgelaufener Login-Versuch", invalid or expired login attempt) | Login older than 10 minutes, callback called twice, or the state cookie is missing (login started in a different browser; the cookie is `Secure` and is only set over https except on `localhost`). |
| `not_configured` (404 "SSO für diese Seite nicht eingerichtet", SSO is not set up for this page) | `oidc_enabled` is off or the page ID is wrong. |
| 429 | Rate limit: login, callback and test sign-in 20 calls per minute each, the query `GET /api/public/pages/<slug>/sso` 60 per minute. For login and callback the browser still shows the rate limiter's JSON response here. |

## Provider examples

In all cases: redirect URI `<BASE_URL>/api/auth/oidc/callback?page=<page ID>`, client type
"confidential" (with secret), authorization code flow. The backend always sends PKCE (S256).

### Authentik

- Create an application with an **OAuth2/OpenID provider**, client type *Confidential*, redirect
  URI as above.
- Issuer: `https://<authentik-host>/application/o/<application-slug>/` **with** the trailing
  slash, exactly as it appears in the discovery document.
- Take client ID and secret from the provider.

### Keycloak

- In the realm, create a client of type *OpenID Connect*, turn on *Client authentication*, keep
  *Standard flow* enabled, and enter the redirect URI under *Valid redirect URIs*.
- Issuer: `https://<keycloak-host>/realms/<realm>` (older versions with the `/auth` prefix:
  `https://<host>/auth/realms/<realm>`).
- The secret is under *Credentials* of the client.

### Microsoft Entra ID

- Create an *app registration*, platform **Web**, redirect URI as above.
- Under *Certificates & secrets* create a client secret and take the **value** (not the secret
  ID); the client ID is the *Application (client) ID*.
- Tenant-specific issuer: `https://login.microsoftonline.com/<tenant ID>/v2.0`. The multi-tenant
  endpoints (`common`, `organizations`) do not work because their discovery document reports a
  placeholder issuer (issuer mismatch). Multi-tenant sign-in is therefore not supported
 ; it would need a dedicated issuer check.

### Google

- In the Google Cloud Console create an *OAuth client ID* of type **Web application** and enter
  the redirect URI under *Authorized redirect URIs*.
- Issuer: `https://accounts.google.com`.
- This page SSO is independent of the system's global Google login; links apply only to the
  respective status page.

## Local testing with the mock

For development there is a mock provider in the Compose profile `sso`. The steps are in the
README, section ["Lokale SSO-Tests mit Mock-OIDC"](../../../README.md#lokale-sso-tests-mit-mock-oidc)
(local SSO tests with mock OIDC; the README is in German).

The key is the variable `OIDC_DEV_INSECURE_ISSUERS`: a comma-separated list of issuers for which
the https requirement and SSRF protection are lifted (for example
`http://localhost:8090/statushafen`). It only applies on an exact match (no prefix) and only with
`APP_ENV` set to `development`, `dev`, `test` or `local`. It must not be set on a server.
