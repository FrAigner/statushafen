package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/mocksamlidp"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// SAML-Anmeldung je Statusseite gegen einen IdP im Testprozess
// (saml_testidp_test.go). Geprüft werden Metadaten, Login-Start, ACS mit allen
// Prüfungen (Signatur, Audience, Recipient, Ablauf, InResponseTo, Replay) und
// die Konto-Regeln wie beim OIDC-Login.

type samlFixture struct {
	h    *Handler
	db   *gorm.DB
	idp  *samlTestIdP
	page models.StatusPage
	user models.User
}

func newSAMLFixture(t *testing.T) *samlFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("BASE_URL", "https://status.example.org")
	db := testutils.SetupTestDB()
	role := models.Role{Name: "Viewer", Permissions: "incidents:read"}
	require.NoError(t, db.Create(&role).Error)
	page := models.StatusPage{Name: "SAML-Seite", Slug: "saml-seite"}
	require.NoError(t, db.Create(&page).Error)
	user := models.User{Email: "erika@example.org", RoleID: role.ID}
	require.NoError(t, db.Create(&user).Error)
	f := &samlFixture{h: NewHandler(db), db: db, page: page, user: user}
	f.idp = newSAMLTestIdP(t, f.spMetadata)

	keyPEM, certPEM := generateSAMLKeyPair(page.ID)
	require.NoError(t, db.Create(&models.StatusPageSSOConfig{
		StatusPageID:       page.ID,
		SAMLEnabled:        true,
		SAMLIdPMetadataXML: f.idp.metadataXML(),
		SAMLAttributeEmail: "email",
		SAMLAttributeName:  "name",
		SAMLSPKey:          keyPEM,
		SAMLSPCertificate:  certPEM,
		TestLoginEnabled:   true,
	}).Error)
	resetSAMLMetadataCache()
	t.Cleanup(resetSAMLMetadataCache)
	return f
}

func (f *samlFixture) pageParam() string { return strconv.FormatUint(uint64(f.page.ID), 10) }

func (f *samlFixture) updateSSO(t *testing.T, column string, value interface{}) {
	t.Helper()
	require.NoError(t, f.db.Model(&models.StatusPageSSOConfig{}).Where("status_page_id = ?", f.page.ID).
		Update(column, value).Error)
}

// spMetadata holt die SP-Metadaten über den echten Endpunkt.
func (f *samlFixture) spMetadata() *saml.EntityDescriptor {
	w := serveWithParams(f.h.SAMLMetadataHandler, http.MethodGet, "/", gin.Params{{Key: "page_id", Value: f.pageParam()}}, 0)
	if w.Code != http.StatusOK {
		return nil
	}
	var ed saml.EntityDescriptor
	if xml.Unmarshal(w.Body.Bytes(), &ed) != nil {
		return nil
	}
	return &ed
}

func (f *samlFixture) linkIdentity(t *testing.T, userID uint, nameID string) {
	t.Helper()
	require.NoError(t, f.db.Create(&models.UserSSOIdentity{
		UserID: userID, StatusPageID: f.page.ID, Protocol: models.SSOProtocolSAML, Issuer: f.idp.entityID(), Subject: nameID,
		SubjectFormat: string(saml.PersistentNameIDFormat),
	}).Error)
}

func samlStateCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, ck := range w.Result().Cookies() {
		if ck.Name == samlStateCookieName {
			return ck
		}
	}
	return nil
}

// startLogin ruft den Login-Start auf und liefert die Redirect-URL zum IdP
// samt State-Cookie.
func (f *samlFixture) startLogin(t *testing.T) (string, *http.Cookie) {
	t.Helper()
	w := serveOIDC(f.h.SAMLLoginHandler, http.MethodGet, "/api/auth/saml/login?page="+f.pageParam(), nil, 0)
	require.Equal(t, http.StatusTemporaryRedirect, w.Code, w.Body.String())
	return w.Header().Get("Location"), samlStateCookie(w)
}

func (f *samlFixture) acs(samlResponse, relayState string, cookie *http.Cookie) *httptest.ResponseRecorder {
	return f.acsFor(f.pageParam(), samlResponse, relayState, cookie)
}

func (f *samlFixture) acsFor(pageID, samlResponse, relayState string, cookie *http.Cookie) *httptest.ResponseRecorder {
	form := url.Values{}
	if samlResponse != "" {
		form.Set("SAMLResponse", samlResponse)
	}
	if relayState != "" {
		form.Set("RelayState", relayState)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/saml/"+pageID+"/acs", strings.NewReader(form.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		c.Request.AddCookie(cookie)
	}
	c.Params = gin.Params{{Key: "page_id", Value: pageID}}
	f.h.SAMLACSHandler(c)
	// Bei POST schreibt http.Redirect keinen Body; gin schickt den Status dann
	// erst nach dem Handler (im Router), hier also von Hand.
	c.Writer.WriteHeaderNow()
	return w
}

// loginWith spielt Start, IdP-Antwort und ACS komplett durch.
func (f *samlFixture) loginWith(t *testing.T, o samlRespOpts) *httptest.ResponseRecorder {
	t.Helper()
	redirect, cookie := f.startLogin(t)
	resp, relay := f.idp.respond(redirect, o)
	return f.acs(resp, relay, cookie)
}

// --- SP-Metadaten ---

