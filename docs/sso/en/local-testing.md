# Local single sign-on tests with mock providers

For development there is an OIDC and a SAML mock identity provider. They replace a real identity
provider; for production, set up SSO with the guides in this folder.

## Mock OIDC

Guide for the production setup (redirect URL, provider examples, error cases):
[`docs/user-guide/en/sso-oidc.md`](../../user-guide/en/sso-oidc.md).

For per-page OIDC login, `docker-compose.yml` contains a mock provider
([mock-oauth2-server](https://github.com/navikt/mock-oauth2-server)) in the Compose profile `sso`.
Without the profile it does not start.

1. Start the mock: `docker compose --profile sso up -d mock-oidc`
   (discovery: <http://localhost:8090/statushafen/.well-known/openid-configuration>).
2. Start the backend with these variables (for example in `backend/.env`):
   ```
   APP_ENV="development"
   SEED_DB="true"
   OIDC_DEV_INSECURE_ISSUERS="http://localhost:8090/statushafen"
   ```
   Without `OIDC_DEV_INSECURE_ISSUERS` the backend rejects the mock (https is required and issuers
   are protected against SSRF). The exception only applies to exactly the listed issuers and only
   with `APP_ENV` set to `development`, `dev`, `test` or `local`.
3. Configure the status page: with `pages:write` on the page (or as global admin) pick the status
   page at the top of the admin area, then "System → SSO" (or `PUT /api/admin/pages/<id>/sso`):
   discovery URL `http://localhost:8090/statushafen/.well-known/openid-configuration`, any client ID
   and secret (the mock does not check them), scopes `openid email profile` (empty = this default).
   The secret is never returned; when editing later leave the field empty to keep the stored one.
   With "Allow test login", "Start test login" checks the setup without creating a session.
4. Link an account: the page SSO does not create new accounts. A signed-in user without the global
   admin right and without a role on another page chooses "Link with SSO" for the page in the
   profile (`/admin/profile`) (API: `POST /api/auth/oidc/link?page=<id>`, then open the returned `url`).
5. Sign in: on the status page choose "Sign in", then "Sign in with SSO" (directly: `/login?page=<slug>`
   in the frontend or `http://localhost:8080/api/auth/oidc/login?page=<id>`). In the mock's form
   "username" becomes the `sub` claim (use the same value every time); further claims can be set as
   JSON in the text field, for example `{"name": "Erika"}`.

The Playwright spec `frontend/e2e/sso-oidc.spec.js` runs the whole flow through the user interface
(own page and account, linking in the profile, signing in via the status page, checking the session,
error page for an unlinked identity) and skips itself if the mock is not reachable:
`cd frontend && npx playwright test sso-oidc` (the backend must be running; other addresses via
`E2E_BACKEND_URL` / `MOCK_OIDC_URL`). With `E2E_REQUIRE_SSO_MOCK=1` it fails instead of skipping; that
is how it runs every night in `.github/workflows/e2e-sso-nachts.yml` (GitHub-hosted runner, Postgres
and mock as service containers, also manually via `workflow_dispatch`).

## Mock SAML

Guide for the production setup: [`docs/user-guide/en/sso-saml.md`](../../user-guide/en/sso-saml.md).

For per-page SAML login there is a mock IdP in the repository (`backend/cmd/mocksamlidp`, package
`backend/mocksamlidp`, based on `crewjam/saml`). It knows every service provider whose ACS URL starts
with `MOCK_SAML_ACS_PREFIX` (default `http://localhost:8080/api/auth/saml/`), so it needs no
registration per status page. It generates a new key and certificate on every start.

1. Start the mock, either in the Compose profile `sso`: `docker compose --profile sso up -d mock-saml`,
   or directly: `cd backend && go run ./cmd/mocksamlidp` (metadata: <http://localhost:8091/metadata>).
2. Start the backend as usual (for example with `SEED_DB="true"`). An exception like
   `OIDC_DEV_INSECURE_ISSUERS` is not needed: the backend does not load the mock itself.
3. Configure the status page: in the admin area "System → SSO", section "SAML 2.0", paste the content
   of <http://localhost:8091/metadata> into "IdP metadata (XML)" (the backend accepts a metadata URL
   only over https), enable SAML login, save. The attributes `email`/`name` (default) match the mock.
   After restarting the mock paste the metadata again (new key).
4. Link and sign in as with OIDC: profile → "Link with SSO" (entry "SAML"), then on the status page
   "Sign in" → "Sign in with SSO (SAML)". In the mock's form "username" becomes the persistent
   NameID (use the same value every time); e-mail and name are sent as attributes. Submit the form
   within 90 seconds, otherwise the AuthnRequest counts as expired.

The Playwright spec `frontend/e2e/sso-saml.spec.js` runs this through the user interface and skips
itself without the mock: `cd frontend && npx playwright test sso-saml` (other address via
`MOCK_SAML_URL`). With `E2E_REQUIRE_SSO_MOCK=1` the mock is mandatory; that is how the spec runs in
the same nightly job `.github/workflows/e2e-sso-nachts.yml` as the OIDC spec.
