package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// contentLangFixture is a page with default de, additional en and a legacy fr
// assignment (fr inactive in the catalog, as after the startup migration), plus an
// incident with confirmed en and fr translations.
type contentLangFixture struct {
	db       *gorm.DB
	r        http.Handler
	admin    string
	page     models.StatusPage
	incident models.Incident
}

func newContentLangFixture(t *testing.T) contentLangFixture {
	t.Helper()
	db := testutils.SetupTestDB()
	f := contentLangFixture{db: db, r: newRouter(handlers.NewHandler(db))}
	for _, l := range []models.Language{{Code: "de", Name: "Deutsch", IsActive: true}, {Code: "en", Name: "English", IsActive: true}, {Code: "fr", Name: "Français", IsActive: true}} {
		require.NoError(t, db.Create(&l).Error)
	}
	require.NoError(t, db.Model(&models.Language{}).Where("code = ?", "fr").Update("is_active", false).Error)
	f.page = models.StatusPage{Name: "Languages", Slug: "content-languages"}
	require.NoError(t, db.Create(&f.page).Error)
	for _, l := range []models.StatusPageLanguage{
		{StatusPageID: f.page.ID, LanguageCode: "de", IsDefault: true},
		{StatusPageID: f.page.ID, LanguageCode: "en"},
		{StatusPageID: f.page.ID, LanguageCode: "fr"},
	} {
		require.NoError(t, db.Create(&l).Error)
	}
	f.incident = models.Incident{StatusPageID: f.page.ID, Title: "Störung", Message: "Text", Status: "investigating", Severity: "minor"}
	require.NoError(t, db.Create(&f.incident).Error)
	for _, code := range []string{"en", "fr"} {
		require.NoError(t, db.Create(&models.IncidentTranslation{IncidentID: f.incident.ID, LanguageCode: code, Title: "T-" + code, Message: "M-" + code, Status: "confirmed", Source: "manual"}).Error)
	}
	u := models.User{Email: "admin@content-languages.test", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&u).Error)
	f.admin, _ = middleware.GenerateToken(u.ID, u.Email, "", "", true)
	return f
}

func (f contentLangFixture) do(t *testing.T, method, url string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, url, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+f.admin)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	var obj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &obj)
	return w, obj
}

func (f contentLangFixture) pid() string { return strconv.Itoa(int(f.page.ID)) }

func languageCodes(t *testing.T, raw []byte, key string) []string {
	t.Helper()
	var rows []map[string]any
	require.NoError(t, json.Unmarshal(raw, &rows), string(raw))
	codes := []string{}
	for _, r := range rows {
		codes = append(codes, r[key].(string))
	}
	return codes
}

// The catalog API only creates or activates de and en.
func TestContentLanguages_CatalogOnlyGermanAndEnglish(t *testing.T) {
	f := newContentLangFixture(t)

	w, body := f.do(t, http.MethodPost, "/api/languages", map[string]any{"code": "it", "name": "Italiano"})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, "language_not_supported", body["code"])

	var fr models.Language
	require.NoError(t, f.db.Where("code = ?", "fr").First(&fr).Error)
	w, body = f.do(t, http.MethodPut, "/api/languages/"+strconv.Itoa(int(fr.ID)), map[string]any{"name": "Français", "is_active": true})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, "language_not_supported", body["code"])
	// Renaming without activating stays possible.
	w, _ = f.do(t, http.MethodPut, "/api/languages/"+strconv.Itoa(int(fr.ID)), map[string]any{"name": "Francais"})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w, _ = f.do(t, http.MethodGet, "/api/languages", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"de", "en"}, languageCodes(t, w.Body.Bytes(), "Code"))

	// A page cannot select another language, even if it were active in the catalog.
	require.NoError(t, f.db.Model(&models.Language{}).Where("code = ?", "fr").Update("is_active", true).Error)
	w, body = f.do(t, http.MethodPut, "/api/pages/"+f.pid()+"/languages", map[string]any{"codes": []string{"de", "fr"}, "default": "de"})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, "language_codes_unknown", body["code"])
}

// Legacy assignments and translations of other languages stay stored but are
// neither offered nor delivered.
func TestContentLanguages_OtherLanguagesNotDelivered(t *testing.T) {
	f := newContentLangFixture(t)

	w, _ := f.do(t, http.MethodGet, "/api/pages/"+f.pid()+"/languages", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"de", "en"}, languageCodes(t, w.Body.Bytes(), "LanguageCode"))

	w, _ = f.do(t, http.MethodGet, "/api/public/pages/"+f.page.Slug+"/dashboard", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var dash struct {
		Languages json.RawMessage `json:"languages"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &dash))
	assert.Equal(t, []string{"de", "en"}, languageCodes(t, dash.Languages, "code"))

	w, _ = f.do(t, http.MethodGet, "/api/public/pages/"+f.page.Slug+"/translations?lang=fr", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	w, _ = f.do(t, http.MethodGet, "/api/public/pages/"+f.page.Slug+"/translations?lang=en", nil)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	iid := strconv.Itoa(int(f.incident.ID))
	w, _ = f.do(t, http.MethodGet, "/api/incidents/"+iid+"/translations", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"en"}, languageCodes(t, w.Body.Bytes(), "LanguageCode"))
	w, _ = f.do(t, http.MethodGet, "/api/incidents/"+iid+"/translations/all", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"en"}, languageCodes(t, w.Body.Bytes(), "LanguageCode"))

	w, body := f.do(t, http.MethodPut, "/api/incidents/"+iid+"/translations/fr", map[string]any{"title": "T", "message": "M", "status": "draft"})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, "language_not_available", body["code"])

	w, _ = f.do(t, http.MethodPut, "/api/pages/"+f.pid()+"/notification-templates/incident_created/subscriber/fr", map[string]any{"Subject": "S", "BodyHTML": "B"})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	var stored int64
	require.NoError(t, f.db.Model(&models.IncidentTranslation{}).Where("language_code = ?", "fr").Count(&stored).Error)
	assert.EqualValues(t, 1, stored, "the fr translation stays stored")
}
