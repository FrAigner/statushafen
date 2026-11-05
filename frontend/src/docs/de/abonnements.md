# Abonnements

Besucher können sich über Änderungen einer Statusseite benachrichtigen lassen, per E-Mail oder
per Google-Chat-Webhook.

## Abonnieren

Auf der Statusseite öffnet „Jetzt abonnieren“ den Abo-Assistenten:

1. Kanal wählen: **E-Mail** oder **Google Chat** (Webhook-URL des Chat-Raums, beginnt mit `https://`).
2. Komponenten auswählen, über die man informiert werden möchte. Ohne Auswahl gilt
   „Keine Einschränkung: alle Komponenten“.
3. Abschicken.

Benachrichtigt wird, sobald ein Vorfall oder eine Wartung mindestens eine der abonnierten
Komponenten betrifft. Dazu zählen auch gespiegelte Meldungen
[externer Quellen](/docs/externe-quellen#gespiegelte-meldungen), die eine gekoppelte Komponente
betreffen. Private Statusseiten kann nur abonnieren, wer Zugriff auf die Seite hat.

## Double-Opt-in bei E-Mail

Ein neues E-Mail-Abo ist zunächst **unbestätigt**. Statushafen schickt eine Mail
„Bitte bestätige dein Abo für …“ mit einem Link auf `/subscribe/confirm`. Erst der Klick auf den
Knopf auf dieser Seite aktiviert das Abo.

- Der Link gilt 24 Stunden. Ist er abgelaufen, wird das unbestätigte Abo gelöscht und man
  abonniert einfach neu.
- Unbestätigte Abos erhalten keine Benachrichtigungen. Im Admin-Bereich tragen sie das Badge
  „Unbestätigt“.
- Keine Bestätigung brauchen Google-Chat-Abos und angemeldete Nutzer, die ihre eigene
  Konto-Adresse eintragen.
- Die Antwort beim Abonnieren ist für neue und bereits abonnierte Adressen gleich; so lässt sich
  nicht abfragen, wer abonniert ist. Nur wer mit der eigenen Konto-Adresse angemeldet ist, erfährt
  „bereits abonniert“.
- Schreibvarianten derselben Mailbox (Groß-/Kleinschreibung, `name+tag@…`) zählen als eine
  Adresse. Je Mailbox gehen seitenübergreifend höchstens drei Bestätigungsmails in 24 Stunden raus.

Beim Abonnieren wird die Sprache des Besuchers als Mailsprache gespeichert, sofern die Seite sie aktiviert hat (siehe [Mail-Vorlagen](/docs/mail-templates)).

## Abo verwalten

Angemeldete Nutzer öffnen ihr Abo über das Benutzermenü („Abonnement verwalten“) und passen
dort die Komponenten an oder löschen das Abo. Ohne Auswahl gilt wieder „alle Komponenten“.

Bietet die Seite mehr als eine Sprache an, steht dort auch **Sprache der Benachrichtigungen**:
„Seitenstandard“ oder eine der Sprachen der Seite. Sie gilt für E-Mails und Google-Chat-Nachrichten.
Ist die gewählte Sprache bei der Seite später nicht mehr aktiv, geht der Versand in der
Standardsprache der Seite raus; bis zum nächsten Speichern bleibt die Wahl gespeichert. Das
Formular zeigt eine nicht mehr aktive Sprache als „Seitenstandard“, und Speichern schreibt dann
„Seitenstandard“. Auch eine ausdrücklich gewählte Standardsprache wird als „Seitenstandard“
gespeichert und folgt damit einem späteren Wechsel der Standardsprache der Seite.

## Abmelden

Jede Mail an Abonnenten enthält einen persönlichen Link auf `/unsubscribe?token=…`. Er öffnet
eine Bestätigungsseite; erst der Klick auf „Jetzt abbestellen“ beendet das Abo. So können
Link-Scanner von Mail-Gateways ein Abo nicht versehentlich beenden.

Mail-Programme mit One-Click-Abbestellen nach RFC 8058 zeigen zusätzlich einen eigenen
Abbestellen-Knopf. Statushafen setzt dafür die Header `List-Unsubscribe` und
`List-Unsubscribe-Post`.

## Google-Chat-Webhook

Für Google Chat trägt man die Webhook-URL eines Chat-Raums ein. Statushafen schickt dorthin eine
Textnachricht mit Komponente, Tags, Vorfall, Status und Details, auf Deutsch oder Englisch je nach
Sprache des Abonnenten (jede andere Sprache bekommt Englisch). Die URL wird beim Speichern
geprüft: Beim öffentlichen Abonnieren sind nur Webhooks von Google Chat erlaubt
(`https://chat.googleapis.com/v1/spaces/…`, Port 443, ohne Zugangsdaten); jede andere Adresse wird
mit derselben Meldung abgelehnt. Admins können beim Anlegen im Admin-Bereich auch andere
`https`-Adressen eintragen, deren Host sich auflösen lässt und die nicht auf interne Adressen
zeigen. Ankündigungen gehen nur an E-Mail-Abos.

Google begrenzt Chat-Webhooks (etwa eine Nachricht pro Sekunde je Chat-Raum, zusätzlich pro
Projekt) und antwortet bei Überschreitung mit „zu viele Anfragen“ (HTTP 429) oder bei Überlast mit
503. Statushafen schickt deshalb höchstens eine Nachricht pro Sekunde an dieselbe URL (kurze
Spitzen bis zu zwei Nachrichten), beachtet den `Retry-After`-Hinweis von Google und wiederholt eine
Benachrichtigung höchstens zweimal, insgesamt mit höchstens 30 Sekunden Wartezeit. Fehler mit
falscher URL oder falschem Schlüssel (400, 401, 403, 404) werden nicht wiederholt. **Test senden**
wartet nie: Drosselt Google gerade, erscheint „Google drosselt gerade – bitte in n Sekunden erneut
versuchen“; lehnt das Ziel ab, prüfe URL und Schlüssel; ist es nicht erreichbar, versuche es später
noch einmal. Im Server-Log stehen nur Statuscode, Fehlerklasse und eine Kurzkennung der URL, nie
die URL selbst.

Ausgehende Webhooks an beliebige eigene Systeme gibt es nicht. Für eingehende Statusmeldungen
aus dem Monitoring siehe [API und Webhooks](/docs/api-und-webhooks).

## Abonnenten im Admin-Bereich

Unter **Abonnenten** sieht man alle Abos der Seite mit Kanal, Kontakt, Komponenten und Ersteller
(Recht `subscribers:read`). Bietet die Seite mehrere Sprachen an, legt man dort auch die
**Sprache der Benachrichtigungen** je Abonnent fest (ohne Angabe: Standardsprache der Seite).
Als Ersteller erscheint die E-Mail-Adresse des anlegenden Kontos nur, wenn es selbst zum Personal dieser Seite gehört (oder du selbst es angelegt hast); hat ein fremdes Konto die Seite abonniert, zeigt die Liste nur „mit Konto verknüpft“ und keine weiteren Kontodaten.
Anlegen, Ändern und Löschen brauchen `subscribers:create`, `subscribers:update` bzw. `subscribers:delete`.
