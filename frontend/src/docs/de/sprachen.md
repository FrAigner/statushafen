# Sprachen und Übersetzungen

Statushafen trennt drei Dinge: die Sprache der **Oberfläche**, die Sprachen der **Inhalte** einer
Statusseite und **eigene UI-Texte**.

## Oberflächensprache

Die Oberfläche gibt es auf Deutsch und Englisch. Die Sprache wählt man oben rechts in der
Sprachauswahl. Angemeldete Nutzer speichern sie im Profil, anonyme Besucher im Browser.

Auf einer öffentlichen Statusseite zeigt die Sprachauswahl **alle für diese Seite aktivierten
Sprachen** (mit ihrer Eigenbezeichnung), nicht nur Deutsch und Englisch. Die Inhalte erscheinen in
der gewählten Sprache. Gibt es für eine Sprache keine Oberflächentexte (etwa Kroatisch), bleiben
Beschriftungen wie Knöpfe und Überschriften auf Deutsch; die Texte der Vorfälle, Komponenten und
Ankündigungen sind trotzdem übersetzt.

## Sprachen einer Statusseite

Unter **Sprachen** legt man fest, in welchen Sprachen eine Statusseite Inhalte anbietet (Recht
`languages:update`):

- Mindestens eine Sprache muss aktiv sein, eine davon ist die Standardsprache.
- Neue Statusseiten starten mit Deutsch (Standard) und Englisch.
- Global Admins pflegen zusätzlich den **Sprachkatalog**: Code, Anzeigename und ob die Sprache
  aktiv ist. Nur Sprachen aus dem Katalog lassen sich einer Seite zuweisen.
  Der Katalog enthält von Anfang an 30 Sprachen (Deutsch, Englisch und viele weitere europäische
  Sprachen, mit ihrer Eigenbezeichnung). Was der Betreiber ändert oder abschaltet, bleibt erhalten.
  Bei vielen Sprachen zeigt das Bearbeiten-Formular statt der Reiter eine Auswahlliste.
- Die Tabelle zeigt vor dem Code die Landesflagge (sofern bekannt) und die Spalte **Sprache** mit dem
  Namen in der Oberflächensprache (etwa „Bulgarisch“ bzw. „Bulgarian“); **Name** ist die Eigenbezeichnung.
- **Format des Codes:** 2 bis 3 Buchstaben, optional mit Untertags aus 2 bis 8 Buchstaben oder
  Ziffern, durch Bindestrich getrennt (`fr`, `en-us`, `zh-hant-tw`). Großbuchstaben werden zu
  Kleinbuchstaben; alles andere (`FRA!`, `e`) lehnt Statushafen ab. Name und Code sind Pflicht.

## Sprache aus dem Katalog löschen

Global Admins löschen eine Sprache im Zeilenmenü des Katalogs mit **Löschen** (nach einer
Rückfrage). Die Sprache verschwindet dabei auch bei allen Statusseiten, die sie aktiviert hatten.
Es geht nur, wenn

- die Sprache bei **keiner** Statusseite die Standardsprache ist (die Meldung nennt die Seiten;
  stelle dort zuerst eine andere Standardsprache ein) und
- keine **Mail-Vorlagen** in dieser Sprache mehr existieren (zuerst unter Mail-Vorlagen löschen).

Bereits gespeicherte Übersetzungen von Vorfällen, Komponenten und Ankündigungen bleiben erhalten,
werden aber nicht mehr angezeigt. Legt man denselben Code später erneut an, sind sie wieder da.
Eine gelöschte Sprache kommt beim Neustart nicht von selbst zurück. Wer sie nur aus der Auswahl
nehmen will, schaltet sie stattdessen auf **inaktiv**.

## Inhalte übersetzen

Vorfälle (Titel und Nachricht), deren Updates (nur Nachricht), Komponenten (Name und Beschreibung)
und Ankündigungen lassen sich je Sprache übersetzen. Jede Übersetzung ist zunächst ein **Entwurf** und muss **bestätigt**
werden; nur bestätigte Übersetzungen gibt die öffentliche API aus.

Die öffentliche Statusseite zeigt Namen und Beschreibungen von Komponenten und Gruppen sowie Titel
und Meldung von Vorfällen und Ankündigungen sowie die Nachrichten der Updates eines Vorfalls in der
Sprache, die der Besucher im Sprachwähler gewählt hat. Fehlt eine bestätigte Übersetzung in dieser Sprache, nimmt Statushafen die
Übersetzung in der Standardsprache der Seite, sonst den Originaltext. Entwürfe und Meldungen
externer Quellen werden nie übersetzt angezeigt. Die Statusverlauf-Texte haben keine
Übersetzung und erscheinen im Original.