func TestSAMLMetadata_DescribesServiceProvider(t *testing.T) {
	f := newSAMLFixture(t)

	w := serveWithParams(f.h.SAMLMetadataHandler, http.MethodGet, "/", gin.Params{{Key: "page_id", Value: f.pageParam()}}, 0)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "application/samlmetadata+xml", w.Header().Get("Content-Type"))
	var ed saml.EntityDescriptor
	require.NoError(t, xml.Unmarshal(w.Body.Bytes(), &ed))
	base := "https://status.example.org/api/auth/saml/" + f.pageParam()
	assert.Equal(t, base+"/metadata", ed.EntityID)
	require.Len(t, ed.SPSSODescriptors, 1)
	sp := ed.SPSSODescriptors[0]
	// Nur POST-Binding, kein Artifact (die Artifact-Auflösung wäre ein Abruf
	// einer vom Seiten-Admin gepflegten Adresse).
	require.Len(t, sp.AssertionConsumerServices, 1)
	assert.Equal(t, saml.HTTPPostBinding, sp.AssertionConsumerServices[0].Binding)
	assert.Equal(t, base+"/acs", sp.AssertionConsumerServices[0].Location)
	assert.True(t, *sp.WantAssertionsSigned)
	assert.Equal(t, []saml.NameIDFormat{saml.PersistentNameIDFormat}, sp.NameIDFormats)
	assert.True(t, *sp.AuthnRequestsSigned)
	uses := []string{}
	for _, kd := range sp.KeyDescriptors {
		uses = append(uses, kd.Use)
	}
	// Security-Review: keine Verschlüsselung angeboten, der ACS
	// nimmt keine EncryptedAssertion an.
	assert.Equal(t, []string{"signing"}, uses)
	assert.NotContains(t, w.Body.String(), "PRIVATE KEY")
}

func TestSAMLMetadata_Errors(t *testing.T) {
	f := newSAMLFixture(t)
	get := func(id string) int {
		return serveWithParams(f.h.SAMLMetadataHandler, http.MethodGet, "/", gin.Params{{Key: "page_id", Value: id}}, 0).Code
	}
	assert.Equal(t, http.StatusBadRequest, get("abc"))
	assert.Equal(t, http.StatusNotFound, get("999"))
	// Für gelöschte Seiten gibt es keine Metadaten.
	// Private Seiten behalten sie, weil gerade sie das SSO brauchen und viele
	// IdPs die SP-Metadaten per URL laden; mehr als Entity-ID, ACS und
	// Zertifikat steht nicht darin, und dass die Seite existiert, verrät schon
	// GET /api/public/pages/:slug/sso.
	t.Run("private Seite liefert weiter", func(t *testing.T) {
		require.NoError(t, f.db.Model(&f.page).Update("is_private", true).Error)
		t.Cleanup(func() { f.db.Model(&f.page).Update("is_private", false) })
		assert.Equal(t, http.StatusOK, get(f.pageParam()))
	})
	t.Run("gelöschte Seite", func(t *testing.T) {
		require.NoError(t, f.db.Delete(&f.page).Error)
		t.Cleanup(func() { f.db.Unscoped().Model(&f.page).Update("deleted_at", nil) })
		assert.Equal(t, http.StatusNotFound, get(f.pageParam()))
	})
	t.Run("ohne Schlüssel (noch nie gespeichert)", func(t *testing.T) {
		f.updateSSO(t, "saml_sp_certificate", "")
		assert.Equal(t, http.StatusNotFound, get(f.pageParam()))
	})
	t.Run("kaputtes Zertifikat", func(t *testing.T) {
		f.updateSSO(t, "saml_sp_certificate", "kein PEM")
		assert.Equal(t, http.StatusInternalServerError, get(f.pageParam()))
	})
}

// --- Login-Start ---

func TestSAMLLogin_RedirectsToIdPWithSignedRequest(t *testing.T) {
	f := newSAMLFixture(t)
	before := time.Now()

	redirect, ck := f.startLogin(t)

	loc, err := url.Parse(redirect)
	require.NoError(t, err)
	assert.Equal(t, samlTestIdPBase+"/sso", loc.Scheme+"://"+loc.Host+loc.Path)
	q := loc.Query()
	assert.NotEmpty(t, q.Get("SAMLRequest"))
	assert.NotEmpty(t, q.Get("Signature"))
	assert.Equal(t, "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256", q.Get("SigAlg"))

	var st models.SAMLLoginState
	require.NoError(t, f.db.Where("state = ?", q.Get("RelayState")).First(&st).Error)
	assert.Equal(t, f.page.ID, st.StatusPageID)
	assert.Zero(t, st.LinkUserID)
	assert.False(t, st.TestLogin)
	assert.WithinDuration(t, before.Add(10*time.Minute), st.ExpiresAt, 5*time.Second)
	// Die Request-ID im AuthnRequest ist die gespeicherte.
	req := f.idp.authnRequest(redirect, time.Now())
	assert.Equal(t, st.RequestID, req.Request.ID)
	assert.Equal(t, "https://status.example.org/api/auth/saml/"+f.pageParam()+"/acs", req.Request.AssertionConsumerServiceURL)
	// Security-Review: Verlangt wird eine persistente NameID.
	require.NotNil(t, req.Request.NameIDPolicy.Format)
	assert.Equal(t, string(saml.PersistentNameIDFormat), *req.Request.NameIDPolicy.Format)

	require.NotNil(t, ck)
	assert.Equal(t, st.State, ck.Value)
	assert.Equal(t, "/api/auth/saml", ck.Path)
	assert.Equal(t, 600, ck.MaxAge)
	assert.True(t, ck.HttpOnly)
	assert.True(t, ck.Secure)
	// Der IdP schickt die Antwort per Cross-Site-POST zurück; ohne
	// SameSite=None schickte der Browser das Cookie dabei nicht mit.
	assert.Equal(t, http.SameSiteNoneMode, ck.SameSite)
}

func TestSAMLLogin_RemovesExpiredStatesAndAssertionUses(t *testing.T) {
	f := newSAMLFixture(t)
	require.NoError(t, f.db.Create(&models.SAMLLoginState{State: "alt", RequestID: "r", StatusPageID: f.page.ID, ExpiresAt: time.Now().Add(-time.Second)}).Error)
	require.NoError(t, f.db.Create(&models.SAMLLoginState{State: "frisch", RequestID: "r", StatusPageID: f.page.ID, ExpiresAt: time.Now().Add(time.Minute)}).Error)
	require.NoError(t, f.db.Create(&models.SAMLAssertionUse{Issuer: "i", AssertionID: "alt", ExpiresAt: time.Now().Add(-time.Second)}).Error)
	require.NoError(t, f.db.Create(&models.SAMLAssertionUse{Issuer: "i", AssertionID: "frisch", ExpiresAt: time.Now().Add(time.Minute)}).Error)

	f.startLogin(t)

	var states, uses []string
	f.db.Model(&models.SAMLLoginState{}).Pluck("state", &states)
	f.db.Model(&models.SAMLAssertionUse{}).Pluck("assertion_id", &uses)
	assert.NotContains(t, states, "alt")
	assert.Contains(t, states, "frisch")
	assert.Len(t, states, 2)
	assert.Equal(t, []string{"frisch"}, uses)
}

