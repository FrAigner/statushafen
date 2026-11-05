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
	"github.com/fraigner/statushafen/permissions"
	"github.com/fraigner/statushafen/testutils"
)

// Das Frontend blendet Navigation und Schreibknöpfe nach den
// Rechten der Rolle aus, maßgeblich ist aber das Backend. Dieser Test geht
// über ALLE registrierten Schreib-Routen (r.Routes()) und prüft, dass ein
// Viewer (Seed-Rolle, nur :read) auf seiner eigenen Seite überall 403 bekommt
// und ein Editor nur bei Vorfällen, Wartungsfenstern, Vorfall-Vorlagen und
// Ankündigungen durchkommt. Eine neue Schreib-Route ohne Rechteprüfung lässt
// den Test rot werden, statt erst im UI aufzufallen.

// Rechte wie in database.SeedData (Viewer/Editor); seit der Umstellung aus den
// früheren read/write-Listen abgeleitet (gleiche Wirkung, jetzt als CRUD).
var (
	seedViewerPerms = permissions.MigrateLegacy("components:read,incidents:read,notices:read")
	seedEditorPerms = permissions.MigrateLegacy("components:read,incidents:write,notices:write")
)

// selfServiceWriteRoutes sind Schreib-Routen ohne Seiten-Recht: öffentlich
// (Login, Abo, Bestellung, IdP-Rückkanal), per eigenem Token geschützt
// (Komponenten-Webhook) oder Selbstverwaltung des eigenen Kontos (/me).
var selfServiceWriteRoutes = map[string]bool{
	"POST /api/auth/saml/:page_id/acs":                   true,
	"POST /api/auth/sso-exchange":                        true,
	"POST /api/login":                                    true,
	"POST /api/subscribe":                                true,
	"POST /api/subscribe/test-webhook":                   true,
	"POST /api/webhooks/components/:component_id/status": true,
	"POST /api/webhooks/component-slugs/:slug/status":    true,
	"POST /api/public/subscribers/unsubscribe":           true,
	"POST /api/public/subscribers/confirm":               true,
	"POST /api/public/orders":                            true,
	"POST /api/public/orders/confirm":                    true,
	"POST /api/public/orders/set-password":               true,
	"POST /api/auth/oidc/link":                           true,
	"POST /api/auth/saml/link":                           true,
	"PATCH /api/me":                                      true,
	"DELETE /api/me":                                     true,
	"DELETE /api/me/sso/:id":                             true,
	"PUT /api/me/subscription":                           true,
	"DELETE /api/me/subscription":                        true,
	// eigene Einladung (nur das eingeladene Konto).
	"POST /api/invitations/preview": true,
	"POST /api/invitations/accept":  true,
	"POST /api/invitations/decline": true,
}

// editorWritePrefixes: Routen, für die incidents:write bzw. notices:write genügt.
var editorWritePrefixes = []string{
	"/api/pages/:page_id/incidents",
	"/api/pages/:page_id/incident-templates",
	"/api/pages/:page_id/translate", // Übersetzungsvorschlag
	"/api/incidents/",
	"/api/incident-translations",
	"/api/pages/:page_id/notices",
	"/api/notices/",
	"/api/notice-translations",
}

