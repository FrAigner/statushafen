// Package mocksamlidp ist ein SAML-Identity-Provider für lokale Tests und
// den nächtlichen E2E-Lauf (analog zum Mock-OIDC). Nie
// produktiv einsetzen: Er meldet jeden an, der einen Benutzernamen eintippt.
//
// Fertige Test-IdPs (SimpleSAMLphp, kristophjunge/test-saml-idp u. a.) kennen
// nur fest eingetragene Service Provider. Bei der Statusseite hängen Entity-ID
// und ACS-URL aber an der Seiten-ID, und der E2E-Spec legt bei jedem Lauf eine
// neue Seite an. Dieser Mock nimmt deshalb jeden SP an, dessen ACS-URL mit
// MOCK_SAML_ACS_PREFIX beginnt, und baut dessen Metadaten aus dem
// AuthnRequest selbst. Er stützt sich auf crewjams IdentityProvider
// (AuthnRequest prüfen, Assertion bauen, Response und Assertion signieren).
package mocksamlidp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/xml"
	"html/template"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/logger"
)

// Config sind die Einstellungen aus der Umgebung (siehe FromEnv).
type Config struct {
	// Addr ist die Listen-Adresse, z.B. ":8091".
	Addr string
	// BaseURL ist die Adresse, unter der Browser den Mock erreichen; daraus
	// entstehen Entity-ID (BaseURL/metadata) und SSO-URL (BaseURL/sso).
	BaseURL string
	// ACSPrefix: nur Service Provider mit einer ACS-URL mit diesem Anfang.
	ACSPrefix string
}

// FromEnv liest MOCK_SAML_ADDR, MOCK_SAML_URL und MOCK_SAML_ACS_PREFIX mit
// den Standardwerten für die lokale Entwicklung.
func FromEnv(getenv func(string) string) Config {
	or := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	return Config{
		Addr:      or("MOCK_SAML_ADDR", ":8091"),
		BaseURL:   strings.TrimSuffix(or("MOCK_SAML_URL", "http://localhost:8091"), "/"),
		ACSPrefix: or("MOCK_SAML_ACS_PREFIX", "http://localhost:8080/api/auth/saml/"),
	}
}

// Mock ist der IdP. Er ist ein http.Handler für /metadata und /sso.
type Mock struct {
	idp       *saml.IdentityProvider
	acsPrefix string
	mux       *http.ServeMux
}

// New erzeugt den Mock mit frischem Schlüssel und selbstsigniertem
// Zertifikat. Fehler sind dabei nicht möglich: crypto/rand bricht seit Go 1.24
// selbst ab, die Zertifikatsvorlage ist fest, und BaseURL kommt aus der
// eigenen Konfiguration (ein kaputter Wert fiele beim ersten Login auf).
func New(cfg Config) *Mock {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "statuspage mock SAML IdP"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(der)
	metadataURL, _ := url.Parse(cfg.BaseURL + "/metadata")
	ssoURL, _ := url.Parse(cfg.BaseURL + "/sso")
	m := &Mock{acsPrefix: cfg.ACSPrefix, mux: http.NewServeMux()}
	m.idp = &saml.IdentityProvider{
		Key:                     key,
		Certificate:             cert,
		Logger:                  logger.DefaultLogger,
		MetadataURL:             *metadataURL,
		SSOURL:                  *ssoURL,
		ServiceProviderProvider: spFromRequest{},
		SessionProvider:         formSession{},
	}
	m.mux.HandleFunc("GET /metadata", m.idp.ServeMetadata)
	m.mux.HandleFunc("/sso", m.serveSSO)
	return m
}

func (m *Mock) ServeHTTP(w http.ResponseWriter, r *http.Request) { m.mux.ServeHTTP(w, r) }

type acsKey struct{}

// serveSSO liest den AuthnRequest vorab, prüft die ACS-URL gegen den
// erlaubten Anfang und gibt sie crewjam über den Request-Kontext mit (siehe
// spFromRequest). Den Rest erledigt crewjams ServeSSO.
func (m *Mock) serveSSO(w http.ResponseWriter, r *http.Request) {
	req, err := saml.NewIdpAuthnRequest(m.idp, r)
	var authn saml.AuthnRequest
	if err == nil {
		err = xml.Unmarshal(req.RequestBuffer, &authn)
	}
	if err != nil {
		http.Error(w, "AuthnRequest unlesbar", http.StatusBadRequest)
		return
	}
	acs := authn.AssertionConsumerServiceURL
	if acs == "" || !strings.HasPrefix(acs, m.acsPrefix) {
		http.Error(w, "ACS-URL nicht erlaubt: "+acs, http.StatusForbidden)
		return
	}
	m.idp.ServeSSO(w, r.WithContext(context.WithValue(r.Context(), acsKey{}, acs)))
}

