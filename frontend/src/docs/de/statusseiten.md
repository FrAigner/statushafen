# Statusseiten und Mandanten

Jede Statusseite ist ein eigener Mandant: Komponenten, Vorfälle, Abonnenten, Vorlagen und
Rollen gehören immer zu genau einer Seite. Statusseiten anlegen und auflisten dürfen nur Global
Admins (Menü **Statusseiten**, Knopf „+ Neue Statusseite“). Das Menü **Statusseiten** mit allen
Einstellungen dieses Abschnitts steht in der Oberfläche nur Global Admins offen. Die Rechte
`pages:create`, `pages:update` und `pages:delete` (zum Beispiel in der Rolle „Page Owner“, siehe
[Benutzer, Rollen und Rechte](/docs/benutzer-und-rollen)) erlaubt dieselben Änderungen an der
eigenen Seite über die API; in der Oberfläche öffnet es SSO, Mail-Templates, Sprachen und UI-Texte.

## Adresse einer Statusseite

Eine Statusseite ist auf mehreren Wegen erreichbar:

| Weg | Beispiel | Hinweis |
|---|---|---|
| Subdomain | `acme.example.com` | Die erste Ebene ist der Slug. |
| Pfad | `/status/acme` | Funktioniert immer, unabhängig vom Host. |
| Eigene Domain | `status.acme.example` | Muss per DNS verifiziert werden, siehe unten. |

Ruft man die Hauptadresse ohne Slug auf, erscheint die Startseite von Statushafen.

### Regeln für den Slug

- Erlaubt sind Kleinbuchstaben, Ziffern und einzelne Bindestriche, nicht am Anfang oder Ende
  (`acme`, `acme-cloud`).
- Leer gelassen, erzeugt Statushafen den Slug aus dem Namen.
- Ein Slug ist je Seite eindeutig. Der Slug einer gelöschten Seite wird wieder frei.
- **Reservierte Namen** sind nicht erlaubt, weil sie als Subdomain oder Pfad schon belegt sind:
  `www`, `staging`, `api`, `admin`, `app`, `mail`, `login`, `docs`, `status`, `order`, `auth`,
  `subscribe`, `unsubscribe`, `profile`, `healthz`, `static`, `assets`, `fonts` und `system`
  sowie die typischen Postfachnamen `support`, `security`, `billing`, `postmaster`, `abuse`,
  `noreply`, `no-reply`, `info`, `root`, `hostmaster`, `webmaster` und `edge`.
  Die Seite mit dem Slug `system` ist die Statusseite von Statushafen selbst: Ihr Slug lässt sich
  nicht ändern und sie lässt sich nicht löschen.

## Einstellungen einer Seite

| Feld | Wirkung | Beispiel |
|---|---|---|
| Name der Statusseite | Titel der Seite, Pflicht | `Acme Cloud` |
| Slug (URL-Bezeichner) | Teil der Adresse, siehe oben | `acme` |
| Beschreibung | Kurztext zur Seite | `Offizielle Statusseite von Acme` |
| Logo-URL (optional) | Logo im Seitenkopf | `https://acme.example/logo.svg` |
| Primärfarbe / Sekundärfarbe (optional) | Markenfarben der öffentlichen Seite, als Hex-Wert wie `#4c6ef5`; leer = Standardfarben | |
| Header-Design | Darstellung des Seitenkopfs: Classic (Standard), Zentriert oder Minimal | |
| Seiten-Layout | „Standard“ oder „Liste“ (Gesamtstatus, Komponenten mit 90-Tage-Balken, Vorfall-Zeitleiste) | |
| Antwortadresse für Benachrichtigungen (optional) | Reply-To der Mails dieser Seite. Absenderadresse ist immer die des Servers, der Seitenname steht als Absendername; leer = kein Reply-To | `"Acme Status" <status@acme.example>` |
| Private Statusseite (Login erforderlich) | Nur angemeldete Nutzer mit Zugriff sehen die Seite; sie erscheint nicht auf der Startseite | an/aus |
| Standard-Zugriff für alle eingeloggten Nutzer | Rolle für den automatischen Beitritt, siehe unten | `Viewer` |
| E-Mail-Domains für automatischen Beitritt | Domains, deren Konten der Seite automatisch beitreten | `acme.example` |
| Monitoring-Webhook | Token für automatische Statusmeldungen, siehe [API und Webhooks](/docs/api-und-webhooks) | |

