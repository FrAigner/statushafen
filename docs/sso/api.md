# SSO-API

Alle Pfade beginnen mit `/api`. JSON-Endpunkte melden Fehler als `{"error": "<Text>"}`. Endpunkte,
die der Browser direkt aufruft (Login, Callback, ACS), leiten bei einem Fehler stattdessen mit einem
festen Code ins Frontend um (siehe [Fehler-Redirects](#fehler-redirects)). Den Ablauf zeigt die
[Architektur](architecture.md), die Einrichtung der [OIDC-Guide](oidc.md) bzw. der
[SAML-Guide](saml.md).

**Anmeldung an der API:** Endpunkte mit Login erwarten das JWT im Header `X-Auth-Token: <token>`
(). `Authorization: Bearer <token>` wird weiter angenommen; ist `X-Auth-Token` gesetzt, hat
er Vorrang.

## Seiten-Konfiguration

Diese Endpunkte brauchen ein JWT und die Rechte `sso:read`, `sso:update` bzw. `sso:delete` auf der Statusseite (oder Global Admin).

### `GET /api/admin/pages/:page_id/sso`

Liest die SSO-Konfiguration. Ohne gespeicherte Konfiguration kommen Standardwerte (`oidc_enabled`
und `saml_enabled` `false`, `test_login_enabled` `true`). Das Client-Secret und der private
SP-Schlüssel werden nie ausgegeben.

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
  "test_login_enabled": true,
  "saml_enabled": false,
  "saml_idp_metadata_url": "",
  "saml_idp_metadata_xml": "",
  "saml_attribute_email": "email",
  "saml_attribute_name": "name",
  "saml_allow_unspecified_nameid": false,
  "saml_entity_id": "https://status.example.org/api/auth/saml/1/metadata",
  "saml_metadata_url": "https://status.example.org/api/auth/saml/1/metadata",
  "saml_acs_url": "https://status.example.org/api/auth/saml/1/acs",
  "saml_sp_certificate": "-----BEGIN CERTIFICATE-----…"
}
```

`oidc_acs_url`, `saml_entity_id`, `saml_metadata_url` und `saml_acs_url` werden bei jeder Anfrage
aus `BASE_URL` gebildet. Fehler: `400` (`Invalid page_id`), `404` (`Statusseite nicht gefunden`).

### `PUT /api/admin/pages/:page_id/sso`

Speichert die Konfiguration (Upsert, genau eine Zeile je Seite; auch nach `DELETE` erneut möglich).
Body (JSON, höchstens 256 KB):

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
| `test_login_enabled` | bool | Erlaubt die Testanmeldung (OIDC und SAML). |
| `saml_enabled` | bool | Bei `true` sind IdP-Metadaten Pflicht. |
| `saml_idp_metadata_url` | string | Nur `https://`. Nicht zusammen mit `saml_idp_metadata_xml`. |
| `saml_idp_metadata_xml` | string | Eingefügtes Metadaten-XML, höchstens 128 KB, wird beim Speichern geprüft. |
| `saml_attribute_email` | string | Attribut für die E-Mail an der Verknüpfung (leer = `email`). |
| `saml_attribute_name` | string | Attribut für den Anzeigenamen (leer = `name`). |
| `saml_allow_unspecified_nameid` | bool | Lässt NameIDs im Format „unspecified“ zu (Standard aus). |

Beim ersten Speichern erzeugt der Server das SP-Schlüsselpaar der Seite. Ändert sich der OIDC-Issuer
bzw. der SAML-Vertrauensanker (Metadaten), löscht das Speichern die Verknüpfungen des Protokolls auf
dieser Seite.

Antwort `200`: die gespeicherte Konfiguration wie bei `GET`. Fehler: `400` (`Invalid page_id`,
`Invalid request`, `OIDC enabled erfordert Discovery URL, Client ID und Secret`,
`SAML enabled erfordert IdP-Metadaten (URL oder XML)` und weitere SAML-Prüfungen), `404`, `413`
(`Anfrage zu groß`), `500` (`Fehler beim Speichern der SSO-Config`). Audit-Log:
`statuspage.sso_updated`.

### `DELETE /api/admin/pages/:page_id/sso`

Löscht die Konfiguration. Ein späteres `PUT` übernimmt weder das alte Client-Secret noch das alte
SP-Schlüsselpaar (neues Paar, SP-Metadaten beim IdP neu einlesen). Antwort `200`:
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

### `POST /api/admin/pages/:page_id/sso/saml-test`

Wie `sso/test`, nur für SAML. Fehler: `400` (`Ungültige Seite`, `Für die Testanmeldung fehlen die
IdP-Metadaten`), `403`, `404`, `502` (Metadaten nicht abrufbar oder ungültig), `500`.

## Öffentlich

### `GET /api/public/pages/:slug/sso`

Begrenzt auf 60 Aufrufe pro Minute. Meldet nur, ob das Seiten-SSO aktiv ist, damit das Frontend
„Mit SSO anmelden“ anzeigt. Antwort `200`: `{"oidc_enabled": true, "saml_enabled": false,
"page_id": 1}`. Bei einer **privaten Seite** nennt die Auskunft Seiten-ID und SSO-Art nur
Berechtigten (Rolle auf der Seite oder Global Admin); für alle anderen antwortet sie genau wie
bei einer nicht vorhandenen Seite mit `404` (`Status page not found`), damit sich anonym nicht
erkennen lässt, welche Slugs private Seiten sind. Den Einstieg für diese Besucher bietet
[`GET /api/auth/sso/login`](#get-apiauthssologin).

### `GET /api/auth/sso/login`

Öffentlich, begrenzt auf 20 Aufrufe pro Minute. Query: `slug` (Slug der Statusseite). Einstieg ins
Seiten-SSO ohne Seiten-ID: Der Server wählt das Verfahren (OIDC, wenn aktiv, sonst SAML), startet
den Ablauf wie `oidc/login` bzw. `saml/login` und leitet mit `307` zum IdP weiter. Eine unbekannte
Seite, eine Seite ohne aktives SSO und ein fehlender `slug` enden gleich: Weiterleitung auf die
Login-Seite mit dem Code `not_configured`. Wer die Adresse für einen Slug aufruft und zum IdP
geleitet wird, erfährt damit nur, dass die Seite SSO nutzt. Die Statusseite bietet diesen Weg an,
wenn sie für den Besucher nicht erreichbar ist (nicht vorhanden oder privat).

## Anmeldung mit OIDC

### `GET /api/auth/oidc/login`

Öffentlich, begrenzt auf 20 Aufrufe pro Minute. Query: `page` (ID der Statusseite, Pflicht). Legt
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

## Anmeldung mit SAML

### `GET /api/auth/saml/login`

Öffentlich, begrenzt auf 20 Aufrufe pro Minute. Query: `page`. Erzeugt einen signierten AuthnRequest
(HTTP-Redirect-Binding), setzt das Cookie `saml_state` (HttpOnly, Secure, `SameSite=None`) und
leitet mit `307` zum IdP weiter. Fehler leiten wie bei OIDC auf die Login-Seite um (zusätzlich
`metadata_invalid`).

### `GET /api/auth/saml/:page_id/metadata`

Öffentlich, begrenzt auf 60 Aufrufe pro Minute. Liefert die SP-Metadaten der Seite (XML). Die URL ist
zugleich die SP Entity ID. Fehler: `400` (`Ungültige Seite`), `404` (`SAML für diese Seite nicht
eingerichtet`, auch vor dem ersten Speichern und für gelöschte Seiten), `500`
(`SP-Zertifikat ungültig`).

### `POST /api/auth/saml/:page_id/acs`

Öffentlich, begrenzt auf 20 Aufrufe pro Minute. Assertion Consumer Service: Der IdP schickt
`SAMLResponse` und `RelayState` per HTTP-POST. Bei Erfolg `303` auf
`<FRONTEND_URL>/auth/sso-callback?code=<Einmal-Code>`. Fehler leiten mit Code um (wie beim
OIDC-Callback, dazu `saml_response_invalid`, `saml_replay`, `saml_nameid_format`,
`metadata_invalid`). Audit-Log: `auth.sso_login` bzw. `auth.sso_linked` mit `provider=saml`.

### `POST /api/auth/saml/link`

Braucht ein JWT. Query: `page`. Wie [`oidc/link`](#post-apiauthoidclink), nur für SAML. Antwort
`200`: `{"url": "<SSO-URL des IdP>"}`.

## Sitzung und Verknüpfungen

### `POST /api/auth/sso-exchange`

Öffentlich, begrenzt auf 10 Aufrufe pro Minute. Tauscht den Einmal-Code aus dem Redirect gegen das
JWT. Body: `{"code": "<Einmal-Code>"}`. Antwort `200`: `{"token": "<JWT>"}`. Der Code gilt nur einmal
und nur kurz. Das JWT gilt nur für die Seite, deren IdP die Anmeldung bestätigt hat.

Fehler: `400` (`Code erforderlich`), `401` (`Ungültiger oder abgelaufener Code`).

### `GET /api/me/sso`

Braucht ein JWT. Liefert die eigenen Verknüpfungen und die Seiten, die das Konto noch verknüpfen
darf, je Protokoll:

```json
{
  "identities": [{"id": 7, "status_page_id": 1, "page_name": "Demo", "protocol": "oidc", "email": "user@example.org", "created_at": "…"}],
  "linkable": [{"status_page_id": 1, "page_name": "Demo", "protocol": "saml"}]
}
```

Fehler: `401` (`Nicht angemeldet`).

### `DELETE /api/me/sso/:id`

Braucht ein JWT. Löst eine eigene Verknüpfung. Antwort `204`. Fehler: `400` (`Ungültige ID`), `401`,
`404` (`Verknüpfung nicht gefunden`, auch für fremde IDs). Audit-Log: `auth.sso_unlinked`.

## Fehler-Redirects

Login, Callback und ACS antworten bei einem Fehler nicht mit JSON, sondern leiten um:

| Ablauf | Ziel |
| --- | --- |
| Login | `<FRONTEND_URL>/login?oidc_error=<Code>` |
| Verknüpfung | `<FRONTEND_URL>/admin/profile?oidc_error=<Code>` |
| Testanmeldung | `<FRONTEND_URL>/admin/sso?oidc_test=<Code>` |

Die Parameter heißen aus historischen Gründen `oidc_*` und tragen die Codes beider Protokolle. In der
URL steht nur der feste Code, nie eine Meldung des IdP. Ursachen und Abhilfe je Code stehen in den
Fehlerbildern des Admin-Handbuchs:
[OIDC](../user-guide/sso-oidc.md#fehlerbilder), [SAML](../user-guide/sso-saml.md#fehlerbilder).
