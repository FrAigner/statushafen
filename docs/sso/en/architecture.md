# SSO architecture

> English version of the German document [`../architecture.md`](../architecture.md).

Flow of the page SSO (OIDC and SAML) per status page. Endpoints and errors are in the
[API](api.md), the setup in the [OIDC guide](oidc.md) and [SAML guide](saml.md).

## Login with OIDC

```mermaid
sequenceDiagram
    autonumber
    participant B as Browser
    participant S as Statushafen
    participant I as IdP
    B->>S: GET /api/auth/oidc/login?page=N
    S->>S: Store state, nonce, PKCE verifier
    S-->>B: 307 to the IdP + cookie oidc_state
    B->>I: Authorize (state, nonce, code_challenge)
    I-->>B: Sign-in, then redirect with code and state
    B->>S: GET /api/auth/oidc/callback?page=N&state=…&code=…
    S->>S: Check state against cookie, redeem exactly once
    S->>I: Exchange code for tokens (with PKCE verifier)
    I-->>S: ID token
    S->>S: Check signature, issuer, audience, expiry and nonce
    S->>S: Look up account via (page, issuer, subject)
    S-->>B: 307 to the frontend with one-time code
    B->>S: POST /api/auth/sso-exchange (one-time code)
    S-->>B: JWT
```

The JWT is never in a URL: the browser only receives the short-lived one-time code, which the
frontend redeems. The account is never found through the email address. The JWT is valid for this
page only.

## Login with SAML

```mermaid
sequenceDiagram
    autonumber
    participant B as Browser
    participant S as Statushafen
    participant I as IdP
    B->>S: GET /api/auth/saml/login?page=N
    S->>S: Load IdP metadata, sign AuthnRequest, store state
    S-->>B: 307 to the IdP (redirect binding, RelayState) + cookie saml_state
    B->>I: AuthnRequest
    I-->>B: Sign-in, then form with SAMLResponse
    B->>S: POST /api/auth/saml/N/acs (SAMLResponse, RelayState)
    S->>S: Check state against cookie, redeem exactly once
    S->>S: Check signature, issuer, audience, destination, InResponseTo, times, NameID
    S->>S: Remember assertion ID against replay
    S->>S: Look up account via (page, entity ID, NameID)
    S-->>B: 303 to the frontend with one-time code
    B->>S: POST /api/auth/sso-exchange (one-time code)
    S-->>B: JWT
```

There is no IdP-initiated sign-in: every response must match an AuthnRequest of its own.

## Linking an existing account

```mermaid
sequenceDiagram
    autonumber
    participant B as Browser (signed in)
    participant S as Statushafen
    participant I as IdP
    B->>S: POST /api/auth/oidc/link?page=N (X-Auth-Token)
    S->>S: Check exclusions, store state with account ID
    S-->>B: 200 {"url": authorize URL} + cookie oidc_state
    B->>I: Authorize
    I-->>B: Redirect with code and state
    B->>S: GET /api/auth/oidc/callback
    S->>S: As at login, then assign the identity to the account
    S-->>B: 307 to the frontend with one-time code
```

For SAML, linking works the same way via `POST /api/auth/saml/link?page=N` and the ACS.

For global admins or accounts with a role on another page, the link call already rejects with
`403`. If the identity already belongs to another account, the callback or ACS redirects back to
the profile with the code `already_linked`.

## Building blocks

```mermaid
flowchart LR
    A[Page admin] -->|PUT /api/admin/pages/:id/sso| C[(SSO configuration per page)]
    U[User] --> L[Login / callback / ACS]
    I[IdP] -->|SP metadata| M[saml/:id/metadata]
    M --> C
    L --> C
    L --> D[(Login state)]
    L --> R[(Used assertions)]
    L --> K[(Linked identities)]
    L --> X[One-time code store]
    X --> E[sso-exchange]
```
