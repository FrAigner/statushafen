package mocksamlidp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/xml"
	"html"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/crewjam/saml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Der Mock-SAML-IdP wird hier mit einem crewjam-Service-Provider
// durchgespielt, wie ihn das Backend nutzt: AuthnRequest per Redirect,
// Anmeldeformular, signierte Antwort per POST an die ACS-URL.

const (
	testBase = "http://mock.test"
	testACS  = "http://localhost:8080/api/auth/saml/7/acs"
)

func TestFromEnv(t *testing.T) {
	assert.Equal(t, Config{Addr: ":8091", BaseURL: "http://localhost:8091", ACSPrefix: "http://localhost:8080/api/auth/saml/"},
		FromEnv(func(string) string { return "" }))
	env := map[string]string{"MOCK_SAML_ADDR": ":9", "MOCK_SAML_URL": "https://idp.example/", "MOCK_SAML_ACS_PREFIX": "https://sp/"}
	assert.Equal(t, Config{Addr: ":9", BaseURL: "https://idp.example", ACSPrefix: "https://sp/"},
		FromEnv(func(k string) string { return env[k] }))
}

func newTestMock() *Mock {
	return New(Config{BaseURL: testBase, ACSPrefix: "http://localhost:8080/api/auth/saml/"})
}

func serve(m *Mock, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return w
}

