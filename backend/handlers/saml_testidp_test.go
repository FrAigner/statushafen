package handlers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	dsig "github.com/russellhaering/goxmldsig"
	"github.com/stretchr/testify/require"
)

// samlTestIdP ist ein SAML-Identity-Provider im Testprozess. Er
// baut auf crewjams IdentityProvider auf (AuthnRequest lesen und gegen die
// SP-Metadaten prüfen, Assertion erzeugen) und setzt Antworten für die
// Negativfälle selbst zusammen: unsigniert, nur Assertion bzw. nur Response
// signiert, mit fremdem Schlüssel signiert, nachträglich verändert. Die
// Schlüssel entstehen im Test, nichts davon liegt im Repo.
type samlTestIdP struct {
	t    *testing.T
	idp  *saml.IdentityProvider
	key  *rsa.PrivateKey
	cert *x509.Certificate
	// spMetadata liefert die Metadaten unseres SP (aus dem Metadaten-Endpunkt
	// des Fixtures), gegen die der IdP AuthnRequests prüft.
	spMetadata func() *saml.EntityDescriptor
}

var (
	samlTestKeysOnce sync.Once
	samlTestIdPKey   *rsa.PrivateKey
	samlTestOtherKey *rsa.PrivateKey
)

// samlTestKeys erzeugt die RSA-Schlüssel einmal pro Testlauf (2048 Bit kosten
// je Schlüssel spürbar Zeit).
func samlTestKeys() (idpKey, otherKey *rsa.PrivateKey) {
	samlTestKeysOnce.Do(func() {
		samlTestIdPKey, _ = rsa.GenerateKey(rand.Reader, 2048)
		samlTestOtherKey, _ = rsa.GenerateKey(rand.Reader, 2048)
	})
	return samlTestIdPKey, samlTestOtherKey
}

func samlTestCert(t *testing.T, key *rsa.PrivateKey, cn string) *x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

const samlTestIdPBase = "https://idp.example.test"

func newSAMLTestIdP(t *testing.T, spMetadata func() *saml.EntityDescriptor) *samlTestIdP {
	t.Helper()
	key, _ := samlTestKeys()
	cert := samlTestCert(t, key, "test-idp")
	ti := &samlTestIdP{t: t, key: key, cert: cert, spMetadata: spMetadata}
	metaURL, _ := url.Parse(samlTestIdPBase + "/metadata")
	ssoURL, _ := url.Parse(samlTestIdPBase + "/sso")
	ti.idp = &saml.IdentityProvider{
		Key:                     key,
		Certificate:             cert,
		MetadataURL:             *metaURL,
		SSOURL:                  *ssoURL,
		ServiceProviderProvider: ti,
	}
	return ti
}

// GetServiceProvider erfüllt saml.ServiceProviderProvider.
func (ti *samlTestIdP) GetServiceProvider(_ *http.Request, _ string) (*saml.EntityDescriptor, error) {
	return ti.spMetadata(), nil
}

func (ti *samlTestIdP) entityID() string { return ti.idp.MetadataURL.String() }

// metadataXML sind die IdP-Metadaten, wie ein Admin sie einfügt.
func (ti *samlTestIdP) metadataXML() string {
	buf, err := xml.Marshal(ti.idp.Metadata())
	require.NoError(ti.t, err)
	return string(buf)
}

// samlRespOpts steuert die Antwort. Nullwert: Assertion und Response mit dem
// IdP-Schlüssel signiert, unverschlüsselt, NameID persistent.
type samlRespOpts struct {
	nameID         string
	nameIDFormat   string
	attrs          map[string]string
	unsignedAssert bool
	unsignedResp   bool
	signKey        *rsa.PrivateKey // nil: IdP-Schlüssel
	crewjamResp    bool            // crewjam-Standardweg (MakeResponse): Assertion und Response signiert
	now            time.Time       // Ausstellungszeit, Nullwert: jetzt
	status         string
	mutate         func(*saml.Assertion)
	mutateResp     func(*saml.Response)
	// tamper ändert die serialisierte Antwort nach dem Signieren.
	tamper func([]byte) []byte
}

