# Anmeldung über globale OIDC-Anbieter

Statushafen meldet Nutzer über OpenID Connect (OIDC) an. Die **globalen Anbieter** richtet der
Betreiber für die ganze Instanz über Umgebungsvariablen ein; die Login-Seite zeigt je Anbieter einen
Knopf. Das ist unabhängig vom [Seiten-SSO](oidc.md), das ein Seiten-Admin je Statusseite einträgt.
Anleitungen je Anbieter: [GitLab](gitlab.md), [Google](google.md), [Keycloak](keycloak.md),
[Okta](okta.md), [Azure AD](azure-ad.md). Endpunkte: [API](api.md), Ablauf und Bedrohungsmodell:
[Architektur](architecture.md).

## Konfiguration

| Variable | Bedeutung |
| --- | --- |
| `OIDC_PROVIDERS` | Kommagetrennte Anbieter-IDs, z. B. `gitlab,google`. Erlaubt sind `a-z`, `0-9` und `-`, 1 bis 32 Zeichen. Die Reihenfolge ist die der Knöpfe. |
| `OIDC_<ID>_ISSUER` | Issuer des Anbieters (oder die volle `.well-known/openid-configuration`-Adresse). Nur `https://`. |
| `OIDC_<ID>_CLIENT_ID` | Client-ID der beim Anbieter registrierten Anwendung. |
| `OIDC_<ID>_CLIENT_SECRET` | Client-Secret. Wird nie ausgegeben oder geloggt. |
| `OIDC_<ID>_NAME` | Knopftext, z. B. `GitLab`. Standard: die ID. |
| `OIDC_<ID>_SCOPES` | Scopes, Standard `openid email profile`; `openid` wird immer ergänzt. |
| `OIDC_<ID>_EMAIL_TRUSTED` | `true` = fehlt `email_verified`, gilt die E-Mail als bestätigt. Nur für Anbieter, bei denen Nutzer ihre Adresse nicht selbst wählen können (z. B. Entra ID mit bestätigten Firmen-Domains). Standard: aus. |
| `PASSWORD_LOGIN_ENABLED` | `true`, `1` oder `yes` (Groß-/Kleinschreibung egal) schaltet die Anmeldung mit Passwort ein. Alles andere und ungesetzt: **aus**. |
| `SSO_ALLOWED_DOMAINS` | Kommagetrennte E-Mail-Domains, für die beim ersten Login ein **neues** Konto entstehen darf. Leer: keine Einschränkung. |
| `SSO_BOOTSTRAP_ADMIN_EMAILS` | E-Mail-Adressen, deren Konto bei jedem Login über einen globalen Anbieter Global Admin wird (nur mit bestätigter E-Mail, siehe unten). |

`<ID>` ist die Anbieter-ID in Großbuchstaben, `-` wird zu `_`: Für `google-work` heißen die
Variablen `OIDC_GOOGLE_WORK_ISSUER` usw.

Beispiel:

```bash
OIDC_PROVIDERS=gitlab,google
OIDC_GITLAB_ISSUER=https://gitlab.com
OIDC_GITLAB_CLIENT_ID=CLIENT-ID
OIDC_GITLAB_CLIENT_SECRET=CLIENT-SECRET
OIDC_GITLAB_NAME=GitLab
OIDC_GOOGLE_ISSUER=https://accounts.google.com
OIDC_GOOGLE_CLIENT_ID=CLIENT-ID
OIDC_GOOGLE_CLIENT_SECRET=CLIENT-SECRET
OIDC_GOOGLE_NAME=Google
```

**Redirect-URI**, die beim Anbieter eingetragen wird:

```text
<BASE_URL>/api/auth/provider/<id>/callback
```

also z. B. `https://status.example.org/api/auth/provider/gitlab/callback`.

Ein unvollständig oder falsch konfigurierter Anbieter (fehlende Variable, ungültige ID, doppelte ID,
Issuer ohne `https://`) wird beim Start mit einer Warnung im Log abgeschaltet; der Server startet
trotzdem. Ist gar keine Anmeldemethode aktiv (kein gültiger Anbieter und Passwort-Login aus), warnt
der Start deutlich: Dann kann sich niemand anmelden, nur der Befehl `create-admin` legt noch Konten
an. Die Discovery beim Anbieter läuft erst beim ersten Login, nicht beim Start.