func TestSAMLLogin_Errors(t *testing.T) {
	f := newSAMLFixture(t)
	login := func(query string) *httptest.ResponseRecorder {
		return serveOIDC(f.h.SAMLLoginHandler, http.MethodGet, "/api/auth/saml/login?"+query, nil, 0)
	}

	t.Run("ungültige Seite", func(t *testing.T) {
		assertOIDCError(t, login("page=abc"), "/login", "bad_page")
	})
	t.Run("Seite ohne aktives SAML", func(t *testing.T) {
		assertOIDCError(t, login("page=999"), "/login", "not_configured")
	})
	t.Run("kaputte IdP-Metadaten", func(t *testing.T) {
		prev := f.idp.metadataXML()
		f.updateSSO(t, "saml_idp_metadata_xml", "<kaputt")
		t.Cleanup(func() { f.updateSSO(t, "saml_idp_metadata_xml", prev) })
		w := login("page=" + f.pageParam())
		assertOIDCError(t, w, "/login", "metadata_invalid")
		assert.Nil(t, samlStateCookie(w))
	})
	t.Run("kaputter SP-Schlüssel", func(t *testing.T) {
		var cfg models.StatusPageSSOConfig
		require.NoError(t, f.db.Where("status_page_id = ?", f.page.ID).First(&cfg).Error)
		f.updateSSO(t, "saml_sp_key", "kein PEM")
		t.Cleanup(func() { f.updateSSO(t, "saml_sp_key", cfg.SAMLSPKey) })
		assertOIDCError(t, login("page="+f.pageParam()), "/login", "internal")
	})
	t.Run("State nicht speicherbar", func(t *testing.T) {
		require.NoError(t, f.db.Migrator().DropTable(&models.SAMLLoginState{}))
		t.Cleanup(func() { _ = f.db.AutoMigrate(&models.SAMLLoginState{}) })
		w := login("page=" + f.pageParam())
		assertOIDCError(t, w, "/login", "internal")
		assert.Nil(t, samlStateCookie(w))
	})
}

// --- ACS: erfolgreiche Anmeldung ---

func TestSAMLACS_LogsInLinkedUser(t *testing.T) {
	f := newSAMLFixture(t)
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	f.linkIdentity(t, f.user.ID, "saml-user-1")

	w := f.loginWith(t, samlRespOpts{attrs: map[string]string{"email": "erika@idp.example.org", "name": "Erika SAML"}})

	claims := jwtFromSAMLRedirect(t, w)
	assert.Equal(t, f.user.ID, claims.UserID)
	assert.Equal(t, "saml", claims.SSOProvider)
	assert.Equal(t, "Erika SAML", claims.Name)
	assert.Equal(t, f.page.ID, claims.SSOPageID)
	loc, _ := url.Parse(w.Header().Get("Location"))
	assert.Empty(t, loc.Query().Get("next"))
	var ident models.UserSSOIdentity
	require.NoError(t, f.db.Where("user_id = ?", f.user.ID).First(&ident).Error)
	assert.Equal(t, "erika@idp.example.org", ident.Email)
	var audit models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "auth.sso_login").First(&audit).Error)
	assert.Equal(t, "provider=saml page="+f.pageParam(), audit.Details)
	assert.Equal(t, int64(0), countRows(t, f.db, &models.SAMLLoginState{}))
	assert.Equal(t, int64(1), countRows(t, f.db, &models.SAMLAssertionUse{}))
}

// assertSAMLError und jwtFromSAMLRedirect: Der ACS antwortet auf den POST des
// IdP mit 303 statt 307, damit der Browser das Ziel per GET lädt.
func assertSAMLError(t *testing.T, w *httptest.ResponseRecorder, path, code string) {
	t.Helper()
	assertSSOError(t, w, http.StatusSeeOther, path, code)
}

func jwtFromSAMLRedirect(t *testing.T, w *httptest.ResponseRecorder) *middleware.Claims {
	t.Helper()
	return jwtFromRedirectStatus(t, w, http.StatusSeeOther)
}

func countRows(t *testing.T, db *gorm.DB, model interface{}) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(model).Count(&n).Error)
	return n
}

func TestSAMLACS_AcceptsSignatureVariants(t *testing.T) {
	for name, o := range map[string]samlRespOpts{
		"Assertion und Response signiert": {},
		"nur Assertion signiert":          {unsignedResp: true},
		"nur Response signiert":           {unsignedAssert: true},
		"crewjam-IdP (MakeResponse)":      {crewjamResp: true},
		// Ohne Response-Signatur darf Destination fehlen (SAML-Bindings 3.5.5.2).
		"ohne Destination": {unsignedResp: true, mutateResp: func(r *saml.Response) { r.Destination = "" }},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSAMLFixture(t)
			t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
			f.linkIdentity(t, f.user.ID, "saml-user-1")
			claims := jwtFromSAMLRedirect(t, f.loginWith(t, o))
			assert.Equal(t, f.user.ID, claims.UserID)
		})
	}
}

// --- ACS: abgelehnte Antworten ---

