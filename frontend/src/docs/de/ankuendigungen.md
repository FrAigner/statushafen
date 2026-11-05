# Ankündigungen

Ankündigungen sind allgemeine Hinweise auf der Statusseite, zum Beispiel zu einer neuen
Funktion oder einer geänderten Supportzeit. Sie haben keinen Status-Verlauf und keine
Komponenten. Gepflegt werden sie unter **Ankündigungen** (Rechte `notices:read`,
`notices:create`, `notices:update` und `notices:delete`).

## Felder

| Feld | Wirkung | Beispiel |
|---|---|---|
| Titel * | Überschrift | `Neue Supportzeiten` |
| Nachricht / Details * | Text mit Formatierung, siehe [Formatierte Texte](/docs/formatierte-texte) | `Ab 1. 11. sind wir 24/7 erreichbar.` |
| Stufe * | `info` (Info), `warning` (Warnung) oder `critical` (Kritisch) | `info` |
| Aktiv | nur aktive Ankündigungen sind sichtbar | an |
| Eingeklappt anzeigen (spart Platz) | die Ankündigung erscheint nicht direkt, sondern hinter „… weitere Ankündigung(en) anzeigen“ | aus |
| Beginn / Ende (optional) | Zeitraum der Anzeige; leer = unbegrenzt | |
| Subscriber per E-Mail benachrichtigen | verschickt Mails an E-Mail-Abonnenten | aus |

## Sichtbarkeit

Eine Ankündigung erscheint öffentlich im Abschnitt „Ankündigungen“ (unter den Komponenten, siehe
[Die öffentliche Statusseite](/docs/oeffentliche-statusseite)), wenn sie aktiv ist und der
aktuelle Zeitpunkt im Zeitraum liegt. Neueste zuerst; eingeklappte Ankündigungen stehen gesammelt
hinter „… weitere Ankündigung(en) anzeigen“. Die Stufe bestimmt Farbe und Symbol. Ankündigungen
lassen sich in weitere Sprachen übersetzen, siehe [Sprachen](/docs/sprachen).

## Benachrichtigungen

Nur wenn „Subscriber per E-Mail benachrichtigen“ gesetzt ist, gehen Mails an aktive
E-Mail-Abonnenten:

- beim Anlegen oder Aktivieren („Ankündigung: Erstellt“),
- beim Bearbeiten einer aktiven Ankündigung („Ankündigung: Aktualisiert“),
- beim Deaktivieren („Ankündigung: Beendet“).

Webhook-Abonnenten erhalten keine Ankündigungen.