Die Adressen globaler Anbieter kommen vom Betreiber. Anders als beim Seiten-SSO dürfen sie deshalb
auch im internen Netz liegen (z. B. ein selbst gehostetes GitLab). `http://` geht nur in der
Entwicklung über `OIDC_DEV_INSECURE_ISSUERS`, siehe [Lokale Tests](local-testing.md).

## Konten

Das Konto wird über den Issuer und das `sub` des ID-Tokens gefunden. Beim **ersten** Login ohne
Verknüpfung entscheidet die E-Mail-Adresse aus dem Claim `email`:

| Lage | Ergebnis |
| --- | --- |
| Konto mit dieser E-Mail existiert, `email_verified` ist `true` | Das Konto wird verknüpft und angemeldet (auch ein Konto mit Passwort oder ein Global Admin). |
| Konto existiert, `email_verified` fehlt oder ist `false` | Abgelehnt mit `email_unverified`. Das Konto kann den Anbieter selbst im Profil verknüpfen. |
| Kein Konto, `email_verified` ist `true` | Neues Konto mit der Rolle „Viewer“, ohne Tags und ohne Passwort, sofern die Domain in `SSO_ALLOWED_DOMAINS` steht (sonst `domain_not_allowed`). |
| Kein Konto, `email_verified` fehlt oder ist `false` | Abgelehnt mit `email_unverified`. |
| Kein `email`-Claim | Abgelehnt mit `email_missing`. |

Nur eine bestätigte E-Mail verknüpft ein Konto oder legt eines an. Sonst könnte jemand über einen
Anbieter ohne Bestätigung ein Konto für eine fremde Adresse vorab anlegen, das die spätere, bestätigte
Anmeldung der echten Person (oder `SSO_BOOTSTRAP_ADMIN_EMAILS`) übernehmen würde. Mit
`OIDC_<ID>_EMAIL_TRUSTED=true` zählt bei diesem Anbieter ein fehlendes `email_verified` als
bestätigt; ein ausdrückliches `false` nie. Steht die bestätigte E-Mail des Kontos in `SSO_BOOTSTRAP_ADMIN_EMAILS`, wird es Global
Admin. Spätere Logins finden das Konto über `sub`, auch wenn sich die E-Mail beim Anbieter ändert.

Angemeldete Nutzer verknüpfen weitere Anbieter im Profil („SSO-Anmeldung“,
`POST /api/auth/provider/<id>/link`); das geht auch für Global Admins, aber nicht aus einer Sitzung,
die über das Seiten-SSO entstanden ist. Die Sitzung eines globalen Anbieters gilt für das ganze
Konto, nicht nur für eine Seite.

Einladungen auf eine Statusseite nimmt ein Konto an, nachdem es sich über einen globalen Anbieter mit
der eingeladenen E-Mail-Adresse angemeldet hat.

## Passwort-Login

Standardmäßig ist die Anmeldung mit Passwort **aus**. Dann antworten mit `403` und dem Code
`password_login_disabled`:

- `POST /api/login`,
- `POST /api/public/orders/set-password`,
- `POST /api/users` mit Passwort und `PUT /api/users/:id` mit neuem Passwort.

Konten lassen sich ohne Passwort anlegen (`POST /api/users` ohne `password`); die Self-Service-
Bestellung ist nach dem Bestätigen der E-Mail sofort aktiv, und der Besteller meldet sich über einen
globalen Anbieter mit derselben Adresse an. Der Befehl `./server create-admin` bleibt als Notzugang
des Betreibers; das so angelegte Konto meldet sich mit `PASSWORD_LOGIN_ENABLED=true` oder über einen
globalen Anbieter mit bestätigter, gleicher E-Mail an.

## Rücksprung nach dem Login

Alle Login-Starts (`/api/auth/provider/<id>/login`, `/api/auth/oidc/login`, `/api/auth/sso/login`)
nehmen `return_to` an, den Frontend-Pfad, von dem der Login ausging. Der Server prüft ihn (ein
führender `/`, kein `//`, keine Steuer- oder Leerzeichen, kein Backslash, nicht unter `/api`,
höchstens 2048 Byte) und verwirft ihn sonst stillschweigend. `POST /api/auth/sso-exchange` liefert
ihn als `return_to` zurück. Nach dem Verknüpfen geht es weiter ins Profil.

