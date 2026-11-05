# Wartungsfenster

Wartungsfenster kündigen geplante Arbeiten an. Sie werden unter **Wartungsfenster** gepflegt
(Rechte `maintenance:read`, `maintenance:create`, `maintenance:update`, `maintenance:delete`) und erscheinen auf der Statusseite unter „Geplante Wartungsarbeiten“.

## Felder

| Feld | Wirkung | Beispiel |
|---|---|---|
| Titel * | Überschrift | `Datenbank-Update` |
| Nachricht / Details * | Beschreibung für Besucher, mit Formatierung ([Formatierte Texte](/docs/formatierte-texte)) | `Kurze Unterbrechungen möglich.` |
| Betroffene Komponente(n) * | Komponenten, die während der Wartung betroffen sind | `API` |
| Tags (Reichweite) | wie bei [Vorfällen](/docs/vorfaelle) | `EU` |
| Start * / Ende * | geplanter Zeitraum; das Ende muss nach dem Start liegen | `02:00` bis `03:00` |
| Erinnerung vorher | Vorlauf für eine Erinnerungsmail in Minuten, Stunden oder Tagen; `0` = keine | `1 Stunde` |

## Automatischer Ablauf

Statushafen prüft jede Minute alle Wartungsfenster und setzt den Status selbst:

| Wert | Anzeige | Wann |
|---|---|---|
| `scheduled` | Geplant | vor dem Start |
| `in_progress` | Läuft | ab dem Start; die betroffenen Komponenten stehen auf „Wartung“ |
| `resolved` | Abgeschlossen | ab dem Ende; die Komponenten gehen auf „Betriebsbereit“ zurück |

Auf der Statusseite steht ein Fenster vor dem Start unter „Geplante Wartungsarbeiten“, danach
unter „Aktuelle Vorfälle“ (mit Uhr-Symbol). In der 90-Tage-Verfügbarkeit zählt Wartung nicht als
Ausfallzeit, siehe [Die öffentliche Statusseite](/docs/oeffentliche-statusseite#verfuegbarkeit-der-letzten-90-tage).

Beim Anlegen wird der Status aus den Zeiten abgeleitet. Liegt der Start schon in der
Vergangenheit, läuft die Wartung sofort.

> Hinweis: Nach dem Ende stehen die Komponenten wieder auf „Betriebsbereit“, auch wenn sie vor
> der Wartung anders gestört waren. Bei Bedarf danach den Status prüfen.

## Benachrichtigungen

- **Erinnerung:** einmalig vor dem Start, wenn ein Vorlauf eingestellt ist.
- **Gestartet** und **Beendet:** bei jedem automatischen Übergang, jeweils mit einem Eintrag im
  Verlauf.

Die Texte stehen in den [Mail-Templates](/docs/mail-templates) unter „Wartungsfenster:
Erinnerung/Gestartet/Beendet“.
