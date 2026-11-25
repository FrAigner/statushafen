# SSO (OIDC)

Statushafen meldet über OpenID Connect (OIDC) an, auf zwei Wegen:

- **Globale Anmeldeanbieter** (z. B. GitLab oder der Anmeldedienst einer Firma) richtet der
  Betreiber auf dem Server ein. Die Login-Seite zeigt je aktivem Anmeldeanbieter einen Knopf
  „Mit <Name> anmelden“.
- **SSO einer Statusseite:** Jede Statusseite kann einen eigenen Anmeldedienst (Identity Provider,
  IdP) bekommen. Eingerichtet wird das unter **SSO Konfiguration** (Rechte `sso:read`,
  `sso:update`, `sso:delete` auf der Seite oder Global Admin).

Ob zusätzlich die Anmeldung mit E-Mail und Passwort angeboten wird, legt ebenfalls der Betreiber
fest. Ist sie abgeschaltet, fehlt das Formular auf der Login-Seite. Ist gar keine Anmeldemethode
eingerichtet, weist die Login-Seite darauf hin.

## So funktioniert die Anmeldung

1. Auf der Statusseite „Anmelden“ wählen. Die Login-Seite zeigt die Knöpfe der Anmeldeanbieter und
   „Mit SSO anmelden“, wenn SSO für die Seite aktiv ist.
2. Der Browser geht zum IdP und kommt nach der Anmeldung zurück auf die Seite, von der die
   Anmeldung ausging.
3. Eine Sitzung über das SSO einer Statusseite gilt nur für diese Statusseite.

**Wichtig:** Über das SSO einer Statusseite entstehen keine neuen Konten. Man meldet sich einmal
anders an und klickt im **Profil** unter „SSO-Anmeldung“ auf „Mit SSO verknüpfen“. Erkannt wird das
Konto danach an der Kennung des IdP, nie an der E-Mail. Ein bestehendes Konto lässt sich dort auch
mit einem globalen Anmeldeanbieter verknüpfen („Mit <Name> verknüpfen“).

Global Admins und Konten mit einer Rolle auf einer anderen Statusseite können sich nicht über
das SSO einer Statusseite anmelden.

## OIDC für eine Statusseite einrichten

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

## Testen

„Testanmeldung starten“ prüft die **gespeicherte** Konfiguration, auch bevor SSO aktiviert ist.
Der Test legt weder eine Sitzung noch eine Verknüpfung an.

## Häufige Fehler

| Code | Bedeutung |
|---|---|
| `not_linked` | Konto ist noch nicht verknüpft, siehe oben |
| `already_linked` | Diese IdP-Identität ist schon mit einem anderen Konto verknüpft |
| `global_admin`, `other_page_role` | Konto ist vom SSO der Statusseite ausgeschlossen |
| `state_invalid` | Anmeldung dauerte länger als 10 Minuten oder Cookie fehlt |
| `provider_unreachable` | IdP nicht erreichbar oder Issuer weicht ab, oft ein Schrägstrich am Ende der Discovery URL |
| `provider_unreachable` (Log: Endpunkt muss https sein) | Das Discovery-Dokument nennt für Anmeldung oder Token-Abruf eine Adresse ohne https |
| `token_exchange` | Client ID/Secret falsch oder Redirect-URI nicht exakt eingetragen |
| `provider_unknown` | Der globale Anmeldeanbieter ist unbekannt oder nicht mehr aktiv |
| `password_login_disabled` | Die Anmeldung mit E-Mail und Passwort ist abgeschaltet |

Wird der Issuer geändert, werden bestehende Verknüpfungen der Statusseite gelöscht und müssen neu
angelegt werden.

Ausführliche Anleitungen für Betreiber mit Beispielen für Authentik, Keycloak, Entra ID, Google
und Okta liegen im Repository unter `docs/user-guide/sso-oidc.md` und `docs/sso/`.
