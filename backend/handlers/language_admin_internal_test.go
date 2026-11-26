package handlers

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Error paths of the language catalog and page languages: broken inputs and
// database errors yield an error code without raw error text; on an
// error during saving, the page's previous language selection is preserved.
func TestLanguageHandlers_ErrorPaths(t *testing.T) {
	langID := func(l models.Language, _ models.StatusPage) gin.Params {
		return gin.Params{{Key: "id", Value: strconv.Itoa(int(l.ID))}}
	}
	pageParam := func(_ models.Language, p models.StatusPage) gin.Params {
		return gin.Params{{Key: "page_id", Value: strconv.Itoa(int(p.ID))}}
	}
	put := `{"codes":["de","en"],"default":"de"}`

	cases := []struct {
		name       string
		method     string
		body       string
		params     func(models.Language, models.StatusPage) gin.Params
		op, table  string
		skip       int
		call       func(h *Handler, c *gin.Context)
		wantStatus int
		wantCode   string
	}{
		{name: "Katalog lesen scheitert", method: "GET", op: "query", table: "languages", call: (*Handler).GetLanguagesHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Anlegen: kaputter Body", method: "POST", body: "{", call: (*Handler).CreateLanguageHandler, wantStatus: 400, wantCode: "invalid_request"},
		{name: "Anlegen: Duplikatprüfung scheitert", method: "POST", body: `{"code":"en","name":"English"}`, op: "query", table: "languages", call: (*Handler).CreateLanguageHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Anlegen: Speichern scheitert", method: "POST", body: `{"code":"en","name":"English"}`, op: "create", table: "languages", call: (*Handler).CreateLanguageHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Ändern: kaputter Body", method: "PUT", body: "{", params: langID, call: (*Handler).UpdateLanguageHandler, wantStatus: 400, wantCode: "invalid_request"},
		{name: "Ändern: Speichern scheitert", method: "PUT", body: `{"name":"Deutsch 2"}`, op: "update", table: "languages", params: langID, call: (*Handler).UpdateLanguageHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Seitensprachen: kaputter Body", method: "PUT", body: "{", params: pageParam, call: (*Handler).PutPageLanguagesHandler, wantStatus: 400, wantCode: "invalid_request"},
		{name: "Seitensprachen: Prüfung scheitert", method: "PUT", body: put, op: "query", table: "languages", params: pageParam, call: (*Handler).PutPageLanguagesHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Seitensprachen: Löschen der alten Auswahl scheitert", method: "PUT", body: put, op: "delete", table: "status_page_languages", params: pageParam, call: (*Handler).PutPageLanguagesHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Seitensprachen: Anlegen der neuen Auswahl scheitert", method: "PUT", body: put, op: "create", table: "status_page_languages", params: pageParam, call: (*Handler).PutPageLanguagesHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Seitensprachen: Nachladen scheitert", method: "PUT", body: put, op: "query", table: "status_page_languages", params: pageParam, call: (*Handler).PutPageLanguagesHandler, wantStatus: 500, wantCode: "server_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := testutils.SetupTestDB()
			require.NoError(t, testutils.EnableTranslation(db))
			h := NewHandler(db)
			page := models.StatusPage{Name: "P", Slug: "p-lang-err"}
			require.NoError(t, db.Create(&page).Error)
			de := models.Language{Code: "de", Name: "Deutsch", IsActive: true}
			en := models.Language{Code: "en", Name: "English", IsActive: true}
			require.NoError(t, db.Create(&de).Error)
			require.NoError(t, db.Create(&en).Error)
			require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true}).Error)
			if tc.method == "POST" {
				// Only de and en can be created: remove en entirely so creating it is a fresh insert.
				require.NoError(t, db.Unscoped().Delete(&en).Error)
			}

			c, w := pageTestContext(tc.method, tc.body, uint(1))
			if tc.params != nil {
				c.Params = tc.params(de, page)
			}
			remove := func() {}
			if tc.op != "" {
				remove = injectDBError(t, db, tc.op, tc.table, tc.skip)
			}
			tc.call(h, c)
			remove()

			assert.Equal(t, tc.wantStatus, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"code":"`+tc.wantCode+`"`)
			assert.NotContains(t, w.Body.String(), "injizierter")

			if tc.name == "Seitensprachen: Anlegen der neuen Auswahl scheitert" {
				var kept []models.StatusPageLanguage
				require.NoError(t, db.Where("status_page_id = ?", page.ID).Find(&kept).Error)
				require.Len(t, kept, 1, "Transaktion zurückgerollt: alte Auswahl bleibt")
				assert.Equal(t, "de", kept[0].LanguageCode)
			}
		})
	}
}

// A catalog entry created with is_active is saved, and empty
// entries in the page's code list are skipped.
func TestLanguageHandlers_CreateWithActiveFlagAndBlankCodes(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "P", Slug: "p-lang-blank"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&models.Language{Code: "de", Name: "Deutsch", IsActive: true}).Error)

	c, w := pageTestContext(http.MethodPost, `{"code":"en","name":"English","is_active":false}`, uint(1))
	h.CreateLanguageHandler(c)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var n int64
	require.NoError(t, db.Model(&models.Language{}).Where("code = ?", "en").Count(&n).Error)
	assert.Equal(t, int64(1), n)

	c, w = pageTestContext(http.MethodPut, `{"codes":["de","  ",""],"default":"de"}`, uint(1))
	c.Params = gin.Params{{Key: "page_id", Value: strconv.Itoa(int(page.ID))}}
	h.PutPageLanguagesHandler(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var stored []models.StatusPageLanguage
	require.NoError(t, db.Where("status_page_id = ?", page.ID).Find(&stored).Error)
	require.Len(t, stored, 1)
	assert.Equal(t, "de", stored[0].LanguageCode)
}

// With the switch off, changing the default language only moves the default flag;
// a failing step rolls back and answers 500 without touching the assignments.
func TestPutPageLanguages_SwitchOffErrorPaths(t *testing.T) {
	cases := []struct {
		name, op string
		skip     int
	}{
		{"clearing the old default fails", "update", 0},
		{"looking up the new default fails", "query", 0},
		{"setting the new default fails", "update", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := testutils.SetupTestDB()
			h := NewHandler(db)
			page := models.StatusPage{Name: "P", Slug: "p-lang-off"}
			require.NoError(t, db.Create(&page).Error)
			for _, l := range []models.Language{{Code: "de", Name: "Deutsch", IsActive: true}, {Code: "en", Name: "English", IsActive: true}} {
				require.NoError(t, db.Create(&l).Error)
			}
			require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true}).Error)
			require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "en"}).Error)

			c, w := pageTestContext(http.MethodPut, `{"codes":["en"],"default":"en"}`, uint(1))
			c.Params = gin.Params{{Key: "page_id", Value: strconv.Itoa(int(page.ID))}}
			remove := injectDBError(t, db, tc.op, "status_page_languages", tc.skip)
			h.PutPageLanguagesHandler(c)
			remove()

			assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"code":"server_error"`)
			var def models.StatusPageLanguage
			require.NoError(t, db.Where("status_page_id = ? AND is_default = ?", page.ID, true).First(&def).Error)
			assert.Equal(t, "de", def.LanguageCode, "rolled back")
		})
	}
}