Übersetzt wird von Hand oder mit einem Vorschlag: In Vorfällen, Wartungen und Ankündigungen steht
beim Reiter einer weiteren Sprache der Knopf **Aus {Standardsprache} übersetzen**. Er schickt Titel
und Nachricht der Standardsprache an den eingerichteten Übersetzungsdienst und trägt das Ergebnis
in die Felder ein. Das Ergebnis ist ein **Entwurf**: Der Haken „Übersetzung ist fertig“ wird
zurückgenommen, veröffentlicht wird erst nach deiner Prüfung. Dafür brauchst du das Recht „Ändern“
des jeweiligen Tabs und einen Titel in der Standardsprache (bei Updates: eine Nachricht, denn ein
Update hat keinen Titel); höchstens 5000 Zeichen je Anfrage.

Ist kein Dienst eingerichtet, der Dienst nicht erreichbar oder die Sprache nicht unterstützt (der
Standarddienst kennt nur die Sprachen, die der Betreiber geladen hat), erscheint eine Meldung unter
dem Knopf; das Formular bleibt voll nutzbar. Den Dienst richten Global Admins unter
[Systemeinstellungen](/docs/systemeinstellungen) ein.

Ist als Dienst DeepL gewählt und fällt aus (Kontingent erschöpft, Zugang abgelehnt, nicht erreichbar
oder Sprache nicht unterstützt), übersetzt automatisch der Standarddienst des Betreibers
(LibreTranslate), sofern er eingerichtet ist und die Sprache kann. Ein Hinweis unter dem Knopf nennt
den Ersatzdienst; scheitert auch er, erscheint der Fehler von DeepL. Titel und Nachricht gehen dabei
in einer einzigen Anfrage an den Dienst.

Mit **In alle n leeren Sprachen übersetzen** (über den Reitern) übersetzt Statushafen die Standardsprache
nacheinander in alle weiteren Sprachen, die noch leer sind, jeweils als Entwurf. Ausgefüllte Sprachen
bleiben unberührt. Nicht unterstützte Sprachen werden übersprungen und in der Schlussmeldung gezählt;
jeder andere Fehler (Kontingent, Zugang, Dienst oder Verbindung nicht erreichbar, Sitzung abgelaufen)
bricht den Lauf ab und wird genannt.

Meldungen im Verwaltungsbereich (z. B. „gespeichert“) stehen oben im Inhaltsbereich und bleiben beim
Scrollen sichtbar. Erfolgsmeldungen blenden sich nach wenigen Sekunden von selbst aus; Fehler und
Warnungen bleiben stehen, bis man sie schließt.

Der Standarddienst (LibreTranslate) unterstützt **23 der 30** Katalogsprachen: Bulgarisch, Dänisch, Deutsch,
Englisch, Estnisch, Finnisch, Französisch, Griechisch, Irisch, Italienisch, Lettisch, Litauisch, Niederländisch,
Polnisch, Portugiesisch, Rumänisch, Schwedisch, Slowakisch, Slowenisch, Spanisch, Tschechisch, Türkisch und
Ungarisch. **Nicht unterstützt** sind Bosnisch, Kroatisch, Luxemburgisch, Mazedonisch, Maltesisch, Rumantsch und
Serbisch; dafür bleibt nur die Übersetzung von Hand. (Stand Version 1.9.6; auf dem Betriebsserver sind
bewusst nur Deutsch und Englisch geladen.)

## UI-Texte anpassen

Unter **UI-Texte** lassen sich einzelne Texte der Oberfläche überschreiben, etwa eine
Knopf-Beschriftung:

- **Global** (nur Global Admins): gilt in der ganzen App, auch auf öffentlichen Seiten.
- **Für eine Statusseite** (Recht `ui_translations:update`): gilt im Admin-Bereich dieser Seite.

Es gilt der speziellste Wert: Seite vor global vor Standardtext.

Die Tabelle ist nach Bereich und Sprache filterbar und durchsuchbar (nach Schlüssel oder Text).
Geänderte Einträge sind als „angepasst“ bzw. „ungespeichert“ markiert; „Alle speichern“ speichert
alle offenen Änderungen auf einmal. Ein leerer Text lässt sich nicht speichern: Um eine Anpassung
zu entfernen, nutzt man „Zurücksetzen“ (mit Rückfrage; betrifft nur die gewählte Sprache und den
gewählten Geltungsbereich).
