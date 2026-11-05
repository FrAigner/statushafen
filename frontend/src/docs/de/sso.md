# SSO (OIDC und SAML)

Jede Statusseite kann einen eigenen Anmeldedienst (Identity Provider, IdP) bekommen: per OpenID
Connect (OIDC), per SAML 2.0 oder beides parallel. Eingerichtet wird das unter
**SSO Konfiguration** (Rechte `sso:read`, `sso:update`, `sso:delete` auf der Seite oder Global Admin).

## So funktioniert die Anmeldung

1. Auf der Statusseite „Login“ wählen. Die Login-Seite zeigt „Mit SSO anmelden“ bzw.
   „Mit SSO anmelden (SAML)“, wenn SSO für die Seite aktiv ist.
2. Der Browser geht zum IdP und kommt nach der Anmeldung zurück.
3. Die Sitzung gilt nur für diese Statusseite.

**Wichtig:** Über SSO entstehen keine neuen Konten. Man meldet sich einmal wie gewohnt an
(Passwort, Google oder GitHub) und klickt im **Profil** unter „SSO-Anmeldung“ auf
„Mit SSO verknüpfen“. Erkannt wird das Konto danach an der Kennung des IdP, nie an der E-Mail.

Global Admins und Konten mit einer Rolle auf einer anderen Statusseite können sich nicht über
das Seiten-SSO anmelden.

## OIDC einrichten

| Feld | Inhalt |
|---|---|
| OIDC-Anmeldung aktivieren | schaltet den SSO-Knopf frei |
| Discovery URL | Adresse des IdP, z. B. `https://idp.example/realms/acme` |
| Client ID / Client Secret | Zugangsdaten der App beim IdP; das Secret wird nie wieder angezeigt |
| Scopes | Standard `openid email profile` |
| Claim für den Namen / die E-Mail-Adresse | Standard `name` bzw. `email` |
| ACS URL (nur lesend) | als Redirect-URI beim IdP eintragen |

Die Redirect-URI hat die Form `<Adresse>/api/auth/oidc/callback?page=<Seiten-ID>`. Das
`?page=` gehört dazu, viele IdPs vergleichen zeichengenau.

## SAML einrichten

| Feld | Inhalt |
|---|---|
| SAML-Anmeldung aktivieren | schaltet den SAML-Knopf frei |
| IdP-Metadaten-URL **oder** IdP-Metadaten (XML) | eins von beiden; URL nur `https` |
| Attribut für E-Mail-Adresse / Namen | Attributnamen aus der SAML-Antwort |
| NameID-Format „unspecified“ zulassen | Standard aus |
| SP Entity ID, SP ACS URL, SP-Zertifikat (nur lesend) | beim IdP eintragen |

Die Metadaten der Statusseite liegen unter `<Adresse>/api/auth/saml/<Seiten-ID>/metadata`, die
ACS unter `<Adresse>/api/auth/saml/<Seiten-ID>/acs` (HTTP-POST). Schlüssel und Zertifikat
entstehen beim ersten Speichern. Als NameID wird `persistent` erwartet.

## Testen

„Testanmeldung starten“ prüft die **gespeicherte** Konfiguration, auch bevor SSO aktiviert ist.
Der Test legt weder eine Sitzung noch eine Verknüpfung an.

## Häufige Fehler

| Code | Bedeutung |
|---|---|
| `not_linked` | Konto ist noch nicht verknüpft, siehe oben |
| `already_linked` | Diese IdP-Identität ist schon mit einem anderen Konto verknüpft |
| `global_admin`, `other_page_role` | Konto ist vom Seiten-SSO ausgeschlossen |
| `state_invalid` | Anmeldung dauerte länger als 10 Minuten oder Cookie fehlt |
| `provider_unreachable` | IdP nicht erreichbar oder Issuer weicht ab, oft ein Schrägstrich am Ende der Discovery URL |
| `provider_unreachable` (Log: Endpunkt muss https sein) | Das Discovery-Dokument nennt für Anmeldung oder Token-Abruf eine Adresse ohne https |
| `token_exchange` | OIDC: Client ID/Secret falsch oder Redirect-URI nicht exakt eingetragen |
| `saml_nameid_format` | SAML: NameID-Format wird nicht akzeptiert |

Wird der Issuer oder die IdP-Metadaten geändert, werden bestehende Verknüpfungen gelöscht und
müssen neu angelegt werden.

Ausführliche Anleitungen für Betreiber mit Beispielen für Authentik, Keycloak, Entra ID, Google
und Okta liegen im Repository unter `docs/user-guide/sso-oidc.md`, `docs/user-guide/sso-saml.md`
und `docs/sso/`.
