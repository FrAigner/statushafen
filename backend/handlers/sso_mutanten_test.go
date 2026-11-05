package handlers

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Nachtlauf 2026-10-29 (Mutationstests nachts): Diese Tests töten die Mutanten, die in
// statuspage_sso.go überlebt haben.

// Grenzfall der Größenprüfung: Genau samlMaxMetadataXMLBytes sind noch erlaubt (die Meldung "zu groß"
// kommt erst darüber); das Leerzeichen-freie Füllzeichen-XML scheitert dann erst an der Lesbarkeit.
func TestSAMLRequestValidate_MetadatenGroesseGrenze(t *testing.T) {
	genau := samlRequest{SAMLIdPMetadataXML: strings.Repeat("x", samlMaxMetadataXMLBytes)}
	assert.NotContains(t, genau.validate(), "zu groß")

	darueber := samlRequest{SAMLIdPMetadataXML: strings.Repeat("x", samlMaxMetadataXMLBytes+1)}
	assert.Equal(t, "IdP-Metadaten zu groß (höchstens 128 KB)", darueber.validate())
}

// Verschlüsselungszertifikate zählen nicht zum Vertrauensanker: Wechselt nur das, bleiben die Verknüpfungen.
func TestSAMLTrustAnchor_IgnoriertVerschluesselungszertifikat(t *testing.T) {
	const kopf = `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.test/e">` +
		`<IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">`
	const fuss = `<SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.example.test/sso"/>` +
		`</IDPSSODescriptor></EntityDescriptor>`
	schluessel := func(use, zert string) string {
		return `<KeyDescriptor use="` + use + `"><KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#"><X509Data>` +
			`<X509Certificate>` + zert + `</X509Certificate></X509Data></KeyInfo></KeyDescriptor>`
	}
	xml := kopf + schluessel("signing", "AAAA BBBB") + schluessel("encryption", "CCCC") + fuss

	assert.Equal(t, "saml:https://idp.example.test/e|AAAABBBB", samlTrustAnchor("", xml))
}

// Ändern sich in einer Anfrage Issuer UND SAML-Anker, werden beide Protokolle zurückgesetzt:
// Der OIDC-Reset darf den SAML-Reset nicht überspringen.
func TestUpdateSSO_OIDCUndSAMLWechselSetztBeidesZurueck(t *testing.T) {
	f := newIdentityResetFixture(t)
	body := func(discovery, samlURL string) map[string]interface{} {
		return map[string]interface{}{
			"oidc_discovery_url": discovery, "oidc_client_id": "c", "oidc_client_secret": "s",
			"saml_idp_metadata_url": samlURL,
		}
	}
	code, _ := f.put(t, body("https://idp.example.org/realms/a", "https://idp.example.test/a"))
	require.Equal(t, http.StatusOK, code)
	f.seed(t)

	code, _ = f.put(t, body("https://idp.example.org/realms/b", "https://idp.example.test/b"))
	require.Equal(t, http.StatusOK, code)

	assert.Equal(t, []uint{3}, f.remaining(t), "OIDC- und SAML-Verknüpfung der Seite weg, fremde Seite bleibt")
	assert.Len(t, f.resetAudits(t, "sso.oidc_identities_reset"), 1)
	assert.Len(t, f.resetAudits(t, "sso.saml_identities_reset"), 1)
}
