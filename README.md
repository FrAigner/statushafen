# Statushafen

[Deutsch](README.de.md) · English

Statushafen is a self-hosted platform for **status pages**. Companies show whether their services
are running, report incidents, announce maintenance windows and notify subscribers by e-mail or
Google Chat. One installation serves many status pages (tenants), each with its own address,
branding, components and permissions.

Go (Gin, GORM) · Vue 3 (Vite, sit-onyx) · PostgreSQL · [MIT license](LICENSE)

## Features

- **Status pages per tenant:** subdomain (`acme.example.com`), path (`/status/acme`) or your own
  domain with automatic TLS check, private pages for signed-in users, branding per page.
- **Components, tags and groups** with a 90-day availability view.
- **Incidents** with status timeline, updates, post-mortems and **maintenance windows** that start
  and end automatically; general **announcements**.
- **External sources:** include other public status pages (Summary API v2) and let their state
  influence your components.
- **Subscribers:** e-mail with double opt-in or Google Chat webhook, editable mail templates per
  language and audience.
- **Roles and permissions**, audit log, per-page single sign-on with OIDC and SAML 2.0.
- **Multilingual** interface and content (30 languages in the catalog), optional automatic
  translation (LibreTranslate or DeepL).
- **Formatted texts** in a restricted Markdown subset, rendered safely in pages and mails.
- **Self-monitoring** with a system page, Prometheus metrics, structured logs without secrets.
- Documented REST API ([OpenAPI](docs/api/openapi.yaml)) and in-app help.

## Quick start (development)

Requirements: Go (version from `backend/go.mod`), Node.js 22.12+ or 24+ (CI, Docker image and `frontend/.nvmrc` use 24), Docker.

```bash
cp backend/.env.example backend/.env      # set SEED_DB="true" for demo data
cp frontend/.env.example frontend/.env
docker compose up -d db mailpit           # PostgreSQL and a local mail catcher (UI on :8025); bound to 127.0.0.1 only
./start.sh                                # backend on :8080, frontend on :5173
./start.sh status                         # show what is running
./stop.sh
```

`./start.sh` generates a random `JWT_SECRET` on first start (`backend/.jwt-secret.local`, git-ignored) when none is
set; the backend refuses to start with a missing, short or placeholder secret.

With `SEED_DB="true"` the database is reset (tables are dropped!) and filled with demo data. This
only works with `APP_ENV=development`, `dev`, `test` or `local` (`./start.sh` sets `development` by
default); any other or empty `APP_ENV` makes the backend ignore `SEED_DB` and log a warning. The
demo data contains three accounts: `admin@statushafen.local` / `admin123`, `editor@statushafen.local` /
`editor123` and `viewer@statushafen.local` / `viewer123`. Use them for local development only.

## Configuration

The backend reads environment variables (a `backend/.env` file is loaded if present). See
[`backend/.env.example`](backend/.env.example) for all options with comments.

