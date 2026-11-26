# SSO-API

Alle Pfade beginnen mit `/api`. JSON-Endpunkte melden Fehler als `{"error": "<Text>"}`. Endpunkte,
die der Browser direkt aufruft (Login, Callback), leiten bei einem Fehler stattdessen mit einem
festen Code ins Frontend um (siehe [Fehler-Redirects](#fehler-redirects)). Den Ablauf zeigt die
[Architektur](architecture.md), die Einrichtung der [OIDC-Guide](oidc.md).

**Anmeldung an der API:** Endpunkte mit Login erwarten das JWT im Header `X-Auth-Token: <token>`
(). `Authorization: Bearer <token>` wird weiter angenommen; ist `X-Auth-Token` gesetzt, hat
er Vorrang.

## Seiten-Konfiguration

Diese Endpunkte brauchen ein JWT und die Rechte `sso:read`, `sso:update` bzw. `sso:delete` auf der Statusseite (oder Global Admin).

### `GET /api/admin/pages/:page_id/sso`

Liest die SSO-Konfiguration. Ohne gespeicherte Konfiguration kommen Standardwerte (`oidc_enabled`
`false`, `test_login_enabled` `true`). Das Client-Secret wird nie ausgegeben.

Antwort `200` (gekürzt):

```json
{
  "status_page_id": 1,
  "oidc_enabled": true,
  "oidc_discovery_url": "https://idp.example.org/realms/demo",
  "oidc_client_id": "CLIENT-ID",
  "oidc_client_secret_set": true,
  "oidc_scopes": "openid email profile",
  "oidc_attribute_email": "email",
  "oidc_attribute_name": "name",
  "oidc_acs_url": "https://status.example.org/api/auth/oidc/callback?page=1",
  "test_login_enabled": true
}
```

`oidc_acs_url` wird bei jeder Anfrage aus `BASE_URL` gebildet. Fehler: `400` (`Invalid page_id`), `404` (`Statusseite nicht gefunden`).

### `PUT /api/admin/pages/:page_id/sso`

Speichert die Konfiguration (Upsert, genau eine Zeile je Seite; auch nach `DELETE` erneut möglich).
Body (JSON, höchstens 64 KB; Felder `saml_*` früherer Versionen werden ignoriert):

| Feld | Typ | Hinweis |
| --- | --- | --- |
| `oidc_enabled` | bool | Bei `true` sind Discovery-URL, Client-ID und ein Client-Secret Pflicht. |
| `oidc_discovery_url` | string | Issuer oder volle `.well-known/openid-configuration`-Adresse. |
| `oidc_client_id` | string | |
| `oidc_client_secret` | string | Leer oder weggelassen behält das gespeicherte Secret; ein neuer Wert ersetzt es. |
| `oidc_scopes` | string | Leerzeichen-getrennt (leer = `openid email profile`), `openid` wird ergänzt. |
| `oidc_attribute_email` | string | Claim für die E-Mail an der Verknüpfung, nur zur Anzeige (leer = `email`). |
| `oidc_attribute_name` | string | Claim für den Anzeigenamen (leer = `name`). |
| `oidc_attribute_role` | string | Wird gespeichert, hat aber keine Wirkung (kein Rollen-Mapping aus dem IdP). |
| `test_login_enabled` | bool | Erlaubt die Testanmeldung. |

Ändert sich der OIDC-Issuer, löscht das Speichern die OIDC-Verknüpfungen dieser Seite.

Antwort `200`: die gespeicherte Konfiguration wie bei `GET`. Fehler: `400` (`Invalid page_id`,
`Invalid request`, `OIDC enabled erfordert Discovery URL, Client ID und Secret`), `404`, `413`
(`Anfrage zu groß`), `500` (`Fehler beim Speichern der SSO-Config`). Audit-Log:
`statuspage.sso_updated`.

### `DELETE /api/admin/pages/:page_id/sso`

Löscht die Konfiguration. Ein späteres `PUT` übernimmt das alte Client-Secret nicht. Antwort `200`:
`{"message": "SSO-Config gelöscht"}`. Fehler: `400`, `404`, `500` (`Fehler beim Löschen`).
Audit-Log: `statuspage.sso_deleted`.

### `POST /api/admin/pages/:page_id/sso/test`

Startet eine OIDC-Testanmeldung mit der gespeicherten Konfiguration, auch vor `oidc_enabled`.
Begrenzt auf 20 Aufrufe pro Minute. Antwort `200`: `{"url": "<Authorize-URL des IdP>"}`. Das
Ergebnis kommt als `oidc_test=<Code>` (bzw. `ok`) auf `<FRONTEND_URL>/admin/sso` zurück; Sitzung und
Verknüpfung entstehen nicht.

Fehler: `400` (`Ungültige Seite`, `Für die Testanmeldung fehlen Discovery URL, Client ID oder
Secret`), `403` (`Testanmeldung ist für diese Seite abgeschaltet`), `404` (`SSO für diese Seite nicht
eingerichtet`), `502` (`OIDC-Anbieter nicht erreichbar`), `500`.

## Öffentlich

### `GET /api/public/pages/:slug/sso`

Begrenzt auf 60 Aufrufe pro Minute. Meldet nur, ob das Seiten-SSO aktiv ist, damit das Frontend
„Mit SSO anmelden“ anzeigt. Antwort `200`: `{"oidc_enabled": true,
"page_id": 1}`. Bei einer **privaten Seite** nennt die Auskunft Seiten-ID und SSO-Art nur
Berechtigten (Rolle auf der Seite oder Global Admin); für alle anderen antwortet sie genau wie
bei einer nicht vorhandenen Seite mit `404` (`Status page not found`), damit sich anonym nicht
erkennen lässt, welche Slugs private Seiten sind. Den Einstieg für diese Besucher bietet
[`GET /api/auth/sso/login`](#get-apiauthssologin).

### `GET /api/auth/sso/login`

Öffentlich, begrenzt auf 20 Aufrufe pro Minute. Query: `slug` (Slug der Statusseite), optional
`return_to` und `return_host` (siehe [eigene Domain](providers.md#statusseiten-auf-eigener-domain)). Einstieg ins
Seiten-SSO ohne Seiten-ID: Der Server startet
den Ablauf wie `oidc/login` und leitet mit `307` zum IdP weiter. Eine unbekannte
Seite, eine Seite ohne aktives SSO und ein fehlender `slug` enden gleich: Weiterleitung auf die
Login-Seite mit dem Code `not_configured`. Wer die Adresse für einen Slug aufruft und zum IdP
geleitet wird, erfährt damit nur, dass die Seite SSO nutzt. Die Statusseite bietet diesen Weg an,
wenn sie für den Besucher nicht erreichbar ist (nicht vorhanden oder privat).

## Anmeldung mit OIDC

### `GET /api/auth/oidc/login`

Öffentlich, begrenzt auf 20 Aufrufe pro Minute. Query: `page` (ID der Statusseite, Pflicht),
`return_to` und `return_host` (optional, siehe [Rücksprung](providers.md#rücksprung-nach-dem-login)). Legt
State, Nonce und PKCE-Verifier an (10 Minuten gültig), setzt das Cookie `oidc_state` (HttpOnly,
Secure) und leitet mit `307` zum IdP weiter. Fehler leiten auf die Login-Seite um (Codes
`bad_page`, `not_configured`, `provider_unreachable`, `internal`).

### `GET /api/auth/oidc/callback`

Öffentlich, begrenzt auf 20 Aufrufe pro Minute. Der IdP leitet hierher zurück. Query: `page`, `state`,
`code`; das Cookie `oidc_state` muss zum `state` passen. Bei Erfolg `307` auf
`<FRONTEND_URL>/auth/sso-callback?code=<Einmal-Code>`, den das Frontend per
[`sso-exchange`](#post-apiauthsso-exchange) einlöst. Fehler leiten mit Code um (`state_invalid`,
`provider_denied`, `token_exchange`, `no_id_token`, `id_token_invalid`, `not_linked`,
`already_linked`, `global_admin`, `other_page_role`, `not_configured`, `provider_unreachable`,
`internal`). Audit-Log: `auth.sso_login` bzw. `auth.sso_linked`.

### `POST /api/auth/oidc/link`

Braucht ein JWT. Query: `page`. Startet die Verknüpfung des angemeldeten Kontos. Antwort `200`:
`{"url": "<Authorize-URL des IdP>"}`; der Client leitet den Browser dorthin. Danach läuft derselbe
Callback, Fehler landen im Profil.

Fehler (JSON): `401` (`Nicht angemeldet`), `403` (`Global Admins melden sich nicht über das
Seiten-SSO an`, `Konten mit Rolle auf einer anderen Seite nutzen dieses Seiten-SSO nicht`), `400`
(`Ungültige Seite`), `404` (`SSO für diese Seite nicht eingerichtet`), `502`
(`OIDC-Anbieter nicht erreichbar`), `500`.

## Globale OIDC-Anbieter

Anbieter aus `OIDC_PROVIDERS` (siehe [Globale Anbieter](providers.md)); `:id` ist die Anbieter-ID.

### `GET /api/public/auth-config`

Öffentlich, begrenzt auf 60 Aufrufe pro Minute. Anmeldemethoden für die Login-Seite. Antwort `200`:

```json
{"password_login": false, "providers": [{"id": "gitlab", "name": "GitLab"}]}
```

`providers` enthält nur aktive Anbieter, in der Reihenfolge von `OIDC_PROVIDERS`, ohne Issuer oder
Client-Daten.

### `GET /api/auth/provider/:id/login`

Öffentlich, begrenzt auf 20 Aufrufe pro Minute. Query: `return_to` und `return_host` (optional, siehe
[Rücksprung](providers.md#rücksprung-nach-dem-login)). Kommt einer dieser drei Login-Starts auf einer
verifizierten eigenen Domain an, leitet er zuerst mit `307` auf denselben Pfad unter `BASE_URL` weiter
(mit der Domain als `return_host`). Legt State, Nonce und PKCE-Verifier an (10
Minuten gültig), setzt das Cookie `oidc_state` (Pfad `/api/auth/provider`, HttpOnly, Secure) und leitet
mit `307` zum Anbieter weiter. Fehler leiten auf die Login-Seite um (`provider_unknown`,
`provider_unreachable`, `internal`).

### `GET /api/auth/provider/:id/callback`

Öffentlich, begrenzt auf 20 Aufrufe pro Minute. Redirect-URI beim Anbieter
(`<BASE_URL>/api/auth/provider/<id>/callback`). Query: `state`, `code`; nur ein State dieses Anbieters
wird angenommen. Bei Erfolg `307` auf `<FRONTEND_URL>/auth/sso-callback?code=<Einmal-Code>` (bei
Verknüpfung mit `&next=profile`). Fehler leiten mit Code um (`provider_unknown`, `state_invalid`,
`provider_denied`, `provider_unreachable`, `token_exchange`, `no_id_token`, `id_token_invalid`,
`email_missing`, `email_unverified`, `domain_not_allowed`, `not_linked`, `already_linked`,
`internal`). Audit-Log: `auth.sso_login` bzw. `auth.sso_linked` mit `provider=<id>`.

### `POST /api/auth/provider/:id/link`

Braucht ein JWT, ohne Query und Body. Startet die Verknüpfung des angemeldeten Kontos (auch Global
Admins). Antwort `200`: `{"url": "<Authorize-URL des Anbieters>"}`. Fehler (JSON): `401`, `403`
(Sitzung aus einem Seiten-SSO), `404` (unbekannte ID), `502` (Anbieter nicht erreichbar), `500`.

## Sitzung und Verknüpfungen

### `POST /api/auth/sso-exchange`

Öffentlich, begrenzt auf 10 Aufrufe pro Minute. Tauscht den Einmal-Code aus dem Redirect gegen das
JWT. Body: `{"code": "<Einmal-Code>"}`. Antwort `200`: `{"token": "<JWT>", "return_to": "/pfad"}`
(`return_to` ist der geprüfte Rücksprungpfad aus dem Login-Start, sonst `""`). Ein Code für eine eigene
Domain wird nur dort eingelöst, ein Code für `FRONTEND_URL` nie auf einer eigenen Domain (sonst `401`). Eingelöst wird
nur mit dem Cookie `sso_binding`, das der Login-Start in genau diesem Browser gesetzt hat (Schutz gegen Login-CSRF;
das Frontend schickt es mit `credentials: 'include'`, danach wird es gelöscht); ohne passendes Cookie `401`, der Code ist
dann verbraucht. Der Code gilt nur einmal
und nur kurz. Nach einem Seiten-SSO gilt das JWT nur für die Seite, deren IdP die Anmeldung bestätigt
hat; nach einem globalen Anbieter für das ganze Konto.

Fehler: `400` (`Code erforderlich`), `401` (`Ungültiger oder abgelaufener Code`).

### `GET /api/me/sso`

Braucht ein JWT. Liefert die eigenen Verknüpfungen und die Seiten, die das Konto noch verknüpfen
darf:

```json
{
  "identities": [
    {"id": 7, "status_page_id": 1, "page_name": "Demo", "protocol": "oidc", "provider": "", "email": "user@example.org", "created_at": "…"},
    {"id": 8, "status_page_id": 0, "page_name": "GitLab", "protocol": "oidc", "provider": "gitlab", "email": "user@example.org", "created_at": "…"}
  ],
  "linkable": [
    {"status_page_id": 0, "page_name": "Google", "protocol": "oidc", "provider": "google"},
    {"status_page_id": 2, "page_name": "Andere", "protocol": "oidc", "provider": ""}
  ]
}
```

Einträge mit `status_page_id` `0` gehören zu einem globalen Anbieter: `provider` ist seine ID,
`page_name` sein Anzeigename (verknüpfen über
[`POST /api/auth/provider/:id/link`](#post-apiauthprovideridlink)). Ist der Anbieter nicht mehr
konfiguriert, bleibt die Verknüpfung mit leerem `provider` sichtbar und lösbar. Globale Anbieter
erscheinen auch für Global Admins, nie in einer Sitzung aus einem Seiten-SSO.

Fehler: `401` (`Nicht angemeldet`).

### `DELETE /api/me/sso/:id`

Braucht ein JWT. Löst eine eigene Verknüpfung. Antwort `204`. Fehler: `400` (`Ungültige ID`), `401`,
`404` (`Verknüpfung nicht gefunden`, auch für fremde IDs). Audit-Log: `auth.sso_unlinked`.

## Fehler-Redirects

Login und Callback antworten bei einem Fehler nicht mit JSON, sondern leiten um:

| Ablauf | Ziel |
| --- | --- |
| Login | `<FRONTEND_URL>/login?oidc_error=<Code>` |
| Verknüpfung | `<FRONTEND_URL>/admin/profile?oidc_error=<Code>` |
| Testanmeldung | `<FRONTEND_URL>/admin/sso?oidc_test=<Code>` |

In der
URL steht nur der feste Code, nie eine Meldung des IdP. Ursachen und Abhilfe je Code stehen in den
Fehlerbildern des Admin-Handbuchs:
[OIDC](../user-guide/sso-oidc.md#fehlerbilder).
