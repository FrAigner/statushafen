package handlers_test

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Attrappen-Anbieter für Google und GitHub: Der Client antwortet selbst, es geht
// kein Verkehr ins Netz. Er zählt die Aufrufe und merkt sich, was der Server beim
// Token-Tausch gesendet hat.
type fakeOAuthProvider struct {
	mu        sync.Mutex
	calls     int
	tokenForm []url.Values
	failToken bool
}

func (f *fakeOAuthProvider) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	reply := func(status int, body string) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}
	switch req.URL.String() {
	case "https://oauth2.googleapis.com/token", "https://github.com/login/oauth/access_token":
		raw, _ := io.ReadAll(req.Body)
		form, _ := url.ParseQuery(string(raw))
		f.tokenForm = append(f.tokenForm, form)
		if f.failToken {
			return reply(http.StatusBadRequest, `{"error":"invalid_grant"}`)
		}
		return reply(http.StatusOK, `{"access_token":"fake-access-token","token_type":"Bearer"}`)
	case "https://www.googleapis.com/oauth2/v2/userinfo":
		return reply(http.StatusOK, `{"id":"g-1001","email":"oauth-nutzer@example.com","name":"Oda Auth","verified_email":true}`)
	case "https://api.github.com/user/emails":
		return reply(http.StatusOK, `[{"email":"oauth-nutzer@example.com","primary":true,"verified":true}]`)
	case "https://api.github.com/user":
		return reply(http.StatusOK, `{"id":2002,"login":"oda","name":"Oda Auth"}`)
	}
	return reply(http.StatusNotFound, `{}`)
}

func (f *fakeOAuthProvider) outbound() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type oauthEnv struct {
	t        *testing.T
	h        *handlers.Handler
	r        http.Handler
	provider *fakeOAuthProvider
	now      time.Time
}

func newOAuthEnv(t *testing.T) *oauthEnv {
	t.Helper()
	t.Setenv("GOOGLE_CLIENT_ID", "google-client-id.example.com")
	t.Setenv("GOOGLE_CLIENT_SECRET", "unit-test-google-secret")
	t.Setenv("GITHUB_CLIENT_ID", "github-client-id")
	t.Setenv("GITHUB_CLIENT_SECRET", "unit-test-github-secret")
	t.Setenv("BASE_URL", "https://status.example.com")
	t.Setenv("FRONTEND_URL", "https://status.example.com")
	t.Setenv("SSO_ALLOWED_DOMAINS", "")
	db := testutils.SetupTestDB()
	require.NoError(t, db.Create(&models.Role{Name: "Viewer"}).Error)
	e := &oauthEnv{t: t, provider: &fakeOAuthProvider{}, now: time.Date(2031, 3, 4, 10, 0, 0, 0, time.UTC)}
	e.h = handlers.NewHandler(db)
	e.h.OAuthClient = &http.Client{Transport: e.provider}
	e.h.Now = func() time.Time { return e.now }
	r := testutils.SetupRouter()
	r.GET("/api/auth/:provider", e.h.OAuthBeginHandler)
	r.GET("/api/auth/:provider/callback", e.h.OAuthCallbackHandler)
	e.r = r
	return e
}

type oauthBegin struct {
	redirect *url.URL
	cookie   *http.Cookie
	state    string
}

func (e *oauthEnv) begin(provider string) oauthBegin {
	e.t.Helper()
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/auth/"+provider, nil))
	require.Equal(e.t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(e.t, err)
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "oauth_state" {
			cookie = c
		}
	}
	require.NotNil(e.t, cookie, "State-Cookie fehlt")
	return oauthBegin{redirect: loc, cookie: cookie, state: loc.Query().Get("state")}
}

