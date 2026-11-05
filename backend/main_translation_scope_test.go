package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"gorm.io/gorm"
)

// Die Übersetzungs-Schreibrouten
// (POST /api/{incidents,components,notices}/:id/translations,
// PATCH/DELETE /api/*-translations/:translation_id) hingen an
// RequirePermission ohne :page_id und lieferten jedem Nicht-Global-Admin
// 400. Die Seite wird jetzt aus der Ressource abgeleitet; geprüft wird die
// Berechtigung auf genau dieser Seite - für fremde Seiten gibt es 403.

type translationRouteCase struct {
	name       string
	permission string
	createPath string // %d = Eltern-ID
	createBody string
	adminPath  string // Präfix für /:translation_id
	// seed legt Elternobjekt + Übersetzung auf der Seite an und liefert
	// (parentID, translationID).
	seed func(db *gorm.DB, pageID uint, lang string) (uint, uint)
	// orphan legt eine Übersetzung an, deren Elternobjekt gelöscht ist.
	orphan func(db *gorm.DB, pageID uint) uint
}

func translationRouteCases() []translationRouteCase {
	return []translationRouteCase{
		{
			name: "incident", permission: "incidents:write",
			createPath: "/api/incidents/%d/translations",
			createBody: `{"language_code":"fr","title":"T","message":"M","source":"manual"}`,
			adminPath:  "/api/incident-translations/",
			seed: func(db *gorm.DB, pageID uint, lang string) (uint, uint) {
				inc := models.Incident{StatusPageID: pageID, Title: "I", Message: "m", Status: "investigating"}
				db.Create(&inc)
				tr := models.IncidentTranslation{IncidentID: inc.ID, LanguageCode: lang, Title: "T", Message: "M", Status: "draft", Source: "manual"}
				db.Create(&tr)
				return inc.ID, tr.ID
			},
			orphan: func(db *gorm.DB, pageID uint) uint {
				inc := models.Incident{StatusPageID: pageID, Title: "Weg", Message: "m", Status: "investigating"}
				db.Create(&inc)
				tr := models.IncidentTranslation{IncidentID: inc.ID, LanguageCode: "it", Title: "T", Message: "M"}
				db.Create(&tr)
				db.Delete(&inc)
				return tr.ID
			},
		},
		{
			name: "component", permission: "components:write",
			createPath: "/api/components/%d/translations",
			createBody: `{"language_code":"fr","title":"T","description":"D","source":"manual"}`,
			adminPath:  "/api/component-translations/",
			seed: func(db *gorm.DB, pageID uint, lang string) (uint, uint) {
				comp := models.Component{StatusPageID: pageID, Name: "C"}
				db.Create(&comp)
				tr := models.ComponentTranslation{ComponentID: comp.ID, LanguageCode: lang, Title: "T", Description: "D", Status: "draft", Source: "manual"}
				db.Create(&tr)
				return comp.ID, tr.ID
			},
			orphan: func(db *gorm.DB, pageID uint) uint {
				comp := models.Component{StatusPageID: pageID, Name: "Weg"}
				db.Create(&comp)
				tr := models.ComponentTranslation{ComponentID: comp.ID, LanguageCode: "it", Title: "T", Description: "D"}
				db.Create(&tr)
				db.Delete(&comp)
				return tr.ID
			},
		},
		{
			name: "notice", permission: "notices:write",
			createPath: "/api/notices/%d/translations",
			createBody: `{"language_code":"fr","title":"T","message":"M","source":"manual"}`,
			adminPath:  "/api/notice-translations/",
			seed: func(db *gorm.DB, pageID uint, lang string) (uint, uint) {
				n := models.GeneralNotice{StatusPageID: pageID, Title: "N", Message: "m", Level: "info", IsActive: true}
				db.Create(&n)
				tr := models.GeneralNoticeTranslation{GeneralNoticeID: n.ID, LanguageCode: lang, Title: "T", Message: "M", Status: "draft", Source: "manual"}
				db.Create(&tr)
				return n.ID, tr.ID
			},
			orphan: func(db *gorm.DB, pageID uint) uint {
				n := models.GeneralNotice{StatusPageID: pageID, Title: "Weg", Message: "m", Level: "info", IsActive: true}
				db.Create(&n)
				tr := models.GeneralNoticeTranslation{GeneralNoticeID: n.ID, LanguageCode: "it", Title: "T", Message: "M"}
				db.Create(&tr)
				db.Delete(&n)
				return tr.ID
			},
		},
	}
}

