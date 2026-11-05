# SSO-Einrichtung (Administratoren)

> English: [`en/README.md`](./en/README.md)

Jede Statusseite kann ein eigenes Single Sign-on (SSO) bekommen, mit OIDC, mit SAML 2.0 oder mit
beiden parallel. Diese Guides erklären, wie du es einrichtest, ohne in den Code zu schauen.

## Stand

| Verfahren | Stand | Guides |
| --- | --- | --- |
| OIDC (je Statusseite) | verfügbar | [OIDC allgemein](oidc.md), [Google](google.md), [Keycloak](keycloak.md) |
| SAML 2.0 (je Statusseite) | verfügbar | [SAML allgemein](saml.md), [Okta](okta.md), [Azure AD](azure-ad.md) |

Für beide gilt: Über das Seiten-SSO entstehen keine neuen Konten, ein bestehendes Konto wird einmal
verknüpft. Global Admins und Konten mit Rolle auf einer anderen Seite sind ausgenommen, und die
Sitzung gilt nur für diese Seite.

## Inhalt

- [OIDC allgemein](oidc.md): Issuer, Client-ID/-Secret, Redirect-URI, Scopes, Testanmeldung,
  Verknüpfen eines Kontos.
- [Google](google.md): OIDC mit Google als Anbieter.
- [Keycloak](keycloak.md): OIDC mit Keycloak als Anbieter.
- [SAML allgemein](saml.md): SP-Werte, IdP-Metadaten, NameID-Format, Verknüpfen eines Kontos.
- [Okta](okta.md): SAML mit Okta als Anbieter.
- [Azure AD](azure-ad.md): SAML mit Microsoft Entra ID als Anbieter.
- [Lokale Tests mit Mock-Anbietern](local-testing.md): OIDC- und SAML-Mock für die Entwicklung.
- [API](api.md): Endpunkte, Parameter und Fehlercodes.
- [Architektur](architecture.md): Ablauf von Login und Verknüpfung als Diagramme.

Alle Felder, Regeln und Fehlerbilder im Detail stehen im Admin-Handbuch:
[OIDC-Anmeldung je Statusseite](../user-guide/sso-oidc.md),
[SAML-Anmeldung je Statusseite](../user-guide/sso-saml.md).

Zurück zum [Admin-Guide](../user-guide/admin.md).
