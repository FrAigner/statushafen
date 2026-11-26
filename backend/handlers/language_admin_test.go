package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Stage 1 of 5 of multilingual support: CRUD tests for the
// global language catalog (Language) and the languages activated per status page
// languages (StatusPageLanguage).

func itoa(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}

func TestLanguageCatalogHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/languages", h.GetLanguagesHandler)
	r.POST("/api/languages", h.CreateLanguageHandler)
	r.PUT("/api/languages/:id", h.UpdateLanguageHandler)

	t.Run("Create language", func(t *testing.T) {
		w := doTagRequest(r, "POST", "/api/languages", map[string]interface{}{"code": "EN", "name": "English"})
		requireStatusCode(t, w, http.StatusCreated)
		var lang models.Language
		_ = json.Unmarshal(w.Body.Bytes(), &lang)
		if lang.Code != "en" || !lang.IsActive {
			t.Errorf("Language created incorrectly: %+v", lang)
		}
	})

	t.Run("Duplicate code rejected", func(t *testing.T) {
		w := doTagRequest(r, "POST", "/api/languages", map[string]interface{}{"code": "en", "name": "English again"})
		requireStatusCode(t, w, http.StatusConflict)
	})

	t.Run("List includes seeded + new language", func(t *testing.T) {
		w := doTagRequest(r, "GET", "/api/languages", nil)
		requireStatusCode(t, w, http.StatusOK)
		var list []models.Language
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 1 {
			t.Fatalf("expected exactly one language (test DB has no seeding), got %+v", list)
		}
	})

	t.Run("Update renames and can deactivate", func(t *testing.T) {
		var lang models.Language
		db.Where("code = ?", "en").First(&lang)
		active := false
		w := doTagRequest(r, "PUT", "/api/languages/"+itoa(lang.ID), map[string]interface{}{"name": "English (updated)", "is_active": &active})
		requireStatusCode(t, w, http.StatusOK)
		var updated models.Language
		_ = json.Unmarshal(w.Body.Bytes(), &updated)
		if updated.Name != "English (updated)" || updated.IsActive {
			t.Errorf("expected renamed + deactivated language, got %+v", updated)
		}
	})

	t.Run("Create rejects empty code/name", func(t *testing.T) {
		w := doTagRequest(r, "POST", "/api/languages", map[string]interface{}{"code": "  ", "name": "Egal"})
		requireStatusCode(t, w, http.StatusBadRequest)
		w = doTagRequest(r, "POST", "/api/languages", map[string]interface{}{"code": "xx", "name": "  "})
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Create rejects missing code field", func(t *testing.T) {
		w := doTagRequest(r, "POST", "/api/languages", map[string]interface{}{"name": "Fehlt der Code"})
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Update rejects invalid id and unknown id", func(t *testing.T) {
		w := doTagRequest(r, "PUT", "/api/languages/not-a-number", map[string]interface{}{"name": "Egal"})
		requireStatusCode(t, w, http.StatusBadRequest)
		w = doTagRequest(r, "PUT", "/api/languages/999999", map[string]interface{}{"name": "Egal"})
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Update rejects empty name", func(t *testing.T) {
		var lang models.Language
		db.Where("code = ?", "en").First(&lang)
		w := doTagRequest(r, "PUT", "/api/languages/"+itoa(lang.ID), map[string]interface{}{"name": "  "})
		requireStatusCode(t, w, http.StatusBadRequest)
	})
}

func TestPageLanguagesHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	require.NoError(t, testutils.EnableTranslation(db))
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/languages", h.CreateLanguageHandler)
	r.GET("/api/pages/:page_id/languages", h.GetPageLanguagesHandler)
	r.PUT("/api/pages/:page_id/languages", h.PutPageLanguagesHandler)

	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	if err := db.Create(&page).Error; err != nil {
		t.Fatalf("failed to seed status page: %v", err)
	}

	// The global catalog deliberately does not exist automatically in the test (see
	// TestLanguageCatalogHandlers) - PutPageLanguagesHandler validates the
	// requested codes against it, so they must be created first.
	doTagRequest(r, "POST", "/api/languages", map[string]interface{}{"code": "de", "name": "Deutsch"})
	doTagRequest(r, "POST", "/api/languages", map[string]interface{}{"code": "en", "name": "English"})

	t.Run("Empty page has no configured languages", func(t *testing.T) {
		w := doTagRequest(r, "GET", "/api/pages/"+itoa(page.ID)+"/languages", nil)
		requireStatusCode(t, w, http.StatusOK)
		var list []models.StatusPageLanguage
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 0 {
			t.Fatalf("expected no configured languages yet, got %+v", list)
		}
	})

	t.Run("Put enables de+en with de as default", func(t *testing.T) {
		w := doTagRequest(r, "PUT", "/api/pages/"+itoa(page.ID)+"/languages", map[string]interface{}{
			"codes": []string{"de", "en"}, "default": "de",
		})
		requireStatusCode(t, w, http.StatusOK)
		var list []models.StatusPageLanguage
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 2 {
			t.Fatalf("expected two configured languages, got %+v", list)
		}
		var defaults int
		for _, l := range list {
			if l.IsDefault {
				defaults++
				if l.LanguageCode != "de" {
					t.Errorf("expected de to be default, got %q", l.LanguageCode)
				}
			}
		}
		if defaults != 1 {
			t.Fatalf("expected exactly one default language, got %d", defaults)
		}
	})

	t.Run("Put replaces the previous set (en instead of de/en)", func(t *testing.T) {
		w := doTagRequest(r, "PUT", "/api/pages/"+itoa(page.ID)+"/languages", map[string]interface{}{
			"codes": []string{"en"}, "default": "en",
		})
		requireStatusCode(t, w, http.StatusOK)
		var list []models.StatusPageLanguage
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 1 || list[0].LanguageCode != "en" || !list[0].IsDefault {
			t.Fatalf("expected exactly [en] as default, got %+v", list)
		}
	})

	t.Run("Default must be part of codes", func(t *testing.T) {
		w := doTagRequest(r, "PUT", "/api/pages/"+itoa(page.ID)+"/languages", map[string]interface{}{
			"codes": []string{"de"}, "default": "en",
		})
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Unknown code is rejected", func(t *testing.T) {
		w := doTagRequest(r, "PUT", "/api/pages/"+itoa(page.ID)+"/languages", map[string]interface{}{
			"codes": []string{"xx"}, "default": "xx",
		})
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Empty codes list is rejected", func(t *testing.T) {
		w := doTagRequest(r, "PUT", "/api/pages/"+itoa(page.ID)+"/languages", map[string]interface{}{
			"codes": []string{}, "default": "",
		})
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Invalid page_id is rejected", func(t *testing.T) {
		w := doTagRequest(r, "GET", "/api/pages/not-a-number/languages", nil)
		requireStatusCode(t, w, http.StatusBadRequest)
		w = doTagRequest(r, "PUT", "/api/pages/not-a-number/languages", map[string]interface{}{
			"codes": []string{"de"}, "default": "de",
		})
		requireStatusCode(t, w, http.StatusBadRequest)
	})
}

// is_active=false on creation must not be overridden by the column default (true);
// response and stored row stay inactive.
func TestCreateLanguageHandler_IsActiveWirdUebernommen(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/languages", h.CreateLanguageHandler)

	cases := []struct {
		name, code string
		payload    map[string]any
		want       bool
	}{
		{"inaktiv", "de", map[string]any{"code": "de", "name": "Deutsch", "is_active": false}, false},
		{"aktiv explizit", "en", map[string]any{"code": "en", "name": "English", "is_active": true}, true},
		{"ohne Angabe: aktiv", "de", map[string]any{"code": "de", "name": "Deutsch"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doTagRequest(r, "POST", "/api/languages", tc.payload)
			requireStatusCode(t, w, http.StatusCreated)
			var resp models.Language
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, tc.want, resp.IsActive, "Antwort")
			var stored models.Language
			require.NoError(t, db.Where("code = ?", tc.code).First(&stored).Error)
			assert.Equal(t, tc.want, stored.IsActive, "gespeicherte Zeile")
			// Only de and en exist: remove the row entirely so the next case can create it again.
			require.NoError(t, db.Unscoped().Delete(&stored).Error)
		})
	}

	t.Run("gelöschte Sprache lebt inaktiv wieder auf", func(t *testing.T) {
		en := models.Language{Code: "en", Name: "English", IsActive: true}
		require.NoError(t, db.Create(&en).Error)
		require.NoError(t, db.Delete(&en).Error)
		w := doTagRequest(r, "POST", "/api/languages", map[string]any{"code": "en", "name": "English 2", "is_active": false})
		requireStatusCode(t, w, http.StatusCreated)
		var stored models.Language
		require.NoError(t, db.Where("code = ?", "en").First(&stored).Error)
		assert.False(t, stored.IsActive)
	})
}

// If the subsequent setting of is_active fails, nothing is reported as success.
func TestCreateLanguageHandler_InaktivSetzenScheitertGibt500(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/languages", h.CreateLanguageHandler)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("lang_fail", func(tx *gorm.DB) {
		_ = tx.AddError(errors.New("db down"))
	}))
	w := doTagRequest(r, "POST", "/api/languages", map[string]any{"code": "de", "name": "Deutsch", "is_active": false})
	requireStatusCode(t, w, http.StatusInternalServerError)
	assert.Contains(t, w.Body.String(), `"code":"server_error"`)
	var n int64
	db.Model(&models.Language{}).Where("code = ?", "de").Count(&n)
	assert.EqualValues(t, 0, n, "nichts halb angelegt")
}
