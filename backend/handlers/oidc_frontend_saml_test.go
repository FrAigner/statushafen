package handlers

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Frontend-Einstieg ins SAML-Seiten-SSO (Login-Knopf, Profil).

func TestPublicSSO_ReportsSAML(t *testing.T) {
	f := newSAMLFixture(t)
	w := serveWithParams(f.h.GetPublicOIDCHandler, http.MethodGet, "/", gin.Params{{Key: "slug", Value: "saml-seite"}}, 0)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, map[string]any{"oidc_enabled": false, "saml_enabled": true, "page_id": float64(f.page.ID)}, decodeJSON(t, w))

	f.updateSSO(t, "saml_enabled", false)
	w = serveWithParams(f.h.GetPublicOIDCHandler, http.MethodGet, "/", gin.Params{{Key: "slug", Value: "saml-seite"}}, 0)
	assert.Equal(t, false, decodeJSON(t, w)["saml_enabled"])
}

func (f *samlFixture) mySSO(t *testing.T) mySSOResponse {
	t.Helper()
	of := &oidcFixture{h: f.h, db: f.db}
	code, body := of.mySSO(t, f.user.ID)
	require.Equal(t, http.StatusOK, code)
	return body
}

// TestMySSO_ListsLinkablePagesPerProtocol: Eine Seite mit OIDC und SAML
// erscheint je Protokoll einmal; nach der SAML-Verknüpfung bleibt nur OIDC.
func TestMySSO_ListsLinkablePagesPerProtocol(t *testing.T) {
	f := newSAMLFixture(t)
	f.updateSSO(t, "o_id_c_enabled", true)

	body := f.mySSO(t)
	assert.Equal(t, []mySSOLinkable{
		{StatusPageID: f.page.ID, PageName: "SAML-Seite", Protocol: "oidc"},
		{StatusPageID: f.page.ID, PageName: "SAML-Seite", Protocol: "saml"},
	}, body.Linkable)

	f.linkIdentity(t, f.user.ID, "saml-user-1")
	body = f.mySSO(t)
	assert.Equal(t, []mySSOLinkable{{StatusPageID: f.page.ID, PageName: "SAML-Seite", Protocol: "oidc"}}, body.Linkable)
	require.Len(t, body.Identities, 1)
	assert.Equal(t, "saml", body.Identities[0].Protocol)
}

// TestMySSO_SortsLinkableByPageName: Die Liste ist über beide Protokolle nach
// Seitennamen sortiert.
func TestMySSO_SortsLinkableByPageName(t *testing.T) {
	f := newSAMLFixture(t)
	of := &oidcFixture{h: f.h, db: f.db}
	zulu := of.ssoPage(t, "Zulu", "zulu", true, func(p *models.StatusPage) { p.IsPublicIndexed = true })
	alpha := of.ssoPage(t, "Alpha", "alpha", true, func(p *models.StatusPage) { p.IsPublicIndexed = true })

	body := f.mySSO(t)
	assert.Equal(t, []mySSOLinkable{
		{StatusPageID: alpha.ID, PageName: "Alpha", Protocol: "oidc"},
		{StatusPageID: f.page.ID, PageName: "SAML-Seite", Protocol: "saml"},
		{StatusPageID: zulu.ID, PageName: "Zulu", Protocol: "oidc"},
	}, body.Linkable)
}

func TestDeleteMySSO_SAMLIdentity(t *testing.T) {
	f := newSAMLFixture(t)
	f.linkIdentity(t, f.user.ID, "saml-user-1")
	var ident models.UserSSOIdentity
	require.NoError(t, f.db.First(&ident).Error)

	w := serveWithParams(f.h.DeleteMySSOHandler, http.MethodDelete, "/", gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(ident.ID), 10)}}, f.user.ID)

	require.Equal(t, http.StatusNoContent, w.Code)
	var audit models.AuditLog
	require.NoError(t, f.db.Where("action = ?", "auth.sso_unlinked").First(&audit).Error)
	assert.Equal(t, "provider=saml page="+f.pageParam(), audit.Details)
}