func TestSAMLACS_RejectsInvalidResponses(t *testing.T) {
	_, otherKey := samlTestKeys()
	past := time.Now().Add(-time.Hour)
	for name, o := range map[string]samlRespOpts{
		"unsigniert":              {unsignedAssert: true, unsignedResp: true},
		"fremder Schlüssel":       {signKey: otherKey},
		"nur Assertion, fremd":    {signKey: otherKey, unsignedResp: true},
		"nach Signatur verändert": {unsignedResp: true, tamper: func(b []byte) []byte { return bytes.Replace(b, []byte("saml-user-1"), []byte("saml-user-2"), 1) }},
		"abgelaufen":              {now: past},
		"Conditions abgelaufen":   {mutate: func(a *saml.Assertion) { a.Conditions.NotOnOrAfter = past }},
		"falsche Audience": {mutate: func(a *saml.Assertion) {
			a.Conditions.AudienceRestrictions[0].Audience.Value = "https://andere.example.org"
		}},
		"ohne Audience": {mutate: func(a *saml.Assertion) { a.Conditions.AudienceRestrictions = nil }},
		"falscher Recipient": {mutate: func(a *saml.Assertion) {
			a.Subject.SubjectConfirmations[0].SubjectConfirmationData.Recipient = "https://andere.example.org/acs"
		}},
		"falsches InResponseTo": {mutate: func(a *saml.Assertion) {
			a.Subject.SubjectConfirmations[0].SubjectConfirmationData.InResponseTo = "id-fremd"
		}},
		"Response-InResponseTo":    {mutateResp: func(r *saml.Response) { r.InResponseTo = "id-fremd" }},
		"falscher Issuer":          {mutate: func(a *saml.Assertion) { a.Issuer.Value = "https://boese.example.org" }},
		"ohne SubjectConfirmation": {mutate: func(a *saml.Assertion) { a.Subject.SubjectConfirmations = nil }},
		"ohne NameID":              {mutate: func(a *saml.Assertion) { a.Subject.NameID = nil }},
		"leere NameID":             {mutate: func(a *saml.Assertion) { a.Subject.NameID.Value = "" }},
		"ohne Subject":             {mutate: func(a *saml.Assertion) { a.Subject = nil }},
		"ohne Conditions (Panik)":  {mutate: func(a *saml.Assertion) { a.Conditions = nil }},
		"ohne Assertion-ID":        {mutate: func(a *saml.Assertion) { a.ID = "" }},
		"falsche Destination":      {mutateResp: func(r *saml.Response) { r.Destination = "https://andere.example.org/acs" }},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSAMLFixture(t)
			f.linkIdentity(t, f.user.ID, "saml-user-1")
			assertSAMLError(t, f.loginWith(t, o), "/login", "saml_response_invalid")
			assert.Equal(t, int64(0), countRows(t, f.db, &models.SAMLAssertionUse{}))
		})
	}
}

// TestSAMLACS_RejectsWrappingAndEncryptedAssertions (Security-Review):
// Antworten mit mehr als einer Assertion (auch versteckt,
// etwa als Kopie mit fremder NameID vor dem signierten Original) oder mit
// EncryptedAssertion lehnt der ACS ab, bevor crewjam sie auswertet.
func TestSAMLACS_RejectsWrappingAndEncryptedAssertions(t *testing.T) {
	edit := func(change func(root, assertion *etree.Element)) func([]byte) []byte {
		return func(b []byte) []byte {
			doc := etree.NewDocument()
			require.NoError(t, doc.ReadFromBytes(b))
			root := doc.Root()
			change(root, root.FindElement("./Assertion"))
			out, err := doc.WriteToBytes()
			require.NoError(t, err)
			return out
		}
	}
	forged := func(a *etree.Element) *etree.Element {
		c := a.Copy()
		c.FindElement("./Subject/NameID").SetText("victim-1")
		return c
	}
	for name, change := range map[string]func(root, assertion *etree.Element){
		"Kopie vor dem Original": func(root, a *etree.Element) { root.InsertChildAt(a.Index(), forged(a)) },
		"Kopie in Extensions": func(root, a *etree.Element) {
			ext := root.CreateElement("samlp:Extensions")
			ext.AddChild(forged(a))
		},
		"EncryptedAssertion zusätzlich": func(root, _ *etree.Element) {
			root.CreateElement("saml:EncryptedAssertion").CreateAttr("xmlns:saml", "urn:oasis:names:tc:SAML:2.0:assertion")
		},
		"nur EncryptedAssertion": func(root, a *etree.Element) {
			root.RemoveChild(a)
			root.CreateElement("saml:EncryptedAssertion").CreateAttr("xmlns:saml", "urn:oasis:names:tc:SAML:2.0:assertion")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSAMLFixture(t)
			victim := models.User{Email: "opfer@example.org", RoleID: f.user.RoleID}
			require.NoError(t, f.db.Create(&victim).Error)
			f.linkIdentity(t, victim.ID, "victim-1")
			f.linkIdentity(t, f.user.ID, "saml-user-1")
			w := f.loginWith(t, samlRespOpts{unsignedResp: true, tamper: edit(change)})
			assertSAMLError(t, w, "/login", "saml_response_invalid")
		})
	}
}

