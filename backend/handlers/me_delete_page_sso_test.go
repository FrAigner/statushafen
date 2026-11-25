package handlers_test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deleteMeWithPageSSO calls DELETE /api/me with a session limited to page's SSO.
func (e meDeleteEnv) deleteMeWithPageSSO(t *testing.T, u models.User, page models.StatusPage, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	token, err := middleware.GeneratePageSSOToken(u, "U", models.SSOProtocolOIDC, page.ID)
	require.NoError(t, err)
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodDelete, "/api/me", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func (e meDeleteEnv) pageIdentity(t *testing.T, u models.User, pageID uint, subject string) {
	t.Helper()
	require.NoError(t, e.db.Create(&models.UserSSOIdentity{UserID: u.ID, StatusPageID: pageID, Protocol: models.SSOProtocolOIDC, Issuer: "https://idp.example", Subject: subject}).Error)
}

// A page's IdP (run by the tenant) can mint a page SSO session for every
// account linked to it. Without a password, that session confirms the deletion
// by e-mail only, so it must not delete an account that also exists beyond the
// page: another sign-in (global provider, legacy SSO, another page's SSO) or a
// membership on another page. An account that exists only through this page's
// SSO keeps self-service deletion (GDPR): the page SSO is its full sign-in.
func TestDeleteMeHandler_PageSSOSession(t *testing.T) {
	for name, tc := range map[string]struct {
		setup   func(t *testing.T, e meDeleteEnv, u *models.User, page, other models.StatusPage)
		allowed bool
	}{
		"only this page": {allowed: true, setup: func(*testing.T, meDeleteEnv, *models.User, models.StatusPage, models.StatusPage) {}},
		"global provider identity": {setup: func(t *testing.T, e meDeleteEnv, u *models.User, _, _ models.StatusPage) {
			e.pageIdentity(t, *u, 0, "global")
		}},
		"other page identity": {setup: func(t *testing.T, e meDeleteEnv, u *models.User, _, other models.StatusPage) {
			e.pageIdentity(t, *u, other.ID, "other")
		}},
		"other page membership": {setup: func(t *testing.T, e meDeleteEnv, u *models.User, _, other models.StatusPage) {
			e.assign(t, *u, other, e.viewRole)
		}},
		"legacy SSO": {setup: func(t *testing.T, e meDeleteEnv, u *models.User, _, _ models.StatusPage) {
			require.NoError(t, e.db.Model(u).Updates(map[string]any{"sso_provider": "google", "sso_subject": "legacy"}).Error)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			e := newMeDeleteEnv(t)
			u := e.createUser(t, "sso@example.com", false)
			page, other := e.createPage(t, "SSO Page"), e.createPage(t, "Other Page")
			e.assign(t, u, page, e.viewRole)
			e.pageIdentity(t, u, page.ID, "here")
			tc.setup(t, e, &u, page, other)

			w := e.deleteMeWithPageSSO(t, u, page, map[string]any{"confirm_email": u.Email})
			if tc.allowed {
				assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
				assert.False(t, e.userExists(u))
				return
			}
			assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assert.Equal(t, "page_sso_session", codeFromBody(t, w))
			assert.True(t, e.userExists(u))
		})
	}
}

// With a password the deletion re-authenticates by password, whatever session
// asks for it.
func TestDeleteMeHandler_PageSSOSessionWithPassword(t *testing.T) {
	e := newMeDeleteEnv(t)
	password := rand.Text()
	u := models.User{Email: "pw@example.com", PasswordHash: testutils.MustHash(password)}
	require.NoError(t, e.db.Create(&u).Error)
	page, other := e.createPage(t, "SSO Page"), e.createPage(t, "Other Page")
	e.assign(t, u, page, e.viewRole)
	e.assign(t, u, other, e.viewRole)

	w := e.deleteMeWithPageSSO(t, u, page, map[string]any{"confirm_email": u.Email, "password": rand.Text()})
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Equal(t, "confirmation_invalid", codeFromBody(t, w))

	w = e.deleteMeWithPageSSO(t, u, page, map[string]any{"confirm_email": u.Email, "password": password})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, e.userExists(u))
}

// A failing lookup refuses the deletion (fail closed).
func TestDeleteMeHandler_PageSSOSessionLookupError(t *testing.T) {
	for _, table := range []string{"user_sso_identities", "user_status_page_roles"} {
		t.Run(table, func(t *testing.T) {
			e := newMeDeleteEnv(t)
			u := e.createUser(t, "err@example.com", false)
			page := e.createPage(t, "SSO Page")
			e.assign(t, u, page, e.viewRole)
			failOnTable(t, e.db, "query", table, 0)

			w := e.deleteMeWithPageSSO(t, u, page, map[string]any{"confirm_email": u.Email})
			assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			assert.Equal(t, "page_sso_session", codeFromBody(t, w))
			assert.True(t, e.userExists(u))
		})
	}
}