// spFromRequest erfüllt saml.ServiceProviderProvider: Metadaten eines SP mit
// der Entity-ID aus dem AuthnRequest und der vorab geprüften ACS-URL. Ohne
// Zertifikat, also ohne Verschlüsselung der Assertion.
type spFromRequest struct{}

func (spFromRequest) GetServiceProvider(r *http.Request, serviceProviderID string) (*saml.EntityDescriptor, error) {
	acs, _ := r.Context().Value(acsKey{}).(string)
	return &saml.EntityDescriptor{
		EntityID: serviceProviderID,
		SPSSODescriptors: []saml.SPSSODescriptor{{
			AssertionConsumerServices: []saml.IndexedEndpoint{{Binding: saml.HTTPPostBinding, Location: acs, Index: 1}},
		}},
	}, nil
}

var loginForm = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="de"><head><meta charset="utf-8"><title>Mock-SAML-IdP</title></head>
<body>
<h1>Mock-SAML-IdP</h1>
<p>Anmeldung für {{.SP}}. Nur für Tests: jeder Benutzername wird angenommen.</p>
<form method="post" action="{{.Action}}">
<input type="hidden" name="SAMLRequest" value="{{.SAMLRequest}}">
<input type="hidden" name="RelayState" value="{{.RelayState}}">
<p><label>Benutzername (NameID) <input name="username" required></label></p>
<p><label>E-Mail (Attribut email) <input name="email"></label></p>
<p><label>Name (Attribut name) <input name="name"></label></p>
<p><input type="submit" value="Anmelden"></p>
</form>
</body></html>`))

// formSession erfüllt saml.SessionProvider: Ohne ausgefülltes Formular zeigt
// es das Formular (mit dem AuthnRequest als verstecktem Feld, POST-Binding),
// danach entsteht die Sitzung aus den Eingaben.
type formSession struct{}

func (formSession) GetSession(w http.ResponseWriter, r *http.Request, req *saml.IdpAuthnRequest) *saml.Session {
	// PostFormValue liest nur den Body eines POST; ein GET (Redirect-Binding
	// vom SP) zeigt also immer das Formular.
	username := r.PostFormValue("username")
	if username == "" {
		var buf bytes.Buffer
		_ = loginForm.Execute(&buf, map[string]string{
			"SP":          req.Request.Issuer.Value,
			"Action":      req.IDP.SSOURL.String(),
			"SAMLRequest": base64.StdEncoding.EncodeToString(req.RequestBuffer),
			"RelayState":  req.RelayState,
		})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(buf.Bytes())
		return nil
	}
	return &saml.Session{
		ID:           base64.RawURLEncoding.EncodeToString([]byte(username + time.Now().String())),
		CreateTime:   time.Now(),
		ExpireTime:   time.Now().Add(time.Hour),
		NameID:       username,
		NameIDFormat: string(saml.PersistentNameIDFormat),
		UserName:     username,
		CustomAttributes: []saml.Attribute{
			basicAttribute("email", r.PostFormValue("email")),
			basicAttribute("name", r.PostFormValue("name")),
		},
	}
}

func basicAttribute(name, value string) saml.Attribute {
	return saml.Attribute{
		Name: name, NameFormat: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic",
		Values: []saml.AttributeValue{{Type: "xs:string", Value: value}},
	}
}

// Run startet den Mock und kehrt nur bei einem Fehler zurück (Exit-Code 1).
func Run(getenv func(string) string) int {
	cfg := FromEnv(getenv)
	srv := newServer(cfg)
	slog.Info("Mock-SAML-IdP gestartet", "addr", cfg.Addr, "metadata", cfg.BaseURL+"/metadata", "acs_prefix", cfg.ACSPrefix)
	slog.Error("Mock-SAML-IdP beendet", "error", srv.ListenAndServe())
	return 1
}

// newServer: ReadHeaderTimeout gegen langsame Clients (gosec G112).
func newServer(cfg Config) *http.Server {
	return &http.Server{Addr: cfg.Addr, Handler: New(cfg), ReadHeaderTimeout: 10 * time.Second}
}
