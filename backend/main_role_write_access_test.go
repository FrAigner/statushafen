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

// The frontend hides navigation and write buttons according to the
// role's permissions, but the backend is authoritative. This test goes
// over ALL registered write routes (r.Routes()) and checks that a
// viewer (seed role, :read only) gets 403 everywhere on its own page
// and an editor gets through only for incidents, maintenance windows, incident templates and
// notices. A new write route without a permission check turns
// the test red instead of only being noticed in the UI.

// Permissions as in database.SeedData (viewer/editor); since the changeover derived from the
// former read/write lists (same effect, now as CRUD).
var (
	seedViewerPerms = permissions.MigrateLegacy("components:read,incidents:read,notices:read")
	seedEditorPerms = permissions.MigrateLegacy("components:read,incidents:write,notices:write")
)

// selfServiceWriteRoutes are write routes without a page permission: public
// (login, subscription, order, IdP back channel), protected by their own token
// (component webhook) or self-management of the own account (/me).
var selfServiceWriteRoutes = map[string]bool{
	"POST /api/auth/sso-exchange":                        true,
	"POST /api/login":                                    true,
	"POST /api/subscribe":                                true,
	"POST /api/webhooks/components/:component_id/status": true,
	"POST /api/webhooks/component-slugs/:slug/status":    true,
	"POST /api/public/subscribers/unsubscribe":           true,
	"POST /api/public/subscribers/confirm":               true,
	"POST /api/public/orders":                            true,
	"POST /api/public/orders/confirm":                    true,
	"POST /api/public/orders/set-password":               true,
	"POST /api/auth/oidc/link":                           true,
	"POST /api/auth/provider/:id/link":                   true,
	"PATCH /api/me":                                      true,
	"DELETE /api/me":                                     true,
	"DELETE /api/me/sso/:id":                             true,
	"PUT /api/me/subscription":                           true,
	"DELETE /api/me/subscription":                        true,
	// own invitation (only the invited account).
	"POST /api/invitations/preview": true,
	"POST /api/invitations/accept":  true,
	"POST /api/invitations/decline": true,
}

// editorWritePrefixes: routes for which incidents:write or notices:write suffices.
var editorWritePrefixes = []string{
	"/api/pages/:page_id/incidents",
	"/api/pages/:page_id/incident-templates",
	"/api/pages/:page_id/translate", // translation suggestion
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
	writeRoutes []string // "METHOD /path/with/:params"
}

func setupRoleAccessFixture(t *testing.T) roleAccessFixture {
	t.Helper()
	db := testutils.SetupTestDB()
	r := newRouter(handlers.NewHandler(db))

	page := models.StatusPage{Name: "Rechte", Slug: "rechte-503"}
	db.Create(&page)
	// incident, component, notice and one translation each (all ID 1), so that the page-scope middleware
	// of the translation routes (IncidentPageScope etc.) resolves the page and
	// the permission check actually decides, not a 404.
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

// call replaces :page_id with the role's page and all other
// parameters with "1" (the IDs of the fixture objects).
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

// Read endpoints on which the frontend bases the visibility of the navigation
// (AdminLayout.vue): incident templates already need a permission to read,
// incident_templates:read, mail templates notification_templates:read,
// notices need notices:read.
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