func TestSAMLACS_ResponseFormErrors(t *testing.T) {
	f := newSAMLFixture(t)
	t.Run("ohne SAMLResponse", func(t *testing.T) {
		redirect, ck := f.startLogin(t)
		relay := relayStateOf(t, redirect)
		assertSAMLError(t, f.acs("", relay, ck), "/login", "saml_response_invalid")
	})
	t.Run("kein base64", func(t *testing.T) {
		redirect, ck := f.startLogin(t)
		assertSAMLError(t, f.acs("%%%", relayStateOf(t, redirect), ck), "/login", "saml_response_invalid")
	})
	t.Run("kein XML", func(t *testing.T) {
		redirect, ck := f.startLogin(t)
		assertSAMLError(t, f.acs(base64.StdEncoding.EncodeToString([]byte("<kaputt")), relayStateOf(t, redirect), ck), "/login", "saml_response_invalid")
	})
	t.Run("Artifact-Binding wird nicht aufgelöst", func(t *testing.T) {
		redirect, ck := f.startLogin(t)
		form := url.Values{"SAMLart": {"AAQAA"}, "RelayState": {relayStateOf(t, redirect)}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c.Request.AddCookie(ck)
		c.Params = gin.Params{{Key: "page_id", Value: f.pageParam()}}
		f.h.SAMLACSHandler(c)
		c.Writer.WriteHeaderNow()
		assertSAMLError(t, w, "/login", "saml_response_invalid")
	})
}

func relayStateOf(t *testing.T, redirect string) string {
	t.Helper()
	u, err := url.Parse(redirect)
	require.NoError(t, err)
	return u.Query().Get("RelayState")
}

func TestSAMLACS_IdPStatusErrorIsProviderDenied(t *testing.T) {
	f := newSAMLFixture(t)
	w := f.loginWith(t, samlRespOpts{status: "urn:oasis:names:tc:SAML:2.0:status:Responder"})
	assertSAMLError(t, w, "/login", "provider_denied")
}

// TestSAMLACS_NameIDFormat (Security-Review): Nur eine
// persistente NameID zählt. emailAddress wird immer abgelehnt (die Adresse
// kann der IdP-Admin einem anderen Konto geben), transient ebenso (wechselt
// bei jeder Anmeldung), unspecified (auch ein fehlendes Format-Attribut) nur
// mit saml_allow_unspecified_nameid.
func TestSAMLACS_NameIDFormat(t *testing.T) {
	clearFormat := func(a *saml.Assertion) { a.Subject.NameID.Format = "" }
	for name, tc := range map[string]struct {
		opts       samlRespOpts
		allow      bool
		wantFormat string // leer: abgelehnt mit saml_nameid_format
	}{
		"persistent":                    {samlRespOpts{}, false, string(saml.PersistentNameIDFormat)},
		"transient":                     {samlRespOpts{nameIDFormat: string(saml.TransientNameIDFormat)}, true, ""},
		"emailAddress":                  {samlRespOpts{nameIDFormat: string(saml.EmailAddressNameIDFormat)}, false, ""},
		"emailAddress trotz Freigabe":   {samlRespOpts{nameIDFormat: string(saml.EmailAddressNameIDFormat)}, true, ""},
		"unspecified ohne Freigabe":     {samlRespOpts{nameIDFormat: string(saml.UnspecifiedNameIDFormat)}, false, ""},
		"unspecified mit Freigabe":      {samlRespOpts{nameIDFormat: string(saml.UnspecifiedNameIDFormat)}, true, string(saml.UnspecifiedNameIDFormat)},
		"ohne Format ohne Freigabe":     {samlRespOpts{mutate: clearFormat}, false, ""},
		"ohne Format zählt unspecified": {samlRespOpts{mutate: clearFormat}, true, string(saml.UnspecifiedNameIDFormat)},
		"anderes Format (kerberos)":     {samlRespOpts{nameIDFormat: "urn:oasis:names:tc:SAML:2.0:nameid-format:kerberos"}, true, ""},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSAMLFixture(t)
			t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
			f.updateSSO(t, "saml_allow_unspecified_nameid", tc.allow)
			_, redirect, ck := f.startLink(t, f.user.ID)
			resp, relay := f.idp.respond(redirect, tc.opts)
			w := f.acs(resp, relay, ck)
			if tc.wantFormat == "" {
				assertSAMLError(t, w, "/admin/profile", "saml_nameid_format")
				assert.Equal(t, int64(0), countRows(t, f.db, &models.UserSSOIdentity{}))
				return
			}
			jwtFromSAMLRedirect(t, w)
			var ident models.UserSSOIdentity
			require.NoError(t, f.db.First(&ident).Error)
			assert.Equal(t, tc.wantFormat, ident.SubjectFormat)
		})
	}
}

// TestSAMLACS_NameIDFormatIsPartOfIdentity: Eine mit unspecified verknüpfte
// Identität passt nicht zu einer persistenten NameID mit gleichem Wert.
func TestSAMLACS_NameIDFormatIsPartOfIdentity(t *testing.T) {
	f := newSAMLFixture(t)
	require.NoError(t, f.db.Create(&models.UserSSOIdentity{
		UserID: f.user.ID, StatusPageID: f.page.ID, Protocol: models.SSOProtocolSAML, Issuer: f.idp.entityID(),
		Subject: "saml-user-1", SubjectFormat: string(saml.UnspecifiedNameIDFormat),
	}).Error)
	assertSAMLError(t, f.loginWith(t, samlRespOpts{}), "/login", "not_linked")
}

// TestSAMLLogin_RequestsUnspecifiedWhenAllowed: Mit Freigabe verlangt der
// AuthnRequest kein Format, damit IdPs ohne persistente NameID antworten.
func TestSAMLLogin_RequestsUnspecifiedWhenAllowed(t *testing.T) {
	f := newSAMLFixture(t)
	f.updateSSO(t, "saml_allow_unspecified_nameid", true)
	redirect, _ := f.startLogin(t)
	req := f.idp.authnRequest(redirect, time.Now())
	require.NotNil(t, req.Request.NameIDPolicy)
	assert.Nil(t, req.Request.NameIDPolicy.Format)
}

func TestSAMLACS_StateIsSingleUse(t *testing.T) {
	f := newSAMLFixture(t)
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	f.linkIdentity(t, f.user.ID, "saml-user-1")
	redirect, ck := f.startLogin(t)
	resp, relay := f.idp.respond(redirect, samlRespOpts{})

	jwtFromSAMLRedirect(t, f.acs(resp, relay, ck))
	assertSAMLError(t, f.acs(resp, relay, ck), "/login", "state_invalid")
}

// TestSAMLACS_AssertionIDIsSingleUse: Dieselbe Assertion-ID unter einem
// neuen, gültigen Login-Versuch wird abgelehnt (Replay-Schutz zusätzlich zur
// einmaligen Request-ID).
func TestSAMLACS_AssertionIDIsSingleUse(t *testing.T) {
	f := newSAMLFixture(t)
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	f.linkIdentity(t, f.user.ID, "saml-user-1")
	fixedID := func(a *saml.Assertion) { a.ID = "id-einmalig" }

	jwtFromSAMLRedirect(t, f.loginWith(t, samlRespOpts{mutate: fixedID}))
	assertSAMLError(t, f.loginWith(t, samlRespOpts{mutate: fixedID}), "/login", "saml_replay")
}

