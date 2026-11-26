package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The tag restriction (region EU may not edit resources of region US)
// applies to incidents and components themselves. The same restriction
// must apply to their translations, otherwise foreign-tagged content can
// be read, published (confirm) and deleted via the translation routes.

type tagTranslationCase struct {
	name       string
	permission string
	// seed creates the parent object with the given tags and four
	// translations (es, de, it, nl) and returns (parentID, translation IDs).
	seed func(t *testing.T, db *gorm.DB, pageID uint, tagIDs ...uint) (uint, []uint)
	// createBody returns the body for creating a translation in lang.
	createBody  func(lang string) string
	saveBody    string
	collection  string // %d = parent ID
	adminPrefix string // prefix for /:translation_id
}

func tagTranslationCases() []tagTranslationCase {
	return []tagTranslationCase{
		{
			name: "incident", permission: "incidents:write",
			collection: "/api/incidents/%d/translations", adminPrefix: "/api/incident-translations/",
			createBody: func(lang string) string {
				return `{"language_code":"` + lang + `","title":"T","message":"M","source":"manual"}`
			},
			saveBody: `{"title":"T","message":"M","status":"draft"}`,
			seed: func(t *testing.T, db *gorm.DB, pageID uint, tagIDs ...uint) (uint, []uint) {
				inc := models.Incident{StatusPageID: pageID, Title: "I", Message: "m", Status: "investigating"}
				require.NoError(t, db.Create(&inc).Error)
				for _, id := range tagIDs {
					require.NoError(t, db.Create(&models.IncidentTag{IncidentID: inc.ID, TagID: id}).Error)
				}
				var ids []uint
				for _, lang := range []string{"es", "de", "it", "nl"} {
					tr := models.IncidentTranslation{IncidentID: inc.ID, LanguageCode: lang, Title: "T", Message: "M", Status: "draft", Source: "manual"}
					require.NoError(t, db.Create(&tr).Error)
					ids = append(ids, tr.ID)
				}
				return inc.ID, ids
			},
		},
		{
			name: "component", permission: "components:write",
			collection: "/api/components/%d/translations", adminPrefix: "/api/component-translations/",
			createBody: func(lang string) string {
				return `{"language_code":"` + lang + `","title":"T","description":"D","source":"manual"}`
			},
			saveBody: `{"title":"T","description":"D","status":"draft"}`,
			seed: func(t *testing.T, db *gorm.DB, pageID uint, tagIDs ...uint) (uint, []uint) {
				comp := models.Component{StatusPageID: pageID, Name: "C"}
				require.NoError(t, db.Create(&comp).Error)
				for _, id := range tagIDs {
					require.NoError(t, db.Create(&models.ComponentTag{ComponentID: comp.ID, TagID: id}).Error)
				}
				var ids []uint
				for _, lang := range []string{"es", "de", "it", "nl"} {
					tr := models.ComponentTranslation{ComponentID: comp.ID, LanguageCode: lang, Title: "T", Description: "D", Status: "draft", Source: "manual"}
					require.NoError(t, db.Create(&tr).Error)
					ids = append(ids, tr.ID)
				}
				return comp.ID, ids
			},
		},
	}
}

