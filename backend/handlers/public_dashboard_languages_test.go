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

// Das öffentliche Dashboard nennt die für die Seite aktivierten
// Sprachen (Code, Eigenbezeichnung, Standard), damit der Sprachumschalter der
// Statusseite alle anbietet und nicht nur die Oberflächensprachen de/en.
func TestPublicDashboard_ListetSeitenSprachen(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/pages/:slug/dashboard", middleware.OptionalAuthMiddleware(), h.GetPublicDashboardHandler)

	page := models.StatusPage{Name: "Sprachen", Slug: "sprachen"}
	require.NoError(t, db.Create(&page).Error)
	other := models.StatusPage{Name: "Andere", Slug: "andere"}
	require.NoError(t, db.Create(&other).Error)
	for code, name := range map[string]string{"de": "Deutsch", "hr": "Hrvatski", "cs": "Čeština"} {
		require.NoError(t, db.Where("code = ?", code).FirstOrCreate(&models.Language{Code: code, Name: name, IsActive: true}).Error)
	}
	for _, l := range []models.StatusPageLanguage{
		{StatusPageID: page.ID, LanguageCode: "hr"},
		{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true},
		{StatusPageID: page.ID, LanguageCode: "cs"},
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

	assert.Equal(t, []map[string]any{
		{"code": "cs", "name": "Čeština", "is_default": false, "is_active": true},
		{"code": "de", "name": "Deutsch", "is_default": true, "is_active": true},
		{"code": "hr", "name": "Hrvatski", "is_default": false, "is_active": true},
	}, get("sprachen"))

	// Eine im Katalog deaktivierte Sprache bleibt für den Umschalter in der
	// Liste, ist aber als is_active=false markiert (die Abo-Auswahlen blenden sie aus).
	require.NoError(t, db.Model(&models.Language{}).Where("code = ?", "hr").Update("is_active", false).Error)
	assert.Contains(t, get("sprachen"), map[string]any{"code": "hr", "name": "Hrvatski", "is_default": false, "is_active": false})

	// Nur die Sprachen der eigenen Seite.
	assert.Len(t, get("andere"), 1)

	// Ohne gepflegte Sprachen: leere Liste (kein null), der Umschalter fällt zurück.
	empty := models.StatusPage{Name: "Leer", Slug: "leer"}
	require.NoError(t, db.Create(&empty).Error)
	assert.Equal(t, []map[string]any{}, get("leer"))
}
