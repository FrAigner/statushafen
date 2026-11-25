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
3. Der Callback prüft die Domain erneut und leitet nach `https://<Domain>/auth/sso-callback?code=…`
   weiter, Fehler nach `https://<Domain>/login?oidc_error=<Code>`. Das Frontend dort tauscht den Code
   auf demselben Origin ein. Der Passwort-Login läuft ohnehin auf demselben Origin.

Eine Sitzung auf einer eigenen Domain gilt nur für die Statusseite dieser Domain und nie mit
Global-Admin-Rechten (wie eine Seiten-SSO-Sitzung). Grund: Wer das DNS der Domain kontrolliert, also
der Mandant, kann dort eigenen Code ausliefern und lesen, was der Browser für diese Domain speichert.
Für eine private Seite braucht man deshalb eine Rolle auf dieser Seite; Global Admins melden sich für
die Verwaltung unter `FRONTEND_URL` an.

Voraussetzungen: Das Frontend auf der eigenen Domain erreicht die API auf demselben Origin (Standard-Image,
`VITE_API_URL` leer), und der Reverse-Proxy reicht den `Host`-Header unverändert durch.

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