// TestSAMLACS_AssertionUseExpiresWithAssertion: Die Assertion-ID bleibt bis
// zum spätesten NotOnOrAfter (Conditions oder SubjectConfirmationData) plus
// Uhrentoleranz gesperrt, danach räumt der nächste Login-Start sie weg.
func TestSAMLACS_AssertionUseExpiresWithAssertion(t *testing.T) {
	later := time.Now().Add(5 * time.Minute).UTC().Truncate(time.Second)
	for name, tc := range map[string]struct {
		mutate func(*saml.Assertion)
	}{
		"SubjectConfirmationData später": {func(a *saml.Assertion) {
			a.Subject.SubjectConfirmations[0].SubjectConfirmationData.NotOnOrAfter = later
		}},
		"Conditions später": {func(a *saml.Assertion) { a.Conditions.NotOnOrAfter = later }},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSAMLFixture(t)
			t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
			f.linkIdentity(t, f.user.ID, "saml-user-1")
			jwtFromSAMLRedirect(t, f.loginWith(t, samlRespOpts{mutate: tc.mutate}))
			var use models.SAMLAssertionUse
			require.NoError(t, f.db.First(&use).Error)
			assert.WithinDuration(t, later.Add(3*time.Minute), use.ExpiresAt, time.Second)
			assert.Equal(t, f.idp.entityID(), use.Issuer)
			assert.Equal(t, f.page.ID, use.StatusPageID)
		})
	}
}

func TestSAMLACS_StateErrors(t *testing.T) {
	f := newSAMLFixture(t)
	t.Run("Cookie fehlt", func(t *testing.T) {
		redirect, _ := f.startLogin(t)
		resp, relay := f.idp.respond(redirect, samlRespOpts{})
		assertSAMLError(t, f.acs(resp, relay, nil), "/login", "state_invalid")
	})
	t.Run("RelayState fehlt", func(t *testing.T) {
		redirect, ck := f.startLogin(t)
		resp, _ := f.idp.respond(redirect, samlRespOpts{})
		assertSAMLError(t, f.acs(resp, "", ck), "/login", "state_invalid")
	})
	t.Run("Cookie passt nicht", func(t *testing.T) {
		redirect, _ := f.startLogin(t)
		resp, relay := f.idp.respond(redirect, samlRespOpts{})
		assertSAMLError(t, f.acs(resp, relay, &http.Cookie{Name: samlStateCookieName, Value: "anders"}), "/login", "state_invalid")
	})
	t.Run("abgelaufen", func(t *testing.T) {
		redirect, ck := f.startLogin(t)
		resp, relay := f.idp.respond(redirect, samlRespOpts{})
		require.NoError(t, f.db.Model(&models.SAMLLoginState{}).Where("state = ?", relay).Update("expires_at", time.Now().Add(-time.Second)).Error)
		assertSAMLError(t, f.acs(resp, relay, ck), "/login", "state_invalid")
	})
	t.Run("andere Seite im Pfad", func(t *testing.T) {
		redirect, ck := f.startLogin(t)
		resp, relay := f.idp.respond(redirect, samlRespOpts{})
		assertSAMLError(t, f.acsFor("999", resp, relay, ck), "/login", "state_invalid")
	})
	t.Run("ungültige Seite im Pfad", func(t *testing.T) {
		assertSAMLError(t, f.acsFor("abc", "x", "y", nil), "/login", "state_invalid")
	})
	t.Run("Cookie wird gelöscht", func(t *testing.T) {
		redirect, ck := f.startLogin(t)
		resp, relay := f.idp.respond(redirect, samlRespOpts{})
		w := f.acs(resp, relay, ck)
		cleared := samlStateCookie(w)
		require.NotNil(t, cleared)
		assert.Equal(t, -1, cleared.MaxAge)
	})
}

func TestSAMLACS_ConfigGoneOrBrokenAfterStart(t *testing.T) {
	t.Run("SAML inzwischen aus", func(t *testing.T) {
		f := newSAMLFixture(t)
		redirect, ck := f.startLogin(t)
		resp, relay := f.idp.respond(redirect, samlRespOpts{})
		f.updateSSO(t, "saml_enabled", false)
		assertSAMLError(t, f.acs(resp, relay, ck), "/login", "not_configured")
	})
	t.Run("Metadaten inzwischen kaputt", func(t *testing.T) {
		f := newSAMLFixture(t)
		redirect, ck := f.startLogin(t)
		resp, relay := f.idp.respond(redirect, samlRespOpts{})
		f.updateSSO(t, "saml_idp_metadata_xml", "<kaputt")
		assertSAMLError(t, f.acs(resp, relay, ck), "/login", "metadata_invalid")
	})
	t.Run("Replay-Tabelle nicht beschreibbar", func(t *testing.T) {
		f := newSAMLFixture(t)
		f.linkIdentity(t, f.user.ID, "saml-user-1")
		redirect, ck := f.startLogin(t)
		resp, relay := f.idp.respond(redirect, samlRespOpts{})
		require.NoError(t, f.db.Migrator().DropTable(&models.SAMLAssertionUse{}))
		assertSAMLError(t, f.acs(resp, relay, ck), "/login", "saml_replay")
	})
}

// --- Konto-Regeln (wie OIDC) ---

func TestSAMLACS_AccountRules(t *testing.T) {
	t.Run("nicht verknüpft", func(t *testing.T) {
		f := newSAMLFixture(t)
		assertSAMLError(t, f.loginWith(t, samlRespOpts{}), "/login", "not_linked")
	})
	t.Run("Verknüpfung einer OIDC-Identität zählt nicht", func(t *testing.T) {
		f := newSAMLFixture(t)
		require.NoError(t, f.db.Create(&models.UserSSOIdentity{
			UserID: f.user.ID, StatusPageID: f.page.ID, Protocol: models.SSOProtocolOIDC, Issuer: f.idp.entityID(), Subject: "saml-user-1",
		}).Error)
		assertSAMLError(t, f.loginWith(t, samlRespOpts{}), "/login", "not_linked")
	})
	t.Run("Global Admin", func(t *testing.T) {
		f := newSAMLFixture(t)
		require.NoError(t, f.db.Model(&f.user).Update("is_global_admin", true).Error)
		f.linkIdentity(t, f.user.ID, "saml-user-1")
		assertSAMLError(t, f.loginWith(t, samlRespOpts{}), "/login", "global_admin")
	})
	t.Run("Rolle auf anderer Seite", func(t *testing.T) {
		f := newSAMLFixture(t)
		other := models.StatusPage{Name: "Andere", Slug: "andere"}
		require.NoError(t, f.db.Create(&other).Error)
		require.NoError(t, f.db.Create(&models.UserStatusPageRole{UserID: f.user.ID, StatusPageID: other.ID, RoleID: f.user.RoleID}).Error)
		f.linkIdentity(t, f.user.ID, "saml-user-1")
		assertSAMLError(t, f.loginWith(t, samlRespOpts{}), "/login", "other_page_role")
	})
}

