# SSO API

> English version of the German document [`../api.md`](../api.md). The error
> messages in `{"error": "<text>"}` are returned by the backend in German; they are quoted here
> unchanged, with an English translation in parentheses where it helps.

All paths begin with `/api`. JSON endpoints report errors as `{"error": "<text>"}`. Endpoints that
the browser calls directly (login, callback, ACS) redirect to the frontend with a fixed code
instead when an error occurs (see [Error redirects](#error-redirects)). The flow is shown in the
[architecture](architecture.md), the setup in the [OIDC guide](oidc.md) and [SAML guide](saml.md).

**Authentication at the API:** Endpoints that require a login expect the JWT in the header
`X-Auth-Token: <token>`. `Authorization: Bearer <token>` is still accepted; if
`X-Auth-Token` is set, it takes precedence.

## Page configuration

These endpoints need a JWT and the permissions `sso:read`, `sso:update` or `sso:delete` on the
status page (or global admin).

### `GET /api/admin/pages/:page_id/sso`

Reads the SSO configuration. Without a stored configuration, default values are returned
(`oidc_enabled` and `saml_enabled` `false`, `test_login_enabled` `true`). The client secret and the
private SP key are never returned.

Response `200` (abridged):

```json
{
  "status_page_id": 1,
  "oidc_enabled": true,
  "oidc_discovery_url": "https://idp.example.org/realms/demo",
  "oidc_client_id": "CLIENT-ID",
  "oidc_client_secret_set": true,
  "oidc_scopes": "openid email profile",
  "oidc_attribute_email": "email",
  "oidc_attribute_name": "name",
  "oidc_acs_url": "https://status.example.org/api/auth/oidc/callback?page=1",
  "test_login_enabled": true,
  "saml_enabled": false,
  "saml_idp_metadata_url": "",
  "saml_idp_metadata_xml": "",
  "saml_attribute_email": "email",
  "saml_attribute_name": "name",
  "saml_allow_unspecified_nameid": false,
  "saml_entity_id": "https://status.example.org/api/auth/saml/1/metadata",
  "saml_metadata_url": "https://status.example.org/api/auth/saml/1/metadata",
  "saml_acs_url": "https://status.example.org/api/auth/saml/1/acs",
  "saml_sp_certificate": "-----BEGIN CERTIFICATE-----…"
}
```

`oidc_acs_url`, `saml_entity_id`, `saml_metadata_url` and `saml_acs_url` are built from `BASE_URL`
on every request. Errors: `400` (`Invalid page_id`), `404` (`Statusseite nicht gefunden`, status
page not found).

### `PUT /api/admin/pages/:page_id/sso`

Saves the configuration (upsert, exactly one row per page; possible again after `DELETE`). Body
(JSON, at most 256 KB):

| Field | Type | Note |
| --- | --- | --- |
| `oidc_enabled` | bool | With `true`, discovery URL, client ID and a client secret are mandatory. |
| `oidc_discovery_url` | string | Issuer or full `.well-known/openid-configuration` address. |
| `oidc_client_id` | string | |
| `oidc_client_secret` | string | Empty or omitted keeps the stored secret; a new value replaces it. |
| `oidc_scopes` | string | Separated by spaces (empty = `openid email profile`), `openid` is added. |
| `oidc_attribute_email` | string | Claim for the email with the link, for display only (empty = `email`). |
| `oidc_attribute_name` | string | Claim for the display name (empty = `name`). |
| `oidc_attribute_role` | string | Is stored but has no effect (no role mapping from the IdP). |
| `test_login_enabled` | bool | Allows the test sign-in (OIDC and SAML). |
| `saml_enabled` | bool | With `true`, IdP metadata is mandatory. |
| `saml_idp_metadata_url` | string | `https://` only. Not together with `saml_idp_metadata_xml`. |
| `saml_idp_metadata_xml` | string | Pasted metadata XML, at most 128 KB, validated on saving. |
| `saml_attribute_email` | string | Attribute for the email with the link (empty = `email`). |
| `saml_attribute_name` | string | Attribute for the display name (empty = `name`). |
| `saml_allow_unspecified_nameid` | bool | Allows NameIDs in the format "unspecified" (default off). |

On the first save, the server generates the page's SP key pair. If the OIDC issuer or the SAML
trust anchor (metadata) changes, saving deletes the links of that protocol on this page.

Response `200`: the saved configuration as with `GET`. Errors: `400` (`Invalid page_id`,
`Invalid request`, `OIDC enabled erfordert Discovery URL, Client ID und Secret`,
`SAML enabled erfordert IdP-Metadaten (URL oder XML)` and further SAML checks), `404`, `413`
(`Anfrage zu groß`, request too large), `500` (`Fehler beim Speichern der SSO-Config`, error
saving the SSO config). Audit log: `statuspage.sso_updated`.

### `DELETE /api/admin/pages/:page_id/sso`

Deletes the configuration. A later `PUT` takes over neither the old client secret nor the old SP
key pair (new pair, re-import the SP metadata at the IdP). Response `200`:
`{"message": "SSO-Config gelöscht"}` (SSO config deleted). Errors: `400`, `404`, `500`
(`Fehler beim Löschen`, error deleting). Audit log: `statuspage.sso_deleted`.

### `POST /api/admin/pages/:page_id/sso/test`

Starts an OIDC test sign-in with the saved configuration, even before `oidc_enabled`. Limited to
20 calls per minute. Response `200`: `{"url": "<authorize URL of the IdP>"}`. The result comes back
as `oidc_test=<code>` (or `ok`) to `<FRONTEND_URL>/admin/sso`; no session and no link are created.

Errors: `400` (`Ungültige Seite` (invalid page), `Für die Testanmeldung fehlen Discovery URL, Client
ID oder Secret` (the test sign-in needs discovery URL, client ID or secret)), `403`
(`Testanmeldung ist für diese Seite abgeschaltet`, test sign-in is disabled for this page), `404`
(`SSO für diese Seite nicht eingerichtet`, SSO is not set up for this page), `502`
(`OIDC-Anbieter nicht erreichbar`, OIDC provider unreachable), `500`.

### `POST /api/admin/pages/:page_id/sso/saml-test`

Like `sso/test`, but for SAML. Errors: `400` (`Ungültige Seite`, `Für die Testanmeldung fehlen die
IdP-Metadaten` (the test sign-in needs the IdP metadata)), `403`, `404`, `502` (metadata cannot be
fetched or is invalid), `500`.

## Public

### `GET /api/public/pages/:slug/sso`

Limited to 60 calls per minute. Only reports whether the page SSO is active, so that the frontend
shows "Sign in with SSO". Response `200`: `{"oidc_enabled": true, "saml_enabled": false,
"page_id": 1}`. For a **private page** the answer names the page ID and the SSO kind only to
authorized callers (a role on the page or global admin); everyone else gets exactly the answer of
a page that does not exist, `404` (`Status page not found`), so that nobody can tell anonymously
which slugs are private pages. Visitors use [`GET /api/auth/sso/login`](#get-apiauthssologin)
instead.

### `GET /api/auth/sso/login`

Public, limited to 20 calls per minute. Query: `slug` (slug of the status page). Entry into the page
SSO without the page ID: the server chooses the method (OIDC if active, otherwise SAML), starts the
flow like `oidc/login` or `saml/login` and redirects to the IdP with `307`. An unknown page, a page
without active SSO and a missing `slug` all end the same way: a redirect to the login page with the
code `not_configured`. Anyone who calls the address for a slug and is redirected to the IdP learns
only that the page uses SSO. The status page offers this path when it is not available to the
visitor (missing or private).

## Sign-in with OIDC

### `GET /api/auth/oidc/login`

Public, limited to 20 calls per minute. Query: `page` (ID of the status page, mandatory). Creates
state, nonce and PKCE verifier (valid for 10 minutes), sets the cookie `oidc_state` (HttpOnly,
Secure) and redirects to the IdP with `307`. Errors redirect to the login page (codes `bad_page`,
`not_configured`, `provider_unreachable`, `internal`).

### `GET /api/auth/oidc/callback`

Public, limited to 20 calls per minute. The IdP redirects back here. Query: `page`, `state`,
`code`; the cookie `oidc_state` must match the `state`. On success `307` to
`<FRONTEND_URL>/auth/sso-callback?code=<one-time code>`, which the frontend redeems via
[`sso-exchange`](#post-apiauthsso-exchange). Errors redirect with a code (`state_invalid`,
`provider_denied`, `token_exchange`, `no_id_token`, `id_token_invalid`, `not_linked`,
`already_linked`, `global_admin`, `other_page_role`, `not_configured`, `provider_unreachable`,
`internal`). Audit log: `auth.sso_login` or `auth.sso_linked`.

### `POST /api/auth/oidc/link`

Needs a JWT. Query: `page`. Starts linking the signed-in account. Response `200`:
`{"url": "<authorize URL of the IdP>"}`; the client sends the browser there. Then the same
callback runs, and errors end up in the profile.

Errors (JSON): `401` (`Nicht angemeldet`, not signed in), `403` (`Global Admins melden sich nicht
über das Seiten-SSO an` (global admins do not sign in via the page SSO), `Konten mit Rolle auf
einer anderen Seite nutzen dieses Seiten-SSO nicht` (accounts with a role on another page do not
use this page SSO)), `400` (`Ungültige Seite`), `404` (`SSO für diese Seite nicht eingerichtet`),
`502` (`OIDC-Anbieter nicht erreichbar`), `500`.

## Sign-in with SAML

### `GET /api/auth/saml/login`

Public, limited to 20 calls per minute. Query: `page`. Creates a signed AuthnRequest (HTTP-Redirect
binding), sets the cookie `saml_state` (HttpOnly, Secure, `SameSite=None`) and redirects to the IdP
with `307`. Errors redirect to the login page as for OIDC (additionally `metadata_invalid`).

### `GET /api/auth/saml/:page_id/metadata`

Public, limited to 60 calls per minute. Returns the page's SP metadata (XML). The URL is at the
same time the SP entity ID. Errors: `400` (`Ungültige Seite`), `404` (`SAML für diese Seite nicht
eingerichtet` (SAML is not set up for this page), also before the first save and for deleted
pages), `500` (`SP-Zertifikat ungültig`, SP certificate invalid).

### `POST /api/auth/saml/:page_id/acs`

Public, limited to 20 calls per minute. Assertion Consumer Service: the IdP sends `SAMLResponse`
and `RelayState` by HTTP-POST. On success `303` to
`<FRONTEND_URL>/auth/sso-callback?code=<one-time code>`. Errors redirect with a code (as for the
OIDC callback, plus `saml_response_invalid`, `saml_replay`, `saml_nameid_format`,
`metadata_invalid`). Audit log: `auth.sso_login` or `auth.sso_linked` with `provider=saml`.

### `POST /api/auth/saml/link`

Needs a JWT. Query: `page`. Like [`oidc/link`](#post-apiauthoidclink), but for SAML. Response
`200`: `{"url": "<SSO URL of the IdP>"}`.

## Session and links

### `POST /api/auth/sso-exchange`

Public, limited to 10 calls per minute. Exchanges the one-time code from the redirect for the JWT.
Body: `{"code": "<one-time code>"}`. Response `200`: `{"token": "<JWT>"}`. The code is valid only
once and only briefly. The JWT is valid only for the page whose IdP confirmed the sign-in
().

Errors: `400` (`Code erforderlich`, code required), `401` (`Ungültiger oder abgelaufener Code`,
invalid or expired code).

### `GET /api/me/sso`

Needs a JWT. Returns the own links and the pages the account may still link, per protocol:

```json
{
  "identities": [{"id": 7, "status_page_id": 1, "page_name": "Demo", "protocol": "oidc", "email": "user@example.org", "created_at": "…"}],
  "linkable": [{"status_page_id": 1, "page_name": "Demo", "protocol": "saml"}]
}
```

Errors: `401` (`Nicht angemeldet`).

### `DELETE /api/me/sso/:id`

Needs a JWT. Removes an own link. Response `204`. Errors: `400` (`Ungültige ID`, invalid ID), `401`,
`404` (`Verknüpfung nicht gefunden`, link not found; also for foreign IDs). Audit log:
`auth.sso_unlinked`.

## Error redirects

On an error, login, callback and ACS do not answer with JSON but redirect:

| Flow | Target |
| --- | --- |
| Login | `<FRONTEND_URL>/login?oidc_error=<code>` |
| Linking | `<FRONTEND_URL>/admin/profile?oidc_error=<code>` |
| Test sign-in | `<FRONTEND_URL>/admin/sso?oidc_test=<code>` |

For historical reasons the parameters are named `oidc_*` and carry the codes of both protocols.
The URL contains only the fixed code, never a message from the IdP. Causes and remedies per code
are in the error cases of the admin manual:
[OIDC](../../user-guide/en/sso-oidc.md#error-cases), [SAML](../../user-guide/en/sso-saml.md#error-cases).
