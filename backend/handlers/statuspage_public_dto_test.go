package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// publicPageKeys sind genau die Felder, die das öffentliche Frontend
// (LandingPage.vue, StatusPage.vue) aus einer Statusseite liest - API-Befund
// zu alles darüber hinaus (AutoJoinEmailDomains, DefaultRoleID,
// NotificationFromAddress, CustomDomain*, IsPrivate, ...) ist interne
// Mandanten-Konfiguration und gehört nicht in öffentliche Antworten.
var publicPageKeys = []string{
	"Description", "HeaderStyle", "ID", "LogoURL", "Name",
	"PageLayout", "PrimaryColor", "SecondaryColor", "Slug",
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestPublicPageEndpointsReturnOnlyPublicFields(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/pages", h.GetPublicPagesHandler)
	r.GET("/api/public/pages/:slug/dashboard", middleware.OptionalAuthMiddleware(), h.GetPublicDashboardHandler)
	r.GET("/api/pages/slug/:slug", middleware.OptionalAuthMiddleware(), h.GetStatusPageBySlugHandler)

	role := models.Role{Name: "DTO Viewer", Permissions: "incidents:read"}
	db.Create(&role)
	page := models.StatusPage{
		Name: "DTO Page", Slug: "dto-page", Description: "Beschreibung", LogoURL: "https://cdn.example/logo.png",
		IsPublicIndexed: true, DefaultRoleID: &role.ID, AutoJoinEmailDomains: "intern.example",
		NotificationFromAddress: "Intern <ops@intern.example>", CustomDomain: "status.intern.example",
		CustomDomainStatus: "verified", PrimaryColor: "#112233", SecondaryColor: "#445566",
		HeaderStyle: "centered", PageLayout: "liste",
	}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&models.Incident{StatusPageID: page.ID, Title: "Störung", Message: "m", Status: "investigating"}).Error)
	require.NoError(t, db.Create(&models.GeneralNotice{StatusPageID: page.ID, Title: "Hinweis", Message: "m", Level: "info", IsActive: true}).Error)

	check := func(t *testing.T, got map[string]interface{}) {
		t.Helper()
		assert.Equal(t, publicPageKeys, sortedKeys(got))
		assert.Equal(t, float64(page.ID), got["ID"])
		assert.Equal(t, "DTO Page", got["Name"])
		assert.Equal(t, "dto-page", got["Slug"])
		assert.Equal(t, "Beschreibung", got["Description"])
		assert.Equal(t, "https://cdn.example/logo.png", got["LogoURL"])
		assert.Equal(t, "#112233", got["PrimaryColor"])
		assert.Equal(t, "#445566", got["SecondaryColor"])
		assert.Equal(t, "centered", got["HeaderStyle"])
		assert.Equal(t, "liste", got["PageLayout"])
	}

	t.Run("GET /api/public/pages", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/public/pages", nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var list []map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
		require.Len(t, list, 1)
		check(t, list[0])
	})

	t.Run("GET /api/public/pages/:slug/dashboard", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/public/pages/dto-page/dashboard", nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		check(t, resp["page"].(map[string]interface{}))
		// Incident/GeneralNotice trugen bisher ein leeres, nie geladenes
		// "StatusPage"-Objekt mit dem kompletten Konfigurations-Schema der
		// Seite (AutoJoinEmailDomains, DefaultRoleID, ...) - wie bei Component
		// fällt es jetzt ganz weg.
		for _, key := range []string{"incidents", "notices"} {
			items := resp[key].([]interface{})
			require.Len(t, items, 1, key)
			assert.NotContains(t, items[0].(map[string]interface{}), "StatusPage", key)
		}
	})

	t.Run("GET /api/pages/slug/:slug", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/pages/slug/dto-page", nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		check(t, resp)
	})
}

// TestPublicPagesHandler_DBErrorReturns500 deckt den Fehlerzweig der
// Listen-Abfrage ab (geschlossene DB-Verbindung).
func TestPublicPagesHandler_DBErrorReturns500(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/pages", h.GetPublicPagesHandler)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	_ = sqlDB.Close()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/public/pages", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
