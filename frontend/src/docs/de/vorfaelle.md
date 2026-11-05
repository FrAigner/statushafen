# Vorfälle

Ein Vorfall (Incident) meldet eine Störung. Vorfälle werden unter **Vorfälle** gepflegt
(Rechte `incidents:read`, `incidents:create`, `incidents:update`, `incidents:delete`) und erscheinen auf der Statusseite unter „Aktuelle Vorfälle“.

## Vorfall anlegen

| Feld | Wirkung | Beispiel |
|---|---|---|
| Titel * | Überschrift auf der Statusseite | `Login nicht möglich` |
| Nachricht / Details * | Beschreibung für Besucher, mit Formatierung ([Formatierte Texte](/docs/formatierte-texte)) | `Wir untersuchen Fehler beim Anmelden.` |
| Status * | aktueller Stand, siehe unten | `Untersuchung` |
| Betroffene Komponente(n) * | Komponenten der Seite, die betroffen sind | `Website`, `API` |
| Tags (Reichweite) | grenzt Sichtbarkeit und interne Mails ein, siehe [Tags](/docs/komponenten-und-tags#tags-und-reichweite) | `EU` |
| Schweregrad | Beeinträchtigt (Standard), Teilausfall oder Großstörung; gewichtet die [Verfügbarkeit](/docs/oeffentliche-statusseite#verfuegbarkeit-und-berechnung) (0,3 / 0,6 / 1,0), auch nachträglich änderbar. Wartungen zählen nicht als Ausfall | `Teilausfall` |
| Beginn / Ende | Zeitpunkte des Vorfalls; das Ende nur bei „Behoben“ | |

Über „Vorlage verwenden“ lassen sich Titel, Nachricht, Status und Komponenten aus einer
[Vorlage](/docs/vorlagen) vorbelegen.

## Status-Ablauf

| Wert | Anzeige | Bedeutung |
|---|---|---|
| `investigating` | Untersuchung | Störung bekannt, Ursache noch offen |
| `identified` | Identifiziert | Ursache gefunden |
| `monitoring` | Beobachtung | Lösung eingespielt, Lage wird beobachtet |
| `resolved` | Behoben | Vorfall abgeschlossen |

Üblich ist die Reihenfolge von oben nach unten. Statushafen erzwingt sie aber nicht: Man kann
jeden der vier Status direkt wählen und einen Vorfall auch wieder öffnen; andere Werte lehnt
Statushafen ab. Setzt man einen Vorfall auf „Behoben“, hält Statushafen den Zeitpunkt als Ende
fest (auch über ein Update). Wer ihn auf einen anderen Status setzt, löscht damit dessen
Endzeit. Das Ende darf nicht vor dem Beginn liegen. Behobene Vorfälle erscheinen auf der
Statusseite unter „Vergangene Vorfälle“.

Solange ein Vorfall offen ist (`investigating`, `identified`, `monitoring`), zeigt die
Statusseite seine Komponenten mindestens mit dem Status seines Schweregrads: „Beeinträchtigt“,
„Teilausfall“ oder „Großstörung“. Das ist dieselbe Zuordnung wie bei den Verfügbarkeitsbalken, Anzeige
und Balken widersprechen sich also nicht. Sind einer Komponente mehrere offene Vorfälle zugeordnet,
gilt der schlimmste Schweregrad. Ein schlechterer, manuell gesetzter Status bleibt bestehen.

## Updates

Ein Klick auf einen Vorfall öffnet die Detailansicht mit dem Verlauf. Dort fügt man mit
„Update hinzufügen“ einen neuen Eintrag hinzu („Neuer Status *“, „Update-Nachricht *“). Das
Update übernimmt den Status in den Vorfall und erscheint öffentlich im Verlauf. Die ursprüngliche
Beschreibung des Vorfalls bleibt als **Erstmeldung** erhalten und steht im Verlauf ganz unten. Auf
der Statusseite zeigt der Vorfall seine jüngsten Updates; das neueste trägt die Kennzeichnung
„Letztes Update“, siehe [Die öffentliche Statusseite](/docs/oeffentliche-statusseite#vorfaelle-und-ihr-verlauf).

Hat die Statusseite mehrere Sprachen und du darfst den Vorfall ändern, steht über der
Update-Nachricht ein Sprachumschalter. Eine Nachricht je weiterer Sprache (kein Titel) wird beim
Posten mitgespeichert. Schon gepostete Updates ergänzt du über **Übersetzungen** am Eintrag im
Verlauf. Besucher sehen die Nachricht in ihrer Sprache, sobald die Übersetzung als fertig markiert
ist; sonst die Standardsprache der Seite bzw. das Original.

Beispiel-Verlauf:

1. `Untersuchung`: „Anmeldung schlägt für einen Teil der Nutzer fehl.“
2. `Identifiziert`: „Ein Zertifikat ist abgelaufen.“
3. `Beobachtung`: „Neues Zertifikat ist eingespielt.“
4. `Behoben`: „Seit 30 Minuten keine Fehler mehr.“

## Benachrichtigungen

Beim Anlegen, beim Bearbeiten und bei jedem Update verschickt Statushafen Benachrichtigungen:

- an interne Nutzer (Global Admins und Nutzer mit einer Rolle auf der Seite, gefiltert nach Tags),
- an [Abonnenten](/docs/abonnements), die mindestens eine betroffene Komponente abonniert haben.

Die Texte kommen aus den [Mail-Templates](/docs/mail-templates). Ein Update mit Status „Behoben“
nutzt das Template „Vorfall behoben“.

Damit ein Fehlgriff oder ein missbrauchtes Konto nicht tausende Mails auslöst, gelten Grenzen je Seite
und je Nutzer: höchstens 30 neue Vorfälle, Wartungen und Meldungen pro Stunde je Seite (60 je Nutzer)
sowie 120 Änderungen und Updates pro Stunde. Darüber antwortet die Oberfläche mit „Zu viele Anfragen“;
nach dem Zeitraum aus `Retry-After` geht es weiter. Der normale Betrieb, auch ein langer Vorfall mit
vielen Updates, bleibt weit darunter.

## Post-Mortem

In der Detailansicht lässt sich nach dem Vorfall ein Post-Mortem schreiben: Zusammenfassung,
Grundursache, Auswirkung, Behebung, Erkennung und Folgemaßnahmen (offen, in Arbeit, erledigt).
Es bleibt Entwurf, bis man es veröffentlicht.

## Übersetzungen

Für jeden Vorfall lassen sich Titel und Nachricht in weitere Sprachen übersetzen, ebenso die
Nachricht jedes Updates (siehe oben), siehe
[Sprachen und Übersetzungen](/docs/sprachen).
