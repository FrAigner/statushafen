# API-Dokumentation

Die REST-API des Backends ist in [`openapi.yaml`](./openapi.yaml) als
**OpenAPI 3.1** beschrieben. Die Datei ist handgeschrieben:
kein Code-Generator, keine Annotationen an den Handlern, keine zusätzliche
Laufzeit-Abhängigkeit im Backend oder Frontend.

## Was drinsteht

- **Vollständig** (Parameter, Request-/Response-Schemas, Fehlerantworten):
  alles, was Externe nutzen, also die öffentliche Lese-API
  (`/api/public/*`, `/api/pages/{page_id}/…`, Übersetzungen), Login und SSO,
  Abonnieren/Bestätigen/Abbestellen, der Monitoring-Webhook samt
  Webhook-Token, die Seitenbestellung und die Konto-Endpunkte unter `/api/me`.
- **Admin-Endpunkte**: Pfad, Methode, benötigte Berechtigung
  (`x-required-permission`), Kurzbeschreibung und Schemas, soweit sie sich
  eindeutig aus den Models ableiten lassen.
- **Authentifizierung, Berechtigungsmodell, Rate-Limits** (`x-rate-limit`
  an der Operation und als Tabelle) und Konventionen stehen in
  `info.description` am Anfang der Datei.

## Ansehen

Ohne Installation im Projekt, jeweils aus der Repo-Wurzel:

```bash
# Redoc als einzelne HTML-Datei bauen und im Browser öffnen (gut zum Lesen)
npx @redocly/cli@1 build-docs docs/api/openapi.yaml -o /tmp/statushafen-api.html
# oder mit Live-Reload beim Bearbeiten:
npx @redocly/cli@1 preview-docs docs/api/openapi.yaml   # http://localhost:8080

# Swagger UI (mit „Try it out“, z. B. gegen das lokale Backend)
docker run --rm -p 8081:8080 -e SWAGGER_JSON=/spec/openapi.yaml \
  -v "$PWD/docs/api:/spec" swaggerapi/swagger-ui            # http://localhost:8081
```

`preview-docs` belegt Port 8080 wie das Backend; bei laufendem Backend
`--port 8082` anhängen. Für „Try it out“ gegen `http://localhost:8080` muss
`CORS_ORIGINS` den Swagger-UI-Ursprung enthalten.

Einen Login-Token für geschützte Routen liefert `POST /api/login`; er wird
im Header `X-Auth-Token: <token>` mitgeschickt und gilt zwei Stunden
(ein vorgeschalteter Basic-Auth-Schutz belegt `Authorization`).
`Authorization: Bearer <token>` wird weiter angenommen; ist `X-Auth-Token`
gesetzt, hat er Vorrang. In „Try it out“ von Swagger UI das Schema `authToken`
verwenden.

## Pflege: der Drift-Test

`backend/apidoc_test.go` läuft mit jedem `go test ./...` (also auch in der
CI) und schlägt fehl, wenn

1. eine in `backend/main.go` registrierte Route in der Spec fehlt,
2. die Spec eine Route enthält, die es nicht (mehr) gibt,
3. die Datei kein gültiges OpenAPI 3.1 ist (Validierung mit
   [kin-openapi](https://github.com/getkin/kin-openapi), nur als
   Test-Abhängigkeit, inkl. Beispiele gegen ihre Schemas), oder
4. einer Operation `summary` oder eine eindeutige `operationId` fehlt.

gin-Pfadparameter werden dabei normalisiert (`:id` bzw. `*path` →
`{id}`/`{path}`). `/metrics` wird mitgeprüft (der Test setzt die
Basic-Auth-Variablen); die statischen Frontend-Routen (`/assets`, SPA-
Fallback) sind keine API und werden im Testlauf gar nicht registriert.

Wer eine Route hinzufügt, ändert oder entfernt, trägt das im selben PR in
`openapi.yaml` nach. Die Fehlermeldung des Tests nennt Methode und Pfad:

```bash
cd backend && go test -run OpenAPI .
```