| Variable | Purpose |
|---|---|
| `DB_DSN` or `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_SSL_MODE` | PostgreSQL connection |
| `JWT_SECRET` | **Required.** Signing key for login tokens: at least 32 random characters (`openssl rand -base64 48`). The backend refuses to start with an empty, shorter or well-known placeholder value. `./start.sh` generates one for local development (`backend/.jwt-secret.local`, git-ignored) |
| `APP_ENV` | `production` in production (JSON logs, no development exceptions); seeding (`SEED_DB`), `OIDC_DEV_INSECURE_ISSUERS` and the token-free domain check only with `development`, `dev`, `test` or `local` |
| `PORT` | Listen port (default `8080`) |
| `FRONTEND_URL`, `BASE_URL`, `CORS_ORIGINS` | Public addresses and allowed origins |
| `TRUSTED_PROXIES` | Comma-separated proxy IPs/CIDRs whose `X-Forwarded-For` is trusted (none by default). Only list your own reverse proxy: networks wider than /8 (IPv4) or /16 (IPv6), including `0.0.0.0/0` and `::/0`, let any client forge its address and defeat all IP limits; the backend refuses to start with them unless `APP_ENV` is `development`, `dev`, `test` or `local` (warning only) |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM` | Outgoing mail (default: local Mailpit). `SMTP_FROM` is the sender address of all mails; status pages only add their name and an optional reply-to |
| `SSO_ALLOWED_DOMAINS`, `SSO_BOOTSTRAP_ADMIN_EMAILS`, `GOOGLE_*`, `GITHUB_*` | Platform login via Google/GitHub (with PKCE; the state cookie is signed with `JWT_SECRET`, 20 requests per minute and IP) and bootstrap admins |
| `CUSTOM_DOMAIN_CNAME_TARGET`, `CADDY_INTERNAL_TOKEN` | Custom domains (see below) |
| `METRICS_BASIC_AUTH_USER`, `METRICS_BASIC_AUTH_PASSWORD` | Enable `/metrics` (off unless both are set) |
| `LOG_FORMAT`, `LOG_LEVEL` | Logging (`json`/`text`, `debug`…`error`) |
| `LIBRETRANSLATE_URL`, `LIBRETRANSLATE_API_KEY` | Default translation service |
| `EXTERNAL_STATUS_POLLING_ENABLED` | Background polling of external sources (default on) |
| `SELFMONITOR_BACKUP_JOB`, `SELFMONITOR_METRICS_DIR` | Optional backup-age check of the self-monitoring |
| `OIDC_DEV_INSECURE_ISSUERS` | Development only (`APP_ENV` `development`, `dev`, `test` or `local`): allow a plain-HTTP OIDC mock issuer |

Frontend (build time, `frontend/.env`): `VITE_API_URL` and `VITE_APP_BASE_DOMAINS` (comma-separated
base domains; `<slug>.<base domain>` is then a tenant address. Without it, pages are reachable under
`/status/<slug>` or a custom domain).

## Deployment

The [`Dockerfile`](Dockerfile) builds one image with the API and the compiled frontend (port 8080).
Run it behind a TLS-terminating reverse proxy with a PostgreSQL database.

Checklist for production:

- Set `APP_ENV=production`, a strong `JWT_SECRET` and never `SEED_DB`.
- Create the first admin without any SSO: run the command once against your production database
  (the password is read from stdin only, never from an argument; it needs at least 8 characters).
  An existing account is never changed, and the command refuses to run while `SEED_DB` is set:

  ```bash
  read -rs ADMIN_PW && printf '%s\n' "$ADMIN_PW" | ./server create-admin --email you@example.org --password-stdin; unset ADMIN_PW
  ```

  In Docker use `docker exec -i <container> ./server create-admin --email you@example.org --password-stdin`.
  Alternatively `SSO_BOOTSTRAP_ADMIN_EMAILS` (e-mail addresses that become global admin on their next
  platform login via Google or GitHub) works too.
- Set `FRONTEND_URL`, `CORS_ORIGINS`, `TRUSTED_PROXIES` and your SMTP settings. The backend sends
  `Strict-Transport-Security` when the request comes from a proxy listed in `TRUSTED_PROXIES` that sets
  `X-Forwarded-Proto: https` (the header is ignored from any other sender).
- For tenant subdomains point a wildcard DNS record and a wildcard certificate at the proxy and set
  `VITE_APP_BASE_DOMAINS` at build time.
- For **custom domains** the proxy must obtain certificates on demand and ask
  `GET /api/internal/custom-domain-check?domain=<host>` (header `X-Internal-Token` = `CADDY_INTERNAL_TOKEN`)
  whether a host is allowed. Customers create a CNAME to `CUSTOM_DOMAIN_CNAME_TARGET`
  (default `edge.<host of FRONTEND_URL>`) and a verification TXT record. **`CADDY_INTERNAL_TOKEN` is
  required** whenever `APP_ENV` is not `development`, `dev`, `test` or `local`: without it the endpoint always
  answers 503 (with a warning at startup) and custom domains get no certificate. Caddy's "ask" call cannot
  send headers itself: route it through an internal hop (for example a small proxy in the Docker
  network) that sets `X-Internal-Token` to the same secret the application knows. A domain moves from "Verified" to "Active" on this request from Caddy, no
  longer on the public page load. Custom domains of private pages no longer resolve to a page for
  non-members (they see the landing page). The endpoint is limited to 120 requests per minute per IP
  (429 afterwards); Caddy only asks for hosts without a certificate.
- Back up the database regularly: [`docs/backup-restore.en.md`](docs/backup-restore.en.md).

## Documentation

- In-app help (German and English) in the app under `/docs`, sources in [`frontend/src/docs`](frontend/src/docs).
- [User guide](docs/user-guide), [single sign-on guides](docs/sso) (German, English in `docs/sso/en`),
  [REST API](docs/api).
- [Backup and restore](docs/backup-restore.en.md).

## Tests and quality

```bash
(cd backend && go test ./...)             # unit tests (SQLite in memory)
(cd frontend && npm ci && npm test)       # component and unit tests
(cd frontend && npx playwright test)      # end-to-end tests (backend and frontend running)
```

Contributions are welcome: see [CONTRIBUTING.md](CONTRIBUTING.md). Security reports:
[SECURITY.md](SECURITY.md).

### Local single sign-on tests

`docker compose --profile sso up -d mock-oidc mock-saml` starts an OIDC and a SAML mock identity
provider; the walkthrough is in [`docs/sso/en/local-testing.md`](docs/sso/en/local-testing.md). The Playwright specs
`sso-oidc` and `sso-saml` run the whole flow and also run nightly in
[`.github/workflows/e2e-sso-nachts.yml`](.github/workflows/e2e-sso-nachts.yml).

## License, name and third-party software

Statushafen is released under the [MIT License](LICENSE). The name "Statushafen" may be used for
unmodified versions and with a reference such as "based on Statushafen"; see
[TRADEMARK.md](TRADEMARK.md). Third-party components and fonts (SIL Open Font License) are listed
in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

External sources use the Summary API v2 (`/api/v2/summary.json`), which many public status pages
offer, including pages built on Atlassian Statuspage. Atlassian and Statuspage are trademarks of
Atlassian Pty Ltd; this project is not affiliated with Atlassian.
