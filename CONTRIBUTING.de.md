# Mitwirken

[English](CONTRIBUTING.md) · Deutsch

Danke fürs Mitmachen! Fehlermeldungen, Ideen und Pull Requests sind willkommen. Melde Sicherheitsprobleme
bitte privat, siehe [SECURITY.md](SECURITY.md), und beachte den [Code of Conduct](CODE_OF_CONDUCT.md).
Mit deinem Beitrag stimmst du zu, dass er unter der [MIT-Lizenz](LICENSE) steht.

## Einrichtung

Siehe den Schnellstart in der [README](README.de.md). Aufbau des Repositorys:

- `backend/`: Go-API (Gin, GORM; PostgreSQL im Betrieb, SQLite im Speicher in den Tests). `main.go`
  registriert alle Routen, das Schema migriert `database.InitDB()`.
- `frontend/`: Vue-3-Anwendung (Vite, vue-i18n, sit-onyx). Tests mit Vitest (`npm test`);
  Komponententests stubben `global.fetch` und nutzen `src/test-support/onyxStubs.js`.
- `docs/`: Benutzerhandbuch, SSO-Anleitungen und OpenAPI-Spezifikation; die Hilfe in der Anwendung
  liegt in `frontend/src/docs`.
- `scripts/`: Entwicklungsskripte hinter `./start.sh`.

## Ablauf

1. Eröffne bei größeren Änderungen zuerst ein Issue, damit wir den Weg abstimmen.
2. Lege einen Branch an und committe in kleinen, fokussierten Schritten. Kommentare im Code und
   Commit-Nachrichten sind üblicherweise deutsch; für Pull Requests und Issues ist Englisch in Ordnung.
3. Schreibe möglichst zuerst den Test (rot, dann grün) und ergänze Tests für Fehlerpfade.
4. Führe die Prüfungen unten aus und öffne dann den Pull Request (ein Entwurf ist in Ordnung).

## Prüfungen vor dem Pull Request

```bash
(cd backend && go vet ./... && go test ./...)
(cd backend && golangci-lint run --config .golangci.yml)   # golangci-lint v2, gebaut mit der Go-Version aus go.mod
(cd frontend && npm ci && npm run lint && npm test)
(cd backend && go run ./cmd/workflowlint ../.github/workflows)  # bei Änderungen an Workflows
```

Einige Tests nutzen Dateirechte (`chmod 0444`) und schlagen als root fehl; führe die Suite als normaler
Nutzer aus. Der PostgreSQL-Migrationstest startet über `embedded-postgres` ein echtes PostgreSQL 16;
schalte ihn mit `MIGRATION_PG_TEST=1 go test ./database/ -run Postgres` ein (als normaler Nutzer, er
lädt Binärdateien).

## Coverage- und Mutations-Gate

Die CI prüft nur den Code, den dein Pull Request ändert: Jede geänderte Zeile muss ausgeführt und
jeder Zweig darauf durchlaufen sein (`backend/cmd/coveragegate check`), und die Gesamtquoten dürfen
nicht unter `coverage-baseline.json` fallen. Steigen sie, hebe die Baseline im selben Pull Request an.
Lokal aus der Repo-Wurzel, Ziel-Branch `main`:

```bash
git diff -U0 --no-color origin/main...HEAD > /tmp/pr.diff
(cd frontend && npm run test:coverage)
(cd backend && go test ./... -coverpkg=./... -coverprofile=coverage.out)
(cd backend && go run ./cmd/coveragegate check -diff /tmp/pr.diff -go-profile coverage.out \
  -lcov ../frontend/coverage/lcov.info -baseline ../coverage-baseline.json -frontend-tolerance 1.5)
# Baseline anheben:
(cd backend && go run ./cmd/coveragegate baseline -go-profile coverage.out \
  -lcov ../frontend/coverage/lcov.info -out ../coverage-baseline.json)
```

Die Frontend-Quoten (V8) schwanken zwischen identischen Läufen um etwa einen Punkt, deshalb die
ausdrückliche Toleranz. Ohne `MIGRATION_PG_TEST=1` fehlt `InitDB()` im Go-Profil und die Go-Quote liegt
unter der Baseline.

