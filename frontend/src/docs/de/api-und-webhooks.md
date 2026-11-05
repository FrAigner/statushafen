# API und Webhooks für Monitoring

## Status per Webhook setzen

Ein Monitoring-System (z. B. Uptime Kuma, Prometheus Alertmanager oder ein eigenes Skript) kann
den Status einer Komponente direkt setzen:

```text
POST /api/webhooks/components/<Komponenten-ID>/status
```

### Token erzeugen

1. Im Admin-Bereich **Statusseiten** öffnen (Global Admin) und die Seite bearbeiten.
2. Im Abschnitt „Monitoring-Webhook“ auf „Token erzeugen“ klicken.
3. Den Token sofort kopieren: Er wird nur dieses eine Mal angezeigt.

Es gibt einen Token je Statusseite, er gilt für alle ihre Komponenten. „Neu erzeugen“ macht den
alten Token sofort ungültig, „Token widerrufen“ sperrt den Webhook für die Seite.
Gespeichert wird nur ein Hash des Tokens, nie der Klartext: Ein verlorener Token lässt sich nicht
wieder anzeigen, dann einfach einen neuen erzeugen. Bestehende Token bleiben gültig und werden
beim Update automatisch auf den Hash umgestellt.

Die Komponenten-ID steht unter **Komponenten** in der Spalte „ID“.

### Anfrage

- Header `X-Webhook-Token: <token>` (alternativ `?token=<token>` in der URL; das landet aber in
  Proxy-Logs und ist deshalb nicht empfohlen).
- JSON-Body mit dem neuen Status: `operational`, `degraded_performance`, `partial_outage`,
  `major_outage` oder `under_maintenance`.

Beispiel:

```bash
curl -X POST "https://acme.example.com/api/webhooks/components/42/status" \
  -H "X-Webhook-Token: <token>" \
  -H "Content-Type: application/json" \
  -d '{"status": "major_outage"}'
```

### Per Kennung statt ID

Alternativ lässt sich die Komponente über ihre Kennung (Slug, Spalte „Kennung“ unter **Komponenten**) ansprechen. Die Statusseite ergibt sich dabei allein aus dem Token; die Kennung wird nur innerhalb dieser Seite aufgelöst. Dieselbe Kennung auf einer anderen Seite ist davon unabhängig.

```text
POST /api/webhooks/component-slugs/<Kennung>/status
```

Token, Body, Limit (20 pro Minute und IP), Statushistorie und Audit-Log sind identisch zum ID-Weg. Archivierte Komponenten bleiben ansprechbar, gelöschte nicht. Ist der Token ungültig, antwortet der Server immer mit 401, auch wenn die Kennung nicht existiert; eine unbekannte Kennung (oder eine Kennung einer anderen Seite) ergibt bei gültigem Token 404.

```bash
curl -X POST "https://acme.example.com/api/webhooks/component-slugs/api/status" \
  -H "X-Webhook-Token: <token>" \
  -H "Content-Type: application/json" \
  -d '{"status": "major_outage"}'
```

### Antworten

| Code | Bedeutung |
|---|---|
| 200 | Status gesetzt, Antwort enthält die Komponente |
| 400 | ungültige ID, ungültiger Body oder unbekannter Status |
| 401 | Token fehlt oder ist falsch, oder die Komponente existiert nicht (beides antwortet identisch, damit sich IDs nicht abtasten lassen) |
| 404 | nur beim Weg über die Kennung: Kennung bei gültigem Token nicht gefunden |
| 429 | mehr als 20 Aufrufe pro Minute von derselben IP |

Jede Änderung erscheint im [Audit-Log](/docs/audit-log) als `component.status_webhook_updated`.

Der Status gilt mit jedem Aufruf sofort. In der Statushistorie (und damit der Verfügbarkeit) werden
Wechsel derselben Komponente innerhalb von 30 Sekunden zu einem Eintrag zusammengefasst, damit
fehlerhaft flatternde Monitore die Datenbank nicht füllen. Ein Rate-Limit pro Token gibt es noch nicht.

## Weitere API

Die Oberfläche von Statushafen nutzt eine REST-API unter `/api`. Öffentlich lesbar sind zum
Beispiel:

| Endpunkt | Inhalt |
|---|---|
| `GET /api/public/pages` | öffentliche Statusseiten |
| `GET /api/public/pages/<slug>/dashboard` | Daten einer Statusseite (Komponenten, Vorfälle, Ankündigungen, externe Meldungen) |
| `GET /api/public/pages/<slug>/availability` | Verfügbarkeit der letzten 90 Tage als Tagesaggregate; `?include=components` ergänzt die Werte je Komponente |
| `GET /api/pages/<Seiten-ID>/components` | Komponenten einer Seite |
| `GET /api/pages/<Seiten-ID>/incidents` | Vorfälle einer Seite |
| `GET /api/pages/<Seiten-ID>/notices` | Ankündigungen einer Seite |
| `POST /api/subscribe` | Abo anlegen |

Private Seiten liefern ihre Daten nur mit Anmeldung.