func (e *oauthEnv) callback(provider, state string, cookie *http.Cookie) *httptest.ResponseRecorder {
	e.t.Helper()
	target := "/api/auth/" + provider + "/callback?code=provider-code&state=" + url.QueryEscape(state)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if cookie != nil {
		req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// PKCE: Die Authorize-URL trägt code_challenge (S256), der Tausch den
// passenden code_verifier.
func TestOAuthLogin_UsesPKCEForGoogleAndGitHub(t *testing.T) {
	for _, provider := range []string{"google", "github"} {
		t.Run(provider, func(t *testing.T) {
			e := newOAuthEnv(t)
			b := e.begin(provider)
			q := b.redirect.Query()
			assert.Equal(t, "S256", q.Get("code_challenge_method"))
			challenge := q.Get("code_challenge")
			require.NotEmpty(t, challenge)

			w := e.callback(provider, b.state, b.cookie)
			require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
			require.Len(t, e.provider.tokenForm, 1)
			verifier := e.provider.tokenForm[0].Get("code_verifier")
			require.NotEmpty(t, verifier, "der Tausch sendet den code_verifier")
			assert.Equal(t, challenge, s256(verifier), "Verifier und Challenge gehören zusammen")
		})
	}
}

func TestOAuthLogin_StateCookieAttributes(t *testing.T) {
	e := newOAuthEnv(t)
	c := e.begin("google").cookie
	assert.True(t, c.HttpOnly)
	assert.True(t, c.Secure)
	assert.Equal(t, http.SameSiteLaxMode, c.SameSite)
	assert.Equal(t, 300, c.MaxAge)
	assert.NotContains(t, e.begin("google").state, ".", "State ist ein reiner Zufallswert")
}

// Gegenprobe: Der ganze Ablauf mit echtem Cookie meldet an, legt das Konto an und
// leitet mit Einmal-Code ins Frontend.
func TestOAuthLogin_FullFlowSignsInAndClearsCookie(t *testing.T) {
	e := newOAuthEnv(t)
	b := e.begin("github")
	w := e.callback("github", b.state, b.cookie)
	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	assert.True(t, strings.HasPrefix(w.Header().Get("Location"), "https://status.example.com/auth/sso-callback?code="))

	var cleared bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "oauth_state" && c.MaxAge < 0 {
			cleared = true
		}
	}
	assert.True(t, cleared, "das State-Cookie wird gelöscht")
	var n int64
	require.NoError(t, e.h.DB.Model(&models.User{}).Where("email = ?", "oauth-nutzer@example.com").Count(&n).Error)
	assert.EqualValues(t, 1, n)
	assert.Equal(t, 3, e.provider.outbound(), "Token, E-Mails und Profil")
}

// Angriff: Wer State und Cookie selbst wählt (Double-Submit mit eigenem Wert),
// löste bisher einen Aufruf beim Anbieter aus. Jetzt weist der Server jede
// Anfrage ab, deren State er nicht selbst ausgestellt hat, ohne den Anbieter
// zu rufen.
func TestOAuthCallback_ForgedOrForeignStateNeverCallsProvider(t *testing.T) {
	e := newOAuthEnv(t)
	real := e.begin("google")
	other := e.begin("google")
	githubCookie := e.begin("github").cookie
	tampered := &http.Cookie{Name: "oauth_state", Value: real.cookie.Value[:len(real.cookie.Value)-2] + "xx"}

	cases := []struct {
		name     string
		provider string
		state    string
		cookie   *http.Cookie
	}{
		{"frei gewählt, Cookie gleich State", "google", "abc", &http.Cookie{Name: "oauth_state", Value: "abc"}},
		{"ohne Cookie", "google", real.state, nil},
		{"weder Cookie noch State", "google", "", nil},
		{"Cookie ohne Wert, State leer", "google", "", &http.Cookie{Name: "oauth_state", Value: ""}},
		{"leerer State", "google", "", real.cookie},
		{"Signatur verändert", "google", real.state, tampered},
		{"State aus anderer Anmeldung", "google", other.state, real.cookie},
		{"Cookie eines anderen Anbieters", "google", real.state, githubCookie},
		{"Cookie ohne Verifier (alte Form)", "google", real.state, &http.Cookie{Name: "oauth_state", Value: real.state}},
		{"Unsinn im Cookie", "google", real.state, &http.Cookie{Name: "oauth_state", Value: "a.b.c.d.e.f"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := e.provider.outbound()
			w := e.callback(tc.provider, tc.state, tc.cookie)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"code":"oauth_state_invalid"`)
			assert.Equal(t, before, e.provider.outbound(), "kein Aufruf beim Anbieter")
		})
	}
	assert.Zero(t, e.provider.outbound())
}

func TestOAuthCallback_StateExpiresAndWorksOnlyOnce(t *testing.T) {
	e := newOAuthEnv(t)

	old := e.begin("google")
	e.now = e.now.Add(5*time.Minute + time.Second)
	w := e.callback("google", old.state, old.cookie)
	assert.Equal(t, http.StatusBadRequest, w.Code, "abgelaufen")
	assert.Zero(t, e.provider.outbound())

	fresh := e.begin("google")
	require.Equal(t, http.StatusTemporaryRedirect, e.callback("google", fresh.state, fresh.cookie).Code)
	calls := e.provider.outbound()
	replay := e.callback("google", fresh.state, fresh.cookie)
	assert.Equal(t, http.StatusBadRequest, replay.Code, "Wiederholung desselben Cookies")
	assert.Equal(t, calls, e.provider.outbound(), "und ohne Aufruf beim Anbieter")
}

func TestOAuthCallback_ProviderRejectsExchangeReturns500WithoutLeak(t *testing.T) {
	e := newOAuthEnv(t)
	e.provider.failToken = true
	b := e.begin("google")
	w := e.callback("google", b.state, b.cookie)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "invalid_grant")
	assert.NotContains(t, w.Body.String(), "unit-test-google-secret")
}

func TestOAuthCallback_UnknownProviderAndMissingCodeKeepTheirAnswers(t *testing.T) {
	e := newOAuthEnv(t)
	b := e.begin("google")
	w := e.callback("bogus", b.state, b.cookie)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"provider_unknown"`)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?state="+b.state, nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: b.cookie.Value})
	rec := httptest.NewRecorder()
	e.r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"auth_code_missing"`)
	assert.Zero(t, e.provider.outbound())
}

// Ein Geheimnis im Anbieter-Fehler (Antwort, Log) gehört nirgends hin: Der
// Tausch-Fehler wird protokolliert, ohne das Client-Secret zu enthalten.
func TestOAuthCallback_LogDoesNotContainClientSecret(t *testing.T) {
	e := newOAuthEnv(t)
	e.provider.failToken = true
	logs := captureLog(t)
	b := e.begin("github")
	e.callback("github", b.state, b.cookie)
	assert.NotContains(t, logs.String(), "unit-test-github-secret")
	assert.NotContains(t, logs.String(), b.cookie.Value)
}
