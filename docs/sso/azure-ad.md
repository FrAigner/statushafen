# Azure AD / Microsoft Entra ID als OIDC-Anbieter

Lies zuerst den [allgemeinen OIDC-Guide](oidc.md). Hier stehen nur die Entra-spezifischen Schritte.
Statushafen spricht mit Entra ID ausschließlich über OpenID Connect; SAML wird nicht mehr unterstützt.

## Schritte

1. In Entra ID unter „App-Registrierungen“ → „Neue Registrierung“ eine Anwendung anlegen.
   „Unterstützte Kontotypen“: **Nur Konten in diesem Organisationsverzeichnis** (ein Mandant).
2. Als Umleitungs-URI die Plattform **Web** wählen und die Redirect-URI der Statusseite eintragen,
   z. B.

   ```text
   https://status.example.org/api/auth/oidc/callback?page=<SEITEN-ID>
   ```

3. Unter „Zertifikate & Geheimnisse“ einen **geheimen Clientschlüssel** anlegen und den Wert
   kopieren (er wird nur einmal angezeigt). Die **Anwendungs-ID (Client)** steht auf der Übersicht.
4. Unter „Tokenkonfiguration“ den optionalen Anspruch **email** für das ID-Token hinzufügen, damit
   die Verknüpfung im Profil eine Adresse zeigt.
5. Der Issuer ist `https://login.microsoftonline.com/<TENANT-ID>/v2.0`. Verwende die Mandanten-ID,
   nicht `common` oder `organizations`: Nur dann passt der `issuer` im Discovery-Dokument.
6. In Statushafen auf der SSO-Seite eintragen (oder per [API](api.md)):

   ```json
   {
     "oidc_enabled": true,
     "oidc_discovery_url": "https://login.microsoftonline.com/TENANT-ID/v2.0",
     "oidc_client_id": "CLIENT-ID",
     "oidc_client_secret": "CLIENT-SECRET",
     "oidc_scopes": "openid email profile"
   }
   ```

7. Testanmeldung starten, OIDC einschalten und Konten verknüpfen, siehe
   [Konten und Verknüpfung](oidc.md#konten-und-verknüpfung).

## Als globaler Anbieter

Dieselbe Anwendung (oder eine zweite) kann als [globaler Anbieter](providers.md) für die Anmeldung an
der ganzen Instanz dienen. Redirect-URI beim Anbieter:
`https://status.example.org/api/auth/provider/entra/callback`.

```bash
OIDC_PROVIDERS=entra
OIDC_ENTRA_ISSUER=https://login.microsoftonline.com/TENANT-ID/v2.0
OIDC_ENTRA_CLIENT_ID=CLIENT-ID
OIDC_ENTRA_CLIENT_SECRET=CLIENT-SECRET
OIDC_ENTRA_NAME=Microsoft
```

Entra ID liefert **kein** `email_verified`. Bestehende Konten werden deshalb nie über die E-Mail
verknüpft; sie verknüpfen den Anbieter im Profil (dafür übergangsweise
`PASSWORD_LOGIN_ENABLED=true`). Neue Konten entstehen wie gewohnt. Verwende immer den Issuer mit
eurer Mandanten-ID, nie `common`: Bei mandantenübergreifenden Anwendungen kann der Claim `email`
von Fremden gesetzt werden.

Zurück zur [Übersicht](README.md).
