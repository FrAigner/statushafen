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
	enableTranslation(t, f.db)

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
	enableTranslation(t, f.db)

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

// With the switch off (the default) content only has the page's default
// language: nothing else is offered, delivered or accepted, and stored
// translations reappear once the switch is on again.
func TestContentLanguages_SwitchOffOnlyDefaultLanguage(t *testing.T) {
	f := newContentLangFixture(t)
	pid, iid := f.pid(), strconv.Itoa(int(f.incident.ID))
	tmpl := models.IncidentTemplate{StatusPageID: f.page.ID, Name: "T", Title: "T", Message: "M"}
	require.NoError(t, f.db.Create(&tmpl).Error)
	upd := models.IncidentUpdate{IncidentID: f.incident.ID, Message: "U", Status: "investigating"}
	require.NoError(t, f.db.Create(&upd).Error)
	var enTr models.IncidentTranslation
	require.NoError(t, f.db.Where("language_code = ?", "en").First(&enTr).Error)
	trID := strconv.Itoa(int(enTr.ID))
	require.NoError(t, f.db.Create(&models.NotificationTemplate{StatusPageID: f.page.ID, Kind: "incident_created", Audience: "subscriber", Language: "en", Subject: "S", BodyHTML: "B"}).Error)
	require.NoError(t, f.db.Create(&models.NotificationTemplate{StatusPageID: f.page.ID, Kind: "incident_created", Audience: "subscriber", Language: "de", Subject: "S", BodyHTML: "B"}).Error)

	w, _ := f.do(t, http.MethodGet, "/api/pages/"+pid+"/languages", nil)
	assert.Equal(t, []string{"de"}, languageCodes(t, w.Body.Bytes(), "LanguageCode"))
	w, _ = f.do(t, http.MethodGet, "/api/public/pages/"+f.page.Slug+"/dashboard", nil)
	var dash struct {
		Languages json.RawMessage `json:"languages"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &dash))
	assert.Equal(t, []string{"de"}, languageCodes(t, dash.Languages, "code"))

	// Reads: only the default language, a requested other language is ignored.
	w, body := f.do(t, http.MethodGet, "/api/public/pages/"+f.page.Slug+"/translations?lang=en", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "de", body["language"])
	assert.Empty(t, body["incidents"])
	w, body = f.do(t, http.MethodGet, "/api/public/pages/"+f.page.Slug+"/incidents/"+iid+"/update-translations?lang=en", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "de", body["language"])
	for _, path := range []string{"/api/incidents/" + iid + "/translations", "/api/incidents/" + iid + "/translations/all",
		"/api/pages/" + pid + "/incident-templates/" + strconv.Itoa(int(tmpl.ID)) + "/translations",
		"/api/pages/" + pid + "/incidents/" + iid + "/updates/" + strconv.Itoa(int(upd.ID)) + "/translations"} {
		w, _ = f.do(t, http.MethodGet, path, nil)
		require.Equal(t, http.StatusOK, w.Code, path)
		assert.JSONEq(t, `[]`, w.Body.String(), path)
	}
	w, _ = f.do(t, http.MethodGet, "/api/pages/"+pid+"/notification-templates", nil)
	assert.Equal(t, []string{"de"}, languageCodes(t, w.Body.Bytes(), "Language"))

	// Writes: refused with translation_disabled, nothing changes.
	writes := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/incidents/" + iid + "/translations", map[string]any{"language_code": "en", "title": "T", "message": "M", "source": "manual"}},
		{http.MethodPut, "/api/incidents/" + iid + "/translations/en", map[string]any{"title": "T", "message": "M", "status": "draft"}},
		{http.MethodPatch, "/api/incident-translations/" + trID + "/confirm", nil},
		{http.MethodDelete, "/api/incident-translations/" + trID, nil},
		{http.MethodPost, "/api/pages/" + pid + "/translate", map[string]any{"target_language": "en", "title": "T"}},
		{http.MethodPut, "/api/pages/" + pid + "/incident-templates/" + strconv.Itoa(int(tmpl.ID)) + "/translations/en", map[string]any{"title": "T", "message": "M", "status": "draft"}},
		{http.MethodPut, "/api/pages/" + pid + "/incidents/" + iid + "/updates/" + strconv.Itoa(int(upd.ID)) + "/translations/en", map[string]any{"message": "M", "status": "draft"}},
		{http.MethodPut, "/api/pages/" + pid + "/notification-templates/incident_created/subscriber/en", map[string]any{"Subject": "S", "BodyHTML": "B"}},
		{http.MethodDelete, "/api/pages/" + pid + "/notification-templates/incident_created/subscriber/en", nil},
		{http.MethodPut, "/api/pages/" + pid + "/languages", map[string]any{"codes": []string{"de", "en"}, "default": "de"}},
	}
	for _, wr := range writes {
		w, body = f.do(t, wr.method, wr.path, wr.body)
		assert.Equal(t, http.StatusConflict, w.Code, "%s %s: %s", wr.method, wr.path, w.Body.String())
		assert.Equal(t, "translation_disabled", body["code"], wr.path)
	}
	var count int64
	f.db.Model(&models.IncidentTranslation{}).Count(&count)
	assert.EqualValues(t, 2, count, "stored translations are kept")
	f.db.Model(&models.NotificationTemplate{}).Where("language = ?", "en").Count(&count)
	assert.EqualValues(t, 1, count, "the en template is kept")

	// The default-language template stays editable.
	w, _ = f.do(t, http.MethodPut, "/api/pages/"+pid+"/notification-templates/incident_created/subscriber/de", map[string]any{"Subject": "S2", "BodyHTML": "B2"})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// The default language can still be changed; other assignments are kept.
	w, _ = f.do(t, http.MethodPut, "/api/pages/"+pid+"/languages", map[string]any{"codes": []string{"en"}, "default": "en"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, []string{"en"}, languageCodes(t, w.Body.Bytes(), "LanguageCode"))
	var rows []models.StatusPageLanguage
	require.NoError(t, f.db.Where("status_page_id = ?", f.page.ID).Order("language_code").Find(&rows).Error)
	require.Len(t, rows, 3)
	assert.Equal(t, []bool{false, true, false}, []bool{rows[0].IsDefault, rows[1].IsDefault, rows[2].IsDefault}, "de, en (default), fr")
	w, body = f.do(t, http.MethodPut, "/api/pages/"+pid+"/languages", map[string]any{"codes": []string{"fr"}, "default": "fr"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "language_codes_unknown", body["code"])

	// Switching on brings the second language and the stored translations back.
	enableTranslation(t, f.db)
	w, _ = f.do(t, http.MethodGet, "/api/pages/"+pid+"/languages", nil)
	assert.Equal(t, []string{"de", "en"}, languageCodes(t, w.Body.Bytes(), "LanguageCode"))
	w, _ = f.do(t, http.MethodGet, "/api/incidents/"+iid+"/translations/all", nil)
	assert.Equal(t, []string{"en"}, languageCodes(t, w.Body.Bytes(), "LanguageCode"))
}

// A page with the switch off can switch its default to a language it had no
// row for yet.
func TestContentLanguages_SwitchOffNewDefaultLanguage(t *testing.T) {
	f := newContentLangFixture(t)
	require.NoError(t, f.db.Unscoped().Where("status_page_id = ? AND language_code = ?", f.page.ID, "en").Delete(&models.StatusPageLanguage{}).Error)
	w, _ := f.do(t, http.MethodPut, "/api/pages/"+f.pid()+"/languages", map[string]any{"codes": []string{"en"}, "default": "en"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, []string{"en"}, languageCodes(t, w.Body.Bytes(), "LanguageCode"))
	var n int64
	f.db.Model(&models.StatusPageLanguage{}).Where("status_page_id = ?", f.page.ID).Count(&n)
	assert.EqualValues(t, 3, n, "de and fr are kept, en is added")
}

func enableTranslation(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, testutils.EnableTranslation(db))
}
