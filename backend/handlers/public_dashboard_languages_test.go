package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The public dashboard lists the languages enabled for the page
// (code, native name, default), so that the status page's language switcher
// offers all of them and not just the UI languages de/en.
func TestPublicDashboard_ListetSeitenSprachen(t *testing.T) {
	db := testutils.SetupTestDB()
	require.NoError(t, testutils.EnableTranslation(db))
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/pages/:slug/dashboard", middleware.OptionalAuthMiddleware(), h.GetPublicDashboardHandler)

	page := models.StatusPage{Name: "Sprachen", Slug: "sprachen"}
	require.NoError(t, db.Create(&page).Error)
	other := models.StatusPage{Name: "Andere", Slug: "andere"}
	require.NoError(t, db.Create(&other).Error)
	for code, name := range map[string]string{"de": "Deutsch", "en": "English", "hr": "Hrvatski"} {
		require.NoError(t, db.Where("code = ?", code).FirstOrCreate(&models.Language{Code: code, Name: name, IsActive: true}).Error)
	}
	for _, l := range []models.StatusPageLanguage{
		{StatusPageID: page.ID, LanguageCode: "hr"},
		{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true},
		{StatusPageID: page.ID, LanguageCode: "en"},
		{StatusPageID: other.ID, LanguageCode: "de", IsDefault: true},
	} {
		require.NoError(t, db.Create(&l).Error)
	}

	get := func(slug string) []map[string]any {
		req, _ := http.NewRequest("GET", "/api/public/pages/"+slug+"/dashboard", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var body struct {
			Languages []map[string]any `json:"languages"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body.Languages
	}

	// hr is a legacy assignment (no content language any more) and is not listed.
	assert.Equal(t, []map[string]any{
		{"code": "de", "name": "Deutsch", "is_default": true, "is_active": true},
		{"code": "en", "name": "English", "is_default": false, "is_active": true},
	}, get("sprachen"))

	// A language deactivated in the catalog stays in the list for the
	// switcher, but is marked is_active=false (the subscription selections hide it).
	require.NoError(t, db.Model(&models.Language{}).Where("code = ?", "en").Update("is_active", false).Error)
	assert.Contains(t, get("sprachen"), map[string]any{"code": "en", "name": "English", "is_default": false, "is_active": false})

	// Only the languages of the page itself.
	assert.Len(t, get("andere"), 1)

	// Without maintained languages: empty list (not null), the switcher falls back.
	empty := models.StatusPage{Name: "Leer", Slug: "leer"}
	require.NoError(t, db.Create(&empty).Error)
	assert.Equal(t, []map[string]any{}, get("leer"))
}
