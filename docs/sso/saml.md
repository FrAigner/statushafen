# SAML einrichten (allgemein)

Mit SAML 2.0 melden sich Nutzer über den Identity Provider (IdP) deiner Organisation an, etwa
Microsoft Entra ID, Okta, Keycloak, Authentik oder ADFS. Die Konfiguration gilt je
Statusseite und lässt sich parallel zu [OIDC](oidc.md) einschalten. Der Ablauf steht in der
[Architektur](architecture.md), die Endpunkte in der [API](api.md), alle Prüfungen und Fehlerbilder
im [Admin-Handbuch](../user-guide/sso-saml.md).

## Voraussetzungen

- Der IdP bietet einen `SingleSignOnService` mit **HTTP-Redirect-Binding** und schickt die Antwort
  per **HTTP-POST** an den ACS.
- Der IdP signiert Response oder Assertion. Verschlüsselte Assertions werden nicht unterstützt.
- Der IdP liefert eine NameID im Format **persistent** (siehe [NameID-Format](#nameid-format)).
- `BASE_URL` zeigt auf die öffentliche Adresse des Servers; alle SP-Werte hängen daran.
- Du hast das Recht `sso:update` auf dieser Statusseite (oder bist Global Admin).

## Schritte

1. **SSO-Konfiguration einmal speichern.** Admin-Bereich → Gruppe „System“ → „SSO“. Beim ersten
   Speichern erzeugt Statushafen das SP-Schlüsselpaar der Seite; erst dann gibt es SP-Metadaten.
2. **SP-Werte beim IdP eintragen.** Die SSO-Seite zeigt sie im Abschnitt „SAML 2.0“ an:

   | Wert | Beispiel |
   | --- | --- |
   | SP Entity ID (= Metadaten-URL) | `https://status.example.org/api/auth/saml/<SEITEN-ID>/metadata` |
   | ACS-URL (Reply URL), Binding HTTP-POST | `https://status.example.org/api/auth/saml/<SEITEN-ID>/acs` |
   | SP-Zertifikat | im Formular und in den SP-Metadaten |

   Kann der IdP SP-Metadaten importieren, genügt die Metadaten-URL.
3. **IdP-Metadaten in Statushafen hinterlegen:** entweder die Metadaten-URL des IdP (nur `https://`,
   wird stündlich neu geladen) oder das Metadaten-XML einfügen (höchstens 128 KB, nach einem
   Schlüsselwechsel beim IdP neu einfügen). Beides zugleich geht nicht.
4. **Attribute zuordnen:** Name oder FriendlyName der Attribute für E-Mail (leer = `email`, nur zur
   Anzeige an der Verknüpfung) und Anzeigename (leer = `name`).
5. **Testen:** Mit „Test-Login erlauben“ startet „SAML-Testanmeldung starten“ einen Probelauf mit der
   gespeicherten Konfiguration, auch bevor SAML eingeschaltet ist. Die SSO-Seite meldet danach den
   Erfolg und nennt fehlende Attribute.
6. **SAML einschalten** und speichern. Auf der Statusseite erscheint „Mit SSO anmelden (SAML)“.

## NameID-Format

Das Konto hängt an *(Statusseite, Entity-ID des IdP, NameID samt Format)*, nie an der E-Mail-Adresse.
Deshalb verlangt Statushafen eine dauerhafte NameID im Format
`urn:oasis:names:tc:SAML:2.0:nameid-format:persistent`. Eine E-Mail-Adresse als NameID kann sich
ändern oder neu vergeben werden. Liefert der IdP nur „unspecified“, lässt sich das in der
Konfiguration ausdrücklich zulassen; die NameID muss dann trotzdem stabil sein.

## Konten und Verknüpfung

Wie bei [OIDC](oidc.md#konten-und-verknüpfung): Neue Konten entstehen nicht. Der angemeldete Nutzer
verknüpft sein Konto im Profil („Mit SSO verknüpfen“, Protokoll „SAML“). Eine OIDC-Verknüpfung
derselben Seite zählt für SAML nicht und umgekehrt. Wechselt der IdP seine Entity-ID bzw. ändern sich
die hinterlegten Metadaten, löscht das Speichern die SAML-Verknüpfungen der Seite.

Nach „SSO-Konfiguration löschen“ entsteht beim nächsten Speichern ein neues SP-Schlüsselpaar; dann
die SP-Metadaten beim IdP neu einlesen.

## Fehlersuche

Fehler landen als Code in der URL der Login-Seite (`?oidc_error=<Code>`; der Name ist historisch und
gilt für beide Protokolle). Die häufigsten:

| Code | Ursache |
| --- | --- |
| `saml_response_invalid` | Signatur fehlt oder passt nicht, falsche Audience/Recipient/Destination, abgelaufen (Uhrzeit des Servers prüfen) oder IdP-initiierte Anmeldung. |
| `saml_nameid_format` | NameID nicht persistent, siehe [NameID-Format](#nameid-format). |
| `metadata_invalid` | IdP-Metadaten unlesbar oder ohne Redirect-SSO bzw. Signaturzertifikat. |
| `provider_unreachable` | Metadaten-URL nicht per https erreichbar oder interne Adresse. |
| `state_invalid` | Login älter als 10 Minuten oder das Cookie fehlt (anderer Browser, Proxy entfernt `SameSite=None`). |

Alle Codes: [Fehlerbilder im Admin-Handbuch](../user-guide/sso-saml.md#fehlerbilder).

## Anbieter

- [Okta](okta.md)
- [Azure AD](azure-ad.md)

Zurück zur [Übersicht](README.md).
