# Google als OIDC-Anbieter

Lies zuerst den [allgemeinen OIDC-Guide](oidc.md). Hier stehen nur die Google-spezifischen Schritte.

> Das ist das **Seiten-SSO** einer Statusseite. Der globale „Mit Google anmelden“-Login des Servers
> (`GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`) ist eine andere Funktion.

## Schritte

1. In der Google Cloud Console ein Projekt wählen oder anlegen und den OAuth-Zustimmungsbildschirm
   einrichten (Nutzertyp „Intern“, wenn nur eure Organisation sich anmelden soll).
2. Unter „APIs & Dienste → Anmeldedaten“ eine **OAuth-Client-ID** vom Typ „Webanwendung“ anlegen.
3. Als **autorisierte Weiterleitungs-URI** eintragen:

   ```text
   https://status.example.org/api/auth/oidc/callback?page=<SEITEN-ID>
   ```

4. Client-ID und Client-Secret kopieren.
5. In Statushafen auf der SSO-Seite eintragen (oder per [API](api.md)):

   ```json
   {
     "oidc_enabled": true,
     "oidc_discovery_url": "https://accounts.google.com",
     "oidc_client_id": "CLIENT-ID",
     "oidc_client_secret": "CLIENT-SECRET",
     "oidc_scopes": "openid email profile",
     "oidc_attribute_name": "name"
   }
   ```

6. Konto verknüpfen und testen, siehe [Konten und Verknüpfung](oidc.md#konten-und-verknüpfung).
