# Contributing

[Deutsch](CONTRIBUTING.de.md) · English

Thanks for helping! Bug reports, ideas and pull requests are welcome. Please report security
problems privately, see [SECURITY.md](SECURITY.md), and follow the [Code of Conduct](CODE_OF_CONDUCT.md).
By contributing you agree that your contribution is released under the [MIT License](LICENSE).

## Setup

See the quick start in the [README](README.md). Layout of the repository:

- `backend/` is the Go API (Gin, GORM; PostgreSQL in production, SQLite in memory in tests).
  `main.go` registers all routes; the schema is migrated by `database.InitDB()`.
- `frontend/` is the Vue 3 app (Vite, vue-i18n, sit-onyx). Tests use Vitest (`npm test`); component
  tests stub `global.fetch` and use `src/test-support/onyxStubs.js`.
- `docs/` holds the user guide, the SSO guides and the OpenAPI specification; the in-app help lives in
  `frontend/src/docs`.
- `scripts/` holds the development scripts behind `./start.sh`.

## Workflow

1. Open an issue for larger changes first, so we can agree on the approach.
2. Create a branch, make small focused commits. Code comments and commit messages are written in
   German by convention; English is fine for pull requests and issues.
3. Write the test first when you can (red, then green) and include tests for error paths.
4. Run the checks below, then open a pull request (draft is fine).

## Checks before a pull request

```bash
(cd backend && go vet ./... && go test ./...)
(cd backend && golangci-lint run --config .golangci.yml)   # golangci-lint v2, built with the Go version of go.mod
(cd frontend && npm ci && npm run lint && npm test)
(cd backend && go run ./cmd/workflowlint ../.github/workflows)  # when you change workflows
```

Some tests rely on file permissions (`chmod 0444`) and fail when run as root; run the suite as a normal
user. The PostgreSQL migration test starts a real PostgreSQL 16 through `embedded-postgres`; enable it
with `MIGRATION_PG_TEST=1 go test ./database/ -run Postgres` (as a normal user, it downloads binaries).

## Coverage and mutation gate (Coverage- und Mutations-Gate)

The CI checks only the code your pull request changes: every changed line must be executed and every
branch on it taken (`backend/cmd/coveragegate check`), and the overall rates must not fall below
`coverage-baseline.json`. If they rise, raise the baseline in the same pull request. Locally, from the
repository root with the target branch `main`:

```bash
git diff -U0 --no-color origin/main...HEAD > /tmp/pr.diff
(cd frontend && npm run test:coverage)
(cd backend && go test ./... -coverpkg=./... -coverprofile=coverage.out)
(cd backend && go run ./cmd/coveragegate check -diff /tmp/pr.diff -go-profile coverage.out \
  -lcov ../frontend/coverage/lcov.info -baseline ../coverage-baseline.json -frontend-tolerance 1.5)
# raise the baseline:
(cd backend && go run ./cmd/coveragegate baseline -go-profile coverage.out \
  -lcov ../frontend/coverage/lcov.info -out ../coverage-baseline.json)
```

Frontend rates (V8) vary by about one point between identical runs, hence the explicit tolerance.
Without `MIGRATION_PG_TEST=1` the function `InitDB()` is missing from the Go profile and the Go rate
falls below the baseline.

Mutation tests run nightly (`.github/workflows/mutation-nachts.yml`): backend with
[gremlins](https://github.com/go-gremlins/gremlins) (`backend/.gremlins.yaml`), frontend with StrykerJS
(`frontend/stryker.config.json`). Notes for gremlins:

- Run it on the module root (`.`), never on a subpath, and only with a fully green Go suite.
- Conditions in `switch { case a == b: }` are not assigned to a coverage block; write `if` or
  `switch x { case 'a': }` instead.
- `package main` under `cmd/` is excluded; put logic into its own package and keep `main.go` a one-liner.
- Mark equivalent frontend mutants with `// Stryker disable next-line <mutator>: <reason>`.

## Conventions

- **New model:** add it to every AutoMigrate list (`backend/database/db.go`, `backend/testutils/setup.go`,
  and local test setups such as `backend/database/db_test.go`).
- **Partial unique indexes** (soft delete, "exactly one default") cannot be created by GORM
  AutoMigrate; create them with raw SQL in helpers in `database/db.go` (patterns:
  `createJoinTableUniqueIndexes`, `createTagUniqueIndexes`, `createLanguageUniqueIndexes`) and list
  join tables in `joinTables`.
- **Backfill and seed functions** in `database/` must be idempotent and must survive admin changes
  (patterns: `seedLanguageCatalog`, `backfillStatusPageLanguages`). Add a test that runs them twice.
- **Tenant isolation:** every query and write is limited to the `status_page_id` of the page; IDs from
  the request are checked against the page. Never return user objects, only DTOs with a fixed key list.
- **New feature:** add a help page in German and English (`frontend/src/docs/de` and `/en`), an entry in
  `backend/database/system_capabilities.go` (a test fails otherwise), the route in
  `docs/api/openapi.yaml`, texts in `de.json` and `en.json`, and a translation for new error codes.
- **New admin route:** also extend the stub router in `frontend/src/layouts/AdminLayout.test.js`.
- **Admin pages** use the shared building blocks (`PageHeader`, `ListCard`, `FormPanel`, `RowMenu`, ...);
  do not build page-local variants.
- **Design tokens:** all colors come from `frontend/src/assets/tokens.css`. `src/hexColors.test.js` is a
  ratchet against new hex colors, `src/tokenContrast.test.js` checks WCAG AA.
- **German texts** address the reader informally ("du"); tests in `frontend/src/i18n/anrede.test.js` and
  `backend/handlers/anrede_test.go` guard this.
- **SSO guides** exist in German (`docs/user-guide`, `docs/sso`) and English (`en/`); change both.
- **Logging:** never log secrets. Use `logging.New`/`logging.Init`, not a raw `slog` handler. New log
  calls with HTTP errors, SQL or user input need a test with a secret as input that checks it does not
  appear in the log buffer. The access log contains only the path, never query string, headers or body.
- **Formatted texts** are a restricted Markdown subset (`backend/richtext`, `frontend/src/utils/richText.js`);
  new places that store messages need the same length check (`requireMessageLength`).
- **Workflows:** only GitHub-hosted runners, every third-party action pinned by commit SHA, explicit
  `permissions` on every job and `persist-credentials: false` on every checkout
  (`backend/cmd/workflowlint` enforces all of it).
- **Secrets in the repository:** CI runs gitleaks (`.gitleaks.toml`). The allowlist only suppresses the
  exact known false positives (anchored patterns on the finding itself, never on the whole line); invent
  test values with low entropy instead of extending it.

## Third-party licenses

When you add or update a dependency, regenerate `THIRD_PARTY_NOTICES.md`; the commands are at the top of
`scripts/third_party_notices.py`.