func TestRouter_TranslationRoutesRespectTagRestriction(t *testing.T) {
	for _, tc := range tagTranslationCases() {
		t.Run(tc.name, func(t *testing.T) {
			db := testutils.SetupTestDB()
			require.NoError(t, testutils.EnableTranslation(db))
			r := newRouter(handlers.NewHandler(db))

			page := models.StatusPage{Name: "Seite", Slug: "tag-tr-" + tc.name}
			require.NoError(t, db.Create(&page).Error)
			// Save requires an additional language of the page.
			require.NoError(t, db.FirstOrCreate(&models.Language{Code: "en"}, models.Language{Code: "en"}).Error)
			require.NoError(t, db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "en"}).Error)
			tagEU := models.Tag{Name: "eu"}
			tagUS := models.Tag{Name: "us"}
			require.NoError(t, db.Create(&tagEU).Error)
			require.NoError(t, db.Create(&tagUS).Error)

			role := models.Role{Name: "Writer " + tc.name, Permissions: tc.permission}
			require.NoError(t, db.Create(&role).Error)
			tokenFor := func(email string, tags ...models.Tag) string {
				u := models.User{Email: email}
				require.NoError(t, db.Create(&u).Error)
				require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: role.ID}).Error)
				for _, tg := range tags {
					require.NoError(t, db.Create(&models.UserTag{UserID: u.ID, TagID: tg.ID}).Error)
				}
				tok, _ := middleware.GenerateToken(u.ID, u.Email, "", "", false)
				return tok
			}
			euToken := tokenFor("eu-"+tc.name+"@example.com", tagEU)
			untaggedToken := tokenFor("frei-" + tc.name + "@example.com")

			do := func(method, path, body, token string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				return w
			}
			id := func(n uint) string { return strconv.FormatUint(uint64(n), 10) }
			coll := func(parent uint) string { return strings.Replace(tc.collection, "%d", id(parent), 1) }

			usParent, usTr := tc.seed(t, db, page.ID, tagUS.ID)
			euParent, euTr := tc.seed(t, db, page.ID, tagEU.ID)
			plainParent, plainTr := tc.seed(t, db, page.ID)

			// Each operation gets a fresh language or
			// translation per call so that the calls do not interfere with each other.
			type op struct {
				desc string
				call func(parent uint, trs []uint, n int, token string) *httptest.ResponseRecorder
			}
			// Each parent gets one new translation in the page's additional language.
			newLangs := []string{"en", "en", "en"}
			ops := []op{
				{"Anlegen", func(p uint, _ []uint, n int, tok string) *httptest.ResponseRecorder {
					return do(http.MethodPost, coll(p), tc.createBody(newLangs[n]), tok)
				}},
				{"Alle lesen", func(p uint, _ []uint, _ int, tok string) *httptest.ResponseRecorder {
					return do(http.MethodGet, coll(p)+"/all", "", tok)
				}},
				{"Speichern", func(p uint, _ []uint, _ int, tok string) *httptest.ResponseRecorder {
					return do(http.MethodPut, coll(p)+"/en", tc.saveBody, tok)
				}},
				{"Bestätigen", func(_ uint, trs []uint, n int, tok string) *httptest.ResponseRecorder {
					return do(http.MethodPatch, tc.adminPrefix+id(trs[n])+"/confirm", "", tok)
				}},
				{"Löschen", func(_ uint, trs []uint, n int, tok string) *httptest.ResponseRecorder {
					return do(http.MethodDelete, tc.adminPrefix+id(trs[n]), "", tok)
				}},
			}

			// Attack: user with tag eu against resource with tag us; the
			// operations run first so that a missing guard visibly
			// changes data.
			for _, o := range ops {
				w := o.call(usParent, usTr, 0, euToken)
				assert.Equal(t, http.StatusForbidden, w.Code, "%s fremder Tag: %s", o.desc, w.Body.String())
				assert.Contains(t, w.Body.String(), `"code":"forbidden"`, o.desc)
			}
			var cnt int64
			var confirmed int64
			if tc.name == "incident" {
				db.Model(&models.IncidentTranslation{}).Where("incident_id = ?", usParent).Count(&cnt)
				db.Model(&models.IncidentTranslation{}).Where("incident_id = ? AND status = ?", usParent, "confirmed").Count(&confirmed)
			} else {
				db.Model(&models.ComponentTranslation{}).Where("component_id = ?", usParent).Count(&cnt)
				db.Model(&models.ComponentTranslation{}).Where("component_id = ? AND status = ?", usParent, "confirmed").Count(&confirmed)
			}
			assert.EqualValues(t, 4, cnt, "Übersetzungen der fremden Ressource unverändert")
			assert.EqualValues(t, 0, confirmed, "nichts der fremden Ressource wurde veröffentlicht")

			// Control case: same tag, resource without tag and user without tags
			// are allowed.
			for i, o := range ops {
				w := o.call(euParent, euTr, 0, euToken)
				assert.Less(t, w.Code, 300, "%s eigener Tag: %s", o.desc, w.Body.String())
				w = o.call(plainParent, plainTr, 0, euToken)
				assert.Less(t, w.Code, 300, "%s Ressource ohne Tag: %s", o.desc, w.Body.String())
				w = o.call(usParent, usTr, 1+i%2, untaggedToken)
				assert.Less(t, w.Code, 300, "%s Nutzer ohne Tags: %s", o.desc, w.Body.String())
			}
		})
	}
}

// Structural test: every admin route for translations of incidents and
// components carries the tag guard. A new route without it would silently
// bypass the tag restriction.
func TestMain_TranslationRoutesHaveTagGuard(t *testing.T) {
	src, err := os.ReadFile("main.go")
	require.NoError(t, err)
	// Every admin route that addresses an incident or a component via the path
	// (:incident_id, :component_id) or one of their translations
	// needs the guard - not only the known translation paths.
	route := regexp.MustCompile(`guarded\(authGroup, http\.Method\w+, "([^"]*(?::incident_id|:component_id|/(?:incident|component)-translations/)[^"]*)"`)
	found := 0
	for _, line := range strings.Split(string(src), "\n") {
		m := route.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		found++
		assert.Contains(t, line, "TagGuard", "Route %s ohne Tag-Wächter", m[1])
	}
	assert.Equal(t, 10, found, "erwartet: 5 Vorfall- und 5 Komponenten-Übersetzungsrouten")
}