func idpMetadata(t *testing.T, m *Mock) *saml.EntityDescriptor {
	t.Helper()
	w := serve(m, httptest.NewRequest(http.MethodGet, testBase+"/metadata", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var ed saml.EntityDescriptor
	require.NoError(t, xml.Unmarshal(w.Body.Bytes(), &ed))
	return &ed
}

// testSP ist ein Service Provider wie im Backend (newSAMLServiceProvider).
func testSP(t *testing.T, m *Mock, acs string) *saml.ServiceProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "sp"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	acsURL, _ := url.Parse(acs)
	metaURL, _ := url.Parse("http://localhost:8080/api/auth/saml/7/metadata")
	return &saml.ServiceProvider{
		EntityID: metaURL.String(), Key: key, Certificate: cert, MetadataURL: *metaURL, AcsURL: *acsURL,
		IDPMetadata: idpMetadata(t, m), AuthnNameIDFormat: saml.UnspecifiedNameIDFormat,
	}
}

func authnRedirect(t *testing.T, sp *saml.ServiceProvider) (string, string) {
	t.Helper()
	req, err := sp.MakeAuthenticationRequest(sp.GetSSOBindingLocation(saml.HTTPRedirectBinding), saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	require.NoError(t, err)
	u, err := req.Redirect("relay-1", sp)
	require.NoError(t, err)
	return u.String(), req.ID
}

var hiddenField = regexp.MustCompile(`name="([A-Za-z]+)" value="([^"]*)"`)

func formFields(body string) url.Values {
	v := url.Values{}
	for _, m := range hiddenField.FindAllStringSubmatch(body, -1) {
		v.Set(m[1], html.UnescapeString(m[2]))
	}
	return v
}

func postForm(m *Mock, fields url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, testBase+"/sso", strings.NewReader(fields.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return serve(m, r)
}

func TestMetadata(t *testing.T) {
	m := newTestMock()
	ed := idpMetadata(t, m)
	assert.Equal(t, testBase+"/metadata", ed.EntityID)
	require.Len(t, ed.IDPSSODescriptors, 1)
	assert.Equal(t, testBase+"/sso", ed.IDPSSODescriptors[0].SingleSignOnServices[0].Location)
	assert.Equal(t, http.StatusMethodNotAllowed, serve(m, httptest.NewRequest(http.MethodPost, testBase+"/metadata", nil)).Code)
}

func TestLoginRoundTrip(t *testing.T) {
	m := newTestMock()
	sp := testSP(t, m, testACS)
	redirect, requestID := authnRedirect(t, sp)

	// 1. Redirect-Binding vom SP: Anmeldeformular mit AuthnRequest und RelayState.
	w := serve(m, httptest.NewRequest(http.MethodGet, redirect, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `name="username"`)
	assert.Contains(t, w.Body.String(), sp.EntityID)
	fields := formFields(w.Body.String())
	assert.Equal(t, "relay-1", fields.Get("RelayState"))
	require.NotEmpty(t, fields.Get("SAMLRequest"))

	// 2. Ohne Benutzernamen bleibt es beim Formular.
	w = postForm(m, fields)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `name="username"`)

	// 3. Mit Benutzernamen: selbst absendendes Formular an die ACS-URL.
	fields.Set("username", "erika")
	fields.Set("email", "erika@example.org")
	fields.Set("name", "Erika Mock")
	w = postForm(m, fields)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `action="`+testACS+`"`)
	resp := formFields(w.Body.String())
	assert.Equal(t, "relay-1", resp.Get("RelayState"))
	raw, err := base64.StdEncoding.DecodeString(resp.Get("SAMLResponse"))
	require.NoError(t, err)

	// Der SP prüft die Antwort wie das Backend (Signatur, Audience, InResponseTo …).
	acsURL, _ := url.Parse(testACS)
	assertion, err := sp.ParseXMLResponse(raw, []string{requestID}, *acsURL)
	require.NoError(t, err)
	assert.Equal(t, "erika", assertion.Subject.NameID.Value)
	assert.Equal(t, string(saml.PersistentNameIDFormat), assertion.Subject.NameID.Format)
	attrs := map[string]string{}
	for _, a := range assertion.AttributeStatements[0].Attributes {
		attrs[a.Name] = a.Values[0].Value
	}
	assert.Equal(t, "erika@example.org", attrs["email"])
	assert.Equal(t, "Erika Mock", attrs["name"])
}

func TestSSORejects(t *testing.T) {
	m := newTestMock()
	t.Run("AuthnRequest unlesbar", func(t *testing.T) {
		w := serve(m, httptest.NewRequest(http.MethodGet, testBase+"/sso?SAMLRequest=%%%", nil))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("kein XML", func(t *testing.T) {
		w := postForm(m, url.Values{"SAMLRequest": {base64.StdEncoding.EncodeToString([]byte("kein xml"))}})
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("ACS außerhalb des erlaubten Anfangs", func(t *testing.T) {
		redirect, _ := authnRedirect(t, testSP(t, m, "https://fremd.example/acs"))
		w := serve(m, httptest.NewRequest(http.MethodGet, redirect, nil))
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
	t.Run("ohne ACS-URL", func(t *testing.T) {
		raw := `<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" ID="x" Version="2.0"></samlp:AuthnRequest>`
		w := postForm(m, url.Values{"SAMLRequest": {base64.StdEncoding.EncodeToString([]byte(raw))}})
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
	t.Run("crewjam lehnt ab (abgelaufener Request)", func(t *testing.T) {
		sp := testSP(t, m, testACS)
		req, err := sp.MakeAuthenticationRequest(testBase+"/sso", saml.HTTPRedirectBinding, saml.HTTPPostBinding)
		require.NoError(t, err)
		req.IssueInstant = time.Now().Add(-time.Hour)
		u, err := req.Redirect("", sp)
		require.NoError(t, err)
		w := serve(m, httptest.NewRequest(http.MethodGet, u.String(), nil))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestRun_ReturnsOneOnListenError(t *testing.T) {
	env := map[string]string{"MOCK_SAML_ADDR": "unmöglich:adresse:1"}
	assert.Equal(t, 1, Run(func(k string) string { return env[k] }))
}

func TestNewServer(t *testing.T) {
	srv := newServer(Config{Addr: ":1", BaseURL: testBase})
	assert.Equal(t, ":1", srv.Addr)
	assert.Equal(t, 10*time.Second, srv.ReadHeaderTimeout)
}

// TestCertificateLifetime: Das Zertifikat des Mocks gilt ein Jahr.
func TestCertificateLifetime(t *testing.T) {
	ed := idpMetadata(t, newTestMock())
	der, err := base64.StdEncoding.DecodeString(ed.IDPSSODescriptors[0].KeyDescriptors[0].KeyInfo.X509Data.X509Certificates[0].Data)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(8760*time.Hour), cert.NotAfter, time.Minute)
}