### Statusseiten auf eigener Domain

Startet der Login auf der verifizierten eigenen Domain einer Statusseite (etwa `status.acme.example`),
geht es nach der Anmeldung dorthin zurück, und die Sitzung gilt auf dieser Domain. Das gilt für
globale Anbieter, das Seiten-SSO und den Passwort-Login.

1. Ein Login-Start, der auf der eigenen Domain ankommt, leitet zuerst auf denselben Pfad unter
   `BASE_URL` weiter und hängt die Domain als `return_host` an. Das State-Cookie muss den Callback
   unter `BASE_URL` erreichen. Der `Host` der Anfrage gewinnt gegen ein `return_host` in der Query.
2. Der Server übernimmt `return_host` nur, wenn es exakt der Hostname einer eigenen Domain im Status
   „Verifiziert“ oder „Aktiv“ ist, beim Seiten-SSO nur die Domain derselben Seite. Alles andere
   (unbestätigte, unbekannte oder fremde Domains, Schema, Port, Pfad, Großschreibung, Punkt am Ende)
   wird verworfen; der Login endet dann wie bisher unter `FRONTEND_URL`.
3. Der Callback prüft die Domain erneut. Hat das Konto einer Rolle auf der Seite der Domain zugestimmt
   (Einladung, Zuweisung durch einen Global Admin, Bestellung), leitet er nach
   `https://<Domain>/auth/sso-callback?code=…` weiter. Sonst endet der Login unter `FRONTEND_URL` auf der
   Startseite (globale Anbieter: volle Sitzung), bei Global Admins auf `/status/<slug>` derselben Seite
   (sie lösen keinen Auto-Join aus). Eine Rolle aus dem Auto-Join zählt nie, denn dessen E-Mail-Domains
   pflegt der Mandant; solche Rollen sind markiert (`joined_via`). Nimmt das Konto danach eine Einladung
   auf dieselbe Seite an, wird die Mitgliedschaft zur zugestimmten (mit der Rolle aus der Einladung). Ändert
   nur ein Seiten-Admin die Rolle, bleibt die Markierung; speichert ein Global Admin das Konto, behalten
   unveränderte Rollen sie ebenfalls. Rollen aus der Zeit vor dieser Markierung zählen, außer das
   Audit-Log (das jeden Auto-Join schon immer protokolliert) kennt einen Auto-Join dieses Kontos auf
   dieser Seite. Fehler im Callback gehen immer an
   `FRONTEND_URL`, nur Fehler beim Login-Start (ohne Kontobezug) an die eigene Domain.
4. Das Frontend dort tauscht den Code auf demselben Origin ein. Der Code ist an diesen Origin gebunden:
   Ein Code für eine eigene Domain wird nur dort eingelöst, ein Code für `FRONTEND_URL` nie auf einer
   eigenen Domain. Der Passwort-Login läuft ohnehin auf demselben Origin.

Wer das DNS einer eigenen Domain kontrolliert, also der Mandant, kann dort eigenen Code ausliefern und
lesen, was der Browser für diese Domain speichert. Eine Sitzung auf einer eigenen Domain (OIDC und
Passwort) gilt deshalb nur für die Statusseite dieser Domain, nie mit Global-Admin-Rechten, und darf
nicht am Konto selbst handeln: Konto löschen (`DELETE /api/me`), Datenexport (`GET /api/me/export`),
Anmeldungen verknüpfen oder lösen (`POST /api/auth/oidc/link`, `POST /api/auth/provider/<id>/link`,
`DELETE /api/me/sso/<id>`) antworten mit `403 custom_domain_session`; die Liste der Verknüpfungen
(`GET /api/me/sso`) ist dort leer. Dafür meldet man sich unter
`FRONTEND_URL` an. Für eine private Seite braucht man eine Rolle auf dieser Seite; Global Admins
verwalten unter `FRONTEND_URL`.

