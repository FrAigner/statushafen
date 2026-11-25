# SSO API

> English version of the German document [`../api.md`](../api.md). The error
> messages in `{"error": "<text>"}` are returned by the backend in German; they are quoted here
> unchanged, with an English translation in parentheses where it helps.

All paths begin with `/api`. JSON endpoints report errors as `{"error": "<text>"}`. Endpoints that
the browser calls directly (login, callback) redirect to the frontend with a fixed code
instead when an error occurs (see [Error redirects](#error-redirects)). The flow is shown in the
[architecture](architecture.md), the setup in the [OIDC guide](oidc.md).

**Authentication at the API:** Endpoints that require a login expect the JWT in the header
`X-Auth-Token: <token>`. `Authorization: Bearer <token>` is still accepted; if
`X-Auth-Token` is set, it takes precedence.

## Page configuration

These endpoints need a JWT and the permissions `sso:read`, `sso:update` or `sso:delete` on the
status page (or global admin).

### `GET /api/admin/pages/:page_id/sso`

Reads the SSO configuration. Without a stored configuration, default values are returned
(`oidc_enabled` `false`, `test_login_enabled` `true`). The client secret is never returned.

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
  "test_login_enabled": true
}
```

`oidc_acs_url` is built from `BASE_URL` on every request. Errors: `400` (`Invalid page_id`), `404` (`Statusseite nicht gefunden`, status
page not found).

### `PUT /api/admin/pages/:page_id/sso`

Saves the configuration (upsert, exactly one row per page; possible again after `DELETE`). Body
(JSON, at most 64 KB; `saml_*` fields from earlier versions are ignored):

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
| `test_login_enabled` | bool | Allows the test sign-in. |

If the OIDC issuer changes, saving deletes the OIDC links of this page.

Response `200`: the saved configuration as with `GET`. Errors: `400` (`Invalid page_id`,
`Invalid request`, `OIDC enabled erfordert Discovery URL, Client ID und Secret`), `404`, `413`
(`Anfrage zu groß`, request too large), `500` (`Fehler beim Speichern der SSO-Config`, error
saving the SSO config). Audit log: `statuspage.sso_updated`.

### `DELETE /api/admin/pages/:page_id/sso`

Deletes the configuration. A later `PUT` does not take over the old client secret. Response `200`:
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

## Public

### `GET /api/public/pages/:slug/sso`

Limited to 60 calls per minute. Only reports whether the page SSO is active, so that the frontend
shows "Sign in with SSO". Response `200`: `{"oidc_enabled": true,
"page_id": 1}`. For a **private page** the answer names the page ID and the SSO kind only to
authorized callers (a role on the page or global admin); everyone else gets exactly the answer of
a page that does not exist, `404` (`Status page not found`), so that nobody can tell anonymously
which slugs are private pages. Visitors use [`GET /api/auth/sso/login`](#get-apiauthssologin)
instead.

### `GET /api/auth/sso/login`

Public, limited to 20 calls per minute. Query: `slug` (slug of the status page), optional
`return_to` and `return_host` (see [custom domain](providers.md#status-pages-on-a-custom-domain)). Entry into the page
SSO without the page ID: the server starts the
flow like `oidc/login` and redirects to the IdP with `307`. An unknown page, a page
without active SSO and a missing `slug` all end the same way: a redirect to the login page with the
code `not_configured`. Anyone who calls the address for a slug and is redirected to the IdP learns
only that the page uses SSO. The status page offers this path when it is not available to the
visitor (missing or private).

## Sign-in with OIDC

### `GET /api/auth/oidc/login`

Public, limited to 20 calls per minute. Query: `page` (ID of the status page, mandatory),
`return_to` and `return_host` (optional, see [returning](providers.md#returning-after-the-login)). Creates
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

## Global OIDC providers

Providers from `OIDC_PROVIDERS` (see [Global providers](providers.md)); `:id` is the provider ID.

### `GET /api/public/auth-config`

Public, limited to 60 calls per minute. Sign-in methods for the login page. Response `200`:

```json
{"password_login": false, "providers": [{"id": "gitlab", "name": "GitLab"}]}
```

`providers` contains only active providers, in the order of `OIDC_PROVIDERS`, without issuer or
client data.

### `GET /api/auth/provider/:id/login`

Public, limited to 20 calls per minute. Query: `return_to` and `return_host` (optional, see
[returning](providers.md#returning-after-the-login)). If one of these three login starts arrives on a
verified custom domain, it first redirects with `307` to the same path under `BASE_URL` (with the
domain as `return_host`). Creates state, nonce and PKCE verifier (valid for
10 minutes), sets the cookie `oidc_state` (path `/api/auth/provider`, HttpOnly, Secure) and redirects
to the provider with `307`. Errors redirect to the login page (`provider_unknown`,
`provider_unreachable`, `internal`).

### `GET /api/auth/provider/:id/callback`

Public, limited to 20 calls per minute. Redirect URI at the provider
(`<BASE_URL>/api/auth/provider/<id>/callback`). Query: `state`, `code`; only a state of this provider
is accepted. On success `307` to `<FRONTEND_URL>/auth/sso-callback?code=<one-time code>` (when
linking with `&next=profile`). Errors redirect with a code (`provider_unknown`, `state_invalid`,
`provider_denied`, `provider_unreachable`, `token_exchange`, `no_id_token`, `id_token_invalid`,
`email_missing`, `email_unverified`, `domain_not_allowed`, `not_linked`, `already_linked`,
`internal`). Audit log: `auth.sso_login` or `auth.sso_linked` with `provider=<id>`.

### `POST /api/auth/provider/:id/link`

Needs a JWT, no query and no body. Starts linking the signed-in account (also global admins).
Response `200`: `{"url": "<authorize URL of the provider>"}`. Errors (JSON): `401`, `403` (session from
a page SSO), `404` (unknown ID), `502` (provider not reachable), `500`.

## Session and links

### `POST /api/auth/sso-exchange`

Public, limited to 10 calls per minute. Exchanges the one-time code from the redirect for the JWT.
Body: `{"code": "<one-time code>"}`. Response `200`: `{"token": "<JWT>", "return_to": "/path"}`
(`return_to` is the checked return path from the login start, otherwise `""`). The code is valid only
once and only briefly. After a page SSO the JWT is valid only for the page whose IdP confirmed the
sign-in; after a global provider for the whole account.

Errors: `400` (`Code erforderlich`, code required), `401` (`Ungültiger oder abgelaufener Code`,
invalid or expired code).

### `GET /api/me/sso`

Needs a JWT. Returns the own links and the pages the account may still link:

```json
{
  "identities": [
    {"id": 7, "status_page_id": 1, "page_name": "Demo", "protocol": "oidc", "provider": "", "email": "user@example.org", "created_at": "…"},
    {"id": 8, "status_page_id": 0, "page_name": "GitLab", "protocol": "oidc", "provider": "gitlab", "email": "user@example.org", "created_at": "…"}
  ],
  "linkable": [
    {"status_page_id": 0, "page_name": "Google", "protocol": "oidc", "provider": "google"},
    {"status_page_id": 2, "page_name": "Other", "protocol": "oidc", "provider": ""}
  ]
}
```

Entries with `status_page_id` `0` belong to a global provider: `provider` is its ID, `page_name` its
display name (link via [`POST /api/auth/provider/:id/link`](#post-apiauthprovideridlink)). If the
provider is no longer configured, the link stays visible and removable with an empty `provider`.
Global providers appear for global admins too, never in a session from a page SSO.

Errors: `401` (`Nicht angemeldet`).

### `DELETE /api/me/sso/:id`

Needs a JWT. Removes an own link. Response `204`. Errors: `400` (`Ungültige ID`, invalid ID), `401`,
`404` (`Verknüpfung nicht gefunden`, link not found; also for foreign IDs). Audit log:
`auth.sso_unlinked`.

## Error redirects

On an error, login and callback do not answer with JSON but redirect:

| Flow | Target |
| --- | --- |
| Login | `<FRONTEND_URL>/login?oidc_error=<code>` |
| Linking | `<FRONTEND_URL>/admin/profile?oidc_error=<code>` |
| Test sign-in | `<FRONTEND_URL>/admin/sso?oidc_test=<code>` |

The URL contains only the fixed code, never a message from the IdP. Causes and remedies per code
are in the error cases of the admin manual:
[OIDC](../../user-guide/en/sso-oidc.md#error-cases).
