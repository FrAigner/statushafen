# Mail-Templates und Platzhalter

Mail-Templates bestimmen Betreff und Text der Benachrichtigungen einer Statusseite. Sie werden
unter **Mail-Templates** gepflegt (Rechte `notification_templates:read`, `notification_templates:update` und `notification_templates:delete`). Gibt es für ein Ereignis kein eigenes
Template, verschickt Statushafen einen eingebauten Standardtext.

## Aufbau eines Templates

Ein Template gilt für eine Kombination aus **Ereignis**, **Empfängergruppe** und **Sprache**:

- **Empfängergruppe:** „Intern (Staff)“ für Nutzer der Seite oder „Abonnenten (Subscriber)“.
- **Sprache:** eine der für die Seite aktivierten Sprachen, siehe [Sprachen](/docs/sprachen).
- **Betreff** und **Text (HTML)** dürfen nicht leer sein, der Betreff darf keinen Zeilenumbruch
  enthalten.

Die Vorschau rendert das Template mit Beispieldaten. Löschen setzt auf den Standardtext zurück.

## Ereignisse

| Ereignis | Wann |
|---|---|
| Vorfall erstellt | neuer Vorfall |
| Vorfall aktualisiert | Vorfall bearbeitet oder neues Update |
| Vorfall behoben | Update bzw. Änderung auf „Behoben“ |
| Wartungsfenster: Erinnerung | eingestellter Vorlauf vor dem Start |
| Wartungsfenster: Gestartet | Start erreicht |
| Wartungsfenster: Beendet | Ende erreicht |
| Ankündigung: Erstellt / Aktualisiert / Beendet | nur bei „Subscriber per E-Mail benachrichtigen“ |
| Abonnement-Bestätigung (Double-Opt-in) | neues E-Mail-Abo |

## Platzhalter

Platzhalter werden beim Versand ersetzt. Ihre Werte werden automatisch für HTML maskiert. Im
Editor zeigt „Verfügbare Platzhalter“ die Liste; tippt man `{`, schlägt der Editor Platzhalter vor.

| Platzhalter | Inhalt |
|---|---|
| `{{.Title}}` | Titel des Vorfalls, der Wartung oder Ankündigung; bei der Abo-Bestätigung der Name der Statusseite |
| `{{.IncidentTitle}}` | gleicher Wert wie `{{.Title}}` (ältere Schreibweise) |
| `{{.Level}}` | nur Ankündigungen: `info`, `warning` oder `critical` |
| `{{.Message}}` | Beschreibung bzw. Text des Updates als Klartext (Markdown-Quelltext, unformatiert) |
| `{{.MessageHTML}}` | dieselbe Nachricht formatiert als fertiges HTML (fett, Listen, Links), siehe [Formatierte Texte](/docs/formatierte-texte); nur im Mailtext verwenden, nicht in Attributen und nicht im Betreff (dort erschiene das HTML als Text, zulässig ist es technisch aber) |
| `{{.Status}}` | Status als Wert, z. B. `investigating` |
| `{{.StatusLabel}}` | Status als Text, deutsch bei Sprache `de`, sonst englisch |
| `{{.ComponentNames}}` | betroffene Komponenten, durch Komma getrennt |
| `{{.Tags}}` | Tags des Vorfalls |
| `{{.Action}}` | Art des Ereignisses, z. B. `created` oder `updated` |
| `{{.Language}}` | Sprachcode des Templates |
| `{{.FrontendURL}}` | Link zur Statusseite |
| `{{.UnsubscribeURL}}` | persönlicher Abmeldelink (nur Abonnenten) |
| `{{.ConfirmURL}}` | Bestätigungslink (nur Abo-Bestätigung) |
| `{{.SourceName}}` | Name der externen Quelle (nur Meldungen externer Quellen) |
| `{{.SourceURL}}` | Link auf die Originalmeldung (nur Meldungen externer Quellen) |
| `{{.FromAddress}}` | Absender der Mail (Seitenname und Adresse des Servers) |

## Erlaubte Konstrukte und Grenzen

