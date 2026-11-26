package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The subscriber list returned the whole user object of the creating
// account via CreatedBy (email, SSO identifier, IsGlobalAdmin, role including
// permissions, language) - even of accounts of other tenants that merely
// subscribed to the page. The tests go through the real router (newRouter).

// forbidden keys/values in every subscriber response
var subscriberLeakKeys = []string{
	"SSOSubject", "SSOProvider", "IsGlobalAdmin", "Permissions", "PreferredLanguage",
	"RoleID", "Role\"", "UserStatusPageRoles", "PasswordHash",
}

func TestRouter_Abonnentenliste_CreatedByLeaktKeinKonto(t *testing.T) {
	db := testutils.SetupTestDB()
	r := newRouter(handlers.NewHandler(db))
	page := models.StatusPage{Name: "A", Slug: "sub-797"}
	require.NoError(t, db.Create(&page).Error)
	other := models.StatusPage{Name: "B", Slug: "sub-797-b"}
	require.NoError(t, db.Create(&other).Error)

	adminRole := models.Role{Name: "Admin797", Permissions: "subscribers:read,subscribers:create,subscribers:update,subscribers:delete"}
	require.NoError(t, db.Create(&adminRole).Error)
	staff := models.User{Email: "staff797@example.com", RoleID: adminRole.ID}
	require.NoError(t, db.Create(&staff).Error)
	require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: staff.ID, StatusPageID: page.ID, RoleID: adminRole.ID}).Error)
	staffTok, _ := middleware.GenerateToken(staff.ID, staff.Email, "", adminRole.Permissions, false)

	// Foreign account of another tenant: global admin, SSO, language.
	gaRole := models.Role{Name: "GA797", Permissions: "*:*"}
	require.NoError(t, db.Create(&gaRole).Error)
	foreign := models.User{Email: "konto-fremd797@example.com", RoleID: gaRole.ID, SSOProvider: "okta-geheim", SSOSubject: "sub-geheim-797",
		IsGlobalAdmin: true, PreferredLanguage: "fr"}
	require.NoError(t, db.Create(&foreign).Error)
	require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: foreign.ID, StatusPageID: other.ID, RoleID: gaRole.ID}).Error)
	foreignTok, _ := middleware.GenerateToken(foreign.ID, foreign.Email, "", gaRole.Permissions, false)

	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	requireClean := func(t *testing.T, body string) {
		t.Helper()
		for _, k := range append(subscriberLeakKeys, "konto-fremd797", "okta-geheim", "sub-geheim-797", "GA797", "*:*") {
			assert.NotContains(t, body, k, "Antwort darf %q nicht enthalten", k)
		}
	}

	// A foreign account subscribes to page A with a different subscription address.
	w := call(http.MethodPost, "/api/subscribe", foreignTok,
		`{"channel":"email","email":"abo-adresse797@example.com","component_ids":"all","slug":"sub-797"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	listPath := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/subscribers"
	t.Run("Liste: kein Fremdkonto in CreatedBy", func(t *testing.T) {
		w := call(http.MethodGet, listPath, staffTok, "")
		require.Equal(t, http.StatusOK, w.Code)
		requireClean(t, w.Body.String())
		var subs []map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &subs))
		require.Len(t, subs, 1)
		assert.Equal(t, "abo-adresse797@example.com", subs[0]["Email"])
		// the frontend needs the information "subscription is linked to an account"
		assert.NotNil(t, subs[0]["CreatedBy"])
	})

	t.Run("Anlegen und Ändern durch Seitenpersonal: nur schlanke Antwort", func(t *testing.T) {
		w := call(http.MethodPost, listPath, staffTok, `{"channel":"email","email":"neu797@example.com","component_ids":"all"}`)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		requireClean(t, w.Body.String())
		var created map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
		assert.Equal(t, "neu797@example.com", created["Email"])
		id := strconv.Itoa(int(created["ID"].(float64)))

		w = call(http.MethodPut, listPath+"/"+id, staffTok, `{"channel":"email","email":"neu797@example.com","component_ids":"all"}`)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		requireClean(t, w.Body.String())
	})

	t.Run("Liste: Seitenpersonal sieht eigene Adresse als Ersteller", func(t *testing.T) {
		w := call(http.MethodGet, listPath, staffTok, "")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "staff797@example.com")
		requireClean(t, w.Body.String())
	})

	t.Run("Besitzer sieht weiterhin sein Abo", func(t *testing.T) {
		w := call(http.MethodGet, "/api/me/subscription?slug=sub-797", foreignTok, "")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "abo-adresse797@example.com")
	})
}
