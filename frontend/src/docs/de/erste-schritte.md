# Erste Schritte

## Statusseite bestellen

Über „Eigene Statusseite bestellen“ auf der Startseite (Adresse `/order`) lässt sich eine
Statusseite selbst anlegen. Der Ablauf hat drei Schritte:

1. **Formular ausfüllen:** „Firmen-/Seitenname“ und „E-Mail“ sind Pflicht. Unter
   „Wunsch-Adresse (optional)“ kann man den Slug wählen, also den Teil vor `.example.com`.
   Bleibt das Feld leer, wird der Slug aus dem Namen erzeugt. Erlaubt sind Kleinbuchstaben,
   Ziffern und einzelne Bindestriche; reservierte Namen wie `www` oder `login` lehnt Statushafen ab,
   die Liste steht unter [Statusseiten](/docs/statusseiten#adresse-einer-statusseite).
2. **E-Mail bestätigen:** Statushafen schickt einen Bestätigungslink. Erst mit dem Klick darauf
   entstehen die Statusseite und das Benutzerkonto mit der Rolle „Page Admin“.
3. **Passwort setzen:** Danach setzt man sein Passwort (mindestens 8 Zeichen) und meldet sich
   ganz normal an. Eine automatische Anmeldung gibt es nicht.

Gut zu wissen:

- Beide Links gelten 24 Stunden. Nicht bestätigte Bestellungen verfallen.
- Je E-Mail-Adresse sind höchstens drei offene Bestellungen möglich.
- Gibt es zur E-Mail-Adresse schon ein Konto, wird die Bestätigung abgelehnt.
- Ein Global Admin kann die Bestellung in den [Systemeinstellungen](/docs/systemeinstellungen)
  abschalten. Das Formular zeigt dann „Bestellung derzeit nicht verfügbar“.

Ohne Self-Service legt ein Global Admin Statusseiten unter **Statusseiten** im Admin-Bereich an.

## Anmelden

Die Anmeldung liegt unter `/login` (Knopf „Anmelden“ oben rechts). Welche Wege die Login-Seite
anbietet, legt der Betreiber fest:

- **Anmeldeanbieter:** je aktivem Anmeldeanbieter ein Knopf „Mit <Name> anmelden“, z. B. für GitLab.
- **E-Mail und Passwort:** nur, wenn der Betreiber diese Anmeldung freigegeben hat.
- **SSO der Statusseite:** Kommt man von einer Statusseite, die OIDC eingerichtet hat, zeigt die
  Login-Seite zusätzlich „Mit SSO anmelden“. Details unter [SSO](/docs/sso).

Nach der Anmeldung geht es zurück auf die Seite, von der man kam, etwa auf die Statusseite. In den
Admin-Bereich führt der Link „Verwaltung“ unten auf jeder öffentlichen Seite. Wer eine Adresse im
Admin-Bereich direkt aufruft, landet nach der Anmeldung ebenfalls dort.

## Nach dem ersten Login

1. Oben in der Kopfzeile die Statusseite wählen (Feld „Mandant“), falls man mehrere hat.
2. Unter **Komponenten** die Dienste anlegen, die die Seite zeigen soll. Dienste eines
   Cloud-Anbieters lassen sich unter **Externe Quellen** übernehmen, siehe
   [Externe Quellen](/docs/externe-quellen).
3. Die öffentliche Seite über das Benutzermenü und „Öffentliche Ansicht“ prüfen.
4. Im Ernstfall unter **Vorfälle** einen Vorfall melden, siehe [Vorfälle](/docs/vorfaelle).

## Der Admin-Bereich

- **Kopfzeile:** links der Mandant-Umschalter, daneben die Schnellsuche, rechts Hell/Dunkel,
  Sprachwahl und das Benutzermenü. Auf schmalen Bildschirmen öffnet das Menü-Symbol links in der Kopfzeile die
  Navigation.
- **Bereiche:** Darunter stehen die Reiter **Betrieb**, **Inhalte**, **Verwaltung** und **System**.
  Ein Klick zeigt die Menüpunkte des Bereichs. Menüpunkte ohne passendes Recht werden
  ausgeblendet, siehe [Benutzer, Rollen und Rechte](/docs/benutzer-und-rollen).
- **Benutzermenü:** zeigt E-Mail und Rolle auf der gewählten Seite und enthält den Schalter
  „Hilfe-Tooltips anzeigen“, „Profil“, „Öffentliche Ansicht“ und „Abmelden“.
- **Hilfe-Tooltips:** Bleibt man kurz auf einem Feld oder Knopf stehen, erklärt ein Hinweis, was
  das Feld bewirkt, welche Folgen es hat und ein Beispiel. Jeder Nutzer kann das im Benutzermenü
  abschalten.
- **Ausfall-Banner:** Fällt eine [externe Quelle](/docs/externe-quellen#ausfall-einer-quelle) aus,
  steht oben im Inhaltsbereich ein Hinweis mit Link auf die Quellenseite.
- **Dokumentation:** Der Menüpunkt „Dokumentation“ im Bereich System öffnet diese Seite.

Auf der öffentlichen Statusseite führt im Benutzermenü der Eintrag „Dashboard“ in den Admin-Bereich.
Er erscheint nur, wenn deine Rolle auf genau dieser Statusseite das Recht `admin_dashboard` hat;
eine Rolle auf einer anderen Seite genügt nicht (Global Admins sehen ihn immer).

## Schnellsuche

Die Schnellsuche in der Kopfzeile findet Admin-Seiten, einzelne Einstellungen und Abschnitte dieser
Dokumentation, ohne dass man wissen muss, wo eine Einstellung steckt. Beispiele: „Antwortadresse“,
„privat“, „Discovery URL“, „Bestellung“ oder „Auto-Beitritt“.

- **Öffnen:** ins Suchfeld klicken, `Strg+K` (Mac: `Cmd+K`) drücken oder `/`, solange der Fokus
  nicht in einem Eingabefeld steht. Auf schmalen Bildschirmen öffnet das Lupen-Symbol das Feld.
- **Ergebnisse:** gruppiert nach Seiten, Einstellungen und Dokumentation. Gesucht wird in der
  eingestellten Oberflächensprache und zusätzlich nach gängigen Begriffen auf Deutsch und Englisch
  (z. B. „Reply-To“). Es erscheint nur, was du mit deinen Rechten auch öffnen darfst.
- **Bedienung:** Pfeiltasten wählen einen Treffer, `Eingabe` öffnet ihn, `Escape` schließt die Liste.
- **Sprung zur Einstellung:** Die Suche öffnet die Seite, bei Einstellungen einer Statusseite auch
  deren Bearbeiten-Formular (für die gewählte Statusseite), scrollt zum Feld, setzt den Fokus darauf
  und hebt es kurz hervor. Erscheint ein Feld erst nach einem Schalter (z. B. die Optionen einer
  privaten Seite), springt die Suche zu diesem Schalter.
