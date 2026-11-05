# Benutzer, Rollen und Rechte

## Global Admin und Seitenrollen

- **Global Admin:** darf alles, auf allen Statusseiten. Nur Global Admins sehen Übersicht,
  Statusseiten, Audit-Log, Benutzer, Rollen & Rechte, Tags, Monitoring und Systemeinstellungen.
- **Rolle je Statusseite:** Alle anderen Nutzer brauchen auf einer Statusseite eine Rolle. Die
  Rolle bündelt Rechte (Permissions). Ohne Rolle auf der Seite gibt es keinen Zugriff.

Benutzer und Rollen verwalten Global Admins unter **Benutzer** und **Rollen & Rechte**. Nutzer
mit `pages:create` dürfen Benutzern ihrer Seite eine Rolle zuweisen (derzeit nur über die API; in
der Oberfläche hat das Zeilenmenü „Benutzer“ unter **Statusseiten** nur der Global Admin). Hat
der Nutzer schon eine Rolle auf der Seite, wird sie ersetzt.

Beim Anlegen eines Benutzers sind E-Mail-Adresse und ein Passwort mit **mindestens 8 Zeichen**
Pflicht; dieselbe Regel gilt, wenn man ein Passwort ändert oder bei der Bestellung setzt. „Global
Admin“ ist keine Seitenrolle, sondern ein Schalter am Benutzer (Vollzugriff auf alle Seiten). Zum
Selbstschutz gilt: Das eigene Konto lässt sich nicht unter **Benutzer** löschen (dafür gibt es das
Profil), die eigenen Global-Admin-Rechte kann man sich nicht selbst entziehen, und der letzte
Global Admin bleibt immer erhalten.

## Standardrollen für Seiten

- **Page Owner:** alle Rechte von „Page Admin“ plus die Rechte an Statusseiten, Mail-Templates, Sprachen, UI-Texten und SSO. Wer eine Seite
  einschließlich ihrer Benutzer, SSO, Domain, Mail-Templates und Sprachen verantwortet, bekommt
  diese Rolle.
- **Page Admin:** pflegt Inhalte (Komponenten, Vorfälle, Ankündigungen, Abonnenten), aber
  nicht die Seiteneinstellungen und Benutzer.
- **Editor:** Komponenten lesen, Vorfälle, Wartungen, Vorlagen und Ankündigungen bearbeiten.
- **Viewer:** nur lesen (Komponenten, Vorfälle, Ankündigungen).

Die Rolle „Page Owner“ wird beim ersten Start automatisch angelegt (einmalig, falls es keine Rolle dieses Namens
gibt). Global Admins können sie unter **Rollen & Rechte** anpassen, umbenennen oder löschen; das bleibt erhalten und sie wird nicht neu angelegt.
Wer eine Statusseite selbst bestellt, wird „Page Admin“ dieser Seite. Soll er auch Benutzer, SSO,
Mail-Templates und Sprachen der eigenen Seite pflegen, weist ein Global Admin ihm „Page Owner“ zu.

Wer auf einer Seite Benutzer verwalten darf (`pages:create`/`pages:delete`), kann nur Rollen
vergeben, deren Rechte er selbst auf dieser Seite hat, und neue Konten nur per E-Mail-Adresse
einladen: Mitglied wird das Konto erst, wenn es die Einladung annimmt (siehe **Statusseiten**). Konten mit altem globalem SSO, Seiten-SSO-Verknüpfung oder Rolle auf einer anderen Seite sowie Global-Admin-Konten fügt nur ein Global Admin hinzu. Beliebige Konten per ID auswählen darf nur ein Global Admin.
Pro Konto und Seite gibt es genau eine Rolle. Als Nicht-Global-Admin lässt sich die eigene Rolle nicht entfernen oder auf eine Rolle ohne `pages:update` herabstufen, wenn man das letzte Mitglied mit diesem Recht auf der Seite ist (Meldung „letzter Benutzer mit Verwaltungsrechten“). Beim Entfernen eines Mitglieds, beim Löschen seiner Rolle und beim Entziehen der Seite am Konto wird auch seine Seiten-SSO-Verknüpfung dieser Seite gelöscht.


## Rechte

