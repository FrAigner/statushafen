# OIDC einrichten (allgemein)

Mit OIDC melden sich Nutzer über den Identity Provider (IdP) deiner Organisation an. Die
Konfiguration gilt je Statusseite. Der Ablauf steht in der [Architektur](architecture.md), die
Endpunkte in der [API](api.md), alle Felder und Fehlerbilder im
[Admin-Handbuch](../user-guide/sso-oidc.md).

## Voraussetzungen

- Der IdP unterstützt OpenID Connect mit Discovery (`/.well-known/openid-configuration`) und den
  Authorization-Code-Flow. PKCE (S256) wird immer verwendet.
- Der **Issuer** beginnt mit `https://`. Mit `http://` lehnt Statushafen ihn ab.
- Der IdP ist aus dem Netz des Servers erreichbar. Interne Adressen (z. B. private IP-Bereiche) sind
  gesperrt (SSRF-Schutz wie bei Webhooks).
- Du hast das Recht `sso:update` auf dieser Statusseite (oder bist Global Admin).

## Schritte

1. **Client beim IdP anlegen** (Typ „Web“/„confidential“, Authorization-Code-Flow). Trage als
   Redirect-URI ein:

   ```text
   https://status.example.org/api/auth/oidc/callback?page=<SEITEN-ID>
   ```

   `https://status.example.org` ist die `BASE_URL` des Servers (Platzhalter), `<SEITEN-ID>` die ID
   der Statusseite. Die Adresse muss beim IdP exakt so hinterlegt sein, inklusive `?page=`. Die
   SSO-Seite im Admin-Bereich zeigt sie dir an (Feld `oidc_acs_url`).

2. **Client-ID und Client-Secret** vom IdP notieren.

3. **In Statushafen eintragen:** Admin-Bereich → Gruppe „System“ → „SSO“, Abschnitt OIDC. Per API
   geht dasselbe mit `PUT /api/admin/pages/<SEITEN-ID>/sso` (siehe [API](api.md)):

   ```json
   {
     "oidc_enabled": true,
     "oidc_discovery_url": "https://idp.example.org/realms/demo",
     "oidc_client_id": "CLIENT-ID",
     "oidc_client_secret": "CLIENT-SECRET",
     "oidc_scopes": "openid email profile",
     "oidc_attribute_name": "name",
     "test_login_enabled": true
   }
   ```

   `oidc_discovery_url` darf der Issuer selbst oder die volle `.well-known/openid-configuration`-Adresse
   sein. **Scopes:** Standard ist `openid email profile`; `openid` ergänzt Statushafen selbst. Mit
   `oidc_enabled: true` sind Discovery-URL, Client-ID und ein Client-Secret Pflicht. Das Secret wird
   nie wieder ausgegeben; ein leeres Feld beim nächsten Speichern behält das gespeicherte.

4. **Testen:** Mit „Test-Login erlauben“ startet „Testanmeldung starten“ einen Probelauf mit der
   gespeicherten Konfiguration, auch bevor OIDC eingeschaltet ist. Die SSO-Seite meldet danach den
   Erfolg oder den Fehlercode. Im Live-Betrieb den Test-Login wieder abschalten.

5. **Anmelden:** Auf der Statusseite „Anmelden“ → „Mit SSO anmelden“. Der Knopf erscheint, sobald
   OIDC aktiv ist; dahinter steht `GET /api/auth/oidc/login?page=<SEITEN-ID>`.

## Konten und Verknüpfung

Das Konto wird nur über das Tripel (Statusseite, Issuer, Subject) gefunden, **nie über die
E-Mail-Adresse**. Neue Konten entstehen beim OIDC-Login nicht. Ein bestehendes Konto muss einmal mit
der Identität verknüpft werden:

1. Der Nutzer meldet sich normal an (Passwort, Google, GitHub).
2. Im Profil zeigt „SSO-Anmeldung“ die Seiten, die er verknüpfen darf. „Mit SSO verknüpfen“ ruft
   `POST /api/auth/oidc/link?page=<SEITEN-ID>` auf und leitet zum IdP weiter.
3. Nach der Anmeldung beim IdP ist das Konto verknüpft, und der Nutzer ist angemeldet.

Im Profil lässt sich eine Verknüpfung auch wieder lösen. Ausgenommen sind Global Admins (sie nutzen
kein Seiten-SSO) und Konten mit einer Rolle auf einer anderen Statusseite. Gehört die Identität schon
einem anderen Konto, bricht die Verknüpfung mit `already_linked` ab. Wechselt der Issuer, löscht das
Speichern die OIDC-Verknüpfungen der Seite; die Konten müssen neu verknüpft werden.

## Umgebungsvariablen

Diese gelten für den Server, nicht je Seite:

| Variable | Wirkung |
| --- | --- |
| `BASE_URL` | Öffentliche Adresse des Servers; Grundlage der Redirect-URI. Ohne Wert: `http://localhost:8080`. |
| `FRONTEND_URL` | Dorthin leitet der Server nach dem Login (`/auth/sso-callback?code=…`) und bei Fehlern. Ohne Wert: `http://localhost:5173`. |
| `JWT_SECRET` | Pflicht für die Sitzung nach dem Login. |

`SSO_ALLOWED_DOMAINS` und `SSO_BOOTSTRAP_ADMIN_EMAILS` wirken **nur** beim Google-/GitHub-Login
(`/api/auth/google`, `/api/auth/github`), nicht beim Seiten-OIDC: Dort gibt es keine automatische
Kontoanlage und keine E-Mail-Zuordnung, die man einschränken müsste.

## Fehlersuche

Fehler bei Login und Callback landen als Code in der URL der Login-Seite (`?oidc_error=<Code>`), bei
der Testanmeldung auf der SSO-Seite (`?oidc_test=<Code>`). Die häufigsten:

| Code | Ursache |
| --- | --- |
| `not_configured` | OIDC ist für die Seite aus oder die Seiten-ID stimmt nicht. |
| `provider_unreachable` | Discovery schlägt fehl: Issuer ohne `https://`, interne Adresse, nicht erreichbar oder anderer `issuer` im Discovery-Dokument (oft ein Schrägstrich am Ende). Fehlschläge werden eine Minute lang gemerkt. |
| `state_invalid` | Login dauerte länger als 10 Minuten, wurde in einem anderen Browser abgeschlossen oder der State wurde schon verwendet. |
| `token_exchange` | Client-ID/-Secret oder Redirect-URI stimmen beim IdP nicht. |
| `id_token_invalid` | Signatur, Aussteller, Zielgruppe (`aud`), Ablauf oder Nonce passen nicht. Uhrzeit des Servers prüfen. |
| `not_linked` | Konto zuerst verknüpfen, siehe oben. |

Alle Codes: [Fehlerbilder im Admin-Handbuch](../user-guide/sso-oidc.md#fehlerbilder).
