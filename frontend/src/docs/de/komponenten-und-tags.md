# Komponenten und Tags

## Komponenten

Komponenten sind die Dienste, deren Zustand die Statusseite zeigt, zum Beispiel „Website“,
„API“ oder „Zahlungen“. Gepflegt werden sie unter **Komponenten** (Lesen `components:read`,
Anlegen `components:create`, Ändern `components:update`, Löschen `components:delete`). Die Liste zeigt Gruppen als einklappbare Zeilen und lässt sich nach
Name, Beschreibung und Tag durchsuchen; Treffer erscheinen samt ihrer Gruppen.

| Feld | Wirkung |
|---|---|
| Name | Pflicht, höchstens 60 Zeichen, erscheint auf der Statusseite |
| Kennung | Kurzer, stabiler Schlüssel (Kleinbuchstaben, Ziffern, Bindestriche, höchstens 60 Zeichen), je Statusseite einmalig. Leer gelassen, wird sie beim Anlegen aus dem Namen erzeugt (Umlaute ausgeschrieben, bei Doppelungen „-2“, „-3“ …). Nach dem Anlegen nicht mehr änderbar, damit Integrationen nicht brechen; Webhooks nutzen weiterhin die Komponenten-ID |
| Beschreibung | Zusatztext zur Komponente, höchstens 240 Zeichen; leer gespeichert löscht sie die bisherige Beschreibung |
| Auf der Statusseite anzeigen | Schalter, standardmäßig an. Aus: Die Komponente fehlt auf der öffentlichen Statusseite, in der Verfügbarkeit, im Abonnement-Assistenten und im öffentlichen Gesamtstatus (bei einer Gruppe samt ihren Mitgliedern). Im Admin bleibt sie mit der Markierung „Nicht öffentlich“ sichtbar; Webhook und Statusänderungen funktionieren weiter |
| Gruppe | Gruppe, unter der die Komponente steht; „Keine Gruppe“ = oberste Ebene |
| Tags | ordnet die Komponente Regionen oder Teams zu, siehe unten |
| Anfangsstatus / Status | Zustand der Komponente, siehe Tabelle |

Bereits gespeicherte Texte, die länger sind als die Grenzen, bleiben unverändert. Beim nächsten
Speichern des Formulars meldet das Feld den Überhang, dann ist der Text zu kürzen.

Hat die Statusseite weitere Sprachen, erscheint oben im Formular ein Sprachumschalter: „Deutsch
(Standard)“ und je eine weitere Sprache mit ihrem Stand („leer“, „Entwurf“, „fertig“). In einer
weiteren Sprache lassen sich Name und Beschreibung übersetzen; die Beschreibung ist dort wie in
der Standardsprache optional, für eine Übersetzung genügt der Name. Gruppen haben nur einen Namen.
Mit dem Haken „Übersetzung ist fertig und wird auf der Statusseite gezeigt“ erscheint sie dort, ohne Haken bleibt sie
ein Entwurf. Wer „Ändern“ für Komponenten darf, sieht den Umschalter; gespeichert wird alles mit
einem Klick auf „Speichern“. Ohne Text in einer Sprache sehen Besucher die Standardsprache. Die
Übersetzungsseite im Zeilenmenü bleibt daneben bestehen, siehe [Sprachen](/docs/sprachen).

Die ID einer Komponente steht in der Spalte „ID“. Sie wird für den
[Monitoring-Webhook](/docs/api-und-webhooks) gebraucht. Über das Zeilenmenü „…“ erreicht man
„Bearbeiten“, „Übersetzungen“, „Archivieren“ und „Löschen“. Eine gelöschte Komponente verschwindet von der
Statusseite; Vorfälle bleiben bestehen, verlieren aber die Zuordnung zu ihr.

### Archivieren

Statt zu löschen lässt sich eine Komponente **archivieren** (Zeilenmenü „Archivieren“, Recht `components:update`).
Der Umschalter im Kartenkopf wechselt zwischen **Aktiv** und **Archiviert** und zeigt je die Anzahl; sind keine
archiviert, steht dort „Keine archivierten Komponenten“. Eine archivierte Komponente verhält sich öffentlich wie eine
nicht angezeigte: Sie fehlt auf der Statusseite, in der Verfügbarkeit, im Abonnement-Assistenten und im Gesamtstatus.
Auch in den Auswahlen für Vorfälle, Wartungen, Vorlagen und Abonnenten wird sie nicht mehr angeboten. Verlauf,
Verfügbarkeit, Vorfall-Zuordnungen und Übersetzungen bleiben erhalten. „Wiederherstellen“ im Zeilenmenü des Segments
„Archiviert“ macht sie wieder aktiv. Eine Gruppe nimmt ihre Mitglieder mit ins Archiv und holt sie beim
Wiederherstellen zurück (einzeln früher archivierte bleiben archiviert); ein Mitglied einer archivierten Gruppe lässt
sich nur über die Gruppe wiederherstellen. Der Monitoring-Webhook funktioniert bei archivierten Komponenten weiter.
Archivieren und Wiederherstellen stehen im [Audit-Log](/docs/audit-log).

