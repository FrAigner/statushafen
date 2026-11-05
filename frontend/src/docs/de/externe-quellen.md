# Externe Quellen

Viele Dienste laufen auf Plattformen anderer Anbieter. Mit einer **externen Quelle** liest
Statushafen die Statusseite eines solchen Anbieters aus und berücksichtigt deren Zustand auf der
eigenen Statusseite: Fällt bei dem Anbieter ein Baustein aus, den eine eigene Komponente nutzt,
zeigt die Komponente das mit an. Verwaltet wird das unter **Externe Quellen** im Bereich
**Betrieb**. Ansehen darf man die Seite mit `external_sources:read`, anlegen mit `external_sources:create`,
ändern, abrufen („Jetzt abrufen“) mit `external_sources:update` und löschen mit
`external_sources:delete`. „Übernehmen“ legt eigene Komponenten an und braucht deshalb zusätzlich
`components:create`.

## Eine Quelle anlegen

Mit „Neue Quelle“ trägt man eine fremde Statusseite ein:

| Feld | Wirkung | Beispiel |
|---|---|---|
| Name * | Anzeigename; steht an gespiegelten Meldungen, in Benachrichtigungen und in der Ausfall-Mail (1 bis 200 Zeichen) | `GitHub` |
| Adresse der Statusseite * | Adresse der fremden Seite, nur `https` | `https://www.githubstatus.com` |
| Abruf-Intervall (Sekunden) | wie oft die Quelle abgerufen wird, 60 bis 86400, Standard 120 | `120` |
| Ausfall nach (Minuten) | Frist ohne erfolgreichen Abruf bis zur Ausfall-Meldung, 10 bis 1440, Standard 60 | `60` |
| Aktiv | schaltet den regelmäßigen Abruf ein oder aus | an |

Unterstützt wird die **Summary-API v2** (`/api/v2/summary.json`). Viele öffentliche Statusseiten
bieten sie an, darunter Seiten auf Basis von Atlassian Statuspage. Atlassian und Statuspage sind
Marken der Atlassian Pty Ltd; Statushafen hat keine Verbindung zu Atlassian.
Private, lokale und interne Adressen lehnt Statushafen ab. Eine neu angelegte Quelle wird beim
nächsten Takt oder sofort mit „Jetzt abrufen“ im Zeilenmenü zum ersten Mal abgefragt; das Ergebnis
des Abrufs wird angezeigt. Pro Quelle läuft immer nur ein Abruf gleichzeitig.

Die Liste zeigt je Quelle den Zustand (Erreichbar, Abruf fehlgeschlagen, Ausfall, Inaktiv, Noch
nicht abgerufen), den letzten Abruf sowie die Zahl der Bausteine und Kopplungen. Beim **Löschen**
entfernt Statushafen auch die Bausteine, Kopplungen und gespiegelten Meldungen der Quelle. Eine
inaktive Quelle wird nicht abgerufen; ihre Kopplungen und gespiegelten Meldungen zählen dann nicht
mehr, die Daten bleiben aber erhalten.

## Bausteine übernehmen

Der schnellste Weg zu gekoppelten Komponenten ist die Karte „Bausteine übernehmen“:

1. Die Quelle wählen. Ihre Bausteine und Gruppen erscheinen in einer Liste (mit Suche).
2. Bausteine oder ganze Gruppen auswählen (höchstens 200 je Übernahme; „Alle sichtbaren wählen“
   hilft). Bereits gekoppelte Bausteine sind nicht wählbar.
3. Unter „Zielgruppe“ wählen, unter welcher eigenen Gruppe die neuen Komponenten stehen
   („Oberste Ebene“ = ohne Gruppe), und optional Tags vergeben.
4. „Übernehmen“ klicken.

Für jeden gewählten Baustein entsteht eine eigene Komponente mit dessen Namen, Status
„Betriebsbereit“ und der Kopplung an den Baustein. Eine ganze externe Gruppe wird als eigene
Gruppe mit ihren aktuellen Bausteinen angelegt; die Gruppe selbst wird nicht gekoppelt, ihr Status
ergibt sich aus den Unterkomponenten. Alles geschieht in einem Schritt: Bei einem Fehler wird
nichts angelegt. Übersprungene Bausteine (bereits gekoppelt, inzwischen verschwunden, Gruppe ohne
Bausteine) werden danach aufgelistet; ein zweiter Klick legt nichts doppelt an.

Gut zu wissen: Später beim Anbieter hinzukommende Bausteine übernimmt Statushafen nicht
automatisch, Namensänderungen beim Anbieter werden nicht nachgezogen, und eine Übernahme lässt
sich nicht rückgängig machen (die Komponenten löscht man einzeln).

## Bausteine koppeln

Hat man die Komponente schon, koppelt man sie auf der Karte „Kopplungen“ von Hand: eigene
Komponente, Quelle und externen Baustein wählen und „Koppeln“ klicken. Der externe Baustein kann
auch eine **ganze externe Gruppe** sein. „Lösen“ entfernt die Kopplung wieder.

- Eine Komponente kann mit mehreren Bausteinen gekoppelt sein. Reine Gruppen lassen sich nicht
  koppeln, nur ihre Komponenten.
- Bei einer gekoppelten externen Gruppe zählt der schlechteste Status ihrer aktuellen Mitglieder;
  neue Mitglieder kommen automatisch dazu, verschwundene zählen nicht.
