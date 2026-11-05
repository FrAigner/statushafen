# Vorlagen

Vorlagen beschleunigen wiederkehrende Vorfälle und Wartungen. Sie werden unter **Vorlagen**
gepflegt und brauchen die Rechte `incident_templates:read`, `incident_templates:create`, `incident_templates:update` bzw. `incident_templates:delete`.

## Felder

| Feld | Wirkung | Beispiel |
|---|---|---|
| Interner Vorlagenname * | nur im Admin-Bereich sichtbar | `DB-Ausfall` |
| Typ * | Vorfall oder Wartungsfenster | Vorfall |
| Titel * | Vorschlag für den Titel | `Datenbank nicht erreichbar` |
| Nachricht / Details * | Vorschlag für den Text | `Wir untersuchen das Problem.` |
| Standard-Status | vorbelegter Status | `Untersuchung` |
| Vorausgewählte Komponente(n) | vorbelegte Komponenten | `API` |

## Verwendung

Beim **Neuanlegen** eines Vorfalls oder Wartungsfensters erscheint „Vorlage verwenden“, sobald passende Vorlagen
existieren. Die Vorlage füllt Titel, Nachricht, Status und Komponenten einmalig aus; danach ist
alles frei änderbar.

## Mehrere Sprachen

Hat die Statusseite weitere Sprachen, erscheint oben im Formular ein Sprachumschalter: „Deutsch
(Standard)“ und je eine weitere Sprache mit ihrem Stand („leer“, „Entwurf“, „fertig“). In einer
weiteren Sprache lassen sich Titel und Nachricht der Vorlage übersetzen; die Standardsprache ist
Pflicht, weitere Sprachen sind optional. Wer „Ändern“ für Vorlagen darf, sieht den Umschalter;
gespeichert wird alles mit einem Klick auf „Speichern“. Beim „Vorlage verwenden“ übernimmt das
Formular auch diese Texte in die Sprachen des neuen Vorfalls bzw. Wartungsfensters (Stand wie in der
Vorlage); danach sind sie frei änderbar. Siehe auch [Sprachen](/docs/sprachen).

Ein Vorfall merkt sich nicht, aus welcher Vorlage er stammt. Wer eine Vorlage ändert oder löscht,
ändert bestehende Vorfälle nicht.

Vorlagen sind etwas anderes als [Mail-Templates](/docs/mail-templates): Vorlagen füllen das
Formular, Mail-Templates bestimmen die Benachrichtigungstexte.
