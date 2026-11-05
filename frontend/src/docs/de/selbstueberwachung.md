# Selbstüberwachung

Statushafen überwacht sich selbst und trägt das Ergebnis auf der Systemseite
(`system.example.com`) ein. Die Komponenten unter **Plattform** stehen dadurch nicht mehr nur
auf „in Betrieb“, bis jemand einen Vorfall anlegt: eine Störung wird erkannt und angezeigt.

## Was geprüft wird

Die Prüfungen laufen im Hintergrund, im Minutentakt (E-Mail und Backup alle fünf Minuten).

| Komponente | Prüfung | Status bei Ausfall |
| --- | --- | --- |
| API | Der Health-Endpunkt der eigenen Anwendung antwortet | Größere Störung |
| Datenbank | Eine echte Abfrage gelingt | Größere Störung |
| Frontend | Die Anwendung liefert ihre Startseite selbst aus (intern, HTML mit Einstieg der Seite und eine eingebundene Datei aus `/assets/`) | Größere Störung |
| E-Mail-Versand | Der Mailserver nimmt Verbindungen an (es wird nichts versendet) | Teilstörung |
| Datenbank-Backup | Das letzte nächtliche Backup ist höchstens 36 Stunden alt | Beeinträchtigt |

Ist der E-Mail-Versand in den [Systemeinstellungen](/docs/systemeinstellungen) abgeschaltet,
wird der Mailserver nicht geprüft. Die Backup-Prüfung gibt es nur, wenn der Betreiber sie
eingerichtet hat (Umgebungsvariable `SELFMONITOR_BACKUP_JOB`, z. B. `backup-prod`; erlaubt sind
Kleinbuchstaben, Ziffern und Bindestrich). Fehlt bei gesetztem Job das Metrik-Verzeichnis
(`SELFMONITOR_METRICS_DIR`, Standard `/job-metrics`, zum Beispiel weil der Mount fehlt), gilt das
als Fehlschlag: die Komponente wird „Beeinträchtigt“ (nach drei Fehlschlägen im Fünf-Minuten-Takt,
also rund 15 Minuten nach dem ersten) und hält die Störung weitere 15 Minuten an, entsteht ein
Vorfall (insgesamt rund 25 bis 30 Minuten ab dem ersten Fehlschlag). Fehlt
nur die Metrikdatei im vorhandenen Verzeichnis (Umgebung ohne Backup-Timer), wird die Prüfung
übersprungen und die Komponente nicht als gestört gezeigt.

## Wie der Status entsteht

- **Entprellung:** Erst nach drei Fehlschlägen in Folge kippt die Komponente. Ein einzelner
  Aussetzer ändert nichts.
- **Vorfall:** Hält die Störung 15 Minuten an, legt das System automatisch einen Vorfall
  „Störung: …“ an (Status „Wird untersucht“, Verfasser „system“). Es werden keine
  Benachrichtigungen an Abonnenten verschickt.
- **Erholung:** Sobald die Prüfung wieder gelingt, geht die Komponente auf „in Betrieb“ zurück
  und der Vorfall wird geschlossen.
- **Eigene Eingriffe haben Vorrang:** Steht eine Komponente nicht auf „in Betrieb“ (zum Beispiel
  wegen einer Wartung oder weil ein Admin sie gesetzt hat), ändert die Überwachung sie nicht.
  Hat ein Admin den Vorfall schon geschlossen, bleibt er geschlossen.
- **Audit-Log:** Jede Änderung steht im [Audit-Log](/docs/audit-log) mit dem Akteur „system“
  (`selfmonitor.component_failed`, `selfmonitor.incident_opened`, `selfmonitor.component_recovered`,
  `selfmonitor.component_released`). Letzteres entsteht, wenn ein Admin die überwachte Komponente
  gelöscht hat: der Monitor räumt auf und schließt seinen offenen Vorfall.

## Ein- und ausschalten

