# API and webhooks for monitoring

## Set status via webhook

A monitoring system (e.g. Uptime Kuma, Prometheus Alertmanager or a script of your own) can set a
component's status directly:

```text
POST /api/webhooks/components/<component ID>/status
```

### Create a token

1. Open **Status Pages** in the admin area (global admin) and edit the page.
2. In the "Monitoring webhook" section, click the button to generate a token.
3. Copy the token right away: it is shown only this once.

There is one token per status page; it applies to all its components. Regenerating invalidates
the old token immediately, revoking disables the webhook for the page.
Only a hash of the token is stored, never the plain text: a lost token cannot be shown again, so
simply generate a new one. Existing tokens stay valid and are converted to the hash automatically
during the update.

The component ID is shown under **Components** in the "ID" column.

### Request

- Header `X-Webhook-Token: <token>` (alternatively `?token=<token>` in the URL; that ends up in
  proxy logs and is therefore not recommended).
- JSON body with the new status: `operational`, `degraded_performance`, `partial_outage`,
  `major_outage` or `under_maintenance`.

Example:

```bash
curl -X POST "https://acme.example.com/api/webhooks/components/42/status" \
  -H "X-Webhook-Token: <token>" \
  -H "Content-Type: application/json" \
  -d '{"status": "major_outage"}'
```

### By slug instead of ID

Alternatively, address the component by its slug (the "Slug" column under **Components**). The status page is derived from the token alone; the slug is resolved only within that page. The same slug on another page is unrelated.

```text
POST /api/webhooks/component-slugs/<slug>/status
```

Token, body, rate limit (20 per minute and IP), status history and audit log are identical to the ID route. Archived components stay addressable, deleted ones do not. An invalid token always yields 401, even if the slug does not exist; with a valid token, an unknown slug (or one from another page) yields 404.

```bash
curl -X POST "https://acme.example.com/api/webhooks/component-slugs/api/status" \
  -H "X-Webhook-Token: <token>" \
  -H "Content-Type: application/json" \
  -d '{"status": "major_outage"}'
```

### Responses

| Code | Meaning |
|---|---|
| 200 | Status set, response contains the component |
| 400 | invalid ID, invalid body or unknown status |
| 401 | token missing or wrong, or the component does not exist (both answer identically so IDs cannot be probed) |
| 404 | only on the slug route: slug not found with a valid token |
| 429 | more than 20 calls per minute from the same IP |

Every change appears in the [audit log](/docs/audit-log) as `component.status_webhook_updated`.

The status applies immediately with every call. In the status history (and thus availability),
changes of the same component within 30 seconds are condensed into one entry so that a misbehaving,
flapping monitor cannot fill the database. There is no per-token rate limit yet.

## Further API

The Statushafen interface uses a REST API under `/api`. Publicly readable are, for example:

| Endpoint | Content |
|---|---|
| `GET /api/public/pages` | public status pages |
| `GET /api/public/pages/<slug>/dashboard` | data of one status page (components, incidents, notices, external reports) |
| `GET /api/public/pages/<slug>/availability` | availability of the last 90 days as daily aggregates; `?include=components` adds the values per component |
| `GET /api/pages/<page ID>/components` | components of a page |
| `GET /api/pages/<page ID>/incidents` | incidents of a page |
| `GET /api/pages/<page ID>/notices` | notices of a page |
| `POST /api/subscribe` | create a subscription |

Private pages only return their data with a login.

These read routes are limited to 120 calls per minute per IP address; beyond that the server answers
with `429`. `GET /api/public/pages` returns at most 200 pages per call (`?limit=` and `?offset=` for
paging, sorted by ID); name (120 characters) and description (500 characters) are shortened there.

### Signing in to the API

You get a login token with `POST /api/login` (email and password); it is valid for two hours. Send
it in the header `X-Auth-Token: <token>`. The header `Authorization: Bearer <token>` is still
accepted; if `X-Auth-Token` is set, it takes precedence, even if the token is invalid. The separate
header is needed because an upstream Basic Auth protection (for example on a staging instance)
occupies the `Authorization` header.

```bash
curl "https://acme.example.com/api/me" -H "X-Auth-Token: <token>"
```

Which permissions an endpoint needs is stated in the OpenAPI file (`x-required-permission`). The
same permissions as in the interface apply, see
[Users, roles and permissions](/docs/benutzer-und-rollen#permissions). For groups, send
`parent_id` (the ID of the parent group) when creating or changing a component; when creating,
`is_group: true` makes the new component a group.

### Lists and limits

The incident list (`GET /api/pages/<id>/incidents`) returns at most 200 entries per call, the newest
50 without `limit`. Use `offset` to page further back, `state=open|resolved` to filter by status and
`is_maintenance=any` to include maintenance windows. The dashboard contains all open and the newest
resolved incidents; `incidents_more: true` shows that older ones exist. Read calls to the dashboard and
incidents are limited to 120 per minute and IP. For the limit to apply per visitor, `TRUSTED_PROXIES`
must be set behind a reverse proxy. Write calls that trigger notifications are limited per page and
user, see [Incidents](/docs/vorfaelle#notifications).

### Error responses

Errors have the form `{"error": "<message>", "code": "<code>"}`. The **code** is machine-readable
and stable; the message is text for humans and may change. The interface translates the code into
the chosen language. Common codes:

| Code | HTTP | Meaning |
|---|---|---|
| `invalid_request` | 400 | input incomplete or invalid |
| `input_too_long` | 400 | input exceeds the limit (subscription: e-mail 254, webhook URL 2048, component list 8192, language 16 bytes) |
| `input_invalid_characters` | 400 | input contains control characters (including NUL) or invalid UTF-8; the server rejects NUL and invalid UTF-8 in the body, query and path on every route |
| `invalid_id` | 400 | invalid identifier in the path or body |
| `forbidden` | 403 | permission missing |
| `page_not_found` | 404 | status page not found |
| `slug_taken` | 409 | slug is used by another page |
| `slug_reserved` | 400 | slug is reserved, see [Status pages](/docs/statusseiten#rules-for-the-slug) |
| `password_too_short` | 400 | password shorter than 8 characters |
| `language_code_invalid` | 400 | language code has an invalid format, see [Languages](/docs/sprachen) |
| `translation_not_configured` | 503 | no automatic translation set up, see [System settings](/docs/systemeinstellungen) |
| `translation_unavailable` | 502 | translation service not reachable |
| `translation_auth_failed` | 502 | translation service rejects the key |
| `translation_quota_exceeded` | 429 | quota of the translation service reached |
| `translation_language_unsupported` | 422 | language pair not supported by the service |
| `translation_text_too_long` | 400 | more than 5000 characters to translate |
| `group_not_empty` | 409 | group still contains components and cannot be deleted |
| `server_error` | 500 | error on the server |

Not every error carries a code yet. In addition there is `429` for too many calls (with a
`Retry-After` header).

### OpenAPI

The full description of all endpoints is available as an OpenAPI file in the repository under
`docs/api/openapi.yaml`. A test compares it with the actually registered routes on every build; it
also contains the endpoints for external sources, the page tags and the availability.
