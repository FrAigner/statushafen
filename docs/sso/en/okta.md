# Okta (SAML)

> English version of the German guide [`../okta.md`](../okta.md).

Read the [general SAML guide](saml.md) first. This page contains only the Okta-specific steps.
Okta can alternatively be connected via [OIDC](oidc.md).

## Steps

1. In Statushafen, save the page's SSO configuration once so that SP entity ID, ACS URL and SP
   certificate are fixed (see [SAML general](saml.md#steps)).
2. In the Okta admin console, create a new app integration under "Applications" with the sign-in
   method **SAML 2.0**.
3. In the SAML settings enter:
   - "Single sign-on URL": the ACS URL, for example
     `https://status.example.org/api/auth/saml/<PAGE-ID>/acs` (use it as Recipient and
     Destination).
   - "Audience URI (SP Entity ID)": the SP entity ID, for example
     `https://status.example.org/api/auth/saml/<PAGE-ID>/metadata`.
   - "Name ID format": **Persistent** (see [NameID format](saml.md#nameid-format)).
4. Under "Attribute Statements" create attributes for email and name, for example `email` and
   `name`, and enter the same names in Statushafen (where `email` and `name` are the default
   anyway).
5. Enter the **Metadata URL** of the Okta app (tab "Sign On") in Statushafen as the IdP metadata
   URL and save.
6. Assign users or groups to the Okta app.
7. Start the test sign-in, switch SAML on and link accounts, see
   [Accounts and linking](saml.md#accounts-and-linking).

Back to the [overview](README.md).
