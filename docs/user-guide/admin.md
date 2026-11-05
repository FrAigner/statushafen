# Admin-Guide

Die Bedienungsanleitung für den Admin-Bereich ist seit der Umstellung App: Sie erscheint
unter `/docs` (Link „Dokumentation“ im Footer und in der Admin-Seitenleiste) und liegt als
Markdown in `frontend/src/docs/de/` und `en/`, ein Abschnitt je Datei (Reihenfolge in
[`index.js`](../../frontend/src/docs/index.js)). Änderungen an der Bedienung bitte dort pflegen, damit Doku und App eine Quelle haben.

Die wichtigsten Abschnitte für Admins:

- [Statusseiten und Mandanten](../../frontend/src/docs/de/statusseiten.md)
- [Komponenten und Tags](../../frontend/src/docs/de/komponenten-und-tags.md)
- [Vorfälle](../../frontend/src/docs/de/vorfaelle.md) und
  [Wartungsfenster](../../frontend/src/docs/de/wartungsfenster.md)
- [Mail-Templates und Platzhalter](../../frontend/src/docs/de/mail-templates.md)
- [Benutzer, Rollen und Rechte](../../frontend/src/docs/de/benutzer-und-rollen.md)
- [Audit-Log](../../frontend/src/docs/de/audit-log.md)

## Nachrichten (Glocke)

Oben rechts im Admin-Bereich zeigt die Glocke Hinweise für Admins, z. B. wenn eine externe
Statusquelle ausfällt oder ein Webhook-Kanal Anfragen drosselt (HTTP 429) beziehungsweise
Benachrichtigungen dauerhaft ablehnt. Die Zahl am Symbol nennt die ungelesenen Nachrichten
(ab zehn „9+“). Gleiche Meldungen innerhalb von 15 Minuten werden zusammengefasst („3-mal“) und
leben als ungelesen wieder auf. „Als gelesen“ gilt nur für dein eigenes Konto. Sichtbar ist die Glocke
für Global Admins und Rollen mit `components:write` oder `subscribers:write` auf der Seite;
systemweite Nachrichten sehen nur Global Admins. Nachrichten werden nach 90 Tagen gelöscht.
Webhook-Adressen, Schlüssel und E-Mail-Adressen von Abonnenten stehen nie in einer Nachricht.

## Ausführliche Betreiber-Anleitungen

Für die Einrichtung von SSO beim Identity Provider gibt es zusätzlich ausführliche Anleitungen
mit Beispielen je Anbieter. Die App-Doku fasst sie unter „SSO“ zusammen.

- [SSO-Guides](../sso/README.md)
- [OIDC-Anmeldung je Statusseite einrichten](./sso-oidc.md)
- [SAML-Anmeldung je Statusseite einrichten](./sso-saml.md)
