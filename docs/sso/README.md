# SSO-Einrichtung (Administratoren)

> English: [`en/README.md`](./en/README.md)

Statushafen meldet Nutzer über OpenID Connect (OIDC) an. Es gibt zwei Ebenen:

- **Globale Anbieter** (z. B. GitLab, Google, Keycloak) richtet der Betreiber per Umgebungsvariablen
  für die ganze Instanz ein. Sie sind die normale Anmeldung; die Login-Seite zeigt je Anbieter einen
  Knopf. Die Anmeldung mit Passwort ist standardmäßig aus (`PASSWORD_LOGIN_ENABLED`).
- **Seiten-SSO**: Jede Statusseite kann zusätzlich einen eigenen OIDC-Anbieter bekommen, den der
  Seiten-Admin einträgt. Die Sitzung daraus gilt nur für diese Seite.

Diese Guides erklären die Einrichtung, ohne in den Code zu schauen.

## Stand

| Verfahren | Stand | Guides |
| --- | --- | --- |
| Globale OIDC-Anbieter | verfügbar | [Globale Anbieter](providers.md), [GitLab](gitlab.md), [Google](google.md), [Keycloak](keycloak.md), [Okta](okta.md), [Azure AD](azure-ad.md) |
| OIDC (je Statusseite) | verfügbar | [OIDC allgemein](oidc.md) und dieselben Anbieter-Guides |
| Google-/GitHub-Login (eingebaut) | entfernt | Google über [OIDC](google.md) |
| SAML 2.0 | entfernt | – |

Über das Seiten-SSO entstehen keine neuen Konten, ein bestehendes Konto wird einmal verknüpft. Global
Admins und Konten mit Rolle auf einer anderen Seite sind dort ausgenommen. Über einen globalen
Anbieter entstehen neue Konten (Rolle „Viewer“), bestehende werden über eine bestätigte E-Mail
verknüpft.

## Upgrade-Hinweis

Google-/GitHub-Login und SAML entfallen, der Passwort-Login ist standardmäßig aus. Richte vor dem
Update mindestens einen globalen Anbieter ein oder setze `PASSWORD_LOGIN_ENABLED=true`. Die Schritte
stehen unter [Upgrade von älteren Versionen](providers.md#upgrade-von-älteren-versionen).

## Inhalt

- [Globale OIDC-Anbieter](providers.md): Umgebungsvariablen, Redirect-URI, Kontoregeln,
  Passwort-Login, Rücksprung, Upgrade.
- [OIDC allgemein](oidc.md): Seiten-SSO mit Issuer, Client-ID/-Secret, Redirect-URI, Scopes,
  Testanmeldung, Verknüpfen eines Kontos.
- [GitLab](gitlab.md): gitlab.com und selbst gehostetes GitLab.
- [Google](google.md): OIDC mit Google als Anbieter.
- [Keycloak](keycloak.md): OIDC mit Keycloak als Anbieter.
- [Okta](okta.md): OIDC mit Okta als Anbieter.
- [Azure AD](azure-ad.md): OIDC mit Microsoft Entra ID als Anbieter.
- [Lokale Tests mit Mock-Anbieter](local-testing.md): OIDC-Mock für die Entwicklung.
- [API](api.md): Endpunkte, Parameter und Fehlercodes.
- [Architektur](architecture.md): Abläufe als Diagramme und Bedrohungsmodell.

Alle Felder, Regeln und Fehlerbilder des Seiten-SSO im Detail stehen im Admin-Handbuch:
[OIDC-Anmeldung je Statusseite](../user-guide/sso-oidc.md).

Zurück zum [Admin-Guide](../user-guide/admin.md).
