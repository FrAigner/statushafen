# Statushafen

Deutsch · [English](README.md)

Statushafen ist eine selbst betreibbare Plattform für **Statusseiten**. Unternehmen zeigen darauf, ob
ihre Dienste laufen, melden Störungen, kündigen Wartungen an und benachrichtigen Abonnenten per
E-Mail oder Google Chat. Eine Installation bedient viele Statusseiten (Mandanten), jede mit eigener
Adresse, eigenem Aussehen, eigenen Komponenten und Rechten.

Go (Gin, GORM) · Vue 3 (Vite, sit-onyx) · PostgreSQL · [MIT-Lizenz](LICENSE)

## Funktionen

- **Statusseiten je Mandant:** Subdomain (`acme.example.com`), Pfad (`/status/acme`) oder eigene
  Domain mit automatischer TLS-Prüfung, private Seiten für angemeldete Nutzer, Gestaltung je Seite.
- **Komponenten, Tags und Gruppen** mit Verfügbarkeitsansicht der letzten 90 Tage.
- **Vorfälle** mit Status-Verlauf, Updates und Post-Mortems sowie **Wartungsfenster**, die
  automatisch beginnen und enden; allgemeine **Ankündigungen**.
- **Externe Quellen:** andere öffentliche Statusseiten einbinden (Summary-API v2) und ihren Zustand
  in eigene Komponenten einfließen lassen.
- **Abonnenten:** E-Mail mit Double-Opt-in oder Google-Chat-Webhook, anpassbare Mailvorlagen je
  Sprache und Zielgruppe.
- **Rollen und Rechte**, Audit-Log, Single Sign-on je Seite mit OIDC und SAML 2.0.
- **Mehrsprachig** (30 Sprachen im Katalog) für Oberfläche und Inhalte, optional automatische
  Übersetzung (LibreTranslate oder DeepL).
- **Formatierte Texte** in einer eingeschränkten Markdown-Teilmenge, sicher dargestellt in Seiten
  und Mails.
- **Selbstüberwachung** mit Systemseite, Prometheus-Metriken, strukturierte Logs ohne Geheimnisse.
- Dokumentierte REST-API ([OpenAPI](docs/api/openapi.yaml)) und Hilfe in der Anwendung.

## Schnellstart (Entwicklung)

Voraussetzungen: Go (Version aus `backend/go.mod`), Node.js 22.12+ oder 24+ (CI, Docker-Image und `frontend/.nvmrc` nutzen 24), Docker.

```bash
cp backend/.env.example backend/.env      # für Demodaten SEED_DB="true" setzen
cp frontend/.env.example frontend/.env
docker compose up -d db mailpit           # PostgreSQL und lokaler Mailfänger (Oberfläche auf :8025); nur an 127.0.0.1 gebunden
./start.sh                                # Backend auf :8080, Frontend auf :5173
./start.sh status                         # zeigt, was läuft
./stop.sh
```

`./start.sh` erzeugt beim ersten Start ein zufälliges `JWT_SECRET` (`backend/.jwt-secret.local`, von Git ignoriert),
wenn keines gesetzt ist; das Backend startet nicht mit fehlendem, kurzem oder Platzhalter-Geheimnis.

Mit `SEED_DB="true"` wird die Datenbank zurückgesetzt (Tabellen werden gelöscht!) und mit Demodaten
gefüllt. Das gilt nur mit `APP_ENV=development`, `dev`, `test` oder `local` (`./start.sh` setzt
standardmäßig `development`); bei jedem anderen oder leeren `APP_ENV` ignoriert das Backend `SEED_DB`
und schreibt eine Warnung ins Log. Die Demodaten enthalten drei Konten: `admin@statushafen.local` / `admin123`, `editor@statushafen.local` / `editor123`
und `viewer@statushafen.local` / `viewer123`. Nur für die lokale Entwicklung verwenden.

## Konfiguration

Das Backend liest Umgebungsvariablen (eine vorhandene Datei `backend/.env` wird geladen). Alle
Optionen mit Erklärung stehen in [`backend/.env.example`](backend/.env.example).