func TestRouter_TranslationWritesAreScopedToResourcePage(t *testing.T) {
	for _, tc := range translationRouteCases() {
		t.Run(tc.name, func(t *testing.T) {
			db := testutils.SetupTestDB()
			r := newRouter(handlers.NewHandler(db))

			own := models.StatusPage{Name: "Eigene", Slug: "eigene-" + tc.name}
			foreign := models.StatusPage{Name: "Fremde", Slug: "fremde-" + tc.name}
			db.Create(&own)
			db.Create(&foreign)
			writer := models.Role{Name: "Writer " + tc.name, Permissions: tc.permission}
			reader := models.Role{Name: "Reader " + tc.name, Permissions: "incidents:read"}
			db.Create(&writer)
			db.Create(&reader)
			editor := models.User{Email: "editor-" + tc.name + "@example.com"}
			viewer := models.User{Email: "viewer-" + tc.name + "@example.com"}
			db.Create(&editor)
			db.Create(&viewer)
			db.Create(&models.UserStatusPageRole{UserID: editor.ID, StatusPageID: own.ID, RoleID: writer.ID})
			db.Create(&models.UserStatusPageRole{UserID: viewer.ID, StatusPageID: own.ID, RoleID: reader.ID})
			editorToken, _ := middleware.GenerateToken(editor.ID, editor.Email, "", "", false)
			viewerToken, _ := middleware.GenerateToken(viewer.ID, viewer.Email, "", "", false)

			ownParent, ownTr := tc.seed(db, own.ID, "es")
			_, ownTr2 := tc.seed(db, own.ID, "nl")
			foreignParent, foreignTr := tc.seed(db, foreign.ID, "es")
			orphanTr := tc.orphan(db, own.ID)

			do := func(method, path, body, token string) int {
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				return w.Code
			}
			create := func(parent uint) string {
				return strings.Replace(tc.createPath, "%d", strconv.FormatUint(uint64(parent), 10), 1)
			}
			tr := func(id uint) string { return tc.adminPath + strconv.FormatUint(uint64(id), 10) }

			cases := []struct {
				desc         string
				method, path string
				body, token  string
				want         int
			}{
				{"Anlegen auf eigener Seite", http.MethodPost, create(ownParent), tc.createBody, editorToken, http.StatusCreated},
				{"Anlegen auf fremder Seite", http.MethodPost, create(foreignParent), tc.createBody, editorToken, http.StatusForbidden},
				{"Anlegen ohne Schreibrecht", http.MethodPost, create(ownParent), tc.createBody, viewerToken, http.StatusForbidden},
				{"Anlegen für unbekanntes Elternobjekt", http.MethodPost, create(999999), tc.createBody, editorToken, http.StatusNotFound},
				{"Anlegen mit ungültiger ID", http.MethodPost, strings.Replace(tc.createPath, "%d", "abc", 1), tc.createBody, editorToken, http.StatusBadRequest},
				{"Bestätigen auf eigener Seite", http.MethodPatch, tr(ownTr) + "/confirm", "", editorToken, http.StatusOK},
				{"Bestätigen auf fremder Seite", http.MethodPatch, tr(foreignTr) + "/confirm", "", editorToken, http.StatusForbidden},
				{"Bestätigen unbekannte Übersetzung", http.MethodPatch, tr(999999) + "/confirm", "", editorToken, http.StatusNotFound},
				{"Bestätigen verwaiste Übersetzung", http.MethodPatch, tr(orphanTr) + "/confirm", "", editorToken, http.StatusNotFound},
				{"Bestätigen mit ungültiger ID", http.MethodPatch, tc.adminPath + "abc/confirm", "", editorToken, http.StatusBadRequest},
				{"Löschen auf fremder Seite", http.MethodDelete, tr(foreignTr), "", editorToken, http.StatusForbidden},
				{"Löschen ohne Schreibrecht", http.MethodDelete, tr(ownTr2), "", viewerToken, http.StatusForbidden},
				{"Löschen auf eigener Seite", http.MethodDelete, tr(ownTr2), "", editorToken, http.StatusNoContent},
			}
			for _, c := range cases {
				if got := do(c.method, c.path, c.body, c.token); got != c.want {
					t.Errorf("%s: %s %s = %d, want %d", c.desc, c.method, c.path, got, c.want)
				}
			}
		})
	}
}
