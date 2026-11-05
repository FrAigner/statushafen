# SSO-Architektur

Ablauf des Seiten-SSO (OIDC und SAML) je Statusseite. Endpunkte und Fehler stehen in der
[API](api.md), die Einrichtung im [OIDC-Guide](oidc.md) bzw. [SAML-Guide](saml.md).

## Login mit OIDC

```mermaid
sequenceDiagram
    autonumber
    participant B as Browser
    participant S as Statushafen
    participant I as IdP
    B->>S: GET /api/auth/oidc/login?page=N
    S->>S: State, Nonce, PKCE-Verifier speichern
    S-->>B: 307 zum IdP + Cookie oidc_state
    B->>I: Authorize (state, nonce, code_challenge)
    I-->>B: Anmeldung, dann Redirect mit code und state
    B->>S: GET /api/auth/oidc/callback?page=N&state=…&code=…
    S->>S: State gegen Cookie prüfen, genau einmal einlösen
    S->>I: Code gegen Token tauschen (mit PKCE-Verifier)
    I-->>S: ID-Token
    S->>S: Signatur, Issuer, Zielgruppe, Ablauf und Nonce prüfen
    S->>S: Konto über (Seite, Issuer, Subject) suchen
    S-->>B: 307 zum Frontend mit Einmal-Code
    B->>S: POST /api/auth/sso-exchange (Einmal-Code)
    S-->>B: JWT
```

Das JWT steht nie in einer URL: Der Browser bekommt nur den kurzlebigen Einmal-Code, den das Frontend
einlöst. Das Konto wird nie über die E-Mail-Adresse gefunden. Das JWT gilt nur für diese Seite
().

## Login mit SAML

```mermaid
sequenceDiagram
    autonumber
    participant B as Browser
    participant S as Statushafen
    participant I as IdP
    B->>S: GET /api/auth/saml/login?page=N
    S->>S: IdP-Metadaten laden, AuthnRequest signieren, State speichern
    S-->>B: 307 zum IdP (Redirect-Binding, RelayState) + Cookie saml_state
    B->>I: AuthnRequest
    I-->>B: Anmeldung, dann Formular mit SAMLResponse
    B->>S: POST /api/auth/saml/N/acs (SAMLResponse, RelayState)
    S->>S: State gegen Cookie prüfen, genau einmal einlösen
    S->>S: Signatur, Issuer, Audience, Destination, InResponseTo, Zeiten, NameID prüfen
    S->>S: Assertion-ID gegen Replay merken
    S->>S: Konto über (Seite, Entity-ID, NameID) suchen
    S-->>B: 303 zum Frontend mit Einmal-Code
    B->>S: POST /api/auth/sso-exchange (Einmal-Code)
    S-->>B: JWT
```

IdP-initiierte Anmeldungen gibt es nicht: Jede Antwort muss zu einem eigenen AuthnRequest passen.

## Verknüpfung eines bestehenden Kontos

```mermaid
sequenceDiagram
    autonumber
    participant B as Browser (angemeldet)
    participant S as Statushafen
    participant I as IdP
    B->>S: POST /api/auth/oidc/link?page=N (X-Auth-Token)
    S->>S: Ausnahmen prüfen, State mit Konto-ID speichern
    S-->>B: 200 {"url": Authorize-URL} + Cookie oidc_state
    B->>I: Authorize
    I-->>B: Redirect mit code und state
    B->>S: GET /api/auth/oidc/callback
    S->>S: wie beim Login, dann Identität dem Konto zuordnen
    S-->>B: 307 zum Frontend mit Einmal-Code
```

Bei SAML läuft die Verknüpfung genauso über `POST /api/auth/saml/link?page=N` und den ACS.

Bei Global Admins oder Konten mit Rolle auf einer anderen Seite lehnt schon der Link-Aufruf mit `403`
ab. Gehört die Identität bereits einem anderen Konto, leitet der Callback bzw. ACS mit dem Code
`already_linked` ins Profil zurück.

## Bausteine

```mermaid
flowchart LR
    A[Seiten-Admin] -->|PUT /api/admin/pages/:id/sso| C[(SSO-Konfiguration je Seite)]
    U[Nutzer] --> L[Login / Callback / ACS]
    I[IdP] -->|SP-Metadaten| M[saml/:id/metadata]
    M --> C
    L --> C
    L --> D[(Login-State)]
    L --> R[(Verwendete Assertions)]
    L --> K[(Verknüpfte Identitäten)]
    L --> X[Einmal-Code-Speicher]
    X --> E[sso-exchange]
```
