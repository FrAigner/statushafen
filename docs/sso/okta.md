# Okta als OIDC-Anbieter

Lies zuerst den [allgemeinen OIDC-Guide](oidc.md). Hier stehen nur die Okta-spezifischen Schritte.
Statushafen spricht mit Okta ausschließlich über OpenID Connect; SAML wird nicht mehr unterstützt.

## Schritte

1. In der Okta-Admin-Konsole unter „Applications“ → „Create App Integration“ wählen:
   - Sign-in method: **OIDC – OpenID Connect**
   - Application type: **Web Application**
2. „Grant type“: **Authorization Code** (PKCE nutzt Statushafen immer zusätzlich).
3. „Sign-in redirect URIs“: die Redirect-URI der Statusseite, z. B.

   ```text
   https://status.example.org/api/auth/oidc/callback?page=<SEITEN-ID>
   ```

4. Unter „Assignments“ die Nutzer oder Gruppen zuweisen, die sich anmelden dürfen.
5. **Client ID** und **Client secret** aus dem Reiter „General“ kopieren.
6. Den Issuer bestimmen. Mit dem Standard-Autorisierungsserver ist das
   `https://<OKTA-DOMAIN>/oauth2/default`, ohne eigenen Autorisierungsserver `https://<OKTA-DOMAIN>`.
   Er muss zum `issuer` in `/.well-known/openid-configuration` passen.
7. In Statushafen auf der SSO-Seite eintragen (oder per [API](api.md)):

   ```json
   {
     "oidc_enabled": true,
     "oidc_discovery_url": "https://okta.example.org/oauth2/default",
     "oidc_client_id": "CLIENT-ID",
     "oidc_client_secret": "CLIENT-SECRET",
     "oidc_scopes": "openid email profile"
   }
   ```

8. Testanmeldung starten, OIDC einschalten und Konten verknüpfen, siehe
   [Konten und Verknüpfung](oidc.md#konten-und-verknüpfung).

Zurück zur [Übersicht](README.md).
