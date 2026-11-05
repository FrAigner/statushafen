# Azure AD / Microsoft Entra ID (SAML)

> English version of the German guide [`../azure-ad.md`](../azure-ad.md).

Read the [general SAML guide](saml.md) first. This page contains only the Entra-specific steps.
Entra ID can alternatively be connected via [OIDC](oidc.md).

## Steps

1. In Statushafen, save the page's SSO configuration once so that SP entity ID, ACS URL and SP
   certificate are fixed (see [SAML general](saml.md#steps)).
2. In Entra ID, create a dedicated application under "Enterprise applications" and choose "Single
   sign-on → SAML".
3. Under "Basic SAML Configuration" enter:
   - "Identifier (Entity ID)": the SP entity ID, for example
     `https://status.example.org/api/auth/saml/<PAGE-ID>/metadata`.
   - "Reply URL (Assertion Consumer Service URL)": the ACS URL, for example
     `https://status.example.org/api/auth/saml/<PAGE-ID>/acs`.
4. Under "Attributes & Claims", set the **unique user identifier** to the format **Persistent**
   (for example `user.objectid`). By default Entra delivers the email as NameID; Statushafen
   rejects that (see [NameID format](saml.md#nameid-format)).
5. In Statushafen, enter the attribute names that Entra sends by default:
   - Email: `http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress`
   - Name: `http://schemas.microsoft.com/identity/claims/displayname`
6. Enter the **App Federation Metadata URL** from "SAML Certificates" in Statushafen as the IdP
   metadata URL and save.
7. Assign users or groups to the application.
8. Start the test sign-in, switch SAML on and link accounts, see
   [Accounts and linking](saml.md#accounts-and-linking).

Back to the [overview](README.md).