Jeder Tab der Admin-Navigation ist eine eigene Ressource mit vier Aktionen: **Anlegen**
(`create`), **Lesen** (`read`), **Ändern** (`update`) und **Löschen** (`delete`). Ein Recht heißt
`<ressource>:<aktion>`. Jede Route der API prüft die zur HTTP-Methode passende Aktion: GET braucht
Lesen, POST Anlegen, PUT und PATCH Ändern, DELETE Löschen. Sonderaktionen sind einer Aktion fest
zugeordnet (siehe Spalte „Routen“).

| Bereich | Tab | Ressource | Gilt für | Rechte | Routen |
|---|---|---|---|---|---|
| Betrieb | Übersicht | `overview` | global | `overview:create`, `overview:read`, `overview:update`, `overview:delete` | GET /admin/overview |
| Betrieb | Komponenten | `components` | Seite | `components:create`, `components:read`, `components:update`, `components:delete` | POST/PUT/DELETE /pages/{id}/components, Komponenten-Übersetzungen |
| Betrieb | Externe Quellen | `external_sources` | Seite | `external_sources:create`, `external_sources:read`, `external_sources:update`, `external_sources:delete` | /pages/{id}/external-sources, -links, -incidents, -outages; „Jetzt abrufen“ = Ändern, „Übernehmen“ = Anlegen (zusätzlich `components:create`) |
| Betrieb | Vorfälle | `incidents` | Seite | `incidents:create`, `incidents:read`, `incidents:update`, `incidents:delete` | /pages/{id}/incidents (ohne Wartungen), Timeline-Einträge = Anlegen, Post-Mortem = Ändern, Vorfall-Übersetzungen |
| Betrieb | Wartung | `maintenance` | Seite | `maintenance:create`, `maintenance:read`, `maintenance:update`, `maintenance:delete` | dieselben Routen wie Vorfälle für Wartungsfenster (die Art des Datensatzes entscheidet) |
| Betrieb | Ankündigungen | `notices` | Seite | `notices:create`, `notices:read`, `notices:update`, `notices:delete` | /pages/{id}/notices, Ankündigungs-Übersetzungen |
| Betrieb | Vorlagen | `incident_templates` | Seite | `incident_templates:create`, `incident_templates:read`, `incident_templates:update`, `incident_templates:delete` | /pages/{id}/incident-templates |
| Betrieb | Abonnenten | `subscribers` | Seite | `subscribers:create`, `subscribers:read`, `subscribers:update`, `subscribers:delete` | /pages/{id}/subscribers |
| Inhalte | Mail-Templates | `notification_templates` | Seite | `notification_templates:create`, `notification_templates:read`, `notification_templates:update`, `notification_templates:delete` | /pages/{id}/notification-templates, Speichern = Ändern, Vorschau = Lesen |
| Inhalte | Sprachen | `languages` | Seite | `languages:create`, `languages:read`, `languages:update`, `languages:delete` | PUT /pages/{id}/languages; die Sprachen der Seite lesen darf jede Seitenrolle, der Sprachkatalog ist Global Admins vorbehalten |
| Inhalte | UI-Texte | `ui_translations` | Seite | `ui_translations:create`, `ui_translations:read`, `ui_translations:update`, `ui_translations:delete` | /pages/{id}/ui-overrides; Lesen darf jede Seitenrolle, globale Texte sind Global Admins vorbehalten |
| Verwaltung | Statusseiten | `pages` | Seite | `pages:create`, `pages:read`, `pages:update`, `pages:delete` | PUT/DELETE /admin/pages/{id}, Benutzer der Seite, Webhook-Token und eigene Domain = Ändern; eine Seite anlegen dürfen nur Global Admins |
| Verwaltung | Benutzer | `users` | global | `users:create`, `users:read`, `users:update`, `users:delete` | /users |
| Verwaltung | Rollen & Rechte | `roles` | global | `roles:create`, `roles:read`, `roles:update`, `roles:delete` | /roles |
| Verwaltung | Tags | `tags` | global | `tags:create`, `tags:read`, `tags:update`, `tags:delete` | /tags |
| Verwaltung | Audit-Log | `audit` | global | `audit:create`, `audit:read`, `audit:update`, `audit:delete` | GET /admin/audit |
| System | Monitoring | `monitoring` | global | `monitoring:create`, `monitoring:read`, `monitoring:update`, `monitoring:delete` | GET /admin/monitoring |
| System | SSO | `sso` | Seite | `sso:create`, `sso:read`, `sso:update`, `sso:delete` | /admin/pages/{id}/sso, Testanmeldung = Ändern |
| System | Systemeinstellungen | `system_settings` | global | `system_settings:create`, `system_settings:read`, `system_settings:update`, `system_settings:delete` | /settings |

