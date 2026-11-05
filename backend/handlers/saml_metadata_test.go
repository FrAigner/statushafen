package handlers

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"encoding/xml"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// IdP-Metadaten per URL (SSRF-Schutz, Cache) und Parser, dazu die
// SP-Schlüssel je Seite.

type samlMetadataServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits int
	body string
	code int
}

// newSAMLMetadataServer startet einen TLS-Server für die IdP-Metadaten und
// biegt samlHTTPClient für die Dauer des Tests auf dessen Client um.
func newSAMLMetadataServer(t *testing.T, body string) *samlMetadataServer {
	t.Helper()
	ms := &samlMetadataServer{body: body, code: http.StatusOK}
	ms.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ms.mu.Lock()
		defer ms.mu.Unlock()
		ms.hits++
		w.WriteHeader(ms.code)
		_, _ = w.Write([]byte(ms.body))
	}))
	t.Cleanup(ms.srv.Close)
	prev := samlHTTPClient
	samlHTTPClient = ms.srv.Client()
	resetSAMLMetadataCache()
	t.Cleanup(func() {
		samlHTTPClient = prev
		resetSAMLMetadataCache()
	})
	return ms
}

func (ms *samlMetadataServer) set(code int, body string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.code, ms.body = code, body
}

func (ms *samlMetadataServer) hitCount() int {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.hits
}

// useMetadataURL stellt das Fixture von eingefügtem XML auf den URL um.
func (f *samlFixture) useMetadataURL(t *testing.T, u string) {
	t.Helper()
	require.NoError(t, f.db.Model(&models.StatusPageSSOConfig{}).Where("status_page_id = ?", f.page.ID).
		Updates(map[string]interface{}{"saml_idp_metadata_xml": "", "saml_idp_metadata_url": u}).Error)
}

func TestSAMLMetadataURL_LoginWorksAndIsCached(t *testing.T) {
	f := newSAMLFixture(t)
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	ms := newSAMLMetadataServer(t, f.idp.metadataXML())
	f.useMetadataURL(t, ms.srv.URL+"/metadata")
	f.linkIdentity(t, f.user.ID, "saml-user-1")

	jwtFromSAMLRedirect(t, f.loginWith(t, samlRespOpts{}))
	jwtFromSAMLRedirect(t, f.loginWith(t, samlRespOpts{}))

	// Login-Start und ACS zweimal, ein einziger Abruf.
	assert.Equal(t, 1, ms.hitCount())
}

func TestSAMLMetadataURL_CacheExpires(t *testing.T) {
	f := newSAMLFixture(t)
	ms := newSAMLMetadataServer(t, f.idp.metadataXML())
	f.useMetadataURL(t, ms.srv.URL+"/metadata")
	now := time.Now()
	prevNow := oidcNow
	oidcNow = func() time.Time { return now }
	t.Cleanup(func() { oidcNow = prevNow })

	f.startLogin(t)
	now = now.Add(samlMetadataTTL() - time.Second)
	f.startLogin(t)
	assert.Equal(t, 1, ms.hitCount())
	now = now.Add(2 * time.Second)
	f.startLogin(t)
	assert.Equal(t, 2, ms.hitCount())
}

func TestSAMLMetadataURL_Errors(t *testing.T) {
	f := newSAMLFixture(t)
	ms := newSAMLMetadataServer(t, "")
	login := func() *httptest.ResponseRecorder {
		resetSAMLMetadataCache()
		return serveOIDC(f.h.SAMLLoginHandler, http.MethodGet, "/api/auth/saml/login?page="+f.pageParam(), nil, 0)
	}

	t.Run("HTTP-Fehler", func(t *testing.T) {
		f.useMetadataURL(t, ms.srv.URL+"/metadata")
		ms.set(http.StatusInternalServerError, "")
		assertOIDCError(t, login(), "/login", "provider_unreachable")
	})
	t.Run("kein XML", func(t *testing.T) {
		f.useMetadataURL(t, ms.srv.URL+"/metadata")
		ms.set(http.StatusOK, "<html>")
		assertOIDCError(t, login(), "/login", "metadata_invalid")
	})
	t.Run("ohne https", func(t *testing.T) {
		f.useMetadataURL(t, strings.Replace(ms.srv.URL, "https://", "http://", 1)+"/metadata")
		assertOIDCError(t, login(), "/login", "provider_unreachable")
	})
	t.Run("nicht erreichbar", func(t *testing.T) {
		f.useMetadataURL(t, "https://127.0.0.1:1/metadata")
		assertOIDCError(t, login(), "/login", "provider_unreachable")
	})
}

