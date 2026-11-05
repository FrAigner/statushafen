package handlers

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

// testIdP ist ein minimaler OIDC-Anbieter für die Tests:
// Discovery, JWKS und Token-Endpunkt inkl. PKCE-Prüfung. Das ID-Token wird
// mit einem echten RSA-Schlüssel signiert, damit die Signaturprüfung von
// go-oidc wirklich greift.
type testIdP struct {
	t   *testing.T
	srv *httptest.Server

	mu            sync.Mutex
	discoveryHits int
	challenge     string // aus der Authorize-URL des Logins
	nonce         string // aus der Authorize-URL des Logins
	tokenStatus   int    // != 0: Token-Endpunkt antwortet mit diesem Status
	omitIDToken   bool
	// authEndpoint/tokenEndpoint: != "" ersetzt die Adresse im Discovery-Dokument.
	authEndpoint  string
	tokenEndpoint string
	signWith      *rsa.PrivateKey // nil: Schlüssel aus dem JWKS
	claims        map[string]interface{}
}

var (
	testIdPKeyOnce sync.Once
	testIdPKey     *rsa.PrivateKey
	testIdPOther   *rsa.PrivateKey
)

func testIdPKeys(t *testing.T) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	testIdPKeyOnce.Do(func() {
		testIdPKey, _ = rsa.GenerateKey(rand.Reader, 2048)
		testIdPOther, _ = rsa.GenerateKey(rand.Reader, 2048)
	})
	return testIdPKey, testIdPOther
}

const testIdPClientID = "statuspage-client"

// newTestIdP startet den Anbieter und biegt oidcHTTPClient für die Dauer des
// Tests auf dessen TLS-Client um. Der Provider-Cache wird vorher und nachher
// geleert, damit Tests sich nicht gegenseitig Discovery-Ergebnisse vererben.
func newTestIdP(t *testing.T) *testIdP {
	t.Helper()
	idp := newTestIdPServer(t, httptest.NewTLSServer)
	prevClient := oidcHTTPClient
	oidcHTTPClient = idp.srv.Client()
	resetOIDCProviderCache()
	t.Cleanup(func() {
		oidcHTTPClient = prevClient
		resetOIDCProviderCache()
	})
	return idp
}

// newPlainHTTPTestIdP startet den Anbieter ohne TLS auf
// 127.0.0.1, wie der Mock-OIDC-Dienst aus docker-compose.yml. oidcHTTPClient
// bleibt dabei der echte Client mit SSRF-Schutz: Kommt ein Login trotzdem
// durch, hat der Entwickler-Client gegriffen.
func newPlainHTTPTestIdP(t *testing.T) *testIdP {
	t.Helper()
	idp := newTestIdPServer(t, httptest.NewServer)
	prevClient := oidcHTTPClient
	oidcHTTPClient = defaultOIDCHTTPClient()
	resetOIDCProviderCache()
	t.Cleanup(func() {
		oidcHTTPClient = prevClient
		resetOIDCProviderCache()
	})
	return idp
}

func newTestIdPServer(t *testing.T, start func(http.Handler) *httptest.Server) *testIdP {
	t.Helper()
	key, _ := testIdPKeys(t)
	idp := &testIdP{t: t, claims: map[string]interface{}{}}
	mux := http.NewServeMux()
	idp.srv = start(mux)
	t.Cleanup(idp.srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		idp.mu.Lock()
		idp.discoveryHits++
		authEndpoint, tokenEndpoint := idp.srv.URL+"/authorize", idp.srv.URL+"/token"
		if idp.authEndpoint != "" {
			authEndpoint = idp.authEndpoint
		}
		if idp.tokenEndpoint != "" {
			tokenEndpoint = idp.tokenEndpoint
		}
		idp.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                                idp.srv.URL,
			"authorization_endpoint":                authEndpoint,
			"token_endpoint":                        tokenEndpoint,
			"jwks_uri":                              idp.srv.URL + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", idp.handleToken)
	return idp
}

func (idp *testIdP) handleToken(w http.ResponseWriter, r *http.Request) {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	if idp.tokenStatus != 0 {
		w.WriteHeader(idp.tokenStatus)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	if r.FormValue("code") != "good-code" ||
		oauth2.S256ChallengeFromVerifier(r.FormValue("code_verifier")) != idp.challenge {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	resp := map[string]interface{}{"access_token": "at", "token_type": "Bearer", "expires_in": 300}
	if !idp.omitIDToken {
		resp["id_token"] = idp.signIDToken()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// signIDToken baut ein gültiges ID-Token; idp.claims überschreibt einzelne
// Claims (z. B. aud, exp, nonce) für die Fehlerfälle.
func (idp *testIdP) signIDToken() string {
	key, _ := testIdPKeys(idp.t)
	if idp.signWith != nil {
		key = idp.signWith
	}
	claims := map[string]interface{}{
		"iss":   idp.srv.URL,
		"aud":   testIdPClientID,
		"sub":   "idp-user-1",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(5 * time.Minute).Unix(),
		"nonce": idp.nonce,
		"name":  "Erika Muster",
	}
	for k, v := range idp.claims {
		claims[k] = v
	}
	payload, _ := json.Marshal(claims)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}},
		(&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		idp.t.Fatalf("signer: %v", err)
	}
	obj, err := signer.Sign(payload)
	if err != nil {
		idp.t.Fatalf("sign: %v", err)
	}
	raw, _ := obj.CompactSerialize()
	return raw
}

func (idp *testIdP) hits() int {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	return idp.discoveryHits
}
