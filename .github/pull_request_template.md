## Was ändert dieser PR? / What does this PR change?

<!-- Kurz beschreiben, warum die Änderung nötig ist. / Briefly explain why. -->

## Prüfung / Checks

- [ ] Backend: `cd backend && go test ./...` und `golangci-lint run --config .golangci.yml`
- [ ] Frontend: `cd frontend && npm test` und `npm run lint`
- [ ] Neue oder geänderte Funktion hat Tests (auch Fehlerpfade) / New or changed behavior has tests
- [ ] Neue Texte in `de.json` und `en.json`; neue Hilfeseite in `frontend/src/docs/de` und `/en`
- [ ] Neue oder geänderte API in `docs/api/openapi.yaml` nachgezogen
- [ ] Keine Geheimnisse, Zugangsdaten oder personenbezogenen Daten im Diff / No secrets or personal data

## Nicht gelaufen / Not run

<!-- Welche Prüfungen konntest du nicht ausführen (z. B. End-to-End-Tests)? -->