// TestSAMLMetadataURL_FailureIsCachedBriefly: Ein Fehlschlag gilt eine Minute,
// danach wird neu abgerufen.
func TestSAMLMetadataURL_FailureIsCachedBriefly(t *testing.T) {
	f := newSAMLFixture(t)
	ms := newSAMLMetadataServer(t, "")
	ms.set(http.StatusServiceUnavailable, "")
	f.useMetadataURL(t, ms.srv.URL+"/metadata")
	now := time.Now()
	prevNow := oidcNow
	oidcNow = func() time.Time { return now }
	t.Cleanup(func() { oidcNow = prevNow })
	login := func() *httptest.ResponseRecorder {
		return serveOIDC(f.h.SAMLLoginHandler, http.MethodGet, "/api/auth/saml/login?page="+f.pageParam(), nil, 0)
	}

	assertOIDCError(t, login(), "/login", "provider_unreachable")
	ms.set(http.StatusOK, f.idp.metadataXML())
	assertOIDCError(t, login(), "/login", "provider_unreachable")
	assert.Equal(t, 1, ms.hitCount())

	now = now.Add(samlMetadataFailureTTL() + time.Second)
	assert.Equal(t, http.StatusTemporaryRedirect, login().Code)
	assert.Equal(t, 2, ms.hitCount())
}

func TestLoadIdPMetadata_UsesFreshCacheEntry(t *testing.T) {
	resetSAMLMetadataCache()
	t.Cleanup(resetSAMLMetadataCache)
	want := samlMetadataEntry{err: errSAMLMetadataInvalid, expires: time.Now().Add(time.Minute)}
	samlMetadataCache["https://idp.example.test/x"] = want
	assert.Equal(t, want, loadIdPMetadata("https://idp.example.test/x"))
}

func TestDefaultSAMLHTTPClient_BlocksInternalAddresses(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)
	client := defaultSAMLHTTPClient()
	assert.Equal(t, 10*time.Second, client.Timeout)
	_, err := client.Get(srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interne Adresse nicht erlaubt")
}

func TestFetchSAMLMetadata_LimitsSize(t *testing.T) {
	ms := newSAMLMetadataServer(t, strings.Repeat("x", samlMetadataMaxBytes+10))
	raw, err := fetchSAMLMetadata(ms.srv.URL)
	require.NoError(t, err)
	assert.Len(t, raw, samlMetadataMaxBytes)
}

func TestParseIdPMetadata(t *testing.T) {
	ti := newSAMLTestIdP(t, nil)
	valid := ti.metadataXML()

	md, err := parseIdPMetadata([]byte(valid))
	require.NoError(t, err)
	assert.Equal(t, ti.entityID(), md.EntityID)
	assert.Equal(t, samlTestIdPBase+"/sso", idpRedirectSSOURL(md))

	t.Run("EntitiesDescriptor: erster Eintrag mit IdP-Rolle", func(t *testing.T) {
		inner := strings.Replace(valid, `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata"`, `<EntityDescriptor`, 1)
		sp := `<EntityDescriptor entityID="https://sp.example"><SPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol"></SPSSODescriptor></EntityDescriptor>`
		raw := `<EntitiesDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata">` + sp + inner + `</EntitiesDescriptor>`
		md, err := parseIdPMetadata([]byte(raw))
		require.NoError(t, err)
		assert.Equal(t, ti.entityID(), md.EntityID)
	})
	for name, raw := range map[string]string{
		"kein XML":                "kein xml",
		"anderes Wurzelement":     `<foo/>`,
		"ohne IdP-Rolle":          `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="x"></EntityDescriptor>`,
		"ohne entityID":           strings.Replace(valid, `entityID="`+ti.entityID()+`"`, "", 1),
		"ohne Redirect-SSO":       strings.Replace(valid, "HTTP-Redirect", "HTTP-Artifact", 1),
		"ohne Signaturzertifikat": strings.Replace(valid, `use="signing"`, `use="encryption"`, 1),
		// xrv lehnt ab, was encoding/xml anders liest als es schreibt.
		"Roundtrip-Trick": `<a:b:c xmlns:a="x"/>`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseIdPMetadata([]byte(raw))
			assert.Error(t, err)
		})
	}
	t.Run("Signatur-KeyDescriptor ohne Zertifikat zählt nicht", func(t *testing.T) {
		md := ti.idp.Metadata()
		for i := range md.IDPSSODescriptors[0].KeyDescriptors {
			if md.IDPSSODescriptors[0].KeyDescriptors[i].Use == "signing" {
				md.IDPSSODescriptors[0].KeyDescriptors[i].KeyInfo.X509Data.X509Certificates = nil
			}
		}
		raw, err := xml.Marshal(md)
		require.NoError(t, err)
		_, err = parseIdPMetadata(raw)
		assert.EqualError(t, err, "kein Signaturzertifikat in den Metadaten")
	})
	t.Run("Zertifikat ohne use zählt als Signaturzertifikat", func(t *testing.T) {
		_, err := parseIdPMetadata([]byte(strings.Replace(valid, ` use="signing"`, "", 1)))
		assert.NoError(t, err)
	})
}

