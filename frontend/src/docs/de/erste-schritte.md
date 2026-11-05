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

Die Anmeldung liegt unter `/login` (Knopf „Login“ oben rechts).

- **E-Mail und Passwort:** Nach dem Login geht es in den Admin-Bereich.
- **Google oder GitHub:** über die Knöpfe „Mit Google anmelden“ und „Mit GitHub anmelden“. Das
  setzt voraus, dass der Betreiber die Anmeldung bei Google bzw. GitHub eingerichtet hat.
- **SSO der Statusseite:** Kommt man von einer Statusseite, die OIDC oder SAML eingerichtet hat,
  zeigt die Login-Seite zusätzlich „Mit SSO anmelden“. Details unter [SSO](/docs/sso).

## Nach dem ersten Login

1. Oben in der Kopfzeile die Statusseite wählen (Feld „Mandant“), falls man mehrere hat.
2. Unter **Komponenten** die Dienste anlegen, die die Seite zeigen soll. Dienste eines
   Cloud-Anbieters lassen sich unter **Externe Quellen** übernehmen, siehe
   [Externe Quellen](/docs/externe-quellen).
3. Die öffentliche Seite über das Benutzermenü und „Öffentliche Ansicht“ prüfen.
4. Im Ernstfall unter **Vorfälle** einen Vorfall melden, siehe [Vorfälle](/docs/vorfaelle).

## Der Admin-Bereich

- **Kopfzeile:** links der Mandant-Umschalter, rechts Hell/Dunkel, Sprachwahl und das
  Benutzermenü. Auf schmalen Bildschirmen öffnet das Menü-Symbol links in der Kopfzeile die
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
Er erscheint nur für Rollen mit dem Recht `admin_dashboard`.
