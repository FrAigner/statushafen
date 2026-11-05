# Lokale SSO-Tests mit Mock-Anbietern

Für die Entwicklung gibt es einen OIDC- und einen SAML-Mock-Anbieter. Sie ersetzen einen echten
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
   OIDC_DEV_INSECURE_ISSUERS="http://localhost:8090/statushafen"
   ```
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

## Mock-SAML

Anleitung für die produktive Einrichtung: [`docs/user-guide/sso-saml.md`](../user-guide/sso-saml.md).

Für die SAML-Anmeldung je Statusseite gibt es einen Mock-IdP im Repo
(`backend/cmd/mocksamlidp`, Paket `backend/mocksamlidp`, auf Basis von `crewjam/saml`). Er kennt
jeden Service Provider, dessen ACS-URL mit `MOCK_SAML_ACS_PREFIX` beginnt (Standard
`http://localhost:8080/api/auth/saml/`), braucht also keine Registrierung je Statusseite. Schlüssel
und Zertifikat erzeugt er bei jedem Start neu.

1. Mock starten, entweder im Compose-Profil `sso`: `docker compose --profile sso up -d mock-saml`,
   oder direkt: `cd backend && go run ./cmd/mocksamlidp`
   (Metadaten: <http://localhost:8091/metadata>).
2. Backend wie gewohnt starten (z. B. mit `SEED_DB="true"`). Eine Ausnahme wie
   `OIDC_DEV_INSECURE_ISSUERS` braucht es nicht: Das Backend lädt den Mock nicht selbst.
3. Statusseite konfigurieren: im Admin-Bereich „System → SSO“, Abschnitt „SAML 2.0“, den Inhalt von
   <http://localhost:8091/metadata> in „IdP-Metadaten (XML)“ einfügen (eine Metadaten-URL nimmt das
   Backend nur per https an), „SAML-Anmeldung aktivieren“, speichern. Attribute `email`/`name`
   (Standard) passen zum Mock. Nach einem Neustart des Mocks die Metadaten neu einfügen (neuer
   Schlüssel).
4. Verknüpfen und Anmelden wie bei OIDC: Profil → „Mit SSO verknüpfen“ (Eintrag „SAML“), danach auf
   der Statusseite „Anmelden“ → „Mit SSO anmelden (SAML)“. Im Formular des Mocks wird „username“
   zur persistenten NameID (bei jeder Anmeldung gleich wählen), E-Mail und Name gehen als Attribute
   mit. Das Formular innerhalb von 90 Sekunden absenden, sonst gilt der AuthnRequest als abgelaufen.

Der Playwright-Spec `frontend/e2e/sso-saml.spec.js` spielt das über die Oberfläche durch und
überspringt sich ohne Mock: `cd frontend && npx playwright test sso-saml` (andere Adresse über
`MOCK_SAML_URL`). Mit `E2E_REQUIRE_SSO_MOCK=1` ist der Mock Pflicht; so läuft der Spec im selben
Nachtjob `.github/workflows/e2e-sso-nachts.yml` wie der OIDC-Spec.
