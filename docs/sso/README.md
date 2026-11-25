# SSO-Einrichtung (Administratoren)

> English: [`en/README.md`](./en/README.md)

Jede Statusseite kann ein eigenes Single Sign-on (SSO) über OpenID Connect (OIDC) bekommen. SAML
wird nicht mehr unterstützt. Diese Guides erklären, wie du es einrichtest, ohne in den Code zu schauen.

## Stand

| Verfahren | Stand | Guides |
| --- | --- | --- |
| OIDC (je Statusseite) | verfügbar | [OIDC allgemein](oidc.md), [Google](google.md), [Keycloak](keycloak.md), [Okta](okta.md), [Azure AD](azure-ad.md) |
| SAML 2.0 | entfernt | – |

Über das Seiten-SSO entstehen keine neuen Konten, ein bestehendes Konto wird einmal
verknüpft. Global Admins und Konten mit Rolle auf einer anderen Seite sind ausgenommen, und die
Sitzung gilt nur für diese Seite.

## Inhalt

- [OIDC allgemein](oidc.md): Issuer, Client-ID/-Secret, Redirect-URI, Scopes, Testanmeldung,
  Verknüpfen eines Kontos.
- [Google](google.md): OIDC mit Google als Anbieter.
- [Keycloak](keycloak.md): OIDC mit Keycloak als Anbieter.
- [Okta](okta.md): OIDC mit Okta als Anbieter.
- [Azure AD](azure-ad.md): OIDC mit Microsoft Entra ID als Anbieter.
- [Lokale Tests mit Mock-Anbieter](local-testing.md): OIDC-Mock für die Entwicklung.
- [API](api.md): Endpunkte, Parameter und Fehlercodes.
- [Architektur](architecture.md): Ablauf von Login und Verknüpfung als Diagramme.

Alle Felder, Regeln und Fehlerbilder im Detail stehen im Admin-Handbuch:
[OIDC-Anmeldung je Statusseite](../user-guide/sso-oidc.md).

Zurück zum [Admin-Guide](../user-guide/admin.md).
