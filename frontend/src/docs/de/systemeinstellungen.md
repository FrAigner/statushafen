# Systemeinstellungen

Unter **Systemeinstellungen** stellen Global Admins Schalter für die gesamte Plattform ein. Ist
ein Schalter nie gesetzt worden, gilt er als eingeschaltet (Ausnahme: die Selbstüberwachung ist dann aus). Jede Änderung landet im
[Audit-Log](/docs/audit-log) (`system_setting.updated`).

## Self-Service-Bestellung aktiviert

Steuert, ob neue Statusseiten über `/order` bestellt werden können, siehe
[Erste Schritte](/docs/erste-schritte).

- **An:** Die Startseite zeigt „Eigene Statusseite bestellen“.
- **Aus:** Der Knopf verschwindet, das Formular zeigt „Bestellung derzeit nicht verfügbar“ und
  neue Bestellungen werden abgelehnt.

## E-Mail-Versand aktiviert

Steuert, ob Statushafen überhaupt E-Mails verschickt.

- **Aus:** Es gehen keine Mails mehr hinaus, weder Bestell- und Abo-Bestätigungen noch
  Benachrichtigungen zu Vorfällen, Wartungen und Ankündigungen. Jede übersprungene Mail steht im
  Audit-Log als `email.skipped_disabled` mit Empfänger.
- Google-Chat-Webhooks laufen weiter.

Der Schalter eignet sich etwa, wenn das Versandkontingent des Mail-Anbieters erschöpft ist oder
eine Testinstanz keine Mails verschicken soll.

## Selbstüberwachung

Schaltet die automatische Selbstüberwachung der Systemseite ein oder aus (siehe
[Selbstüberwachung](/docs/selbstueberwachung)). Anders als die übrigen Schalter ist sie ohne Eintrag **aus** und läuft erst nach ausdrücklichem Einschalten.

- **An:** Statushafen prüft API, Datenbank, Frontend, E-Mail-Versand und Backup und setzt den
  Status der Komponenten auf der Systemseite.
- **Aus:** Es werden keine Prüfungen ausgewertet; bereits gesetzte Störungen bleiben bestehen.

## Automatische Übersetzung

Legt fest, welcher Dienst den Knopf **Übersetzen** in den Formularen bedient (siehe
[Sprachen](/docs/sprachen)). Der Vorschlag landet immer als Entwurf.

- **Standard des Betreibers (LibreTranslate):** Der Betreiber stellt den Dienst bereit
  (Umgebungsvariable `LIBRETRANSLATE_URL`). Die Texte verlassen den Betrieb nicht. Es gibt nur die
  Sprachen, die dort geladen sind; für andere meldet der Knopf „Sprache nicht unterstützt“.
- **Eigener LibreTranslate-Server:** Adresse (nur `https://`, Port 443, öffentlich erreichbar;
  interne Adressen werden abgelehnt) und optional ein API-Schlüssel.
- **DeepL (eigener Schlüssel):** Die Texte werden zur Übersetzung an DeepL gesendet, bitte den
  Datenschutz beachten. Mit einem eigenen kostenlosen Schlüssel stehen rund 500.000 Zeichen im
  Monat zur Verfügung; Schlüssel für das Free-Angebot enden auf `:fx`.

Der Schlüssel wird nur gespeichert und nie wieder angezeigt oder ausgegeben: Das Feld bleibt leer,
„Gespeicherten Schlüssel entfernen“ löscht ihn. Im Audit-Log steht nur „gesetzt“ bzw. „entfernt“.
**Verbindung testen** übersetzt einen Beispieltext mit der gespeicherten Konfiguration.

## Übersicht und Monitoring

Zwei weitere Seiten stehen nur Global Admins offen:

- **Übersicht** (Bereich Betrieb) ist die Mandanten-Übersicht: Kennzahlen zu aktiven Statusseiten,
  offenen Vorfällen, Abonnenten und Gesamtstatus sowie eine durchsuchbare Tabelle aller Seiten mit
  Status, Komponenten, Abonnenten und offenen Vorfällen.
- **Monitoring** (Bereich System) zeigt die Betriebskennzahlen des Servers aus der eingebauten
  Messung: Anfragen, Fehlerquote (nur Antworten mit Status 5xx; 4xx wie 404 oder 409 zählen nicht, Markierung gelb ab 5 %, rot ab 10 %), Latenz (Mittel, p50, p95, Maximum), Anfragen pro Minute und
  Prozess-Uptime, dazu die Auslastung (Goroutinen, Heap, GC-Zyklen, Datenbank-Pool) und eine
  Tabelle je Endpunkt. Ein Link öffnet das Grafana-Dashboard des Betreibers.