// authnRequest liest den AuthnRequest aus der Redirect-URL unseres SP und
// prüft ihn wie ein echter IdP (Destination, SP bekannt, ACS-URL aus den
// SP-Metadaten).
func (ti *samlTestIdP) authnRequest(redirectURL string, now time.Time) *saml.IdpAuthnRequest {
	ti.t.Helper()
	r := httptest.NewRequest(http.MethodGet, redirectURL, nil)
	req, err := saml.NewIdpAuthnRequest(ti.idp, r)
	require.NoError(ti.t, err)
	require.NoError(ti.t, req.Validate())
	req.Now = now
	return req
}

// respond liefert SAMLResponse (base64) und RelayState für den ACS.
func (ti *samlTestIdP) respond(redirectURL string, o samlRespOpts) (string, string) {
	ti.t.Helper()
	now := o.now
	if now.IsZero() {
		now = time.Now()
	}
	req := ti.authnRequest(redirectURL, now)
	session := &saml.Session{
		ID:           "sess-1",
		CreateTime:   now,
		NameID:       o.nameID,
		NameIDFormat: o.nameIDFormat,
	}
	if session.NameID == "" {
		session.NameID = "saml-user-1"
	}
	if session.NameIDFormat == "" {
		session.NameIDFormat = string(saml.PersistentNameIDFormat)
	}
	for name, value := range o.attrs {
		session.CustomAttributes = append(session.CustomAttributes, saml.Attribute{
			Name: name, NameFormat: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic",
			Values: []saml.AttributeValue{{Type: "xs:string", Value: value}},
		})
	}
	require.NoError(ti.t, saml.DefaultAssertionMaker{}.MakeAssertion(req, session))
	req.Assertion.IssueInstant = now
	if o.mutate != nil {
		o.mutate(req.Assertion)
	}

	var responseEl *etree.Element
	if o.crewjamResp {
		require.NoError(ti.t, req.MakeResponse())
		responseEl = req.ResponseEl
	} else {
		responseEl = ti.buildResponse(req, now, o)
	}
	doc := etree.NewDocument()
	doc.SetRoot(responseEl)
	buf, err := doc.WriteToBytes()
	require.NoError(ti.t, err)
	if o.tamper != nil {
		buf = o.tamper(buf)
	}
	return base64.StdEncoding.EncodeToString(buf), req.RelayState
}

func (ti *samlTestIdP) buildResponse(req *saml.IdpAuthnRequest, now time.Time, o samlRespOpts) *etree.Element {
	key := o.signKey
	if key == nil {
		key = ti.key
	}
	assertionEl := req.Assertion.Element()
	if !o.unsignedAssert {
		req.Assertion.Signature = ti.sign(assertionEl, key)
		assertionEl = req.Assertion.Element()
	}
	status := o.status
	if status == "" {
		status = saml.StatusSuccess
	}
	resp := &saml.Response{
		Destination:  req.ACSEndpoint.Location,
		ID:           fmt.Sprintf("resp-%d", time.Now().UnixNano()),
		InResponseTo: req.Request.ID,
		IssueInstant: now,
		Version:      "2.0",
		Issuer:       &saml.Issuer{Format: "urn:oasis:names:tc:SAML:2.0:nameid-format:entity", Value: ti.entityID()},
		Status:       saml.Status{StatusCode: saml.StatusCode{Value: status}},
	}
	if o.mutateResp != nil {
		o.mutateResp(resp)
	}
	responseEl := resp.Element()
	responseEl.AddChild(assertionEl)
	if !o.unsignedResp {
		resp.Signature = ti.sign(responseEl, key)
		responseEl = resp.Element()
		responseEl.AddChild(assertionEl)
	}
	return responseEl
}

// sign signiert el enveloped (wie crewjam) und liefert nur das
// Signature-Element; der Aufrufer baut das Element mit der Signatur an der
// vom Schema verlangten Stelle neu.
func (ti *samlTestIdP) sign(el *etree.Element, key *rsa.PrivateKey) *etree.Element {
	ti.t.Helper()
	cert := ti.cert
	if key != ti.key {
		cert = samlTestCert(ti.t, key, "fremder-idp")
	}
	ctx, err := dsig.NewSigningContext(key, [][]byte{cert.Raw})
	require.NoError(ti.t, err)
	ctx.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
	require.NoError(ti.t, ctx.SetSignatureMethod(dsig.RSASHA256SignatureMethod))
	signed, err := ctx.SignEnveloped(el)
	require.NoError(ti.t, err)
	return signed.ChildElements()[len(signed.ChildElements())-1]
}
