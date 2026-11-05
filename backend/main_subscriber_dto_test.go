package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
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

// Nacharbeit: Soft-Delete-Filter im Abonnenten-DTO, Sicht des
// Anfragenden auf eigene Daten und Abgleich der DTO-Schlüssel mit openapi.yaml.
func TestRouter_AbonnentenDTO_ErstellerSichtbarkeit(t *testing.T) {
	db := testutils.SetupTestDB()
	r := newRouter(handlers.NewHandler(db))
	page := models.StatusPage{Name: "A", Slug: "dto-797b"}
	require.NoError(t, db.Create(&page).Error)
	role := models.Role{Name: "R797b", Permissions: "subscribers:read,subscribers:create"}
	require.NoError(t, db.Create(&role).Error)
	mkUser := func(email string, onPage bool) models.User {
		u := models.User{Email: email, RoleID: role.ID}
		require.NoError(t, db.Create(&u).Error)
		if onPage {
			require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: role.ID}).Error)
		}
		return u
	}
	staff := mkUser("staff797b@example.com", true)
	staffTok, _ := middleware.GenerateToken(staff.ID, staff.Email, "", role.Permissions, false)
	mkSub := func(email string, creator uint) {
		c := creator
		require.NoError(t, db.Create(&models.Subscriber{StatusPageID: page.ID, Email: email, Channel: "email", SubscribedComponentIDs: "all", CreatedByUserID: &c}).Error)
	}
	call := func(method, tok, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/pages/"+strconv.Itoa(int(page.ID))+"/subscribers", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	creatorEmail := func(t *testing.T, sub string) (string, bool) {
		t.Helper()
		w := call(http.MethodGet, staffTok, "")
		require.Equal(t, http.StatusOK, w.Code)
		var subs []struct {
			Email     string
			CreatedBy *struct{ Email string }
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &subs))
		for _, s := range subs {
			if s.Email == sub {
				require.NotNil(t, s.CreatedBy)
				return s.CreatedBy.Email, true
			}
		}
		return "", false
	}

	t.Run("Rolle soft-gelöscht -> Email leer", func(t *testing.T) {
		u := mkUser("rolle-weg797b@example.com", true)
		mkSub("s-rolle@example.com", u.ID)
		require.NoError(t, db.Where("user_id = ?", u.ID).Delete(&models.UserStatusPageRole{}).Error)
		got, found := creatorEmail(t, "s-rolle@example.com")
		require.True(t, found)
		assert.Empty(t, got)
	})
	t.Run("Konto soft-gelöscht -> Email leer", func(t *testing.T) {
		u := mkUser("konto-weg797b@example.com", true)
		mkSub("s-konto@example.com", u.ID)
		require.NoError(t, db.Delete(&u).Error)
		got, found := creatorEmail(t, "s-konto@example.com")
		require.True(t, found)
		assert.Empty(t, got)
	})
	t.Run("Global Admin ohne Seitenrolle -> für andere leer, für ihn selbst sichtbar", func(t *testing.T) {
		ga := models.User{Email: "ga797b@example.com", RoleID: role.ID, IsGlobalAdmin: true}
		require.NoError(t, db.Create(&ga).Error)
		gaTok, _ := middleware.GenerateToken(ga.ID, ga.Email, "", role.Permissions, true)
		mkSub("s-ga@example.com", ga.ID)
		got, found := creatorEmail(t, "s-ga@example.com")
		require.True(t, found)
		assert.Empty(t, got, "Personal der Seite sieht die Adresse eines fremden Global Admins nicht")

		w := call(http.MethodPost, gaTok, `{"channel":"email","email":"s-ga2@example.com","component_ids":"all"}`)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"Email":"ga797b@example.com"`, "eigene Daten in der Create-Antwort")
		w = call(http.MethodGet, gaTok, "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"CreatedBy":{"Email":"ga797b@example.com"}`)
	})
	t.Run("Schlüssel stimmen mit SubscriberAdmin in openapi.yaml überein", func(t *testing.T) {
		w := call(http.MethodGet, staffTok, "")
		var subs []map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &subs))
		require.NotEmpty(t, subs)
		var got []string
		for k := range subs[0] {
			got = append(got, k)
		}
		doc := loadOpenAPISpec(t)
		var want []string
		for k := range doc.Components.Schemas["SubscriberAdmin"].Value.Properties {
			want = append(want, k)
		}
		sort.Strings(got)
		sort.Strings(want)
		assert.Equal(t, want, got)
	})
}