Die Liste unter **Statusseiten** lässt sich nach Name, Slug, Beschreibung und Domain durchsuchen. Im
Zeilenmenü stehen „Öffnen“ (öffentliche Seite) und „Benutzer“ (Rollen auf dieser Seite). Beim
Löschen einer Seite entfernt Statushafen alle zugehörigen Daten.

### Privat und automatischer Beitritt

Eine **private** Seite ist nur nach dem Login sichtbar, und nur für Nutzer mit einer Rolle auf der
Seite. Eine Rolle bekommen Nutzer durch Zuweisung (Zeilenmenü „Benutzer“) oder durch den
**automatischen Beitritt**. Dieser greift nur, wenn beides gesetzt ist:

- eine Standard-Rolle („Standard-Zugriff für alle eingeloggten Nutzer“) und
- die E-Mail-Domain des Kontos steht in der Liste „E-Mail-Domains für automatischen Beitritt“.

Die Domains stehen kommagetrennt und müssen exakt passen; Subdomains zählen nicht (`acme.example`
schließt `mail.acme.example` nicht ein). Ist eines der beiden Felder leer, gilt „nur explizit
zugewiesene Nutzer“. Die Standard-Rolle darf keine Rechte zum Anlegen, Ändern oder Löschen (`…:create`, `…:update`, `…:delete`) und keinen Zugriff
auf Abonnenten enthalten; Statushafen prüft das beim Speichern und noch einmal beim Beitritt.
Wer von einem Admin von der Seite entfernt wurde, tritt nicht automatisch wieder bei.

Ohne Berechtigung (nicht angemeldet, ohne Rolle, nicht beitrittsberechtigt) sieht man eine private
Seite nicht anders als eine Seite, die es nicht gibt: Alle Abrufe antworten mit 404, in der Oberfläche
steht „Statusseite nicht verfügbar“ mit Link zum Login. Der Beitritt geschieht wie bisher beim ersten
Aufruf nach der Anmeldung.

## Eigene Domain

Unter „Custom Domain“ trägt man die gewünschte Domain ein, zum Beispiel `status.acme.example`.
Danach sind zwei DNS-Einträge nötig:

1. Ein **CNAME** der Domain auf den angezeigten Zielhost (Standard `edge.example.com`).
2. Als Besitznachweis ein **TXT-Eintrag** der Form `statushafen-verify=<token>`. Den genauen Wert
   zeigt das Formular nach dem Speichern.

Anschließend klickt man auf „DNS verifizieren“. Der Zustand der Domain steht daneben:
„Ausstehend“, „Verifiziert“ (wartet auf den ersten Aufruf über die Domain) und „Aktiv“. „Domain
entfernen“ löst die Zuordnung wieder. Das Verifizieren ist auf 10 Prüfungen pro Minute je Seite und je
Nutzer begrenzt; darüber meldet die Oberfläche, dass zu viele Anfragen gestellt wurden.

Erlaubt sind nur gültige Hostnamen in Kleinbuchstaben (ohne abschließenden Punkt, ohne Wildcard, ohne
IP-Adresse; internationale Namen in Punycode-Schreibweise). Das Eintragen reserviert die Domain
nicht: Mehrere Seiten können dieselbe Domain eintragen, den Zuschlag bekommt die Seite, die sie
**zuerst per TXT-Eintrag verifiziert**. Ist die Domain bei einer anderen Seite bereits verifiziert
oder aktiv, schlägt das Verifizieren mit dem Hinweis „Diese Domain wird bereits von einer anderen Statusseite verwendet“ fehl.