### Status-Werte

| Wert | Anzeige | Setzen |
|---|---|---|
| `operational` | Betriebsbereit | im Formular, in der Liste, per Webhook |
| `degraded_performance` | Beeinträchtigt | im Formular, in der Liste, per Webhook |
| `partial_outage` | Teilausfall | im Formular, in der Liste, per Webhook |
| `major_outage` | Großstörung | im Formular, in der Liste, per Webhook |
| `under_maintenance` | Wartung | nur durch ein laufendes [Wartungsfenster](/docs/wartungsfenster) oder per Webhook bzw. API; das Formular bietet den Wert nicht an |

Der Status lässt sich beim Anlegen („Anfangsstatus“), beim Bearbeiten und direkt in der Liste
ändern (Schnellstatus, wird ohne Rückfrage gespeichert). Abonnenten werden durch eine reine
Statusänderung **nicht** benachrichtigt; dafür legt man einen [Vorfall](/docs/vorfaelle) an.

Der öffentlich angezeigte Status ist der schlechteste aus mehreren Quellen: dem gespeicherten
Status, einem offenen Vorfall (mit dem Status seines Schweregrads: „Beeinträchtigt“, „Teilausfall“
oder „Großstörung“; der gespeicherte Status bleibt dabei unverändert) und gekoppelten [externen Quellen](/docs/externe-quellen).

### Gruppen

Gruppen fassen Komponenten zusammen, zum Beispiel „Kerndienste“ mit „Web-App“ und „API“. Man legt
sie mit dem Knopf „+ Neue Gruppe“ an; in einer Komponente oder Gruppe wählt man im Feld „Gruppe“
die übergeordnete Gruppe. Gruppen lassen sich auch verschachteln, ein Zyklus (eine Gruppe unter
sich selbst oder ihren Nachkommen) wird abgelehnt.

- Eine Gruppe hat **keinen eigenen Status**. Er ergibt sich aus dem schlechtesten Status ihrer
  Unterkomponenten (Reihenfolge: Großstörung, Teilausfall, Beeinträchtigt, Wartung,
  Betriebsbereit). Man kann ihn nicht setzen, auch nicht per Webhook.
- Eine Gruppe lässt sich nur **löschen, wenn sie leer ist**. Zuerst verschiebt oder löscht man
  ihre Komponenten.
- Als Gruppe wählbar sind nur Gruppen derselben Statusseite.
- Über die API setzt man die Gruppe mit `parent_id`, siehe
  [API und Webhooks](/docs/api-und-webhooks).

### Extern gekoppelte Komponenten

Eine Komponente, die mit einem Baustein einer fremden Statusseite gekoppelt ist, trägt auf der
öffentlichen Seite die Marke „extern“. Kopplungen verwaltet man unter **Externe Quellen**, siehe
[Externe Quellen](/docs/externe-quellen).

## Tags und Reichweite

Tags beschreiben die **Reichweite** von Komponenten, Vorfällen, Wartungen und Benutzern, zum
Beispiel Regionen wie `EU` oder `US`. Gepflegt werden sie unter **Tags** (nur Global Admins).

- **Global** gilt ein Tag für die ganze Instanz, **seitengebunden** nur für eine Statusseite.
  Der Bereich lässt sich nachträglich nicht ändern.
- **Öffentlich** erscheinen die Tags auf der Statusseite als Filter „Region:“. Besucher können
  damit Komponenten und Vorfälle nach Region filtern.
- **Zugriff:** Haben sowohl ein Benutzer als auch eine Komponente bzw. ein Vorfall Tags, darf der
  Benutzer nur Einträge mit einem gemeinsamen Tag bearbeiten. Fehlen auf einer der beiden Seiten
  Tags, gibt es keine Einschränkung. Global Admins sind nie eingeschränkt, der Tag `global` wirkt wie ein
  Platzhalter für alles. Die Einschränkung gilt auch für die Übersetzungen einer Komponente
  oder eines Vorfalls (lesen, anlegen, speichern, bestätigen, löschen).
  Auch das Löschen einer **externen Quelle** entfernt die Kopplungen aller verknüpften
  Komponenten und ist deshalb nur erlaubt, wenn der Benutzer auf jede dieser Komponenten
  Tag-Zugriff hat; sonst wird es abgelehnt und nichts gelöscht.
- **Vergabe:** Seiten-Rollen mit Schreibrecht sehen im Formular die Tags ihrer Seite und
  dürfen **seitengebundene** Tags vergeben. **Globale** Tags (zum Beispiel Regionen, die auch
  Zugriffe steuern) vergeben nur Global Admins; ein bereits gesetzter globaler Tag bleibt beim
  Bearbeiten erhalten.
- **Interne Mails** zu Vorfällen gehen nach derselben Regel nur an Benutzer mit passendem Tag.

Beispiel: Ein Vorfall mit Tag `EU` erreicht intern nur Benutzer mit dem Tag `EU`, `global` oder
ganz ohne Tags.