func TestSAMLKeyPair(t *testing.T) {
	keyPEM, certPEM := generateSAMLKeyPair(42)
	key, cert, err := parseSAMLKeyPair(keyPEM, certPEM)
	require.NoError(t, err)
	assert.Equal(t, 2048, key.N.BitLen())
	assert.Equal(t, "statuspage SAML SP page 42", cert.Subject.CommonName)
	assert.Equal(t, key.Public().(*rsa.PublicKey).N, cert.PublicKey.(*rsa.PublicKey).N)
	// Zehn Jahre, als feste Zahl (nicht über samlCertLifetime, sonst prüfte
	// der Test die Funktion gegen sich selbst).
	assert.WithinDuration(t, time.Now().Add(87600*time.Hour), cert.NotAfter, time.Minute)
	assert.True(t, cert.NotBefore.Before(time.Now()))

	other, _ := generateSAMLKeyPair(42)
	assert.NotEqual(t, keyPEM, other)

	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ecDER, _ := x509.MarshalPKCS8PrivateKey(ecKey)
	ecPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecDER}))
	garbage := string(pem.EncodeToMemory(&pem.Block{Type: "X", Bytes: []byte("kaputt")}))
	for name, pair := range map[string][2]string{
		"Zertifikat kein PEM": {keyPEM, "kein PEM"},
		"Zertifikat kaputt":   {keyPEM, garbage},
		"Schlüssel kein PEM":  {"kein PEM", certPEM},
		"Schlüssel kaputt":    {garbage, certPEM},
		"Schlüssel nicht RSA": {ecPEM, certPEM},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := parseSAMLKeyPair(pair[0], pair[1])
			assert.Error(t, err)
		})
	}
}

// TestFetchSAMLMetadata_Redirects (Security-Review): Redirects
// nur auf https und höchstens drei Sprünge; die https-Prüfung galt vorher nur
// für den ersten URL.
func TestFetchSAMLMetadata_Redirects(t *testing.T) {
	SetWebhookLoopbackAllowedForTests(t.Cleanup)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<plaintext-metadata/>"))
	}))
	t.Cleanup(plain.Close)
	hops := 0
	var tlsSrv *httptest.Server
	tlsSrv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/zu-http":
			http.Redirect(w, r, plain.URL+"/md", http.StatusFound)
		case "/kette":
			hops++
			http.Redirect(w, r, tlsSrv.URL+"/kette", http.StatusFound)
		case "/einmal":
			http.Redirect(w, r, tlsSrv.URL+"/ziel", http.StatusFound)
		default:
			_, _ = w.Write([]byte("<ok/>"))
		}
	}))
	t.Cleanup(tlsSrv.Close)
	prev := samlHTTPClient
	samlHTTPClient = tlsSrv.Client()
	t.Cleanup(func() { samlHTTPClient = prev })

	_, err := fetchSAMLMetadata(tlsSrv.URL + "/zu-http")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")

	_, err = fetchSAMLMetadata(tlsSrv.URL + "/kette")
	require.Error(t, err)
	assert.Equal(t, 4, hops, "Start plus drei Sprünge")

	body, err := fetchSAMLMetadata(tlsSrv.URL + "/einmal")
	require.NoError(t, err)
	assert.Equal(t, "<ok/>", string(body))
	// Der globale Client bleibt unverändert.
	assert.Nil(t, samlHTTPClient.CheckRedirect)
}