## Benutzer einer Seite

Im Zeilenmenü „Benutzer“ weist man Nutzern eine Rolle auf der Seite zu oder entzieht sie (nur
Global Admins in der Oberfläche; die API verlangt `pages:create` bzw. `pages:delete`).
Global Admins wählen das Konto aus der Liste und ordnen es direkt zu. Alle anderen (z. B. Page
Owner, über die API) **laden per E-Mail-Adresse ein**: Statushafen legt eine Einladung an und
schickt dem Konto mit dieser Adresse einen Link. Mitglied wird das Konto erst, wenn sein Inhaber
den Link öffnet, sich mit genau dieser Adresse anmeldet und „Annehmen“ wählt; „Ablehnen“ löscht die
Einladung. Die Antwort beim Einladen ist für bekannte und unbekannte Adressen gleich, und die Liste
„Offene Einladungen“ zeigt jede eingegebene Adresse; ohne Konto geht keine Mail raus. Der Link gilt
7 Tage und nur einmal, gespeichert ist nur ein Hash; annehmen kann nur das Konto, das beim Einladen
existierte. Erneutes Einladen derselben Adresse ersetzt den alten Link frühestens nach 30 Minuten
(vorher passiert nichts, es geht auch keine neue Mail raus) und nur, wenn die eigene Rolle die Rolle
der offenen Einladung abdeckt. Offene Einladungen lassen sich zurückziehen; eine Seite hat höchstens
100 offene und 200 neue Einladungen pro Tag. Abgelaufene Einladungen löscht Statushafen automatisch,
beim Löschen eines Kontos auch alle Einladungen an und von diesem Konto; der Datenexport enthält die
offenen Einladungen an die eigene Adresse. Verliert der Einladende sein Recht, die Rolle zu vergeben, verfällt die Einladung.
Konten mit altem globalem SSO, mit einer Seiten-SSO-Verknüpfung (OIDC/SAML) oder einer Rolle auf
einer anderen Seite sowie Global-Admin-Konten bekommen keine Mail und können eine Einladung nicht
selbst annehmen; sie fügt nur ein Global Admin hinzu (eine zweite Seite würde sonst die
SSO-Anmeldung der ersten sperren). Einladen ist auf 20 Anfragen pro Minute begrenzt, Annehmen und
Ablehnen ebenso. Bestehende Mitglieder der Seite lassen sich auch
ohne E-Mail umstufen. Eine Rolle darf nur vergeben werden, wenn sie nicht mehr Rechte hat als die
eigene Rolle auf dieser Seite; ein Mitglied mit stärkerer Rolle lässt sich nicht ändern oder
entfernen. Die Mitgliederliste zeigt nur E-Mail und Rolle.
Pro Konto und Seite gibt es genau eine Rolle; ein erneutes Hinzufügen ändert die Rolle des bestehenden Mitglieds. Wer nicht Global Admin ist, kann sich nicht selbst entfernen oder auf eine Rolle ohne `pages:update` herabstufen, wenn er das letzte Mitglied mit diesem Recht auf der Seite ist (Fehler „letzter Benutzer mit Verwaltungsrechten“); ein Global Admin kann das jederzeit reparieren. Wird ein Mitglied entfernt, löscht Statushafen auch seine Seiten-SSO-Verknüpfung dieser Seite, damit das Konto später anderswo angemeldet oder hinzugefügt werden kann. Dasselbe gilt, wenn eine Rolle gelöscht oder die Seite beim Bearbeiten des Kontos entzogen wird.
„Global Admin“ ist keine Seitenrolle; Global Admins setzt man unter **Benutzer** mit dem Schalter
„Global Admin“.

## Mandant wechseln

Wer Zugriff auf mehrere Statusseiten hat, wählt die aktive Seite oben in der Kopfzeile im
Feld „Mandant“. Alle Admin-Ansichten beziehen sich dann auf diese Seite. Im Benutzermenü steht
unter der E-Mail-Adresse die Rolle, die man auf dieser Seite hat.
