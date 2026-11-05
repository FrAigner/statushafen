# FAQ

## Mir fehlt ein Menüpunkt im Admin-Bereich.

Viele Menüpunkte hängen an Rechten oder sind nur für Global Admins sichtbar. Welche Rechte es
gibt, steht unter [Benutzer, Rollen und Rechte](/docs/benutzer-und-rollen). Prüfe außerdem, ob
oben in der Kopfzeile die richtige Statusseite als „Mandant“ gewählt ist.

## Nach dem Login sehe ich eine leere Seite.

Meist ist die Anmeldung abgelaufen. Einmal abmelden und neu anmelden.

## Ich bekomme keine Mails zu meinem Abo.

- Ist das Abo bestätigt? Neue E-Mail-Abos brauchen den Klick im Bestätigungslink
  (24 Stunden gültig), siehe [Abonnements](/docs/abonnements#double-opt-in-bei-e-mail).
- Betrifft der Vorfall eine der abonnierten Komponenten?
- Ist in den [Systemeinstellungen](/docs/systemeinstellungen) der E-Mail-Versand eingeschaltet?

## Wie beende ich ein Abo?

Über den Abmeldelink in jeder Benachrichtigung und dann „Jetzt abbestellen“. Angemeldete Nutzer
können ihr Abo auch über das Benutzermenü verwalten.

## Warum steht eine Komponente schlechter als „Betriebsbereit“, obwohl ich nichts geändert habe?

Ein offener Vorfall zeigt seine Komponenten mindestens mit dem Status seines Schweregrads
(„Beeinträchtigt“, „Teilausfall“ oder „Großstörung“, bei mehreren Vorfällen der schlimmste). Ist der Vorfall
behoben, gilt wieder der gespeicherte Status. Ist die Komponente mit einer
[externen Quelle](/docs/externe-quellen) gekoppelt, bestimmt auch deren Status mit: Es gilt der
schlechteste Wert.

## Oben im Admin-Bereich steht „Externe Quelle nicht erreichbar“.

Eine externe Quelle hat länger als die eingestellte Frist keinen Abruf geliefert. Ihr Status zählt
solange nicht. Auf der Seite **Externe Quellen** steht der letzte Fehler; „Jetzt abrufen“ prüft die
Verbindung, siehe [Ausfall einer Quelle](/docs/externe-quellen#ausfall-einer-quelle).

## Mein Ausfall fehlt in der 90-Tage-Verfügbarkeit.

Die Verfügbarkeit stammt aus Vorfällen, Wartungsfenstern und gespiegelten Meldungen externer
Quellen, gewichtet nach Schweregrad. Ein Status, der nur per Webhook oder von Hand gesetzt wurde,
zählt nicht. Siehe
[Die öffentliche Statusseite](/docs/oeffentliche-statusseite#verfuegbarkeit-der-letzten-90-tage).

## Ich kann keine Gruppe löschen.

Eine Gruppe lässt sich nur löschen, wenn sie leer ist. Verschiebe oder lösche zuerst ihre
Komponenten, siehe [Gruppen](/docs/komponenten-und-tags#gruppen).

## Mein Slug wird abgelehnt.

Erlaubt sind Kleinbuchstaben, Ziffern und einzelne Bindestriche; Namen wie `www`, `login` oder
`system` sind reserviert, siehe [Statusseiten](/docs/statusseiten#regeln-fuer-den-slug).

## Nach einer Wartung steht eine Komponente auf „Betriebsbereit“, obwohl sie gestört ist.

Am Ende eines Wartungsfensters setzt Statushafen die betroffenen Komponenten automatisch auf
„Betriebsbereit“. Den Status danach bei Bedarf von Hand oder per
[Webhook](/docs/api-und-webhooks) korrigieren.

## Die SSO-Anmeldung meldet `not_linked`.

Das Konto ist noch nicht mit dem SSO verknüpft. Einmal normal anmelden und im Profil
„Mit SSO verknüpfen“ wählen, siehe [SSO](/docs/sso).

## Der Monitoring-Webhook antwortet mit 401.

Der Token fehlt oder ist nicht mehr gültig, etwa weil er neu erzeugt wurde. Einen neuen Token
erzeugen und im Monitoring eintragen, siehe [API und Webhooks](/docs/api-und-webhooks).

## Wie öffne ich das Menü auf dem Handy?

Im Admin-Bereich über das Menü-Symbol links in der Kopfzeile, in dieser Dokumentation über
„Abschnitte anzeigen“.
