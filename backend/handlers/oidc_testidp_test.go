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

// testIdP is a minimal OIDC provider for the tests:
// discovery, JWKS and token endpoint including PKCE verification. The ID token is
// signed with a real RSA key so that the signature check by
// go-oidc actually takes effect.
type testIdP struct {
	t   *testing.T
	srv *httptest.Server

	mu            sync.Mutex
	discoveryHits int
	challenge     string // from the login's authorize URL
	nonce         string // from the login's authorize URL
	tokenStatus   int    // != 0: token endpoint answers with this status
	omitIDToken   bool
	// authEndpoint/tokenEndpoint: != "" replaces the address in the discovery document.
	authEndpoint  string
	tokenEndpoint string
	signWith      *rsa.PrivateKey // nil: key from the JWKS
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

// newTestIdP starts the provider and redirects oidcHTTPClient for the duration of the
// test to its TLS client. The provider cache is cleared before and after
// so tests do not pass discovery results on to each other.
func newTestIdP(t *testing.T) *testIdP {
	t.Helper()
	idp := newTestIdPServer(t, httptest.NewTLSServer)
	prevClient, prevTrusted := oidcHTTPClient, oidcTrustedHTTPClient
	oidcHTTPClient = idp.srv.Client()
	// The global providers use the trusted client; it gets the test
	// server's TLS client as well.
	oidcTrustedHTTPClient = idp.srv.Client()
	resetOIDCProviderCache()
	t.Cleanup(func() {
		oidcHTTPClient, oidcTrustedHTTPClient = prevClient, prevTrusted
		resetOIDCProviderCache()
	})
	return idp
}

// newPlainHTTPTestIdP starts the provider without TLS on
// 127.0.0.1, like the mock OIDC service from docker-compose.yml. oidcHTTPClient
// remains the real client with SSRF protection: if a login gets
// through anyway, the developer client took effect.
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

// signIDToken builds a valid ID token; idp.claims overrides individual
// claims (e.g. aud, exp, nonce) for the error cases.
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
