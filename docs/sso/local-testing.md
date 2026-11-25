# Lokale SSO-Tests mit Mock-Anbietern

Für die Entwicklung gibt es einen OIDC-Mock-Anbieter. Er ersetzt einen echten
Identity Provider; produktiv eingerichtet wird SSO nach den Anleitungen in diesem Ordner.

## Mock-OIDC

Anleitung für die produktive Einrichtung (Redirect-URL, Anbieter-Beispiele, Fehlerbilder):
[`docs/user-guide/sso-oidc.md`](../user-guide/sso-oidc.md).

Für die OIDC-Anmeldung je Statusseite gibt es in `docker-compose.yml`
einen Mock-Anbieter ([mock-oauth2-server](https://github.com/navikt/mock-oauth2-server))
im Compose-Profil `sso`. Ohne das Profil startet er nicht mit.

1. Mock starten: `docker compose --profile sso up -d mock-oidc`
   (Discovery: <http://localhost:8090/statushafen/.well-known/openid-configuration>).
2. Backend mit diesen Variablen starten (z. B. in `backend/.env`):
   ```
   APP_ENV="development"
   SEED_DB="true"
   PASSWORD_LOGIN_ENABLED="true"
   OIDC_DEV_INSECURE_ISSUERS="http://localhost:8090/statushafen"
   ```
   `PASSWORD_LOGIN_ENABLED` erlaubt die Anmeldung mit den Seed-Konten (Standard: aus;
   `scripts/dev-backend.sh` setzt es lokal auf `true`, solange `backend/.env` nichts anderes sagt).
   Ohne `OIDC_DEV_INSECURE_ISSUERS` lehnt das Backend den Mock ab (https-Pflicht und
   SSRF-Schutz für Issuer). Die Ausnahme gilt nur für exakt genannte Issuer und nur
   mit `APP_ENV` `development`, `dev`, `test` oder `local`.
3. Statusseite konfigurieren: mit `pages:write` auf der Seite (oder als Global Admin) im Admin-Bereich oben die Statusseite
   wählen, dann „System → SSO“ (oder `PUT /api/admin/pages/<id>/sso`): Discovery-URL
   `http://localhost:8090/statushafen/.well-known/openid-configuration`, Client-ID und
   -Secret beliebig (der Mock prüft sie nicht), Scopes `openid email profile` (leer =
   dieser Standard). Das Secret wird nie zurückgeliefert; beim späteren Bearbeiten
   das Feld leer lassen, um das gespeicherte zu behalten. Mit „Test-Login erlauben“
   prüft „Testanmeldung starten“ die Einrichtung, ohne dass eine Sitzung entsteht.
4. Konto verknüpfen: Neue Konten entstehen über das Seiten-SSO nicht. Ein angemeldeter
   Nutzer ohne Global-Admin-Recht und ohne Rolle auf einer anderen Seite wählt im Profil
   (`/admin/profile`) bei der Seite „Mit SSO verknüpfen“ (API:
   `POST /api/auth/oidc/link?page=<id>`, danach die gelieferte `url` öffnen).
5. Anmelden: auf der Statusseite oben „Anmelden“, dann „Mit SSO anmelden“ (direkt:
   `/login?page=<slug>` im Frontend oder `http://localhost:8080/api/auth/oidc/login?page=<id>`).
   Im Formular des Mocks wird „username“ zum `sub`-Claim (bei jeder Anmeldung gleich wählen),
   im Textfeld lassen sich weitere Claims als JSON setzen, z. B. `{"name": "Erika"}`.

Der Playwright-Spec `frontend/e2e/sso-oidc.spec.js` spielt das komplett über die Oberfläche
durch (eigene Seite und eigenes Konto, Verknüpfen im Profil, Anmelden über die Statusseite,
Sitzung prüfen, Fehlerseite bei nicht verknüpfter Identität) und überspringt sich,
wenn der Mock nicht erreichbar ist: `cd frontend && npx playwright test sso-oidc`
(Backend muss laufen; andere Adressen über `E2E_BACKEND_URL` / `MOCK_OIDC_URL`).
Mit `E2E_REQUIRE_SSO_MOCK=1` schlägt er statt zu überspringen fehl; so läuft er jede Nacht
in `.github/workflows/e2e-sso-nachts.yml` (GitHub-gehosteter Runner, Postgres und Mock als
Service-Container, auch manuell per `workflow_dispatch`).

## Mock-OIDC als globaler Anbieter

Derselbe Mock dient auch als [globaler Anbieter](providers.md). In `backend/.env` zusätzlich:

```
OIDC_PROVIDERS="mock"
OIDC_MOCK_ISSUER="http://localhost:8090/statushafen"
OIDC_MOCK_CLIENT_ID="statushafen"
OIDC_MOCK_CLIENT_SECRET="beliebig"
OIDC_MOCK_NAME="Mock-OIDC"
```

Der Issuer muss in `OIDC_DEV_INSECURE_ISSUERS` stehen. Die Login-Seite zeigt dann „Mock-OIDC“
(`GET /api/public/auth-config`). Im Formular des Mocks unter „username“ das `sub` wählen und im
Textfeld z. B. `{"email": "admin@statushafen.local", "email_verified": true}` setzen: Dann wird das
Seed-Konto mit dieser Adresse verknüpft. Ohne passendes Konto entsteht ein neues mit der Rolle
„Viewer“.
