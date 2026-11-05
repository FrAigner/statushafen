# Keycloak as OIDC provider

> English version of the German guide [`../keycloak.md`](../keycloak.md).

Read the [general OIDC guide](oidc.md) first. This page contains only the Keycloak-specific steps.

## Steps

1. In the desired realm, create a client under "Clients":
   - Client type: **OpenID Connect**, client ID for example `statushafen`.
   - "Client authentication": **On** (confidential), flow: "Standard flow".
2. **Valid redirect URIs**:

   ```text
   https://status.example.org/api/auth/oidc/callback?page=<PAGE-ID>
   ```

3. Under "Credentials", copy the **client secret**.
4. Determine the issuer: `https://keycloak.example.org/realms/<REALM>`. (For older Keycloak
   versions with the path prefix `/auth` accordingly `https://keycloak.example.org/auth/realms/<REALM>`.)
   It must be reachable over `https` and match the `issuer` in the discovery document.
5. Enter it in Statushafen on the SSO page (or via the [API](api.md)):

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

6. Link an account and test, see [Accounts and linking](oidc.md#accounts-and-linking).
