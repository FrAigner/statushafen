package handlers_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// createTranslationCase is one POST .../translations endpoint.
type createTranslationCase struct {
	route   string
	handler func(db *gorm.DB, pageID uint) (url string)
	content string
	table   string
	foreign string
}

// Creating a translation accepts only an additional language the page offers
// (like the save per language, PUT .../translations/:language_code): never the
// page's default language, a language the page does not offer or one that is
// no content language (only de and en exist). The code is normalised.
func TestCreateTranslation_OnlyAdditionalPageLanguages(t *testing.T) {
	cases := map[string]createTranslationCase{
		"incident": {route: "/api/incidents/:incident_id/translations", content: "message", table: "incident_translations", foreign: "incident_id",
			handler: func(db *gorm.DB, pageID uint) string {
				return "/api/incidents/" + strconv.Itoa(int(newTestIncident(db, pageID).ID)) + "/translations"
			}},
		"component": {route: "/api/components/:component_id/translations", content: "description", table: "component_translations", foreign: "component_id",
			handler: func(db *gorm.DB, pageID uint) string {
				return "/api/components/" + strconv.Itoa(int(newTestComponent(db, pageID).ID)) + "/translations"
			}},
		"notice": {route: "/api/notices/:notice_id/translations", content: "message", table: "general_notice_translations", foreign: "general_notice_id",
			handler: func(db *gorm.DB, pageID uint) string {
				return "/api/notices/" + strconv.Itoa(int(newTestGeneralNotice(db, pageID).ID)) + "/translations"
			}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for lang, want := range map[string]int{
				"en":  http.StatusCreated,
				" EN": http.StatusCreated,
				"de":  http.StatusBadRequest, // the page's default language
				"fr":  http.StatusBadRequest, // no content language, stored on the page from the old catalog
				"es":  http.StatusBadRequest, // not offered at all
			} {
				t.Run(lang, func(t *testing.T) {
					db, r, h := newTestSetup()
					switch name {
					case "incident":
						r.POST(tc.route, h.CreateIncidentTranslationHandler)
					case "component":
						r.POST(tc.route, h.CreateComponentTranslationHandler)
					default:
						r.POST(tc.route, h.CreateGeneralNoticeTranslationHandler)
					}
					page := newTestPage(db)
					for _, code := range []string{"de", "en", "fr"} {
						db.FirstOrCreate(&models.Language{Code: code, Name: code, IsActive: true}, models.Language{Code: code})
					}
					require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true}).Error)
					require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "en"}).Error)
					require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "fr"}).Error)
					url := tc.handler(db, page.ID)

					body, _ := json.Marshal(map[string]string{"language_code": lang, "title": "T", tc.content: "C", "source": "manual"})
					w := doTranslationRequest(r, http.MethodPost, url, body)
					require.Equal(t, want, w.Code, w.Body.String())
					var n int64
					db.Table(tc.table).Count(&n)
					if want != http.StatusCreated {
						assert.Contains(t, w.Body.String(), `"code":"language_not_available"`)
						assert.Zero(t, n, "nothing stored")
						return
					}
					var stored string
					require.NoError(t, db.Table(tc.table).Select("language_code").Row().Scan(&stored))
					assert.Equal(t, "en", stored)
				})
			}
		})
	}
}
