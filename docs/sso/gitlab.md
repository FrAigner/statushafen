# GitLab als OIDC-Anbieter

Lies zuerst [Globale OIDC-Anbieter](providers.md). Hier stehen nur die GitLab-spezifischen Schritte,
für gitlab.com und für eine selbst gehostete GitLab-Instanz. GitLab liefert `email_verified` im
ID-Token; bestehende Konten mit derselben, in GitLab bestätigten E-Mail werden beim ersten Login
verknüpft.

## gitlab.com

1. In GitLab unter „Edit profile → Applications“ (eigene Anwendung) bzw. in einer Gruppe unter
   „Settings → Applications“ (Gruppenanwendung) **Add new application** wählen.
2. Name z. B. `Statushafen`, **Redirect URI**:

   ```text
   https://status.example.org/api/auth/provider/gitlab/callback
   ```

3. **Confidential** anhaken, Scopes **openid**, **email** und **profile** wählen, speichern.
4. **Application ID** und **Secret** kopieren.
5. In der Umgebung des Backends setzen:

   ```bash
   OIDC_PROVIDERS=gitlab
   OIDC_GITLAB_ISSUER=https://gitlab.com
   OIDC_GITLAB_CLIENT_ID=APPLICATION-ID
   OIDC_GITLAB_CLIENT_SECRET=SECRET
   OIDC_GITLAB_NAME=GitLab
   ```

6. Backend neu starten. Die Login-Seite zeigt „GitLab“; ohne Warnung im Start-Log ist der Anbieter
   aktiv.

Mit gitlab.com kann sich jedes GitLab-Konto anmelden. Schränke neue Konten deshalb mit
`SSO_ALLOWED_DOMAINS` auf eure E-Mail-Domains ein.

## Selbst gehostetes GitLab

Die Schritte sind dieselben, mit der Adresse eurer Instanz:

- Anwendung unter „Admin Area → Applications“ (instanzweit) oder wie oben anlegen; **Trusted**
  erspart den Nutzern den Zustimmungsdialog.
- Issuer ist die Basisadresse der Instanz, z. B. `https://gitlab.example.org` (bei einer Installation
  unter einem Pfad `https://example.org/gitlab`). Prüfen:
  `https://gitlab.example.org/.well-known/openid-configuration` muss erreichbar sein, und das Feld
  `issuer` darin muss genau `OIDC_GITLAB_ISSUER` entsprechen.
- Die Instanz darf im internen Netz liegen; sie muss vom Backend aus über `https` erreichbar sein, mit
  einem Zertifikat, dem das Backend vertraut (im Container: CA in den Systemspeicher aufnehmen).

```bash
OIDC_PROVIDERS=gitlab
OIDC_GITLAB_ISSUER=https://gitlab.example.org
OIDC_GITLAB_CLIENT_ID=APPLICATION-ID
OIDC_GITLAB_CLIENT_SECRET=SECRET
OIDC_GITLAB_NAME=GitLab
```

Mehrere Instanzen gehen mit eigenen IDs, z. B. `OIDC_PROVIDERS=gitlab,gitlab-intern`.

## Als Seiten-SSO

GitLab lässt sich auch als [Seiten-SSO](oidc.md) einer einzelnen Statusseite eintragen
(Discovery-URL = Issuer, Redirect-URI `…/api/auth/oidc/callback?page=<SEITEN-ID>`).

Zurück zur [Übersicht](README.md).