Zusätzlich gibt es `admin_dashboard`: Menüpunkt „Dashboard“ im Benutzermenü der öffentlichen Seite.

**Anlegen, Ändern und Löschen schließen Lesen ein:** Wer `components:update` hat, darf auch lesen,
ohne dass `components:read` eingetragen sein muss. Ändern schließt Anlegen oder Löschen nicht ein.
Das gilt für jede Ressource. Tabs ohne Leserecht werden ausgeblendet, ebenso Bereiche ohne
sichtbaren Tab; ruft jemand die Adresse einer solchen Seite direkt auf, erscheint „Keine
Berechtigung“. Es zählt immer die Rolle auf der aktuell gewählten Statusseite. Knöpfe und
Menüeinträge erscheinen je Aktion: „Neu …“ mit Anlegen, Bearbeiten mit Ändern, Löschen mit Löschen.

**Globale Ressourcen** (Übersicht, Benutzer, Rollen & Rechte, Tags, Audit-Log, Monitoring,
Systemeinstellungen) gelten nicht für eine einzelne Seite, sondern für die ganze Plattform. Sie
sind Global Admins vorbehalten; eine Seitenrolle kann sie nicht wirksam vergeben. Der Rollen-Editor
zeigt sie in der Matrix gesperrt. Dasselbe gilt für das Anlegen von Statusseiten, den Sprachkatalog
und die globalen UI-Texte.

**Umstellung von `:write`:** Bis zur Version mit diesem Modell gab es je Bereich nur Lesen und
Schreiben (`<ressource>:write`). Beim Start ersetzt eine einmalige Migration in allen Rollen
`:write` durch Anlegen, Lesen, Ändern und Löschen und trägt die Tabs ein, die bisher an einer
anderen Ressource hingen (`components:write` gibt auch `external_sources:*`, `incidents:write`
auch `maintenance:*` und `incident_templates:*`, `pages:write` auch `notification_templates:*`,
`languages:*`, `ui_translations:*` und `sso:*`). Niemand bekommt dadurch Rechte dazu oder verliert
welche. Die alten Werte stehen als Sicherung in den Systemeinstellungen
(`permissions.crud_backup`). Alte Anmelde-Token mit `:write` laufen bis zum Ablauf weiter; die
Rechte kommen ohnehin bei jeder Anfrage frisch aus der Rolle. Ein noch vorhandenes `:write` in
einer Rolle zählt beim Prüfen als Anlegen, Lesen, Ändern und Löschen, wird aber nie mehr
gespeichert.

Der Rollen-Editor zeigt eine Matrix mit einer Zeile je Tab, gruppiert nach Bereich, und den
Spalten C (Anlegen), R (Lesen), U (Ändern) und D (Löschen). Die Schalter „alle“ setzen eine ganze
Zeile oder Spalte; Anlegen, Ändern und Löschen setzen Lesen automatisch.

## Wie Nutzer zu einer Rolle kommen

- **Zuweisung** durch einen Global Admin oder einen Nutzer mit `pages:create`.
- **Automatischer Beitritt:** Hat die Statusseite eine Standard-Rolle und passt die E-Mail-Domain
  des Kontos zur Liste für den automatischen Beitritt, bekommt es diese Rolle. Siehe
  [Statusseiten](/docs/statusseiten#privat-und-automatischer-beitritt).
- **Bestellung:** Wer eine Statusseite selbst bestellt, wird „Page Admin“ dieser Seite.

## Tags

Zusätzlich zur Rolle können Tags den Zugriff auf einzelne Komponenten und Vorfälle eingrenzen,
siehe [Tags und Reichweite](/docs/komponenten-und-tags#tags-und-reichweite). Tags an Benutzern
sind immer globale Tags.

## Profil

Unter **Profil** (Benutzermenü oben rechts) stellt jeder Nutzer seine Oberflächensprache ein,
verknüpft sein Konto mit dem SSO einer Statusseite und kann sein Konto löschen. Ist man der
einzige Admin einer Seite, wird diese nach eigener Bestätigung mitgelöscht. Der letzte Global
Admin kann sich nicht löschen. Seine Daten lädt man mit „Meine Daten herunterladen“
als JSON-Datei herunter (technisch `GET /api/me/export`, Anmeldung nötig).
