# Google als OIDC-Anbieter

Google wird wie jeder andere Anbieter über OpenID Connect angebunden: als
[globaler Anbieter](providers.md) für die Anmeldung an der ganzen Instanz oder als
[Seiten-SSO](oidc.md) einer Statusseite. Der frühere eingebaute „Mit Google anmelden“-Login
(`GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `/api/auth/google`) entfällt; zum Umstieg siehe
[Upgrade](providers.md#upgrade-von-älteren-versionen).

## Client bei Google anlegen

1. In der Google Cloud Console ein Projekt wählen oder anlegen und den OAuth-Zustimmungsbildschirm
   einrichten (Nutzertyp „Intern“, wenn nur eure Google-Workspace-Organisation sich anmelden soll).
2. Unter „APIs & Dienste → Anmeldedaten“ eine **OAuth-Client-ID** vom Typ „Webanwendung“ anlegen.
3. Als **autorisierte Weiterleitungs-URI** eintragen:
   - globaler Anbieter: `https://status.example.org/api/auth/provider/google/callback`
   - Seiten-SSO: `https://status.example.org/api/auth/oidc/callback?page=<SEITEN-ID>`
4. Client-ID und Client-Secret kopieren.

Der Issuer ist immer `https://accounts.google.com`. Google liefert `email_verified`; bestehende Konten
mit derselben Adresse werden beim ersten Login verknüpft.

## Als globaler Anbieter

```bash
OIDC_PROVIDERS=google
OIDC_GOOGLE_ISSUER=https://accounts.google.com
OIDC_GOOGLE_CLIENT_ID=CLIENT-ID
OIDC_GOOGLE_CLIENT_SECRET=CLIENT-SECRET
OIDC_GOOGLE_NAME=Google
```

Mit Nutzertyp „Extern“ kann sich jedes Google-Konto anmelden. Begrenze neue Konten dann mit
`SSO_ALLOWED_DOMAINS` auf eure Domains.

## Als Seiten-SSO

In Statushafen auf der SSO-Seite der Statusseite eintragen (oder per [API](api.md)):

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

Danach Konto verknüpfen und testen, siehe [Konten und Verknüpfung](oidc.md#konten-und-verknüpfung).

Zurück zur [Übersicht](README.md).