// TestIdPMetadataFor_CachesPastedXML): Eingefügtes
// XML wird je Seite einmal geparst und nur neu gelesen, wenn es sich ändert.
func TestIdPMetadataFor_CachesPastedXML(t *testing.T) {
	resetSAMLMetadataCache()
	t.Cleanup(resetSAMLMetadataCache)
	ti := newSAMLTestIdP(t, nil)
	xmlA := ti.metadataXML()
	cfg := models.StatusPageSSOConfig{StatusPageID: 7, SAMLIdPMetadataXML: xmlA}

	first, e := idpMetadataFor(cfg)
	require.Nil(t, e)
	second, e := idpMetadataFor(cfg)
	require.Nil(t, e)
	assert.Same(t, first, second, "zweiter Aufruf aus dem Cache")

	other := models.StatusPageSSOConfig{StatusPageID: 8, SAMLIdPMetadataXML: xmlA}
	third, _ := idpMetadataFor(other)
	assert.NotSame(t, first, third, "Cache je Seite")

	cfg.SAMLIdPMetadataXML = strings.Replace(xmlA, ti.entityID(), "https://neu.example/metadata", 1)
	changed, e := idpMetadataFor(cfg)
	require.Nil(t, e)
	assert.Equal(t, "https://neu.example/metadata", changed.EntityID)

	cfg.SAMLIdPMetadataXML = "<kaputt"
	_, e = idpMetadataFor(cfg)
	assert.Equal(t, errSAMLMetadataInvalid, e)
	_, e = idpMetadataFor(cfg)
	assert.Equal(t, errSAMLMetadataInvalid, e, "auch der Fehler kommt aus dem Cache")
}

// Fehlschläge beim Abruf der IdP-Metadaten loggen die URL ohne Query, Userinfo
// und Fragment: Ein Geheimnis in der konfigurierten URL darf selbst bei
// einem Standard-Logger ohne zentrale Bereinigung nicht im Log landen, weder
// im url-Feld noch im Fehlertext des HTTP-Clients.
func TestLoadIdPMetadata_LogGibtKeineQueryPreis(t *testing.T) {
	const secret = "geheim-abruf-token-4711"
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	t.Run("HTTP-Fehlerstatus", func(t *testing.T) {
		ms := newSAMLMetadataServer(t, "")
		ms.set(http.StatusServiceUnavailable, "")
		loadIdPMetadata(ms.srv.URL + "/metadata?token=" + secret + "#frag-" + secret)
	})
	t.Run("Netzwerkfehler", func(t *testing.T) {
		ms := newSAMLMetadataServer(t, "")
		base := ms.srv.URL
		ms.srv.Close()
		loadIdPMetadata(base + "/metadata?token=" + secret)
	})
	t.Run("ungültiger Inhalt", func(t *testing.T) {
		ms := newSAMLMetadataServer(t, "<kein-xml")
		loadIdPMetadata(ms.srv.URL + "/metadata?token=" + secret)
	})
	t.Run("ungültiger Inhalt nennt eine URL im Fehlertext", func(t *testing.T) {
		// Der XML-Parser nennt die Kodierung aus der Kopfzeile im Fehlertext; steht
		// dort eine URL mit Geheimnis, darf sie nicht ins Log.
		ms := newSAMLMetadataServer(t, `<?xml version="1.0" encoding="https://idp.example.org/enc?token=`+secret+`-enc"?><a/>`)
		loadIdPMetadata(ms.srv.URL + "/metadata")
	})

	assert.Contains(t, buf.String(), "SAML metadata fetch failed", "Gegenprobe: die Fehlschläge werden geloggt")
	assert.Contains(t, buf.String(), "SAML metadata invalid")
	assert.Contains(t, buf.String(), "/metadata", "Host und Pfad bleiben zum Zuordnen stehen")
	assert.NotContains(t, buf.String(), secret)
}
