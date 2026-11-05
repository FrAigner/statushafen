# SAML-Anmeldung je Statusseite einrichten

> English: [`en/sso-saml.md`](./en/sso-saml.md)

Neben OIDC ([`sso-oidc.md`](./sso-oidc.md)) kann jede Statusseite einen eigenen SAML-2.0-Identity-
Provider (IdP) bekommen, etwa Microsoft Entra ID, Keycloak, Authentik, ADFS oder Okta.
Beide lassen sich auf derselben Seite parallel einschalten. Es gelten dieselben Regeln wie bei OIDC:
keine Neuanlage von Konten, Verknüpfung durch den angemeldeten Nutzer selbst, keine Global Admins,
keine Konten mit Rolle auf einer anderen Seite, kein Rollen-Mapping aus IdP-Attributen. Single
Logout (SLO) gibt es nicht. Die Sitzung gilt nur für diese Seite und nie mit Global-Admin-Rechten
(siehe [`sso-oidc.md`](./sso-oidc.md#die-sitzung-gilt-nur-für-diese-seite)).

Stand des Backends: `backend/handlers/saml_login.go`, `saml_metadata.go`, `saml_keys.go`,
`statuspage_sso.go`. Bibliothek: [`github.com/crewjam/saml`](https://github.com/crewjam/saml)
(reines Go, XML-Signaturen über `russellhaering/goxmldsig`, kein `xmlsec1` im Image).

## Überblick: so läuft die Anmeldung

1. Auf der Login-Seite (bzw. im Hinweis einer privaten Seite) erscheint „Mit SSO anmelden (SAML)“,
   sobald SAML für die Seite aktiv ist (`GET /api/public/pages/<slug>/sso` meldet `saml_enabled`).
   Der Browser ruft `GET /api/auth/saml/login?page=<Seiten-ID>` auf. Bei einer **privaten Seite**
   nennt die Auskunft Seiten-ID und SSO-Art nur Berechtigten; Besucher sehen im Hinweis „Mit SSO
   anmelden“ und der Browser ruft `GET /api/auth/sso/login?slug=<Slug>` auf, worauf der Server das
   Verfahren (OIDC, wenn aktiv, sonst SAML) selbst wählt.
2. Das Backend lädt die IdP-Metadaten, erzeugt einen mit dem SP-Schlüssel der Seite signierten
   AuthnRequest (HTTP-Redirect-Binding, RSA-SHA256), merkt sich dessen ID 10 Minuten lang
   (`SAMLLoginState`) und leitet zum IdP weiter. Ein zufälliger State geht als `RelayState` mit und
   steht zusätzlich im Cookie `saml_state` (`Secure`, `HttpOnly`, `SameSite=None`, weil der IdP per
   Cross-Site-POST zurückkommt).
3. Der IdP schickt die Antwort per HTTP-POST an `<BASE_URL>/api/auth/saml/<Seiten-ID>/acs`.
4. Das Backend löst den State genau einmal ein und prüft die Antwort:
   - Signatur von Response **oder** Assertion ist Pflicht, geprüft gegen die Zertifikate aus den
     IdP-Metadaten. Die Antwort muss genau eine Assertion enthalten; mehrere Assertions (Schutz
     vor Signature Wrapping) und verschlüsselte Assertions (`EncryptedAssertion`) lehnt der ACS
     ab. Die SP-Metadaten bieten deshalb kein Verschlüsselungszertifikat an; die Verbindung zum
     ACS ist ohnehin per https geschützt.
   - Issuer = Entity-ID des IdP, `Destination` und `Recipient` = ACS-URL, `Audience` = Entity-ID des
     SP (eine `AudienceRestriction` ist Pflicht), `InResponseTo` = ID des eigenen AuthnRequests
     (IdP-initiierte Anmeldungen sind abgeschaltet), `NotBefore`/`NotOnOrAfter` und
     Ausstellungszeit (180 s Uhrentoleranz, höchstens 90 s alt).
   - Mindestens eine `SubjectConfirmation` und eine nicht leere NameID im Format **persistent**
     (siehe [NameID-Format](#nameid-format)).
   - Die Assertion-ID wird bis zum Ablauf der Assertion gespeichert (`SAMLAssertionUse`); eine
     zweite Verwendung wird abgelehnt (Replay-Schutz).
5. Das Konto wird nur über *(Statusseite, Protokoll SAML, Entity-ID des IdP, NameID samt Format)* gefunden,
   nie über die E-Mail-Adresse.
6. Wie bei OIDC folgt der Einmal-Code auf `<FRONTEND_URL>/auth/sso-callback`, diesmal per
   `303 See Other`, damit der Browser das Frontend per GET lädt.

Fehler leiten mit festem Code auf `<FRONTEND_URL>/login?oidc_error=<Code>` um (nach einer
Verknüpfung ins Profil, nach der Testanmeldung auf die SSO-Seite). Die Parameter heißen aus
historischen Gründen `oidc_*` und tragen die gemeinsamen Codes beider Protokolle. Audit-Einträge wie
bei OIDC mit `provider=saml page=<ID>`.

## Werte für den IdP

Die SSO-Seite im Admin-Bereich zeigt im Abschnitt „SAML 2.0“, was beim IdP einzutragen ist
(alle Werte hängen an `BASE_URL`):

| Wert | Beispiel |
|------|----------|
| SP Entity ID (= Metadaten-URL) | `https://status.example.com/api/auth/saml/3/metadata` |
| ACS URL (Reply URL), Binding HTTP-POST | `https://status.example.com/api/auth/saml/3/acs` |
| SP-Metadaten | `GET <BASE_URL>/api/auth/saml/<Seiten-ID>/metadata` (öffentlich, auch für private Seiten, damit der IdP sie laden kann; für gelöschte Seiten 404) |
| SP-Zertifikat | im Formular und in den Metadaten |

Schlüssel und selbstsigniertes Zertifikat (RSA 2048, 10 Jahre gültig) erzeugt das Backend beim
**ersten Speichern** der SSO-Konfiguration der Seite, also auch schon vor dem Einschalten von SAML;
vorher liefert der Metadaten-Endpunkt 404. Das Paar bleibt bei jedem weiteren Speichern erhalten.
Nach „SSO-Konfiguration löschen“ entsteht beim nächsten Speichern ein neues Paar; dann die
SP-Metadaten beim IdP neu einlesen. Der private Schlüssel liegt wie das OIDC-Client-Secret nur in
der Datenbank und erscheint in keiner API-Antwort.

Die SP-Metadaten bieten nur das HTTP-POST-Binding für den ACS an (kein Artifact-Binding), verlangen
signierte Assertions (`WantAssertionsSigned`) und kündigen signierte AuthnRequests an. Als
NameID-Format steht `persistent` (siehe unten).

## NameID-Format

Standardmäßig verlangt der AuthnRequest eine persistente NameID
(`urn:oasis:names:tc:SAML:2.0:nameid-format:persistent`), und der ACS nimmt nur diese an. Abgelehnt
werden mit `saml_nameid_format`:

- `emailAddress` – immer. Eine E-Mail-Adresse vergibt der IdP neu (Umbenennung, Nachfolger im
  Postfach); die neue Person würde sich dann mit dem verknüpften Konto anmelden.
- `transient` – wechselt bei jeder Anmeldung, eine Verknüpfung hielte genau einmal.
- jedes andere Format (`kerberos`, `X509SubjectName` …).
- `unspecified` (auch ein fehlendes Format-Attribut), solange der Schalter **„NameID-Format
  ‚unspecified‘ zulassen“** (`saml_allow_unspecified_nameid`, Standard aus) nicht gesetzt ist. Mit
  Schalter verlangt der AuthnRequest kein Format mehr, damit auch IdPs ohne persistente NameID
  antworten. Nur einschalten, wenn die Kennung beim IdP nie neu vergeben wird (die Oberfläche
  warnt dabei).

Das Format gehört zur Verknüpfung: Dieselbe NameID in einem anderen Format passt nicht zu einer
bestehenden Verknüpfung.

## Wechsel des IdP

Ändert sich beim Speichern die Quelle der IdP-Metadaten (anderer URL, Wechsel zwischen URL und
XML, Metadaten entfernt) oder bei eingefügtem XML die Entity-ID bzw. ein Signaturzertifikat des
IdP, löscht das Backend alle SAML-Verknüpfungen der Seite und vermerkt das im Audit-Log
(`sso.saml_identities_reset`, Details `count=<Anzahl>`). Die Nutzer verknüpfen danach im Profil neu.
Die SSO-Seite warnt vor dem Speichern, sobald sich Metadaten-URL oder -XML ändern. Ein
Schlüsselwechsel, der über einen unveränderten Metadaten-URL ankommt, löscht nichts.

## Felder der Konfiguration

| Feld in der Oberfläche | JSON-Feld | Bedeutung |
|------------------------|-----------|-----------|
| SAML-Anmeldung aktivieren | `saml_enabled` | Schaltet den SAML-Login ein. Verlangt IdP-Metadaten (URL oder XML), sonst 400. |
| IdP-Metadaten-URL | `saml_idp_metadata_url` | Nur `https://`. Abruf mit SSRF-Schutz wie bei der OIDC-Discovery (keine internen Adressen, auch nicht nach Redirects), höchstens 1 MB, Ergebnis 1 Stunde im Cache, ein Fehlschlag 1 Minute. Schlüsselwechsel beim IdP kommen so von selbst an. |
| IdP-Metadaten (XML) | `saml_idp_metadata_xml` | Alternativ das Metadaten-XML einfügen (höchstens 128 KB); wird beim Speichern geprüft (400 bei ungültigem Inhalt) und je Seite geparst zwischengespeichert. Nach einem Schlüsselwechsel neu einfügen. URL und XML zugleich ergeben 400. |
| Attribut für die E-Mail-Adresse | `saml_attribute_email` | Name **oder** FriendlyName des Attributs (leer = `email`). Nur zur Anzeige an der Verknüpfung. |
| Attribut für den Namen | `saml_attribute_name` | Anzeigename der Sitzung (leer = `name`). |
| NameID-Format „unspecified“ zulassen | `saml_allow_unspecified_nameid` | Standard aus, siehe [NameID-Format](#nameid-format). |
| SP Entity ID, SP ACS URL, SP-Zertifikat | `saml_entity_id`, `saml_acs_url`, `saml_metadata_url`, `saml_sp_certificate` | Nur lesend, siehe oben. |

Die Metadaten müssen eine Entity-ID, einen `SingleSignOnService` mit HTTP-Redirect-Binding und
mindestens ein Signaturzertifikat enthalten. Bei einem `EntitiesDescriptor` zählt der erste
Eintrag mit IdP-Rolle. `validUntil` der IdP-Metadaten wird nicht ausgewertet.

## Testanmeldung

Mit „Test-Login erlauben“ erscheint „SAML-Testanmeldung starten“
(`POST /api/admin/pages/<Seiten-ID>/sso/saml-test`). Sie nutzt die gespeicherte Konfiguration,
funktioniert schon vor dem Einschalten und durchläuft alle Prüfungen des ACS, ohne Sitzung und
ohne Verknüpfung. Die SSO-Seite meldet danach den Erfolg und nennt fehlende Attribute für E-Mail
oder Namen. Fehlen die Metadaten, antwortet der Start mit 400 (`saml_test_incomplete`).

## Konto-Verknüpfung

Wie bei OIDC (siehe [`sso-oidc.md`](./sso-oidc.md#konto-verknüpfung-wer-darf-das-seiten-sso-nutzen)):
Im Profil zeigt „SSO-Anmeldung“ die verknüpfbaren Seiten je Protokoll („OIDC“ bzw. „SAML“),
„Mit SSO verknüpfen“ ruft `POST /api/auth/saml/link?page=<Seiten-ID>` auf. Eine OIDC-Verknüpfung
derselben Seite zählt für SAML nicht und umgekehrt. Die Verknüpfung hängt an Entity-ID und NameID:
Wechselt der IdP eines von beiden, müssen die Konten neu verknüpft werden.

## Fehlerbilder

| Code | Ursache und Abhilfe |
|------|---------------------|
| `saml_response_invalid` | Signatur fehlt oder passt nicht zum Zertifikat aus den Metadaten, falsche Audience/Recipient/Destination, abgelaufen (Uhrzeit des Servers prüfen), fremdes `InResponseTo`, fehlende NameID oder SubjectConfirmation, Artifact-Binding statt POST. Details im Backend-Log (`SAML response rejected`). |
| `saml_replay` | Die Assertion-ID wurde schon einmal eingelöst. |
| `saml_nameid_format` | Die NameID ist nicht persistent (siehe [NameID-Format](#nameid-format)); beim IdP das Format `persistent` einstellen. |
| `metadata_invalid` | IdP-Metadaten unlesbar oder ohne Redirect-SSO bzw. Signaturzertifikat (Log `SAML metadata invalid`). |
| `provider_unreachable` | Metadaten-URL nicht per https erreichbar, interne Adresse oder HTTP-Fehler (Log `SAML metadata fetch failed`). |
| `provider_denied` | Der IdP meldet einen Status ungleich Success (z. B. Anmeldung abgebrochen). |
| `state_invalid` | Login älter als 10 Minuten, ACS doppelt aufgerufen, Cookie fehlt (anderer Browser oder ein Proxy, der `SameSite=None` entfernt) oder Seiten-ID im ACS-Pfad passt nicht. |
| `not_linked`, `already_linked`, `global_admin`, `other_page_role`, `not_configured`, `test_disabled`, `internal` | Wie bei OIDC. |

Rate-Limits: Login, ACS und Testanmeldung je 20 Aufrufe pro Minute, Metadaten 60 pro Minute.

## Beispiele für IdPs

### Microsoft Entra ID

*Unternehmensanwendung* → *Einmaliges Anmelden* → *SAML*: Bezeichner (Entity ID) und
Antwort-URL (ACS) wie oben. Als Metadaten-URL die *App-Verbundmetadaten-URL* übernehmen. Die
E-Mail kommt standardmäßig als `http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress`,
der Name als `http://schemas.microsoft.com/identity/claims/displayname`; diese Namen in die
Attributfelder eintragen. Entra liefert standardmäßig die E-Mail als NameID; unter *Attribute &
Ansprüche* → *Eindeutiger Benutzerbezeichner* das Format **Persistent** (z. B. `user.objectid`) wählen.

### Keycloak

Client vom Typ *SAML*, Client-ID = SP Entity ID, *Valid redirect URIs* bzw. *Assertion Consumer
Service POST Binding URL* = ACS-URL. *Name ID format* auf `persistent`
stellen. Metadaten: `https://<host>/realms/<realm>/protocol/saml/descriptor`.

### Authentik

*SAML-Provider* anlegen, ACS-URL wie oben, *Issuer* frei wählbar, *Service Provider Binding* Post.
SP-Metadaten lassen sich direkt importieren. Metadaten-URL des Providers übernehmen.

## Lokales Testen mit dem Mock

README, Abschnitt [„Lokale SSO-Tests mit Mock-SAML“](../../README.md#lokale-sso-tests-mit-mock-saml).