Diese Lese-Routen sind je IP-Adresse auf 120 Aufrufe pro Minute begrenzt, danach antwortet der Server
mit `429`. `GET /api/public/pages` liefert höchstens 200 Seiten je Aufruf (`?limit=` und `?offset=`
zum Blättern, nach ID sortiert); Name (120 Zeichen) und Beschreibung (500 Zeichen) sind dort gekürzt.

### Anmeldung an der API

Ein Login-Token holt man mit `POST /api/login` (E-Mail und Passwort); es gilt zwei Stunden. Man
schickt es im Header `X-Auth-Token: <token>` mit. Der Header `Authorization: Bearer <token>`
wird weiter akzeptiert; ist `X-Auth-Token` gesetzt, hat er Vorrang, auch wenn das Token ungültig
ist. Der eigene Header ist nötig, weil ein vorgeschalteter Basic-Auth-Schutz (z. B. auf einer
Staging-Instanz) den Header `Authorization` belegt.

```bash
curl "https://acme.example.com/api/me" -H "X-Auth-Token: <token>"
```

Welche Rechte ein Endpunkt braucht, steht in der OpenAPI-Datei (`x-required-permission`).
Dieselben Rechte wie in der Oberfläche gelten, siehe
[Benutzer, Rollen und Rechte](/docs/benutzer-und-rollen#rechte). Für Gruppen schickt man beim
Anlegen oder Ändern einer Komponente `parent_id` (die ID der übergeordneten Gruppe); beim
Anlegen macht `is_group: true` die neue Komponente zu einer Gruppe.

### Listen und Limits

Die Vorfall-Liste (`GET /api/pages/<id>/incidents`) liefert höchstens 200 Einträge je Aufruf, ohne
`limit` die neuesten 50. Mit `offset` blättert man weiter zurück, `state=open|resolved` filtert nach
Status und `is_maintenance=any` schließt Wartungen ein. Das Dashboard enthält alle offenen und die
neuesten behobenen Vorfälle; `incidents_more: true` zeigt, dass es ältere gibt. Lesende Aufrufe von
Dashboard und Vorfällen sind auf 120 pro Minute und IP begrenzt. Damit das Limit je Besucher gilt, muss
hinter einem Reverse-Proxy `TRUSTED_PROXIES` gesetzt sein. Schreibende Aufrufe, die Benachrichtigungen
auslösen, sind je Seite und Nutzer begrenzt, siehe [Vorfälle](/docs/vorfaelle#benachrichtigungen).

### Fehlerantworten

Fehler haben die Form `{"error": "<Meldung>", "code": "<Code>"}`. Der **Code** ist maschinenlesbar
und stabil, die Meldung ist ein Text für Menschen und kann sich ändern. Die Oberfläche übersetzt
den Code in die gewählte Sprache. Häufige Codes:

| Code | HTTP | Bedeutung |
|---|---|---|
| `invalid_request` | 400 | Eingabe unvollständig oder ungültig |
| `invalid_id` | 400 | ungültige Kennung im Pfad oder Body |
| `input_too_long` | 400 | Eingabe überschreitet die Obergrenze (Abo: E-Mail 254, Webhook-URL 2048, Komponentenliste 8192, Sprache 16 Byte) |
| `input_invalid_characters` | 400 | Eingabe enthält Steuerzeichen (auch NUL-Byte) oder ungültiges UTF-8; NUL und ungültiges UTF-8 in Body, Query und Pfad weist der Server bei jeder Route ab |
| `forbidden` | 403 | Recht fehlt |
| `page_not_found` | 404 | Statusseite nicht gefunden |
| `slug_taken` | 409 | Slug wird von einer anderen Seite verwendet |
| `slug_reserved` | 400 | Slug ist reserviert, siehe [Statusseiten](/docs/statusseiten#regeln-fuer-den-slug) |
| `password_too_short` | 400 | Passwort kürzer als 8 Zeichen |
| `language_code_invalid` | 400 | Sprachcode hat ein ungültiges Format, siehe [Sprachen](/docs/sprachen) |
| `translation_not_configured` | 503 | Keine automatische Übersetzung eingerichtet, siehe [Systemeinstellungen](/docs/systemeinstellungen) |
| `translation_unavailable` | 502 | Übersetzungsdienst nicht erreichbar |
| `translation_auth_failed` | 502 | Übersetzungsdienst lehnt den Schlüssel ab |
| `translation_quota_exceeded` | 429 | Kontingent des Übersetzungsdienstes erreicht |
| `translation_language_unsupported` | 422 | Sprachpaar vom Dienst nicht unterstützt |
| `translation_text_too_long` | 400 | Mehr als 5000 Zeichen zum Übersetzen |
| `group_not_empty` | 409 | Gruppe enthält noch Komponenten und lässt sich nicht löschen |
| `server_error` | 500 | Fehler auf dem Server |

Nicht jeder Fehler trägt schon einen Code. Dazu kommt `429` bei zu vielen Aufrufen (mit Header
`Retry-After`).

### OpenAPI

Die vollständige Beschreibung aller Endpunkte liegt als OpenAPI-Datei im Repository unter
`docs/api/openapi.yaml`. Ein Test gleicht sie bei jedem Build mit den tatsächlich registrierten
Routen ab; sie enthält auch die Endpunkte für externe Quellen, die Seiten-Tags und die
Verfügbarkeit.