| Variable | Zweck |
|---|---|
| `DB_DSN` oder `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_SSL_MODE` | PostgreSQL-Verbindung |
| `JWT_SECRET` | **Pflicht.** Signaturschlüssel der Login-Tokens: mindestens 32 zufällige Zeichen (`openssl rand -base64 48`). Das Backend startet nicht mit leerem, kürzerem oder bekanntem Platzhalterwert. `./start.sh` erzeugt für die lokale Entwicklung selbst eines (`backend/.jwt-secret.local`, von Git ignoriert) |
| `APP_ENV` | `production` im Produktivbetrieb (JSON-Logs, keine Entwicklungsausnahmen); Seeding (`SEED_DB`), `OIDC_DEV_INSECURE_ISSUERS` und der Domain-Check ohne Token nur mit `development`, `dev`, `test` oder `local` |
| `PORT` | Port (Standard `8080`) |
| `FRONTEND_URL`, `BASE_URL`, `CORS_ORIGINS` | Öffentliche Adressen und erlaubte Origins |
| `TRUSTED_PROXIES` | Kommagetrennte Proxy-IPs/CIDRs, deren `X-Forwarded-For` vertraut wird (Standard: keine). Nur den eigenen Reverse-Proxy eintragen: Netze weiter als /8 (IPv4) bzw. /16 (IPv6), auch `0.0.0.0/0` und `::/0`, lassen jeden Client seine Adresse fälschen und heben alle IP-Limits auf; das Backend startet damit nicht, außer `APP_ENV` ist `development`, `dev`, `test` oder `local` (dann nur Warnung) |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM` | Ausgehende Mails (Standard: lokales Mailpit). `SMTP_FROM` ist die Absenderadresse aller Mails; Statusseiten ergänzen nur ihren Namen und optional eine Antwortadresse |
| `SSO_ALLOWED_DOMAINS`, `SSO_BOOTSTRAP_ADMIN_EMAILS`, `GOOGLE_*`, `GITHUB_*` | Plattform-Login über Google/GitHub (mit PKCE; das State-Cookie ist mit `JWT_SECRET` signiert, 20 Anfragen pro Minute und IP) und Start-Admins |
| `CUSTOM_DOMAIN_CNAME_TARGET`, `CADDY_INTERNAL_TOKEN` | Eigene Domains (siehe unten) |
| `METRICS_BASIC_AUTH_USER`, `METRICS_BASIC_AUTH_PASSWORD` | Schalten `/metrics` ein (aus, solange nicht beide gesetzt sind) |
| `LOG_FORMAT`, `LOG_LEVEL` | Logging (`json`/`text`, `debug` bis `error`) |
| `LIBRETRANSLATE_URL`, `LIBRETRANSLATE_API_KEY` | Standard-Übersetzungsdienst |
| `EXTERNAL_STATUS_POLLING_ENABLED` | Hintergrundabruf externer Quellen (Standard an) |
| `SELFMONITOR_BACKUP_JOB`, `SELFMONITOR_METRICS_DIR` | Optionale Backup-Alters-Prüfung der Selbstüberwachung |
| `OIDC_DEV_INSECURE_ISSUERS` | Nur Entwicklung (`APP_ENV` `development`, `dev`, `test` oder `local`): erlaubt einen OIDC-Mock ohne TLS |

Frontend (Build-Zeit, `frontend/.env`): `VITE_API_URL` und `VITE_APP_BASE_DOMAINS` (kommagetrennte
Basisdomains; `<slug>.<Basisdomain>` ist dann eine Mandantenadresse. Ohne Angabe sind Seiten unter
`/status/<slug>` oder über eine eigene Domain erreichbar).

## Betrieb

Das [`Dockerfile`](Dockerfile) baut ein Image mit API und gebautem Frontend (Port 8080). Betreibe es
hinter einem Reverse-Proxy mit TLS und einer PostgreSQL-Datenbank.

Checkliste für die Produktion:

- `APP_ENV=production`, ein starkes `JWT_SECRET` und niemals `SEED_DB` setzen.
- Den ersten Admin ohne SSO anlegen: den Befehl einmal gegen die Produktionsdatenbank ausführen
  (das Passwort kommt nur über stdin, nie als Argument; mindestens 8 Zeichen). Ein vorhandenes Konto wird nie
  verändert, und der Befehl läuft nicht, solange `SEED_DB` gesetzt ist:

  ```bash
  read -rs ADMIN_PW && printf '%s\n' "$ADMIN_PW" | ./server create-admin --email du@example.org --password-stdin; unset ADMIN_PW
  ```

  In Docker: `docker exec -i <Container> ./server create-admin --email du@example.org --password-stdin`.
  Alternativ geht auch `SSO_BOOTSTRAP_ADMIN_EMAILS` (E-Mail-Adressen, die beim nächsten Plattform-Login über
  Google oder GitHub Global Admin werden).
- `FRONTEND_URL`, `CORS_ORIGINS`, `TRUSTED_PROXIES` und die SMTP-Einstellungen setzen. Das Backend sendet
  `Strict-Transport-Security`, wenn die Anfrage von einem Proxy aus `TRUSTED_PROXIES` kommt, der
  `X-Forwarded-Proto: https` setzt (von jedem anderen Absender wird der Header ignoriert).
- Für Mandanten-Subdomains einen Wildcard-DNS-Eintrag und ein Wildcard-Zertifikat auf den Proxy
  zeigen lassen und `VITE_APP_BASE_DOMAINS` beim Build setzen.
- Für **eigene Domains** holt der Proxy Zertifikate bei Bedarf und fragt vorher
  `GET /api/internal/custom-domain-check?domain=<Host>` (Header `X-Internal-Token` =
  `CADDY_INTERNAL_TOKEN`), ob der Host erlaubt ist. Kunden legen einen CNAME auf
  `CUSTOM_DOMAIN_CNAME_TARGET` (Standard `edge.<Host aus FRONTEND_URL>`) und einen TXT-Eintrag zur
  Bestätigung an. **`CADDY_INTERNAL_TOKEN` ist Pflicht**, sobald `APP_ENV` nicht `development`,
  `dev`, `test` oder `local` ist: Ohne Token antwortet der Endpunkt immer mit 503 (Warnung beim Start), und
  eigene Domains erhalten kein Zertifikat. Der „ask“-Aufruf von Caddy kann selbst keine Header senden: Er muss über einen
  internen Zwischenschritt laufen (zum Beispiel ein kleiner Proxy im Docker-Netz), der
  `X-Internal-Token` mit demselben Secret setzt, das die Anwendung kennt. Der
  Wechsel einer Domain von „Verifiziert“ auf „Aktiv“ passiert bei dieser Anfrage von Caddy, nicht mehr
  beim öffentlichen Aufruf der Seite. Eigene Domains privater Seiten lösen für Nicht-Mitglieder nicht
  mehr zu einem Seitennamen auf (sie sehen die Startseite). Der Endpunkt ist je IP auf 120 Anfragen
  pro Minute begrenzt (danach 429), Caddy fragt nur für Hosts ohne Zertifikat.
- Die Datenbank regelmäßig sichern: [`docs/backup-restore.md`](docs/backup-restore.md).

## Dokumentation

- Hilfe in der Anwendung (Deutsch und Englisch) unter `/docs`, Quellen in [`frontend/src/docs`](frontend/src/docs).
- [Benutzerhandbuch](docs/user-guide), [Anleitungen zum Single Sign-on](docs/sso) (Deutsch, Englisch
  unter `docs/sso/en`), [REST-API](docs/api).
- [Backup und Restore](docs/backup-restore.md).

## Tests und Qualität

```bash
(cd backend && go test ./...)             # Unit-Tests (SQLite im Speicher)
(cd frontend && npm ci && npm test)       # Komponenten- und Unit-Tests
(cd frontend && npx playwright test)      # Ende-zu-Ende-Tests (Backend und Frontend laufen)
```

Beiträge sind willkommen: siehe [CONTRIBUTING.md](CONTRIBUTING.md). Sicherheitsmeldungen:
[SECURITY.md](SECURITY.md).

### Lokale Tests des Single Sign-on

`docker compose --profile sso up -d mock-oidc mock-saml` startet einen OIDC- und einen
SAML-Mock-Anbieter; die Anleitung steht in [`docs/sso/local-testing.md`](docs/sso/local-testing.md).
Die Playwright-Specs `sso-oidc` und `sso-saml` spielen den ganzen Ablauf durch und laufen auch jede
Nacht in [`.github/workflows/e2e-sso-nachts.yml`](.github/workflows/e2e-sso-nachts.yml).

## Lizenz, Name und Drittsoftware

Statushafen steht unter der [MIT-Lizenz](LICENSE). Den Namen „Statushafen“ darfst du für unveränderte
Versionen und mit einem Hinweis wie „basiert auf Statushafen“ verwenden, siehe
[TRADEMARK.md](TRADEMARK.md). Drittkomponenten und Schriften (SIL Open Font License) sind in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) aufgeführt.

Externe Quellen nutzen die Summary-API v2 (`/api/v2/summary.json`), die viele öffentliche
Statusseiten anbieten, darunter Seiten auf Basis von Atlassian Statuspage. Atlassian und Statuspage
sind Marken der Atlassian Pty Ltd; dieses Projekt steht in keiner Verbindung zu Atlassian.
