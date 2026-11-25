# Sprachen und Übersetzungen

Statushafen trennt drei Dinge: die Sprache der **Oberfläche**, die Sprachen der **Inhalte** einer
Statusseite und **eigene UI-Texte**.

## Nur Deutsch und Englisch, zweite Sprache nur mit Schalter

Inhalte (Vorfälle, Wartungen, Ankündigungen, Komponenten, Vorlagen und Benachrichtigungen) gibt es
auf **Deutsch und Englisch**. Ob eine Statusseite neben ihrer Standardsprache die zweite Sprache
anbietet, hängt am Schalter **Automatische Übersetzung** in den
[Systemeinstellungen](/docs/systemeinstellungen) (nur Global Admins). Er ist **standardmäßig aus**.

- **Aus:** Jede Statusseite hat nur ihre Standardsprache.
  - Es gibt keine Sprachreiter in den Formularen, keine Übersetzungsseiten und keinen Knopf
    „Übersetzen“; man kann auch keine zweite Sprache von Hand pflegen.
  - Die öffentliche Statusseite zeigt alle Inhalte in der Standardsprache, Mails an Abonnenten und
    interne Nutzer gehen ebenfalls in der Standardsprache hinaus.
  - Der Sprachwähler oben schaltet dann nur die Oberfläche um.
- **An:** Deutsch und Englisch wie unten beschrieben, inklusive Übersetzungsvorschlag, sofern ein
  Dienst eingerichtet ist.

Bereits gespeicherte Übersetzungen gehen beim Ausschalten nicht verloren: Sie erscheinen wieder,
sobald der Schalter an ist.

**Nach dem Update:**

- Installationen, die vorher Übersetzungen genutzt haben, starten mit ausgeschaltetem Schalter.
  Ein Global Admin muss die automatische Übersetzung in den Systemeinstellungen einmal
  einschalten, damit die zweite Sprache und die Übersetzungen wieder angezeigt werden.
- Sprachen außer Deutsch und Englisch aus dem früheren Katalog werden beim Start abgeschaltet und
  nirgends mehr angeboten; ihre Übersetzungen bleiben in der Datenbank.
- Hatte eine Statusseite eine solche Sprache als Standardsprache, ist es danach Deutsch.

## Oberflächensprache

Die Oberfläche gibt es auf Deutsch und Englisch. Die Sprache wählt man oben rechts in der
Sprachauswahl. Angemeldete Nutzer speichern sie im Profil, anonyme Besucher im Browser.

Bietet eine öffentliche Statusseite beide Sprachen an, wählt die Sprachauswahl zugleich die Sprache
der Inhalte. Hat sie nur ihre Standardsprache (etwa bei ausgeschalteter automatischer Übersetzung),
schaltet die Auswahl nur die Oberfläche um; die Inhalte bleiben in der Standardsprache.

## Sprachen einer Statusseite

Unter **Sprachen** legt man fest, in welcher Sprache eine Statusseite ihre Inhalte pflegt (Recht
`languages:update`):

- Bei ausgeschalteter automatischer Übersetzung wählst du nur die **Standardsprache** (Deutsch oder
  Englisch). Eine früher aktivierte zweite Sprache bleibt gespeichert.
- Bei eingeschalteter automatischer Übersetzung aktivierst du Deutsch, Englisch oder beide; eine
  davon ist die Standardsprache.
- Neue Statusseiten starten mit Deutsch (Standard) und Englisch.
- Global Admins sehen zusätzlich den **Sprachkatalog** mit Deutsch und Englisch: Anzeigename und ob
  die Sprache aktiv ist. Andere Sprachen lassen sich nicht anlegen oder einschalten (Meldung
  „Inhalte gibt es nur auf Deutsch und Englisch“).
- Die Tabelle zeigt vor dem Code die Landesflagge und die Spalte **Sprache** mit dem Namen in der
  Oberflächensprache; **Name** ist die Eigenbezeichnung.

## Sprache aus dem Katalog löschen

Global Admins löschen eine Sprache im Zeilenmenü des Katalogs mit **Löschen** (nach einer
Rückfrage). Die Sprache verschwindet dabei auch bei allen Statusseiten, die sie aktiviert hatten.
Es geht nur, wenn

- die Sprache bei **keiner** Statusseite die Standardsprache ist (die Meldung nennt die Seiten;
  stelle dort zuerst eine andere Standardsprache ein) und
- keine **Mail-Vorlagen** in dieser Sprache mehr existieren (zuerst unter Mail-Vorlagen löschen).

Nach dem Löschen:

- Bereits gespeicherte Übersetzungen von Vorfällen, Komponenten und Ankündigungen bleiben
  erhalten, werden aber nicht mehr angezeigt. Legt man denselben Code später erneut an, sind sie
  wieder da.
- Eine gelöschte Sprache kommt beim Neustart nicht von selbst zurück.
- Wer sie nur aus der Auswahl nehmen will, schaltet sie stattdessen auf **inaktiv**.

## Inhalte übersetzen

