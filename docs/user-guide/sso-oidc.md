# OIDC-Anmeldung je Statusseite einrichten

> English: [`en/sso-oidc.md`](./en/sso-oidc.md)

Jede Statusseite kann einen eigenen OpenID-Connect-Anbieter (IdP) bekommen, etwa Authentik,
Keycloak, Microsoft Entra ID oder Google. Konten, die mit diesem Anbieter verknüpft sind, melden
sich darüber an, ohne ein eigenes Passwort zu brauchen (Doku). SAML
() beschreibt [`sso-saml.md`](./sso-saml.md).

Diese Anleitung beschreibt den Stand des Backends (`backend/handlers/oidc_login.go`,
`backend/handlers/oidc_frontend.go`, `backend/handlers/statuspage_sso.go`).

## Überblick: so läuft die Anmeldung

1. Der Nutzer klickt auf der Statusseite oben „Anmelden“ und auf der Login-Seite „Mit SSO
   anmelden“ (bei einer privaten Seite steht der Knopf auch direkt im Hinweis „Private
   Statusseite“). Der Knopf erscheint nur, wenn das Seiten-SSO aktiv ist; das fragt das
   Frontend über `GET /api/public/pages/<slug>/sso` ab, das nur `oidc_enabled` und die
   Seiten-ID liefert, keine Konfiguration. Der Browser ruft dann
   `GET /api/auth/oidc/login?page=<Seiten-ID>` auf.
2. Das Backend holt die Discovery des Anbieters, legt State, Nonce und PKCE-Verifier an (10 Minuten
   gültig) und leitet zum Anbieter weiter.
3. Der Anbieter leitet nach der Anmeldung zurück auf
   `<BASE_URL>/api/auth/oidc/callback?page=<Seiten-ID>`.
4. Das Backend tauscht den Code (mit PKCE) gegen Tokens und prüft das ID-Token: Signatur über das
   JWKS des Anbieters, `iss`, `aud` (= Client-ID), Ablaufzeit und Nonce.
5. Das Konto wird ausschließlich über das Tripel *(Statusseite, Issuer, `sub`)* gefunden, nie über
   die E-Mail-Adresse. Neue Konten entstehen dabei nicht.
6. Wie beim Google/GitHub-Login leitet das Backend mit einem Einmal-Code auf
   `<FRONTEND_URL>/auth/sso-callback` weiter; das Frontend tauscht ihn gegen die Sitzung.

