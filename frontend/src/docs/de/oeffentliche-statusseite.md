# Die öffentliche Statusseite

Die öffentliche Statusseite ist das, was Besucher und Kunden sehen. Sie erreicht man unter der
Adresse der Seite, siehe [Statusseiten und Mandanten](/docs/statusseiten). Die Seite lädt ihre Daten
beim Öffnen und danach jede Minute neu; „Zuletzt aktualisiert“ zeigt den Stand.

## Gesamtstatus und Aufbau

Oben steht der **Gesamtstatus** („Alle Systeme laufen normal“, „Eingeschränkte Leistung“,
„Teilweise Störung“, „Größere Störung“ oder „Wartungsarbeiten laufen“) mit der Zahl der Komponenten,
die normal laufen. Darunter folgen, soweit es etwas zu zeigen gibt:

1. **Suche und Filter**.
2. **Aktuelle Vorfälle**, auch laufende Wartungen.
3. **Geplante Wartungsarbeiten**, siehe [Wartungsfenster](/docs/wartungsfenster).
4. **Meldungen externer Quellen**, siehe unten.
5. **Verfügbarkeit der letzten 90 Tage**.
6. **Komponenten** als Baum mit ihren Gruppen. Im Seiten-Layout „Liste“ stehen stattdessen
   Karten mit 90-Tage-Balken und eine Vorfall-Zeitleiste.
7. **Ankündigungen** der Seite, siehe [Ankündigungen](/docs/ankuendigungen).
8. **Vergangene Vorfälle**: behobene Vorfälle, die neuesten zuerst.
9. **Benachrichtigungen erhalten** mit dem Knopf „Jetzt abonnieren“, siehe
   [Abonnements](/docs/abonnements).

## Suchen und Filtern

- **Suchfeld:** durchsucht Komponenten (Name und Beschreibung) und Vorfälle (Titel und Nachricht).
  Mehrere Wörter gelten als UND. Mit `AND`/`UND` und `OR`/`ODER` lassen sich Begriffe verknüpfen,
  wobei UND stärker bindet als ODER. Eine Phrase steht in Anführungszeichen. Beispiele:
  `Frankfurt UND Datenbank`, `London ODER Paris`, `"Frankfurt Süd" ODER Hamburg`. Das Fragezeichen
  neben dem Feld zeigt diese Hilfe.
