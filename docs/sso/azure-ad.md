# Azure AD / Microsoft Entra ID (SAML)

Lies zuerst den [allgemeinen SAML-Guide](saml.md). Hier stehen nur die Entra-spezifischen Schritte.
Entra ID lässt sich alternativ über [OIDC](oidc.md) anbinden.

## Schritte

1. In Statushafen die SSO-Konfiguration der Seite einmal speichern, damit SP Entity ID, ACS-URL und
   SP-Zertifikat feststehen (siehe [SAML allgemein](saml.md#schritte)).
2. In Entra ID unter „Unternehmensanwendungen“ eine eigene Anwendung anlegen und „Einmaliges
   Anmelden → SAML“ wählen.
3. Unter „SAML-Basiskonfiguration“ eintragen:
   - „Bezeichner (Entitäts-ID)“: die SP Entity ID, z. B.
     `https://status.example.org/api/auth/saml/<SEITEN-ID>/metadata`.
   - „Antwort-URL (Assertionverbraucherdienst-URL)“: die ACS-URL, z. B.
     `https://status.example.org/api/auth/saml/<SEITEN-ID>/acs`.
4. Unter „Attribute & Ansprüche“ den **eindeutigen Benutzerbezeichner** auf das Format
   **Persistent** stellen (z. B. `user.objectid`). Standardmäßig liefert Entra die E-Mail als NameID;
   das lehnt Statushafen ab (siehe [NameID-Format](saml.md#nameid-format)).
5. In Statushafen die Attributnamen eintragen, die Entra standardmäßig sendet:
   - E-Mail: `http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress`
   - Name: `http://schemas.microsoft.com/identity/claims/displayname`
6. Die **App-Verbundmetadaten-URL** aus „SAML-Zertifikate“ in Statushafen als IdP-Metadaten-URL
   eintragen und speichern.
7. Nutzer oder Gruppen der Anwendung zuweisen.
8. Testanmeldung starten, SAML einschalten und Konten verknüpfen, siehe
   [Konten und Verknüpfung](saml.md#konten-und-verknüpfung).

Zurück zur [Übersicht](README.md).