Schlägt ein Schritt fehl, leitet das Backend auf `<FRONTEND_URL>/login?oidc_error=<Code>` um
(nach einer Verknüpfung ins Profil, nach einer Testanmeldung auf die SSO-Seite). Die Seite zeigt
eine übersetzte Meldung; in der URL steht nur der feste Code, nie eine Meldung des Anbieters.
Codes und Ursachen stehen unter [Fehlerbilder](#fehlerbilder).

Jede Anmeldung, jede Verknüpfung und jedes Lösen einer Verknüpfung landet im Audit-Log
(`auth.sso_login`, `auth.sso_linked` bzw. `auth.sso_unlinked`, Details `provider=oidc page=<ID>`).

## Voraussetzungen

- **Berechtigung:** Die SSO-Konfiguration einer Seite schreibt, wer auf dieser Seite die
  Berechtigung `sso:update` hat, oder ein Global Admin (`GET/PUT/DELETE
  /api/admin/pages/<Seiten-ID>/sso`). Im Admin-Bereich erscheint der Menüpunkt „SSO“ (Gruppe
  „System“) entsprechend für alle mit `sso:update` auf der oben gewählten Statusseite.
- **`BASE_URL`** muss auf die öffentlich erreichbare Adresse des Backends zeigen (z. B.
  `https://status.example.com`). Daraus entsteht die Redirect-URL. Ohne Variable gilt
  `http://localhost:8080`.
- **`FRONTEND_URL`** muss auf das Frontend zeigen (Standard `http://localhost:5173`); dorthin geht
  es nach erfolgreicher Anmeldung.
- **Anbieter über https** mit öffentlich erreichbarer Adresse (siehe [Fehlerbilder](#fehlerbilder)).
- **Seiten-ID:** die numerische ID der Statusseite (steht u. a. in der Admin-URL bzw. kommt aus
  `GET /api/admin/pages`).

## Redirect-/Callback-URL

Beim Anbieter genau diese URL als erlaubte Redirect-URI eintragen:

```
<BASE_URL>/api/auth/oidc/callback?page=<Seiten-ID>
```

Beispiel: `https://status.example.com/api/auth/oidc/callback?page=3`

Das Backend zeigt den Wert als `oidc_acs_url` an (`GET /api/admin/pages/<ID>/sso`, auch bevor eine
Konfiguration gespeichert ist; in der Admin-Oberfläche als Feld „ACS URL“). Er wird bei jedem Abruf
aus dem aktuellen `BASE_URL` berechnet, genau wie beim Login – nach einer Änderung von `BASE_URL`
also die Redirect-URI beim Anbieter anpassen, nicht neu speichern. Der Query-Parameter `?page=…` gehört dazu; die meisten Anbieter
vergleichen Redirect-URIs zeichengenau.

## Felder der Konfiguration

Die Admin-Oberfläche „SSO“ bearbeitet die Konfiguration der oben im Admin-Bereich gewählten
Statusseite. Ihre Felder entsprechen den JSON-Feldern von `PUT /api/admin/pages/<Seiten-ID>/sso`:

| Feld in der Oberfläche | JSON-Feld | Bedeutung |
|------------------------|-----------|-----------|
| OIDC-Anmeldung aktivieren | `oidc_enabled` | Schaltet das Seiten-SSO ein. Ist es aus, antworten Login und Callback mit 404 „SSO für diese Seite nicht eingerichtet“. |
| Discovery URL | `oidc_discovery_url` | Issuer des Anbieters **oder** der volle Discovery-URL (`…/.well-known/openid-configuration`). Das Backend schneidet den `.well-known`-Teil ab und verwendet den Rest als Issuer. Ein abschließender Schrägstrich bleibt stehen. |
| Client ID | `oidc_client_id` | Client-ID der beim Anbieter angelegten Anwendung. Muss im `aud` des ID-Tokens stehen. |
| Client Secret | `oidc_client_secret` | Client-Secret. Wird nie zurückgegeben; GET meldet nur `oidc_client_secret_set` (`true`, wenn eins gespeichert ist). Ist eins gespeichert, zeigt die Oberfläche „Gesetzt – leer lassen zum Beibehalten“: ein leeres Feld (bzw. ein leerer oder fehlender Wert im PUT) behält das gespeicherte Secret, ein neuer Wert ersetzt es. |
| Scopes | `oidc_scopes` | Leerzeichengetrennt; leer gespeichert wird der Standard `openid email profile`. `openid` ergänzt das Backend selbst, falls es fehlt. |
| Claim für den Namen | `oidc_attribute_name` | Claim, aus dem der Anzeigename der Sitzung kommt (leer = Standard `name`). |
| Claim für die E-Mail-Adresse | `oidc_attribute_email` | Claim, dessen Wert an der Verknüpfung gespeichert und im Profil angezeigt wird („verknüpft als …“, bei jeder Anmeldung aktualisiert; leer = Standard `email`). Konten werden **nie** über die E-Mail gefunden. |
| – | `oidc_attribute_role` | **Nicht unterstützt** und nicht in der Oberfläche. Eine Rolle aus einem Claim käme vom IdP, den der Seiten-Admin kontrolliert, und an der Rollenverwaltung vorbei; das wäre eine Rechteausweitung. Ein per API gespeicherter Wert hat keine Wirkung, die Oberfläche leert ihn beim Speichern. |
| Test-Login erlauben | `test_login_enabled` | Erlaubt die [Testanmeldung](#testanmeldung) aus der SSO-Seite, auch bevor `oidc_enabled` eingeschaltet ist (Standard bei neuer Konfiguration: an). Im Live-Betrieb abschalten. |
| ACS URL | `oidc_acs_url` | Nur lesend: die aktuell gültige Redirect-URL (siehe oben). |

Mit `oidc_enabled = true` verlangt das Backend Discovery-URL und Client-ID in der Anfrage sowie ein
Client-Secret – entweder in der Anfrage oder bereits gespeichert –, sonst 400 „OIDC enabled
erfordert Discovery URL, Client ID und Secret“. Die Oberfläche markiert diese Felder dann als
Pflichtfelder; fehlt die Berechtigung, zeigt sie „Keine Berechtigung für diesen Bereich“.

`DELETE /api/admin/pages/<ID>/sso` (Schaltfläche „SSO-Konfiguration löschen“) entfernt die
Konfiguration samt Secret; ein späteres Speichern legt sie wieder an und braucht dann wieder ein
Secret.

## Testanmeldung

Mit „Test-Login erlauben“ (`test_login_enabled`) erscheint in der SSO-Seite der Knopf
„Testanmeldung starten“ (`POST /api/admin/pages/<Seiten-ID>/sso/test`, gleiche Berechtigung wie
die Konfiguration, 20 Aufrufe pro Minute). Er prüft die **gespeicherte** Konfiguration – also
vorher speichern – und funktioniert schon, bevor `oidc_enabled` eingeschaltet ist:

1. Discovery-URL, Client-ID und Secret speichern, „Test-Login erlauben“ anhaken.
2. „Testanmeldung starten“, beim Anbieter mit einem beliebigen Konto anmelden.
3. Zurück auf der SSO-Seite meldet sie „Testanmeldung erfolgreich“ (Discovery, Client-Daten,
   Redirect-URL, Signatur, `aud`, Nonce passen) und nennt Claims für Namen oder E-Mail, die im
   ID-Token fehlten. Bei einem Fehler zeigt sie die Meldung zum Code (siehe
   [Fehlerbilder](#fehlerbilder)).

Die Testanmeldung legt weder eine Sitzung noch eine Verknüpfung an und schreibt keinen
Audit-Eintrag für eine Anmeldung. Fehlen Discovery-URL, Client-ID oder Secret, antwortet der Start
mit 400, ist „Test-Login erlauben“ aus, mit 403.

Den echten Login danach mit einem verknüpften Konto in einem privaten Browserfenster über die
Statusseite („Anmelden“ → „Mit SSO anmelden“) prüfen. Details zu Fehlern stehen im Backend-Log
(`OIDC discovery failed`, `OIDC token exchange failed`, `OIDC ID token rejected`).

## Konto-Verknüpfung: wer darf das Seiten-SSO nutzen

Über das Seiten-SSO entstehen **keine** neuen Konten. Ein Konto muss bereits existieren und wird
einmalig verknüpft:

1. Der Nutzer meldet sich auf dem bisherigen Weg an (Passwort, Google, GitHub).
2. Im Profil (`/admin/profile`) zeigt der Bereich „SSO-Anmeldung“ die Seiten, die er verknüpfen
   darf, und wählt dort „Mit SSO verknüpfen“. Dahinter steht
   `POST /api/auth/oidc/link?page=<Seiten-ID>` (Login-Token im Header `X-Auth-Token`);
   die Antwort enthält `{"url": "…"}`, die Authorize-URL des Anbieters.
3. Er meldet sich beim Anbieter an. Der Callback speichert die Verknüpfung (Seite, Issuer, `sub`,
   dazu den E-Mail-Claim zur Anzeige), meldet ihn an und führt zurück ins Profil.

Danach genügt auf der Statusseite „Anmelden“ → „Mit SSO anmelden“.

Das Profil listet bestehende Verknüpfungen („verknüpft als <E-Mail-Claim>“) und löst sie mit
„Verknüpfung lösen“ (`GET /api/me/sso`, `DELETE /api/me/sso/<ID>`, nur eigene Verknüpfungen).
Als verknüpfbar zeigt es nur Seiten mit aktivem OIDC, die die Regeln unten erlauben: ohne Rolle
auf einer Seite nur öffentliche, gelistete Seiten (damit die Liste keine privaten Seitennamen
verrät), mit Rolle auf genau einer Seite nur diese, mit Rollen auf mehreren Seiten und für Global
Admins keine. Eine private Seite ohne eigene Rolle lässt sich weiterhin per API verknüpfen.

Ausgeschlossen sind, sowohl beim Verknüpfen als auch bei jeder Anmeldung (403):

- **Global Admins** – „Global Admins melden sich nicht über das Seiten-SSO an“. Den IdP einer Seite
  kontrolliert deren Admin; eine Verknüpfung gäbe ihm sonst einen Weg zu globalen Rechten.
- **Konten mit einer Rolle auf einer anderen Statusseite** – „Konten mit Rolle auf einer anderen
  Seite nutzen dieses Seiten-SSO nicht“. Der Admin des fremden IdP könnte sonst Rechte auf anderen
  Seiten erlangen. Eine Rolle auf *dieser* Seite ist erlaubt.

### Die Sitzung gilt nur für diese Seite

Die Sitzung nach einem Seiten-SSO-Login (OIDC und SAML) ist auf die Seite beschränkt, deren IdP
die Anmeldung bestätigt hat (Claim `sso_page_id` im JWT). Das gilt auch für Rechte, die
das Konto erst nach der Anmeldung bekommt: Erhält es während der Sitzung eine Rolle auf einer
anderen Seite oder wird es Global Admin, wirkt das in dieser Sitzung nicht.

- Admin-Routen anderer Seiten und alle Global-Admin-Routen (Nutzer, Rollen, Tags, Seiten anlegen,
  Audit-Log …) antworten mit 403. Private andere Seiten bleiben verborgen.
- Die Seitenauswahl im Admin-Bereich zeigt nur diese Seite.
- Das eigene Profil (`/api/me…`, Datenexport, Konto löschen, SSO-Verknüpfungen, Abmelden)
  funktioniert wie gewohnt.

Wer mehr braucht, meldet sich mit Passwort an.

Weitere Regeln:

- Eine Anbieter-Identität gehört genau einem Konto. Ist sie schon mit einem anderen Konto
  verknüpft, antwortet der Callback mit 409 „Diese Anmeldung ist bereits mit einem anderen Konto
  verknüpft“.
- Eine Anmeldung ohne Verknüpfung endet mit 403 „Diese Anmeldung ist mit keinem Konto verknüpft“.
- Die Verknüpfung hängt am Issuer. Wechselt der Issuer (anderer Realm, anderer Tenant, Schrägstrich
  am Ende), löscht das Speichern der Konfiguration die OIDC-Verknüpfungen der Seite (Audit-Eintrag
  `sso.oidc_identities_reset`,; die SSO-Seite warnt vorher. Die Konten
  müssen neu verknüpft werden.

## Fehlerbilder

Login und Callback zeigen Fehler auf der Login-Seite (bzw. im Profil oder auf der SSO-Seite) als
übersetzte Meldung zum Code in `oidc_error` bzw. `oidc_test`. Die JSON-Endpunkte (Verknüpfen,
Testanmeldung starten) antworten weiter mit Status und `{"error": "…"}`.

| Code | Meldung / Status (JSON) | Ursache und Abhilfe |
|------|-------------------------|---------------------|
| `provider_denied` | – | Der Anbieter hat ohne Code zurückgeleitet (`error=…`), etwa weil der Nutzer die Anmeldung abgebrochen oder der Anbieter sie abgelehnt hat. |
| `not_linked` | 403 „Diese Anmeldung ist mit keinem Konto verknüpft“ | Siehe [Konto-Verknüpfung](#konto-verknüpfung-wer-darf-das-seiten-sso-nutzen). |
| `already_linked` | 409 „Diese Anmeldung ist bereits mit einem anderen Konto verknüpft“ | Siehe [Konto-Verknüpfung](#konto-verknüpfung-wer-darf-das-seiten-sso-nutzen). |
| `global_admin` / `other_page_role` | 403 | Siehe [Konto-Verknüpfung](#konto-verknüpfung-wer-darf-das-seiten-sso-nutzen). |
| `test_disabled` / `test_incomplete` | 403 / 400 | Siehe [Testanmeldung](#testanmeldung). |
| `bad_page` | 400 „Ungültige Seite“ | Seiten-ID im Login-Link ist keine Zahl. |
| `internal` | 500 | Login-State oder Verknüpfung konnte nicht gespeichert werden (Datenbank). |

Die übrigen Codes:

| Meldung / Status | Ursache und Abhilfe |
|------------------|---------------------|
| `provider_unreachable`: 502 „OIDC-Anbieter nicht erreichbar“, Log `OIDC-Issuer muss https verwenden` | Der Issuer beginnt nicht mit `https://`. Produktiv ist https Pflicht. Ausnahme nur für die lokale Entwicklung, siehe [Lokales Testen](#lokales-testen-mit-dem-mock). |
| `provider_unreachable`, Log `interne Adresse nicht erlaubt` | SSRF-Schutz: Discovery, JWKS und Token-Endpunkt dürfen nicht auf Loopback-, private (10/8, 172.16/12, 192.168/16, fc00::/7), link-lokale oder unspezifizierte Adressen zeigen – auch nicht nach Redirects oder DNS-Rebinding. Ein IdP im internen Netz muss über eine öffentliche Adresse erreichbar sein. |
| `provider_unreachable`, Log `OIDC-Endpunkt muss eine https-Adresse sein` | Das Discovery-Dokument nennt für `authorization_endpoint` oder `token_endpoint` eine Adresse ohne https (z. B. `javascript:`, `data:` oder `http://`). Nur https ist erlaubt; `http://` gilt allein für den Entwicklungs-Issuer, siehe [Lokales Testen](#lokales-testen-mit-dem-mock). |
| `provider_unreachable`, Log `issuer URL provided to client (…) did not match the issuer URL returned by provider (…)` | Issuer-Mismatch: Der Issuer aus `oidc_discovery_url` stimmt nicht zeichengenau mit dem `issuer` im Discovery-Dokument überein. Häufigste Ursache ist ein fehlender oder überzähliger Schrägstrich am Ende. Den Wert aus dem Feld `issuer` des Discovery-Dokuments übernehmen. |
| `provider_unreachable` (sonst) | Anbieter nicht erreichbar oder Discovery-URL falsch. Ein fehlgeschlagener Versuch wird 1 Minute zwischengespeichert, ein erfolgreicher 1 Stunde – nach einer Korrektur ggf. kurz warten. |
| `token_exchange` (502 „Token-Austausch fehlgeschlagen“) | Client-ID/-Secret falsch, Redirect-URI beim Anbieter nicht exakt eingetragen, oder der Code ist abgelaufen. |
| `no_id_token` (502 „Anbieter lieferte kein ID-Token“) | Scope `openid` fehlt beim Anbieter bzw. die Anwendung ist kein OIDC-Client. |
| `id_token_invalid` (403 „ID-Token ungültig“) | Signatur, `aud` (Client-ID), Ablaufzeit oder Nonce passen nicht. Uhrzeit des Servers prüfen. |
| `state_invalid` (400 „Ungültiger oder abgelaufener Login-Versuch“) | Login älter als 10 Minuten, Callback doppelt aufgerufen, oder das State-Cookie fehlt (Login in einem anderen Browser begonnen; das Cookie ist `Secure` und wird außer auf `localhost` nur über https gesetzt). |
| `not_configured` (404 „SSO für diese Seite nicht eingerichtet“) | `oidc_enabled` ist aus oder die Seiten-ID stimmt nicht. |
| 429 | Rate-Limit: Login, Callback und Testanmeldung je 20 Aufrufe pro Minute, die Abfrage `GET /api/public/pages/<slug>/sso` 60 pro Minute. Bei Login und Callback zeigt der Browser hier noch die JSON-Antwort des Rate-Limiters. |

## Beispiele für Anbieter

In allen Fällen: Redirect-URI `<BASE_URL>/api/auth/oidc/callback?page=<Seiten-ID>`, Client-Typ
„vertraulich“ (mit Secret), Authorization-Code-Flow. PKCE (S256) schickt das Backend immer mit.

### Authentik

- Anwendung mit **OAuth2/OpenID-Provider** anlegen, Client-Typ *Confidential*, Redirect-URI wie oben.
- Issuer: `https://<authentik-host>/application/o/<anwendungs-slug>/` – **mit** abschließendem
  Schrägstrich, so wie er im Discovery-Dokument steht.
- Client-ID und Secret aus dem Provider übernehmen.

### Keycloak

- Im Realm einen Client vom Typ *OpenID Connect* anlegen, *Client authentication* einschalten,
  *Standard flow* aktiv, Redirect-URI unter *Valid redirect URIs* eintragen.
- Issuer: `https://<keycloak-host>/realms/<realm>` (ältere Versionen mit `/auth`-Präfix:
  `https://<host>/auth/realms/<realm>`).
- Secret unter *Credentials* des Clients.

### Microsoft Entra ID

- *App-Registrierung* anlegen, Plattform **Web**, Redirect-URI wie oben.
- Unter *Zertifikate & Geheimnisse* ein Client-Secret erzeugen und den **Wert** (nicht die
  Secret-ID) übernehmen; Client-ID ist die *Anwendungs-ID (Client)*.
- Issuer mandantenbezogen: `https://login.microsoftonline.com/<Tenant-ID>/v2.0`. Die
  mandantenübergreifenden Endpunkte (`common`, `organizations`) passen nicht, weil ihr
  Discovery-Dokument einen Platzhalter-Issuer meldet (Issuer-Mismatch). Multi-Tenant-Anmeldung
  wird deshalb nicht unterstützt; dafür bräuchte es eine eigene Issuer-Prüfung.

### Google

- In der Google Cloud Console eine *OAuth-Client-ID* vom Typ **Webanwendung** anlegen,
  Redirect-URI unter *Autorisierte Weiterleitungs-URIs* eintragen.
- Issuer: `https://accounts.google.com`.
- Dieses Seiten-SSO ist unabhängig vom globalen Google-Login des Systems; Verknüpfungen gelten nur
  für die jeweilige Statusseite.

## Lokales Testen mit dem Mock

Für die Entwicklung gibt es einen Mock-Anbieter im Compose-Profil `sso`. Die Schritte stehen im
README, Abschnitt [„Lokale SSO-Tests mit Mock-OIDC“](../../README.md#lokale-sso-tests-mit-mock-oidc).

Kern ist die Variable `OIDC_DEV_INSECURE_ISSUERS`: eine kommagetrennte Liste von Issuern, für die
https-Pflicht und SSRF-Schutz entfallen (z. B. `http://localhost:8090/statushafen`). Sie greift nur
bei exakter Übereinstimmung (kein Präfix) und nur mit `APP_ENV` `development`, `dev`, `test` oder `local`. Auf einem Server darf
sie nicht gesetzt sein.