Mutationstests laufen nachts (`.github/workflows/mutation-nachts.yml`): Backend mit
[gremlins](https://github.com/go-gremlins/gremlins) (`backend/.gremlins.yaml`), Frontend mit StrykerJS
(`frontend/stryker.config.json`). Hinweise zu gremlins:

- Auf der Modulwurzel (`.`) laufen lassen, nie auf einem Unterpfad, und nur mit komplett grüner Go-Suite.
- Bedingungen in `switch { case a == b: }` ordnet es keinem Coverage-Block zu; schreibe stattdessen
  `if` oder `switch x { case 'a': }`.
- `package main` unter `cmd/` ist ausgeschlossen; Logik gehört in ein eigenes Paket, `main.go` bleibt
  ein Einzeiler.
- Äquivalente Frontend-Mutanten mit `// Stryker disable next-line <mutator>: <Begründung>` markieren.

## Konventionen

- **Neues Model:** in ALLE AutoMigrate-Listen eintragen (`backend/database/db.go`,
  `backend/testutils/setup.go` und lokale Test-Setups wie `backend/database/db_test.go`).
- **Partielle Unique-Indizes** (Soft-Delete, „genau ein Default“) kann GORM AutoMigrate nicht; lege sie
  per Raw-SQL in Helfern in `database/db.go` an (Muster: `createJoinTableUniqueIndexes`,
  `createTagUniqueIndexes`, `createLanguageUniqueIndexes`) und trage Join-Tabellen in `joinTables` ein.
- **Backfill- und Seed-Funktionen** in `database/` müssen idempotent sein und Admin-Änderungen überleben
  (Muster: `seedLanguageCatalog`, `backfillStatusPageLanguages`). Ergänze einen Test, der sie zweimal
  ausführt.
- **Mandantentrennung:** Jede Abfrage und jeder Schreibzugriff ist auf die `status_page_id` der Seite
  begrenzt; IDs aus dem Request werden gegen die Seite geprüft. Nie Nutzer-Objekte ausliefern, nur DTOs
  mit fester Schlüsselliste.
- **Neue Funktion:** Hilfeseite auf Deutsch und Englisch (`frontend/src/docs/de` und `/en`), Eintrag in
  `backend/database/system_capabilities.go` (sonst schlägt ein Test an), Route in
  `docs/api/openapi.yaml`, Texte in `de.json` und `en.json` und eine Übersetzung für neue Fehlercodes.
- **Neue Admin-Route:** auch den Stub-Router in `frontend/src/layouts/AdminLayout.test.js` ergänzen.
- **Admin-Seiten** nutzen die gemeinsamen Bausteine (`PageHeader`, `ListCard`, `FormPanel`, `RowMenu`,
  …); keine seitenlokalen Varianten bauen.
- **Design-Tokens:** Alle Farben kommen aus `frontend/src/assets/tokens.css`. `src/hexColors.test.js`
  ist eine Ratsche gegen neue Hex-Farben, `src/tokenContrast.test.js` prüft WCAG AA.
- **Deutsche Texte** duzen („du“); Tests in `frontend/src/i18n/anrede.test.js` und
  `backend/handlers/anrede_test.go` sichern das ab.
- **SSO-Anleitungen** gibt es auf Deutsch (`docs/user-guide`, `docs/sso`) und Englisch (`en/`); ändere
  beide.
- **Logging:** Nie Geheimnisse loggen. Nutze `logging.New`/`logging.Init`, keinen rohen `slog`-Handler.
  Neue Log-Aufrufe mit HTTP-Fehlern, SQL oder Nutzereingaben brauchen einen Test mit einem Geheimnis als
  Eingabe, der prüft, dass es nicht im Log-Puffer steht. Das Access-Log enthält nur den Pfad, nie
  Query-String, Header oder Body.
- **Formatierte Texte** sind eine eingeschränkte Markdown-Teilmenge (`backend/richtext`,
  `frontend/src/utils/richText.js`); neue Speicherstellen für Nachrichten brauchen dieselbe
  Längenprüfung (`requireMessageLength`).
- **Workflows:** nur GitHub-gehostete Runner, jede fremde Action per Commit-SHA gepinnt, ausdrückliche
  `permissions` an jedem Job und `persist-credentials: false` an jedem Checkout
  (`backend/cmd/workflowlint` erzwingt das alles).
- **Geheimnisse im Repository:** Die CI führt gitleaks aus (`.gitleaks.toml`). Die Allowlist unterdrückt
  nur genau die bekannten Fehlalarme (verankerte Muster auf den Fund selbst, nie auf die ganze Zeile);
  Testwerte lieber mit niedriger Entropie erfinden, statt sie zu erweitern.

## Drittlizenzen

Wenn du eine Abhängigkeit hinzufügst oder aktualisierst, erzeuge `THIRD_PARTY_NOTICES.md` neu; die
Befehle stehen am Anfang von `scripts/third_party_notices.py`.