// --- Verknüpfen ---

func (f *samlFixture) startLink(t *testing.T, userID uint) (*httptest.ResponseRecorder, string, *http.Cookie) {
	t.Helper()
	w := serveOIDC(f.h.SAMLLinkHandler, http.MethodPost, "/api/auth/saml/link?page="+f.pageParam(), nil, userID)
	if w.Code != http.StatusOK {
		return w, "", nil
	}
	return w, jsonURL(t, w), samlStateCookie(w)
}

func jsonURL(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body["url"]
}

func TestSAMLLink_LinksIdentityAndLogsIn(t *testing.T) {
	f := newSAMLFixture(t)
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")

	_, redirect, ck := f.startLink(t, f.user.ID)
	resp, relay := f.idp.respond(redirect, samlRespOpts{attrs: map[string]string{"email": "erika@idp.example.org"}})
	w := f.acs(resp, relay, ck)

	claims := jwtFromSAMLRedirect(t, w)
	assert.Equal(t, f.user.ID, claims.UserID)
	loc, _ := url.Parse(w.Header().Get("Location"))
	assert.Equal(t, "profile", loc.Query().Get("next"))
	var ident models.UserSSOIdentity
	require.NoError(t, f.db.Where("user_id = ?", f.user.ID).First(&ident).Error)
	assert.Equal(t, models.SSOProtocolSAML, ident.Protocol)
	assert.Equal(t, f.idp.entityID(), ident.Issuer)
	assert.Equal(t, "saml-user-1", ident.Subject)
	assert.Equal(t, "erika@idp.example.org", ident.Email)
	var audit models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "auth.sso_linked").First(&audit).Error)
	assert.Equal(t, "provider=saml page="+f.pageParam(), audit.Details)
}