- Koppelt man eine Gruppe und zugleich einen ihrer Bausteine, hat die doppelte Kopplung keine
  Wirkung auf den Status; die Liste warnt davor.
- Die Tabelle zeigt je Kopplung den aktuellen Status des Bausteins. „Verschwunden“ heißt, dass der
  Anbieter den Baustein nicht mehr liefert; „Unbekannt“ steht für einen Status, den Statushafen
  nicht kennt.

## Wirkung auf den Status

Der öffentlich angezeigte Status einer Komponente ist der **schlechteste** aus ihrem eigenen
Status, einem offenen eigenen Vorfall und den gekoppelten externen Bausteinen. Ändert sich der
externe Status, ändert sich die Anzeige beim nächsten Abruf; der gespeicherte eigene Status bleibt
unverändert. Auf der öffentlichen Seite trägt die Komponente die Marke „extern“ mit Quelle und
Baustein, siehe [Die öffentliche Statusseite](/docs/oeffentliche-statusseite#externe-quellen-auf-der-seite).

## Ausfall einer Quelle

Liefert eine Quelle länger als die eingestellte Frist („Ausfall nach“) keinen erfolgreichen Abruf,
gilt sie als **ausgefallen**. Ein einzelner Fehler löst nichts aus. Dann passiert Folgendes:

- Im Admin-Bereich erscheint oben ein Banner „Externe Quelle nicht erreichbar“ mit Link auf die
  Quellenseite; in der Liste steht der Zustand „Ausfall“.
- Der zuletzt bekannte Status der Quelle **zählt nicht mehr**. Eine Komponente zeigt wieder nur
  ihren eigenen Status; öffentlich steht „Daten veraltet“ statt eines Status.
- Statushafen schickt eine E-Mail an alle Global Admins und alle Nutzer mit `external_sources:update` auf
  der Seite: einmal beim Beginn, einmal als Erinnerung nach 24 Stunden Dauer und einmal bei
  Entwarnung. Die Sprache folgt der Oberflächensprache des Empfängers.

Ist die Quelle wieder erreichbar, zählt ihr Status sofort wieder. Ist der E-Mail-Versand in den
[Systemeinstellungen](/docs/systemeinstellungen) abgeschaltet, protokolliert Statushafen die
übersprungenen Mails im [Audit-Log](/docs/audit-log).

## Gespiegelte Meldungen

Neben dem Status holt Statushafen auch die **Vorfälle und Wartungen** der Quelle. Gespiegelt werden
nur Meldungen, die mindestens einen gekoppelten Baustein betreffen. Die Karte „Gespiegelte
Meldungen“ listet sie mit Art, Stand, betroffenen eigenen Komponenten und Zahl der Updates; Filter
nach Art (Vorfall, Wartung), Zustand (offen, beendet) und Quelle. Die Texte stammen von der
Fremdseite und werden nicht übersetzt oder verändert. Beendete Meldungen zeigt die Liste für die
letzten 30 Tage.

- **Öffentlich** erscheinen offene und in den letzten 14 Tagen beendete Meldungen unter „Meldungen
  externer Quellen“, klar als extern gekennzeichnet und mit Link zur Originalseite.
- **Benachrichtigungen:** [Abonnenten](/docs/abonnements) der betroffenen Komponenten erhalten bei
  neuen, aktualisierten und beendeten Meldungen eine Nachricht (E-Mail oder Google Chat). Beim
  allerersten Abruf einer Quelle bleibt alles still, damit die Historie des Anbieters keine
  Mailflut auslöst. Die Sprache folgt der Standardsprache der Seite (Deutsch oder Englisch).
  Die Texte kommen aus eingebauten Standardtexten; eigene Templates für diese Mails gibt es nur
  über die API, siehe [Mail-Templates](/docs/mail-templates#meldungen-externer-quellen).
- **Verfügbarkeit:** Gespiegelte Vorfälle zählen in der 90-Tage-Verfügbarkeit wie eigene Vorfälle,
  gespiegelte Wartungen wie Wartungen. Die Auswirkung des Anbieters bestimmt den Schweregrad:
  „minor“ zählt als Beeinträchtigt, „major“ als Teilausfall, „critical“ als Großstörung. Meldet
  der Anbieter einen Vorfall ausdrücklich als folgenlos („none“), zählt er nicht. Details unter
  [Verfügbarkeit und Berechnung](/docs/oeffentliche-statusseite#verfuegbarkeit-und-berechnung).
- **Aufbewahrung:** Beendete gespiegelte Meldungen löscht Statushafen 90 Tage nach ihrem Ende
  endgültig (stündlicher Aufräumlauf, Eintrag im Audit-Log). Offene Meldungen bleiben immer.
  Betreiber ändern die Frist mit der Umgebungsvariable `EXTERNAL_INCIDENT_RETENTION_DAYS`
  (Anzahl Tage; ungültige oder nicht positive Werte ergeben wieder 90).

## Rechte und Audit-Log

Alle Aktionen sind im [Audit-Log](/docs/audit-log) vermerkt (`external_source.*`,
`external_link.*`, `external_incident.*`). Eine Quelle gehört genau einer Statusseite; fremde IDs
liefern einen Fehler. Die Schnittstellen stehen in der OpenAPI-Datei, siehe
[API und Webhooks](/docs/api-und-webhooks).