Auch eine Seiten-SSO-Sitzung unter `FRONTEND_URL` stellt der IdP des Mandanten aus. Ohne Passwort
löscht sie das Konto deshalb nur, wenn es ausschließlich über diese Seite besteht (keine andere
Anmeldung, keine Rolle auf einer anderen Seite); sonst antwortet `DELETE /api/me` mit
`403 page_sso_session`. Konten mit Passwort bestätigen die Löschung wie immer mit dem Passwort.

Vertrag mit dem Reverse-Proxy: Er reicht für eigene Domains den `Host`-Header durch und liefert `/api`
auf demselben Origin aus (Standard-Image, `VITE_API_URL` leer). Ob eine Anfrage von einer eigenen
Domain kommt, entscheidet das Backend am `Origin`-Header des Browsers oder am `Host`, beide ohne
Rücksicht auf Groß-/Kleinschreibung, Port und Punkt am Ende. Schreibt der Proxy den `Host` um, bleibt
der `Origin` maßgeblich; der Umweg über `BASE_URL` beim Login-Start braucht aber den echten `Host`.

## Fehlersuche

Fehler im Browser-Ablauf landen als Code auf der Login-Seite (`?oidc_error=<Code>`), beim Verknüpfen
im Profil:

| Code | Ursache |
| --- | --- |
| `provider_unknown` | Die ID steht nicht (oder nicht gültig) in `OIDC_PROVIDERS`; Warnung im Start-Log prüfen. |
| `provider_unreachable` | Discovery schlägt fehl (Issuer falsch, nicht erreichbar, `issuer` im Discovery-Dokument weicht ab). |
| `state_invalid` | Login älter als 10 Minuten, anderer Browser oder State schon verwendet. |
| `token_exchange` | Client-ID/-Secret oder Redirect-URI stimmen beim Anbieter nicht. |
| `id_token_invalid` | Signatur, Issuer, Zielgruppe, Ablauf oder Nonce passen nicht; Uhrzeit prüfen. |
| `email_missing` | Der Anbieter liefert keinen `email`-Claim (Scope `email` fehlt?). |
| `email_unverified` | Siehe Tabelle „Konten“. |
| `domain_not_allowed` | Domain steht nicht in `SSO_ALLOWED_DOMAINS`. |
| `already_linked` | Die Identität gehört schon einem anderen Konto. |

## Upgrade von älteren Versionen

- **Google- und GitHub-Login entfallen** (`GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`,
  `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, Routen `/api/auth/google`, `/api/auth/github`). Google
  richtest du als globalen Anbieter ein ([Anleitung](google.md)); die Redirect-URI ändert sich auf
  `/api/auth/provider/google/callback`. Bestehende Konten werden beim ersten Login mit bestätigter
  E-Mail verknüpft. GitHub bietet kein OIDC für die Anmeldung von Nutzern; nutze einen anderen
  Anbieter.
- **SAML entfällt** (Routen `/api/auth/saml/*`, SAML-Felder der SSO-Konfiguration). Datenbankspalten
  und -tabellen bleiben stehen, bestehende SAML-Verknüpfungen und -Konfigurationen sind wirkungslos.
  Seiten mit SAML stellst du auf das OIDC-Seiten-SSO um ([Okta](okta.md), [Azure AD](azure-ad.md)).
- **Passwort-Login ist standardmäßig aus.** Vor dem Update mindestens einen globalen Anbieter
  einrichten oder `PASSWORD_LOGIN_ENABLED=true` setzen, sonst kann sich niemand anmelden.
- Migrationsschritte: 1. Anbieter beim IdP registrieren (Redirect-URI oben). 2. `OIDC_PROVIDERS`
  und `OIDC_<ID>_*` setzen, `GOOGLE_*`/`GITHUB_*` entfernen. 3. Vorübergehend
  `PASSWORD_LOGIN_ENABLED=true` lassen, bis alle Konten sich über den Anbieter angemeldet haben
  (Anbieter ohne `email_verified`: `OIDC_<ID>_EMAIL_TRUSTED=true` setzen, wenn er nur eure
  Firmenadressen ausgibt, sonst im Profil verknüpfen). 4. Den Passwort-Login abschalten.

Zurück zur [Übersicht](README.md).