Der Schalter **Selbstüberwachung aktiviert** in den [Systemeinstellungen](/docs/systemeinstellungen)
schaltet die Prüfungen ab, etwa bei geplanten Arbeiten am Server. Ohne Eintrag ist sie **ausgeschaltet**: Sie läuft erst, wenn ein Global Admin den Schalter ausdrücklich einschaltet.
Beim Abschalten gibt die Überwachung die Störungen frei, die sie selbst gesetzt hat: Die Komponente
geht zurück auf „in Betrieb“, der automatische Vorfall wird mit dem Hinweis „Automatische Verfolgung
beendet.“ geschlossen (Audit-Log: „Überwachung abgeschaltet“). Dasselbe gilt für die E-Mail-Prüfung,
wenn der Mailversand abgeschaltet wird. Vom Admin gesetzte Status ändert sie nicht.

Wird eine überwachte Komponente (z. B. per Datenbankeingriff) einer anderen Statusseite zugeordnet,
beendet die Überwachung ihre Verfolgung (Marker, eigener Vorfall; `selfmonitor.component_released`
mit dem Hinweis „verschoben“). Steht die Komponente noch auf dem Ausfallstatus, den die Überwachung
gesetzt hat, geht sie zurück auf „in Betrieb“; jeden anderen Status lässt sie unverändert. Ohne
offenen Vorfall ist die Komponente nicht eindeutig bekannt: Dann beendet sie nur die Verfolgung
(Hinweis „nicht mehr auf der Systemseite“) und fasst keine Komponente anderer Seiten an.

Öffentlich ausgeblendete Komponenten wertet die Überwachung nicht aus (archiviert oder unsichtbar,
auch wenn nur die übergeordnete Gruppe archiviert oder ausgeblendet ist; es gilt dieselbe Regel wie
auf der Statusseite): Sie setzt keinen Status und legt keinen öffentlichen Vorfall „Störung: …“
an, der eine öffentlich nicht vorhandene Komponente beträfe. Wird eine Komponente während einer
laufenden Störung archiviert oder ausgeblendet, gibt die Überwachung sie frei (Status zurück auf
„in Betrieb“, Vorfall mit dem Hinweis „Automatische Verfolgung beendet. Die Komponente wird nicht
mehr angezeigt.“ geschlossen, Audit-Log „Komponente archiviert oder ausgeblendet“). Weil der
Vorfall in der öffentlichen Historie bleibt, ersetzt sie dabei Titel und Meldung durch „Störung:
automatische Überwachung“ bzw. „Die automatische Selbstüberwachung meldete eine Störung.“ (ohne
Prüfungsnamen und Fehlerdetail) und löscht deren Übersetzungen. Ersetzt wird nur, was die
Überwachung selbst geschrieben hat und unverändert ist; vom Admin geänderte oder ergänzte Texte
und Updates des Admins bleiben. Dasselbe gilt, wenn die Komponente während einer Störung gelöscht
oder auf eine andere Seite verschoben wird. Bereits geschlossene Vorfälle einer Komponente, die
inzwischen ausgeblendet, gelöscht oder verschoben ist, bereinigt die Überwachung beim Start der
Anwendung und danach alle 10 Minuten auf dieselbe Weise (auch bei ausgeschalteter Überwachung). Nach dem Wiederherstellen
läuft die Überwachung normal weiter, mit frischer Entprellung.

## Grenzen

Die Überwachung läuft in der Anwendung selbst. Fällt die gesamte Anwendung oder der Server aus,
kann sie das nicht mehr eintragen; dafür bleibt eine Überwachung von außen nötig.

Die Frontend-Prüfung ruft die eigene Anwendung intern auf (`127.0.0.1`), nicht die öffentliche
Adresse. Cloudflare, Caddy (Zugangssperre, Bot-Schutz) und das Zertifikat erfasst sie deshalb nicht;
auch das braucht eine Überwachung von außen. `FRONTEND_URL` dient nur noch als Kennzeichen, dass
ein Frontend ausgeliefert wird, nie als Prüfziel. Antwortet die Anwendung selbst mit 401 oder 403,
gilt das weiterhin als Fehler.