func editorMayWrite(path string) bool {
	for _, p := range editorWritePrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

type roleAccessFixture struct {
	r           http.Handler
	pageID      string
	viewerToken string
	editorToken string
	writeRoutes []string // "METHOD /pfad/mit/:params"
}

func setupRoleAccessFixture(t *testing.T) roleAccessFixture {
	t.Helper()
	db := testutils.SetupTestDB()
	r := newRouter(handlers.NewHandler(db))

	page := models.StatusPage{Name: "Rechte", Slug: "rechte-503"}
	db.Create(&page)
	// Vorfall, Komponente, Ankündigung und je eine Übersetzung (alle ID 1), damit die Seiten-Scope-Middleware
	// der Übersetzungs-Routen (IncidentPageScope usw.) die Seite auflöst und
	// tatsächlich die Rechteprüfung entscheidet, nicht ein 404.
	db.Create(&models.Incident{Title: "V", StatusPageID: page.ID})
	db.Create(&models.Component{Name: "K", StatusPageID: page.ID})
	db.Create(&models.GeneralNotice{Title: "A", StatusPageID: page.ID})
	db.Create(&models.IncidentTranslation{IncidentID: 1, LanguageCode: "en", Title: "I"})
	db.Create(&models.ComponentTranslation{ComponentID: 1, LanguageCode: "en", Title: "C"})
	db.Create(&models.GeneralNoticeTranslation{GeneralNoticeID: 1, LanguageCode: "en", Title: "N"})

	viewerRole := models.Role{Name: "Viewer 503", Permissions: seedViewerPerms}
	editorRole := models.Role{Name: "Editor 503", Permissions: seedEditorPerms}
	db.Create(&viewerRole)
	db.Create(&editorRole)
	viewer := models.User{Email: "viewer503@example.com"}
	editor := models.User{Email: "editor503@example.com"}
	db.Create(&viewer)
	db.Create(&editor)
	db.Create(&models.UserStatusPageRole{UserID: viewer.ID, StatusPageID: page.ID, RoleID: viewerRole.ID})
	db.Create(&models.UserStatusPageRole{UserID: editor.ID, StatusPageID: page.ID, RoleID: editorRole.ID})
	viewerToken, _ := middleware.GenerateToken(viewer.ID, viewer.Email, "Viewer", seedViewerPerms, false)
	editorToken, _ := middleware.GenerateToken(editor.ID, editor.Email, "Editor", seedEditorPerms, false)

	var routes []string
	for _, ri := range r.Routes() {
		if ri.Method == http.MethodGet || ri.Method == http.MethodHead || ri.Method == http.MethodOptions {
			continue
		}
		key := ri.Method + " " + ri.Path
		if selfServiceWriteRoutes[key] {
			continue
		}
		routes = append(routes, key)
	}
	return roleAccessFixture{r: r, pageID: strconv.FormatUint(uint64(page.ID), 10), viewerToken: viewerToken, editorToken: editorToken, writeRoutes: routes}
}

// call ersetzt :page_id durch die Seite der Rolle und alle übrigen
// Parameter durch "1" (die IDs der Fixture-Objekte).
func (f roleAccessFixture) call(method, path, token string) int {
	concrete := strings.ReplaceAll(path, ":page_id", f.pageID)
	parts := strings.Split(concrete, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "1"
		}
	}
	req := httptest.NewRequest(method, strings.Join(parts, "/"), strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w.Code
}

func TestRouter_ViewerGetsForbiddenOnEveryWriteRoute(t *testing.T) {
	f := setupRoleAccessFixture(t)
	if len(f.writeRoutes) < 40 {
		t.Fatalf("only %d write routes found, route enumeration seems broken", len(f.writeRoutes))
	}
	for _, key := range f.writeRoutes {
		method, path, _ := strings.Cut(key, " ")
		if got := f.call(method, path, f.viewerToken); got != http.StatusForbidden {
			t.Errorf("%s as Viewer = %d, want 403", key, got)
		}
	}
}

func TestRouter_EditorWritesOnlyIncidentsAndNotices(t *testing.T) {
	f := setupRoleAccessFixture(t)
	for _, key := range f.writeRoutes {
		method, path, _ := strings.Cut(key, " ")
		got := f.call(method, path, f.editorToken)
		if editorMayWrite(path) {
			if got == http.StatusForbidden || got == http.StatusUnauthorized {
				t.Errorf("%s as Editor = %d, want the permission check to pass", key, got)
			}
		} else if got != http.StatusForbidden {
			t.Errorf("%s as Editor = %d, want 403", key, got)
		}
	}
}

// Lese-Endpunkte, an denen das Frontend die Sichtbarkeit der Navigation
// festmacht (AdminLayout.vue): Vorfall-Vorlagen brauchen schon zum Lesen
// incident_templates:read, Mail-Vorlagen notification_templates:read,
// Ankündigungen notices:read.
func TestRouter_NavRelevantReadsFollowRolePermissions(t *testing.T) {
	f := setupRoleAccessFixture(t)
	cases := []struct {
		path         string
		viewer, edit int
	}{
		{"/api/pages/:page_id/incident-templates", http.StatusForbidden, http.StatusOK},
		{"/api/pages/:page_id/notification-templates", http.StatusForbidden, http.StatusForbidden},
		{"/api/pages/:page_id/notices/all", http.StatusOK, http.StatusOK},
		{"/api/pages/:page_id/subscribers", http.StatusForbidden, http.StatusForbidden},
		{"/api/tags", http.StatusForbidden, http.StatusForbidden},
	}
	for _, c := range cases {
		if got := f.call(http.MethodGet, c.path, f.viewerToken); got != c.viewer {
			t.Errorf("GET %s as Viewer = %d, want %d", c.path, got, c.viewer)
		}
		if got := f.call(http.MethodGet, c.path, f.editorToken); got != c.edit {
			t.Errorf("GET %s as Editor = %d, want %d", c.path, got, c.edit)
		}
	}
}
