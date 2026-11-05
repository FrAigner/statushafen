# Okta (SAML)

Lies zuerst den [allgemeinen SAML-Guide](saml.md). Hier stehen nur die Okta-spezifischen Schritte.
Okta lässt sich alternativ über [OIDC](oidc.md) anbinden.

## Schritte

1. In Statushafen die SSO-Konfiguration der Seite einmal speichern, damit SP Entity ID, ACS-URL und
   SP-Zertifikat feststehen (siehe [SAML allgemein](saml.md#schritte)).
2. In der Okta-Admin-Konsole unter „Applications“ eine neue App-Integration mit Anmeldemethode
   **SAML 2.0** anlegen.
3. In den SAML-Einstellungen eintragen:
   - „Single sign-on URL“: die ACS-URL, z. B.
     `https://status.example.org/api/auth/saml/<SEITEN-ID>/acs` (als Recipient und Destination
     verwenden).
   - „Audience URI (SP Entity ID)“: die SP Entity ID, z. B.
     `https://status.example.org/api/auth/saml/<SEITEN-ID>/metadata`.
   - „Name ID format“: **Persistent** (siehe [NameID-Format](saml.md#nameid-format)).
4. Unter „Attribute Statements“ Attribute für E-Mail und Namen anlegen, z. B. `email` und `name`, und
   dieselben Namen in Statushafen eintragen (dort sind `email` und `name` ohnehin der Standard).
5. Die **Metadata URL** der Okta-App (Reiter „Sign On“) in Statushafen als IdP-Metadaten-URL
   eintragen und speichern.
6. Nutzer oder Gruppen der Okta-App zuweisen.
7. Testanmeldung starten, SAML einschalten und Konten verknüpfen, siehe
   [Konten und Verknüpfung](saml.md#konten-und-verknüpfung).

Zurück zur [Übersicht](README.md).
