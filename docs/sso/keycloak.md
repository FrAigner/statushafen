# Keycloak als OIDC-Anbieter

Lies zuerst den [allgemeinen OIDC-Guide](oidc.md). Hier stehen nur die Keycloak-spezifischen Schritte.

## Schritte

1. Im gewünschten Realm unter „Clients“ einen Client anlegen:
   - Client-Typ: **OpenID Connect**, Client-ID z. B. `statushafen`.
   - „Client authentication“: **An** (confidential), Flow: „Standard flow“.
2. **Valid redirect URIs**:

   ```text
   https://status.example.org/api/auth/oidc/callback?page=<SEITEN-ID>
   ```

3. Unter „Credentials“ das **Client secret** kopieren.
4. Den Issuer bestimmen: `https://keycloak.example.org/realms/<REALM>`. (Bei älteren Keycloak-
   Versionen mit Pfadpräfix `/auth` entsprechend `https://keycloak.example.org/auth/realms/<REALM>`.)
   Er muss über `https` erreichbar sein und zum `issuer` in der Discovery passen.
5. In Statushafen auf der SSO-Seite eintragen (oder per [API](api.md)):

   ```json
   {
     "oidc_enabled": true,
     "oidc_discovery_url": "https://keycloak.example.org/realms/REALM",
     "oidc_client_id": "statushafen",
     "oidc_client_secret": "CLIENT-SECRET",
     "oidc_scopes": "openid email profile",
     "oidc_attribute_name": "name"
   }
   ```

6. Konto verknüpfen und testen, siehe [Konten und Verknüpfung](oidc.md#konten-und-verknüpfung).