func TestSAMLLink_Errors(t *testing.T) {
	t.Run("Identität gehört anderem Konto", func(t *testing.T) {
		f := newSAMLFixture(t)
		other := models.User{Email: "max@example.org", RoleID: f.user.RoleID}
		require.NoError(t, f.db.Create(&other).Error)
		f.linkIdentity(t, other.ID, "saml-user-1")
		_, redirect, ck := f.startLink(t, f.user.ID)
		resp, relay := f.idp.respond(redirect, samlRespOpts{})
		assertSAMLError(t, f.acs(resp, relay, ck), "/admin/profile", "already_linked")
	})
	t.Run("nicht angemeldet", func(t *testing.T) {
		f := newSAMLFixture(t)
		w, _, _ := f.startLink(t, 0)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
	t.Run("Global Admin", func(t *testing.T) {
		f := newSAMLFixture(t)
		require.NoError(t, f.db.Model(&f.user).Update("is_global_admin", true).Error)
		w, _, _ := f.startLink(t, f.user.ID)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
	t.Run("Rolle auf anderer Seite", func(t *testing.T) {
		f := newSAMLFixture(t)
		other := models.StatusPage{Name: "Andere", Slug: "andere"}
		require.NoError(t, f.db.Create(&other).Error)
		require.NoError(t, f.db.Create(&models.UserStatusPageRole{UserID: f.user.ID, StatusPageID: other.ID, RoleID: f.user.RoleID}).Error)
		w, _, _ := f.startLink(t, f.user.ID)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
	t.Run("SAML nicht aktiv", func(t *testing.T) {
		f := newSAMLFixture(t)
		f.updateSSO(t, "saml_enabled", false)
		w, _, _ := f.startLink(t, f.user.ID)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// --- Testanmeldung ---

func (f *samlFixture) startTest(t *testing.T, pageParam string) (*httptest.ResponseRecorder, string, *http.Cookie) {
	t.Helper()
	w := serveWithParams(f.h.StartSAMLTestHandler, http.MethodPost, "/", gin.Params{{Key: "page_id", Value: pageParam}}, f.user.ID)
	if w.Code != http.StatusOK {
		return w, "", nil
	}
	return w, jsonURL(t, w), samlStateCookie(w)
}

func TestSAMLTest_WorksBeforeGoingLiveAndCreatesNoSession(t *testing.T) {
	f := newSAMLFixture(t)
	f.updateSSO(t, "saml_enabled", false)

	_, redirect, ck := f.startTest(t, f.pageParam())
	resp, relay := f.idp.respond(redirect, samlRespOpts{attrs: map[string]string{"email": "e@x", "name": "N"}})
	w := f.acs(resp, relay, ck)

	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	assert.Equal(t, getFrontendURL()+"/admin/sso?oidc_test=ok&sso=saml", w.Header().Get("Location"))
	assert.Equal(t, int64(0), countRows(t, f.db, &models.UserSSOIdentity{}))
	assert.Equal(t, int64(0), countRows(t, f.db, &models.AuditLog{}))
}

func TestSAMLTest_ReportsMissingAttributes(t *testing.T) {
	f := newSAMLFixture(t)
	_, redirect, ck := f.startTest(t, f.pageParam())
	resp, relay := f.idp.respond(redirect, samlRespOpts{})
	w := f.acs(resp, relay, ck)
	assert.Equal(t, getFrontendURL()+"/admin/sso?oidc_test=ok&sso=saml&missing=email,name", w.Header().Get("Location"))
}

func TestSAMLTest_ErrorsGoToSSOPage(t *testing.T) {
	f := newSAMLFixture(t)
	_, redirect, ck := f.startTest(t, f.pageParam())
	resp, relay := f.idp.respond(redirect, samlRespOpts{unsignedAssert: true, unsignedResp: true})
	assertSAMLError(t, f.acs(resp, relay, ck), "/admin/sso", "saml_response_invalid")
}

func TestSAMLTest_StartErrors(t *testing.T) {
	f := newSAMLFixture(t)
	t.Run("ungültige Seite", func(t *testing.T) {
		w, _, _ := f.startTest(t, "abc")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("keine Konfiguration", func(t *testing.T) {
		w, _, _ := f.startTest(t, "999")
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
	t.Run("Testanmeldung aus", func(t *testing.T) {
		f.updateSSO(t, "test_login_enabled", false)
		t.Cleanup(func() { f.updateSSO(t, "test_login_enabled", true) })
		w, _, _ := f.startTest(t, f.pageParam())
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Equal(t, "Testanmeldung ist für diese Seite abgeschaltet", errorOf(t, w))
	})
	t.Run("ohne IdP-Metadaten", func(t *testing.T) {
		prev := f.idp.metadataXML()
		f.updateSSO(t, "saml_idp_metadata_xml", "")
		t.Cleanup(func() { f.updateSSO(t, "saml_idp_metadata_xml", prev) })
		w, _, _ := f.startTest(t, f.pageParam())
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
	t.Run("kaputte Metadaten", func(t *testing.T) {
		prev := f.idp.metadataXML()
		f.updateSSO(t, "saml_idp_metadata_xml", "<kaputt")
		t.Cleanup(func() { f.updateSSO(t, "saml_idp_metadata_xml", prev) })
		w, _, _ := f.startTest(t, f.pageParam())
		assert.Equal(t, http.StatusBadGateway, w.Code)
	})
	t.Run("Testanmeldung inzwischen aus", func(t *testing.T) {
		_, redirect, ck := f.startTest(t, f.pageParam())
		resp, relay := f.idp.respond(redirect, samlRespOpts{})
		f.updateSSO(t, "test_login_enabled", false)
		t.Cleanup(func() { f.updateSSO(t, "test_login_enabled", true) })
		assertSAMLError(t, f.acs(resp, relay, ck), "/admin/sso", "test_disabled")
	})
}

func TestSAMLTestResultURL(t *testing.T) {
	assert.Equal(t, getFrontendURL()+"/admin/sso?oidc_test=ok&sso=saml&missing=name", samlTestResultURL(oidcClaims{email: "e"}))
	assert.Equal(t, getFrontendURL()+"/admin/sso?oidc_test=ok&sso=saml&missing=email", samlTestResultURL(oidcClaims{name: "n"}))
}

func TestSAMLAttribute_MatchesNameOrFriendlyName(t *testing.T) {
	a := &saml.Assertion{AttributeStatements: []saml.AttributeStatement{{Attributes: []saml.Attribute{
		{Name: "urn:oid:0.9.2342.19200300.100.1.3", FriendlyName: "mail", Values: []saml.AttributeValue{{Value: "m@x"}}},
		{Name: "leer"},
		{Name: "displayName", Values: []saml.AttributeValue{{Value: "Erika"}, {Value: "Zweit"}}},
	}}}}
	assert.Equal(t, "m@x", samlAttribute(a, "mail"))
	assert.Equal(t, "m@x", samlAttribute(a, "urn:oid:0.9.2342.19200300.100.1.3"))
	assert.Equal(t, "Erika", samlAttribute(a, "displayName"))
	assert.Equal(t, "", samlAttribute(a, "leer"))
	assert.Equal(t, "", samlAttribute(a, "fehlt"))
	assert.Equal(t, "", samlAttribute(a, ""))
}

func TestIsSAMLACSPath(t *testing.T) {
	assert.True(t, IsSAMLACSPath("/api/auth/saml/7/acs"))
	for _, p := range []string{"/api/auth/oidc/7/acs", "/api/auth/saml/7/metadata", "/api/auth/saml//acs", "/api/auth/saml/7/x/acs", "/api/auth/saml/acs"} {
		assert.False(t, IsSAMLACSPath(p), p)
	}
}

// TestSAMLACS_WithMockIdP spielt den Ablauf des E2E-Specs ohne Browser gegen
// den Mock-SAML-IdP (statuspage/mocksamlidp) durch: Login-Start, Formular des
// Mocks, signierte Antwort an den ACS. So fällt ein Bruch zwischen Mock und SP
// schon hier auf und nicht erst im Nachtlauf.
func TestSAMLACS_WithMockIdP(t *testing.T) {
	f := newSAMLFixture(t)
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	mock := mocksamlidp.New(mocksamlidp.Config{BaseURL: "http://mock.test", ACSPrefix: "https://status.example.org/api/auth/saml/"})
	mw := httptest.NewRecorder()
	mock.ServeHTTP(mw, httptest.NewRequest(http.MethodGet, "http://mock.test/metadata", nil))
	f.updateSSO(t, "saml_idp_metadata_xml", mw.Body.String())
	f.linkIdentity(t, f.user.ID, "erika")
	// Die Verknüpfung hängt an der Entity-ID des Mocks, nicht an der des Test-IdP.
	require.NoError(t, f.db.Model(&models.UserSSOIdentity{}).Where("subject = ?", "erika").Update("issuer", "http://mock.test/metadata").Error)

	redirect, ck := f.startLogin(t)
	w := httptest.NewRecorder()
	mock.ServeHTTP(w, httptest.NewRequest(http.MethodGet, redirect, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	form := mockFormFields(w.Body.String())
	form.Set("username", "erika")
	form.Set("email", "erika@mock.test")
	form.Set("name", "Erika Mock")
	r := httptest.NewRequest(http.MethodPost, "http://mock.test/sso", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	mock.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	resp := mockFormFields(w.Body.String())

	acs := f.acs(resp.Get("SAMLResponse"), resp.Get("RelayState"), ck)

	claims := jwtFromSAMLRedirect(t, acs)
	assert.Equal(t, f.user.ID, claims.UserID)
	assert.Equal(t, "Erika Mock", claims.Name)
}

var mockHiddenField = regexp.MustCompile(`name="([A-Za-z]+)" value="([^"]*)"`)

func mockFormFields(body string) url.Values {
	v := url.Values{}
	for _, m := range mockHiddenField.FindAllStringSubmatch(body, -1) {
		v.Set(m[1], html.UnescapeString(m[2]))
	}
	return v
}
