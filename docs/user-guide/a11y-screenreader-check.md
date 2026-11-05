# Screenreader-Prüfung (manuell)

axe prüft automatisch, was sich am Markup und an den gerenderten Farben ablesen lässt
(vitest: `src/test-support/axe.js`, Browser: `frontend/e2e/a11y.spec.js`, hell und dunkel). Ob eine
Seite mit Screenreader tatsächlich bedienbar ist, zeigt nur ein Durchgang von Hand. Diese Liste vor
größeren UI-Änderungen (neue View, neues Formular, neuer Dialog) und vor einem Release einmal
abarbeiten.

## Kombinationen

Mindestens eine pro Lauf, über die Zeit alle:

| Screenreader | Browser | System |
| --- | --- | --- |
| NVDA (aktuell) | Firefox, Chrome | Windows |
| JAWS (aktuell) | Chrome, Edge | Windows |
| VoiceOver | Safari | macOS |
| VoiceOver | Safari | iOS (Statusseite, Abonnieren) |

Nur Tastatur und Screenreader benutzen, keine Maus. Bei VoiceOver die Schnellnavigation (Rotor)
nutzen, bei NVDA/JAWS den Lesemodus und den Fokusmodus.

## Seiten

Landing Page (`/`), öffentliche Statusseite im Standard- und im Listen-Layout, Abonnieren-Dialog,
Abo verwalten, Login, Admin-Übersicht, eine Admin-Liste (z. B. Vorfälle), ein Admin-Formular
(Vorfall anlegen), Profil.

## Prüfpunkte

### Orientierung
- [ ] Seitentitel wird beim Laden angesagt und passt zur Seite.
- [ ] Landmarken-Liste (NVDA `D`, JAWS `R`, VoiceOver Rotor „Orientierungspunkte“): genau ein
      Hauptbereich (`main`), Kopfbereich und Navigation sind benannt bzw. eindeutig.
- [ ] Überschriften-Liste (`H` bzw. Rotor „Überschriften“): genau eine h1, Ebenen ohne Sprünge,
      die Überschriften beschreiben die Abschnitte.
- [ ] Sprache: deutsche Seite wird deutsch, englische (Sprachumschalter) englisch vorgelesen.

### Bedienung
- [ ] Alle Bedienelemente sind per Tab erreichbar, in sinnvoller Reihenfolge, mit sichtbarem Fokus.
- [ ] Buttons und Links haben sprechende Namen (kein „Button“, „Link“ oder nur ein Symbol); reine
      Symbol-Buttons (Theme, Sprache, Menü) sagen ihre Funktion und ggf. ihren Zustand an.
- [ ] Umschalter (Filter-Chips, Theme) melden ihren Zustand (gedrückt/nicht gedrückt).
- [ ] Ausklappbare Bereiche und Menüs melden „erweitert/reduziert“.

### Formulare
- [ ] Jedes Feld nennt Beschriftung, Typ und ggf. „Pflichtfeld“.
- [ ] Fehlermeldungen werden beim Absenden angesagt und sind dem Feld zugeordnet.
- [ ] Erfolgsmeldungen (z. B. „Abonnement gespeichert“) werden ohne Fokuswechsel angesagt
      (Live-Region).

### Dialoge
- [ ] Beim Öffnen landet der Fokus im Dialog, der Dialogtitel wird angesagt.
- [ ] Der Fokus bleibt im Dialog, `Esc` schließt ihn, danach steht der Fokus wieder auf dem
      auslösenden Element.
- [ ] Bestätigungen auf der Seite (statt `window.confirm`) werden angesagt.

### Statusinformationen
- [ ] Der Status je Komponente wird als Text angesagt, nicht nur über die Farbe.
- [ ] Die 90-Tage-Balken im Listen-Layout haben eine verständliche Zusammenfassung; einzelne Tage
      sind nicht als endlose Liste von Fokusstopps erreichbar.
- [ ] Zeitangaben werden lesbar vorgelesen (Datum und Uhrzeit, nicht nur Zahlenfolgen).

## Ergebnis festhalten

Funde als Ticket anlegen: Seite, Screenreader/Browser/System, Schritte, erwartete
und tatsächliche Ansage. Was sich automatisieren lässt (fehlender Name, falsche Rolle), zusätzlich
als Regel bzw. Testfall in `e2e/a11y.spec.js` oder den vitest-axe-Tests nachziehen.