Vorlagen dürfen nur Platzhalter und einfache Bedingungen enthalten: `{{if …}}…{{else}}…{{end}}` mit
den Funktionen `eq`, `ne`, `not`, `and` und `or`, zum Beispiel `{{if eq .Status "resolved"}}…{{end}}`.
Schleifen (`range`), Unterschablonen (`template`, `define`, `block`), Variablen, `printf` und andere
Funktionen werden beim Speichern und in der Vorschau abgelehnt. Der Betreff darf höchstens 500 Zeichen
lang sein, der Inhalt höchstens 64 KB; das gerenderte Ergebnis ist in Größe und Laufzeit begrenzt.
Eine früher gespeicherte Vorlage, die diese Regeln verletzt, wird nicht verwendet; stattdessen
geht der eingebaute Text raus.

Die Vorlage der **Abo-Bestätigung** muss `{{.ConfirmURL}}` enthalten (nicht nur innerhalb einer
Bedingung). Statushafen setzt sie in einen festen Rahmen mit Seitenname und einem Hinweis für
Empfänger, die sich nicht angemeldet haben; der Rahmen lässt sich nicht ändern.

## Beispiel

Betreff:

```text
[{{.StatusLabel}}] {{.Title}}
```

Text:

```html
<p>{{.Message}}</p>
<p>Betroffen: {{.ComponentNames}}</p>
<p><a href="{{.FrontendURL}}">Zur Statusseite</a></p>
<p><a href="{{.UnsubscribeURL}}">Abmelden</a></p>
```

## Meldungen externer Quellen

Für gespiegelte Meldungen [externer Quellen](/docs/externe-quellen#gespiegelte-meldungen) gibt es
sechs weitere Ereignisse (`external_incident_created`, `external_incident_updated`,
`external_incident_resolved` und dieselben drei für Wartungen mit `external_maintenance_…`),
jeweils für die Empfängergruppe „Abonnenten“ in der Standardsprache der Seite. Der Editor bietet
sie unter „Externer Vorfall: …“ und „Externe Wartung: …“ an und stellt die Empfängergruppe dabei
auf „Abonnenten“. Zusätzlich zu den Platzhaltern oben stehen dort `{{.SourceName}}` (Name der Quelle) und `{{.SourceURL}}` (Link auf
die Originalmeldung) zur Verfügung. Ohne eigenes Template verschickt Statushafen einen
eingebauten Text. Die Ausfall-Mails einer Quelle haben immer den eingebauten Text.

## Aktuelle Einschränkung

Beim Abonnieren wird die Sprache des Besuchers als Mailsprache gespeichert, sofern die Seite sie
aktiviert hat (sonst gilt die Standardsprache der Seite). Alle Mails an Abonnenten (Vorfälle,
Wartungen, Ankündigungen, Abo-Bestätigung, Meldungen externer Quellen) nutzen das
Abonnenten-Template in dieser Sprache, ersatzweise in der **Standardsprache der Seite**; fehlt
auch das, geht der eingebaute Standardtext raus (Deutsch bzw. Englisch, bei jeder anderen Sprache
Englisch). Ein Template einer dritten Sprache wird nicht ersatzweise genommen. Die Sprache ändern
Abonnenten im Abo-Manager, Admins im Abonnenten-Formular (oder per API: `PUT /api/me/subscription`, Feld `language`).

Interne Nutzer bekommen Vorfall-Mails in ihrer Oberflächensprache (Deutsch oder Englisch, ohne
gespeicherte Sprache Englisch): zuerst das Staff-Template in dieser Sprache, ersatzweise das
englische, sonst der eingebaute Text.

Die Nachrichten an Google-Chat-Webhooks sind feste Texte ohne Template, auf Deutsch oder Englisch
nach der Sprache des Abonnenten (jede andere Sprache bekommt Englisch); der globale Webhook
(`GOOGLE_CHAT_WEBHOOK_URL`) bleibt englisch. Der globale Webhook ist ein Betreiber-Hilfsmittel für die ganze Instanz: Er erhält nur Vorfälle öffentlicher Seiten, nie die privater Seiten. Die Nachricht darin steht als Klartext (siehe [Formatierte Texte](/docs/formatierte-texte)).