Nur bei eingeschalteter automatischer Übersetzung: Vorfälle (Titel und Nachricht), deren Updates
(nur Nachricht), Komponenten (Name und Beschreibung) und Ankündigungen lassen sich in die zweite
Sprache übersetzen. Jede Übersetzung ist zunächst ein **Entwurf** und muss **bestätigt**
werden; nur bestätigte Übersetzungen gibt die öffentliche API aus.

### Anzeige auf der Statusseite

Die öffentliche Statusseite zeigt Namen und Beschreibungen von Komponenten und Gruppen sowie Titel
und Meldung von Vorfällen und Ankündigungen sowie die Nachrichten der Updates eines Vorfalls in der
Sprache, die der Besucher im Sprachwähler gewählt hat.

- **Fehlende Übersetzung:** Fehlt eine bestätigte Übersetzung in dieser Sprache, nimmt Statushafen
  die Übersetzung in der Standardsprache der Seite, sonst den Originaltext.
- **Nie übersetzt:** Entwürfe und Meldungen externer Quellen werden nie übersetzt angezeigt.
- **Statusverlauf:** Die Statusverlauf-Texte haben keine Übersetzung und erscheinen im Original.

### Übersetzungsvorschlag

Übersetzt wird von Hand oder mit einem Vorschlag: In Vorfällen, Wartungen und Ankündigungen steht
beim Reiter einer weiteren Sprache der Knopf **Aus {Standardsprache} übersetzen**. Er schickt Titel
und Nachricht der Standardsprache an den eingerichteten Übersetzungsdienst und trägt das Ergebnis
in die Felder ein.

- **Entwurf:** Das Ergebnis ist ein **Entwurf**: Der Haken „Übersetzung ist fertig“ wird
  zurückgenommen, veröffentlicht wird erst nach deiner Prüfung.
- **Voraussetzungen:** Dafür brauchst du das Recht „Ändern“ des jeweiligen Tabs und einen Titel in
  der Standardsprache (bei Updates: eine Nachricht, denn ein Update hat keinen Titel).
- **Grenze:** höchstens 5000 Zeichen je Anfrage.
- **Kein Dienst:** Ist kein Dienst eingerichtet, der Dienst nicht erreichbar oder die Sprache nicht
  unterstützt (der Standarddienst kennt nur die Sprachen, die der Betreiber geladen hat), erscheint
  eine Meldung unter dem Knopf; das Formular bleibt voll nutzbar. Den Dienst richten Global Admins
  unter [Systemeinstellungen](/docs/systemeinstellungen) ein.
- **Ersatz für DeepL:** Ist als Dienst DeepL gewählt und fällt aus (Kontingent erschöpft, Zugang
  abgelehnt, nicht erreichbar oder Sprache nicht unterstützt), übersetzt automatisch der
  Standarddienst des Betreibers (LibreTranslate), sofern er eingerichtet ist und die Sprache kann.
  Ein Hinweis unter dem Knopf nennt den Ersatzdienst; scheitert auch er, erscheint der Fehler von
  DeepL.
- Titel und Nachricht gehen dabei in einer einzigen Anfrage an den Dienst.

Mit **In alle n leeren Sprachen übersetzen** (über den Reitern) übersetzt Statushafen die
Standardsprache nacheinander in alle weiteren Sprachen, die noch leer sind, jeweils als Entwurf.

- Ausgefüllte Sprachen bleiben unberührt.
- Nicht unterstützte Sprachen werden übersprungen und in der Schlussmeldung gezählt.
- Jeder andere Fehler (Kontingent, Zugang, Dienst oder Verbindung nicht erreichbar, Sitzung
  abgelaufen) bricht den Lauf ab und wird genannt.

Meldungen im Verwaltungsbereich (z. B. „gespeichert“) stehen oben im Inhaltsbereich und bleiben beim
Scrollen sichtbar. Erfolgsmeldungen blenden sich nach wenigen Sekunden von selbst aus; Fehler und
Warnungen bleiben stehen, bis man sie schließt.

Der Standarddienst (LibreTranslate) unterstützt Deutsch und Englisch.

## UI-Texte anpassen

Unter **UI-Texte** lassen sich einzelne Texte der Oberfläche überschreiben, etwa eine
Knopf-Beschriftung:

- **Global** (nur Global Admins): gilt in der ganzen App, auch auf öffentlichen Seiten.
- **Für eine Statusseite** (Recht `ui_translations:update`): gilt im Admin-Bereich dieser Seite.

Es gilt der speziellste Wert: Seite vor global vor Standardtext.

- **Suchen:** Die Tabelle ist nach Bereich und Sprache filterbar und durchsuchbar (nach Schlüssel
  oder Text).
- **Speichern:** Geänderte Einträge sind als „angepasst“ bzw. „ungespeichert“ markiert; „Alle
  speichern“ speichert alle offenen Änderungen auf einmal.
- **Zurücksetzen:** Ein leerer Text lässt sich nicht speichern: Um eine Anpassung zu entfernen,
  nutzt man „Zurücksetzen“ (mit Rückfrage; betrifft nur die gewählte Sprache und den gewählten
  Geltungsbereich).
