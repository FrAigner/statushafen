# Audit-Log

Das **Audit-Log** (nur Global Admins) zeigt, wer wann was geändert hat. Die neuesten Einträge
stehen oben, 25 je Seite.

## Spalten

| Spalte | Inhalt |
|---|---|
| Zeitpunkt | wann die Aktion stattfand |
| Akteur | E-Mail und Rolle des Nutzers; automatische Aktionen erscheinen als „System“ |
| Aktion | was passiert ist, als lesbarer Text (z. B. „Vorfall angelegt“); der technische Schlüssel wie `incident.created` steht darunter und ist die Grundlage der Tabelle unten |
| Kategorie | Bereich der Aktion |
| IP-Adresse | Adresse, von der die Aktion kam |

## Filtern

- **Suchfeld** „Nach Akteur, Ziel oder IP filtern…“: durchsucht E-Mail des Akteurs, Name und ID
  des Ziels sowie die IP-Adresse.
- **Kategorien:** Je ein Chip für Authentifizierung, Vorfall, Konfiguration, Nutzer, Abonnent,
  Ankündigung, Wartung und System (automatische Prüfungen der Selbstüberwachung). Ein Klick
  filtert, ein zweiter hebt den Filter auf; Chip und Suchfeld lassen sich kombinieren.

## Beispiele für Aktionen

Die Tabelle nennt die technischen Schlüssel; die Oberfläche zeigt dafür die lesbaren Texte.

| Bereich | Aktionen |
|---|---|
| Anmeldung | `auth.login`, `auth.login_failed`, `auth.sso_login`, `auth.sso_linked` |
| Vorfälle und Komponenten | `incident.*`, `postmortem.published`, `component.status_webhook_updated` |
| Statusseiten | `statuspage.*`, z. B. `webhook_token_generated` |
| Nutzer und Rollen | `user.*`, `role.*` |
| Abonnenten und Mails | `subscriber.*`, `notification.email_failed`, `email.skipped_disabled` |
| Externe Quellen | `external_source.*` (u. a. `fetched`, `outage_email_sent`), `external_link.*`, `external_incident.*` (`mirrored`, `failed`, `pruned`) |
| Sprachen und Texte | `language.*`, `page_languages.updated`, `notification_template.*` |
| System | `system_setting.updated`, `pageorder.*` |

Löscht ein Nutzer sein Konto, bleiben seine Audit-Einträge ohne Bezug zur Person erhalten.