- **Region:** Chips mit den [Tags](/docs/komponenten-und-tags#tags-und-reichweite) der Seite. Man
  kann mehrere Regionen wählen. Bei vielen Tags zeigt die Seite zunächst nur einen Teil und den
  Knopf „Mehr anzeigen“.
- **Status:** Chips für Betriebsbereit, Beeinträchtigt, Teilausfall und Großstörung. Sie beziehen
  sich auf den Status der Komponenten; ein Vorfall passt, wenn eine seiner betroffenen
  Komponenten den gewählten Status hat.
- Such- und Filterergebnis zählen Komponenten und Vorfälle; „Filter zurücksetzen“ hebt alles auf.

## Vorfälle und ihr Verlauf

Unter „Aktuelle Vorfälle“ steht jeder offene Vorfall mit Titel, Status, betroffenen Komponenten und
Beginn. Die drei jüngsten Vorfälle zeigen ihre neuesten drei Updates gleich darunter; das neueste
trägt die Kennzeichnung **Letztes Update**. Ein Klick auf „Vollständigen Verlauf anzeigen“ öffnet
alle Updates, neueste zuerst. Ganz unten im Verlauf steht die **Erstmeldung**, also die
ursprüngliche Beschreibung des Vorfalls. Sie bleibt erhalten, auch wenn spätere Updates den Stand
ändern. Sobald ein Vorfall behoben ist, wandert er in „Vergangene Vorfälle“. Die Seite zeigt zunächst
alle offenen und die neuesten 50 behobenen Vorfälle; mit „Ältere Vorfälle laden“ kommen jeweils 50
weitere dazu. Wie Admins Vorfälle pflegen, steht unter [Vorfälle](/docs/vorfaelle).

## Verfügbarkeit der letzten 90 Tage

Der Abschnitt zeigt für jeden der letzten 90 Tage (UTC, einschließlich heute) einen Balken und
darüber die Verfügbarkeit in Prozent. Im Seiten-Layout „Liste“ gibt es die Balken auch je
Komponente. Die Werte stammen aus den Vorfällen und Wartungen der Seite; der Server hält sie bis zu
60 Sekunden zwischen.

- Die Farbe eines Tages zeigt den schlimmsten Status dieses Tages: Beeinträchtigt, Teilausfall
  oder Großstörung. Die Stufen unterscheiden sich auch ohne Farbwahrnehmung in der Form
  (schräg schraffiert, kreuzschraffiert, vollflächig). Ein Tooltip nennt die Verfügbarkeit in
  Prozent und die Dauer je Schweregrad. Wie die Prozentzahl entsteht, steht unter
  [Verfügbarkeit und Berechnung](#verfuegbarkeit-und-berechnung).
- Wartung wird als Wartung angezeigt und zählt **nicht** als Ausfallzeit.
- Gespiegelte Meldungen [externer Quellen](/docs/externe-quellen) zählen wie eigene Vorfälle und
  Wartungen.
- Tage vor Anlage der Seite und Tage ohne Beobachtung zeigt die Seite neutral als „Keine Daten“.
  Statushafen erfindet dafür weder 100 % noch 0 %. Die Gesamt-Prozentzahl erscheint erst, wenn die
  Seite mindestens 24 Stunden beobachtet wurde; davor zeigt die Seite „noch keine Daten“.
- **Statushistorie der Komponenten:** Jede Statusänderung einer Komponente (per Webhook, von Hand,
  durch ein Wartungsfenster oder die Selbstüberwachung) wird mit Zeitpunkt, altem und neuem Status
  und Quelle gespeichert und zählt in der Verfügbarkeit mit ihrem Status, auch ohne Vorfall. Läuft
  zugleich ein Vorfall, gilt der schlimmste Status. Die Historie beginnt mit dem Update, das sie
  einführt: Der damals aktuelle Status gilt ab diesem Zeitpunkt, davor Gesetztes kennt Statushafen
  nicht (dafür bleibt ein [Vorfall](/docs/vorfaelle) nötig). Einträge werden 365 Tage aufbewahrt, der
  jeweils letzte Stand vor dieser Frist bleibt als Ausgangsstatus erhalten.
  Wechselt ein Monitoring per Webhook binnen 30 Sekunden mehrfach (Flattern), fasst Statushafen
  die Historie zusammen: Der Status gilt sofort, aber es bleiben höchstens zwei Einträge je
  30-Sekunden-Fenster: einer mit dem schlimmsten Status des Fensters und einer mit dem Endstand.
  Es geht nie ein Ausfall verloren. Die Verfügbarkeit ist dabei pessimistisch: Das ganze Fenster
  zählt als schlimmster Status, nie als besser. Je Komponente fließen höchstens die neuesten 10.000 Wechsel des Fensters ein; wird
  gekürzt, enthält die Antwort der Verfügbarkeit `history_truncated: true`.
- Prozentwerte werden abgeschnitten, nicht gerundet, damit 99,996 % nicht als 100,00 % erscheint.

### Verfügbarkeit und Berechnung

Nicht jede Störung ist ein voller Ausfall. Statushafen gewichtet die Zeit der Beeinträchtigung
nach Schweregrad:

| Schweregrad | Gewicht |
| --- | --- |
| Beeinträchtigt | 0,3 |
| Teilausfall | 0,6 |
| Großstörung | 1,0 |
| Wartung | 0 (zählt nicht) |

**Verfügbarkeit = 1 − gewichtete Ausfallzeit ÷ beobachtete Zeit**, je Tag und Komponente. Sechs
Stunden „Beeinträchtigt“ ergeben 1,8 Stunden gewichtete Ausfallzeit, also 92,5 % für diesen Tag
(vorher 75 %: jede Störung zählte als voller Ausfall).

- **Überlappungen** zählen nicht doppelt: Zu jedem Zeitpunkt gilt nur der schlimmste Status.
  Ein Vorfall über mehrere Tage oder über Mitternacht wird auf die beteiligten Tage verteilt.
- **Gesamtseite:** Der Wert der Seite ist der Durchschnitt über alle sichtbaren Komponenten, die
  Tagesfarbe der schlimmste Status eines beliebigen Tages. Ein Vorfall ohne Komponente betrifft
  alle sichtbaren Komponenten.
- **Woher der Schweregrad kommt:** Eigene Vorfälle tragen einen Schweregrad
  (Beeinträchtigt 0,3, Teilausfall 0,6, Großstörung 1,0); Vorfälle ohne Angabe, auch ältere,
  zählen als „Beeinträchtigt“. Die Änderung gilt auch rückwirkend (nach spätestens einer Minute,
  wegen des Zwischenspeichers). Gespiegelte Meldungen [externer Quellen](/docs/externe-quellen)
  bringen ihre Auswirkung mit: „minor“ zählt als Beeinträchtigt, „major“ als Teilausfall,
  „critical“ als Großstörung, „none“ gar nicht. Der Status aus der Statushistorie der
  Komponenten zählt mit seiner eigenen Stufe (Beeinträchtigt, Teilausfall, Großstörung; Wartung
  zählt nicht).
- Unter 24 Stunden Beobachtung zeigt die Seite weiter „Keine Daten“ statt einer Prozentzahl.

Die Rohdaten liefert `GET /api/public/pages/<slug>/availability`, siehe
[API und Webhooks](/docs/api-und-webhooks#weitere-api).

## Externe Quellen auf der Seite

Komponenten, die mit einer [externen Quelle](/docs/externe-quellen) gekoppelt sind, tragen die
Marke **extern**. Die Marke ist ein Link auf die Quellseite (öffnet in neuem Tab). Darunter steht
im Text, welcher Baustein der Quelle den Status liefert; bei einer gekoppelten Gruppe die
schlechteste Komponente der Gruppe. Ist die Quelle ausgefallen, steht „Daten veraltet“ statt eines
Status.

Der Abschnitt **Meldungen externer Quellen** zeigt die Vorfälle und Wartungen der Anbieter, die
gekoppelte Komponenten betreffen: offene Meldungen und solche, die in den letzten 14 Tagen endeten.
Jede ist als „extern“ gekennzeichnet, nennt die betroffenen Komponenten und verlinkt auf die
Originalseite („Original bei … ansehen“); maßgeblich ist stets die Originalseite. Suche und
Filter wirken auch auf diese Meldungen, über die betroffenen Komponenten.

## Private Seiten

Eine private Seite zeigt nur angemeldeten Nutzern mit Zugriff Inhalt; alle anderen sehen den Hinweis
„Statusseite nicht verfügbar“ mit Link zum Login (und, wenn die Seite ein SSO nutzt, dem Knopf „Mit
SSO anmelden“). Der Hinweis lautet bewusst „gibt es nicht oder ist privat“: Für Besucher ohne
Berechtigung antwortet eine private Seite technisch genauso wie eine Seite, die es nicht gibt (HTTP 404),
damit niemand von außen erkennen kann, welche Adressen zu privaten Seiten gehören. Auch die
Programmierschnittstelle verrät es nicht. Auf der Startseite von Statushafen erscheinen private
Seiten nicht.
