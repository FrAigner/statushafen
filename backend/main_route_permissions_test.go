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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Jede geschützte Admin-Route hängt an genau einer Ressource und
// einer Aktion. Der Test prüft den echten Router: keine Route an der Registry
// (guarded) vorbei, die Aktion passt zur HTTP-Methode (Sonderaktionen stehen
// ausdrücklich in routeActionExceptions und in der Doku) und die Rechteprüfung
// verhält sich je Ressource und Aktion wie dokumentiert.

// routeActionExceptions: Routen, deren Aktion bewusst von defaultAction
// (GET=read, POST=create, PUT/PATCH=update, DELETE=delete) abweicht.
var routeActionExceptions = map[string]string{
	"POST /api/admin/pages/:page_id/webhook-token":                                      "Token erzeugen ändert die Seite (pages:update)",
	"DELETE /api/admin/pages/:page_id/webhook-token":                                    "Token widerrufen ändert die Seite (pages:update)",
	"DELETE /api/admin/pages/:page_id/custom-domain":                                    "Domain entfernen ändert die Seite (pages:update)",
	"POST /api/admin/pages/:page_id/custom-domain/verify":                               "Prüfen ändert den Domain-Status der Seite (pages:update)",
	"POST /api/settings/translation/test":                                               "Verbindungstest nutzt die gespeicherte Konfiguration (system_settings:update)",
	"POST /api/pages/:page_id/components/:id/archive":                                   "Archivieren ändert die Komponente (components:update)",
	"POST /api/pages/:page_id/components/:id/restore":                                   "Wiederherstellen ändert die Komponente (components:update)",
	"POST /api/pages/:page_id/translate":                                                "Übersetzungsvorschlag ändert nichts (update statt create)",
	"POST /api/admin/pages/:page_id/sso/test":                                           "Testanmeldung schaltet die SSO-Konfiguration (sso:update)",
	"POST /api/admin/pages/:page_id/sso/saml-test":                                      "Testanmeldung schaltet die SSO-Konfiguration (sso:update)",
	"POST /api/pages/:page_id/external-sources/:id/fetch":                               "„Jetzt abrufen“ ändert die Quelle (external_sources:update)",
	"POST /api/pages/:page_id/notification-templates/:kind/:audience/:language/preview": "Vorschau rendert nur (notification_templates:read)",
	"GET /api/pages/:page_id/admin-notifications":                                       "Betriebsmeldungen nur für Bearbeiter (update statt read)",
	"GET /api/pages/:page_id/admin-notifications/unread-count":                          "Betriebsmeldungen nur für Bearbeiter (update statt read)",
	"POST /api/pages/:page_id/admin-notifications/read-all":                             "Als gelesen markieren: nur für Bearbeiter (update statt create)",
	"POST /api/pages/:page_id/admin-notifications/:id/read":                             "Als gelesen markieren: nur für Bearbeiter (update statt create)",
}

func newPermTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	t.Setenv(handlers.MetricsBasicAuthUserEnv, "")
	return newRouter(handlers.NewHandler(testutils.SetupTestDB()))
}

func TestRoutePermissions_JedeGeschuetzteRouteHatGenauEineRessourceUndAktion(t *testing.T) {
	r := newPermTestRouter(t)
	live := map[string]bool{}
	for _, route := range r.Routes() {
		live[route.Method+" "+route.Path] = true
	}
	guarded := guardedRoutes()

	for key, kind := range ssoRouteKinds {
		if kind != ssoPageRoute && kind != ssoGlobalRoute {
			continue
		}
		if _, ok := guarded[key]; !ok {
			t.Errorf("Route %s ist als geschützt eingestuft, aber nicht über guarded() einer Ressource+Aktion zugeordnet", key)
		}
	}

	for key, info := range guarded {
		t.Run(key, func(t *testing.T) {
			require.True(t, live[key], "Registry-Eintrag ohne Route")
			kind, known := ssoRouteKinds[key]
			require.True(t, known, "fehlt in ssoRouteKinds")
			assert.Contains(t, []ssoRouteKind{ssoPageRoute, ssoGlobalRoute}, kind)

			require.NotEmpty(t, info.Resources)
			for _, res := range info.Resources {
				assert.True(t, permissions.IsResource(res), "unbekannte Ressource %q", res)
			}
			assert.Contains(t, permissions.Actions, info.Action)

			method := strings.SplitN(key, " ", 2)[0]
			if reason, ok := routeActionExceptions[key]; ok {
				assert.NotEmpty(t, reason)
				assert.NotEqual(t, defaultActionFor(method), info.Action, "Ausnahme ohne Abweichung: aus routeActionExceptions entfernen")
			} else {
				assert.Equal(t, defaultActionFor(method), info.Action, "Aktion passt nicht zur HTTP-Methode; Sonderaktion in routeActionExceptions begründen")
			}

			switch info.Mode {
			case modeGlobal:
				assert.Equal(t, kind, ssoGlobalRoute)
			case modePage, modePageRole, modeAny, modeAll, modeKind:
				assert.Equal(t, kind, ssoPageRoute)
			default:
				t.Errorf("unbekannter Modus %q", info.Mode)
			}
			if info.Mode == modePage {
				for _, res := range info.Resources {
					for _, pr := range permissions.Resources {
						if pr.Key == res {
							assert.Equal(t, permissions.ScopePage, pr.Scope, "%s ist global und gehört in need()->platform", res)
						}
					}
				}
			}
		})
	}
}

func defaultActionFor(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead:
		return permissions.Read
	case http.MethodPost:
		return permissions.Create
	case http.MethodPut, http.MethodPatch:
		return permissions.Update
	case http.MethodDelete:
		return permissions.Delete
	}
	return ""
}

// permWorld: zwei Seiten mit Inhalten, Nutzer ohne Global Admin mit frei
// wählbaren Rechten auf Seite A.
type permWorld struct {
	r    *gin.Engine
	db   *gorm.DB
	a, b ssoScopePage
	n    int
}

func newPermWorld(t *testing.T) *permWorld {
	t.Helper()
	t.Setenv("JWT_SECRET", "test-jwt-secret-not-for-production")
	t.Setenv(handlers.MetricsBasicAuthUserEnv, "")
	db := testutils.SetupTestDB()
	owner := models.User{Email: "owner@example.com"}
	db.Create(&owner)
	ownerRole := models.Role{Name: "Owner-leer", Permissions: "admin_dashboard"}
	db.Create(&ownerRole)
	w := &permWorld{db: db, r: newRouter(handlers.NewHandler(db))}
	w.a = seedSSOScopePage(db, "perm-a", ownerRole.ID, owner.ID)
	w.b = seedSSOScopePage(db, "perm-b", ownerRole.ID, owner.ID)
	return w
}

// user legt einen Nutzer mit der Rechteliste auf Seite A an (perms == nil: ohne
// Rolle) und liefert sein Token.
func (w *permWorld) user(perms *string) string {
	w.n++
	u := models.User{Email: "u" + string(rune('a'+w.n)) + "@example.com"}
	w.db.Create(&u)
	if perms != nil {
		role := models.Role{Name: "R" + u.Email, Permissions: *perms}
		w.db.Create(&role)
		w.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: w.a.page.ID, RoleID: role.ID})
	}
	tok, _ := middleware.GenerateToken(u.ID, u.Email, "x", "", false)
	return tok
}

func (w *permWorld) code(method, path, token string) int {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", token)
	rec := httptest.NewRecorder()
	w.r.ServeHTTP(rec, req)
	return rec.Code
}

func listOf(perms ...string) *string {
	s := strings.Join(perms, ",")
	return &s
}

// allExcept: Vollzugriff auf alle Ressourcen außer den genannten.
func allExcept(skip ...string) *string {
	var out []string
	for _, r := range permissions.Resources {
		if !contains(skip, r.Key) {
			out = append(out, permissions.CRUD(r.Key)...)
		}
	}
	return listOf(out...)
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func permsFor(resources []string, actions ...string) []string {
	var out []string
	for _, r := range resources {
		for _, a := range actions {
			out = append(out, permissions.Perm(r, a))
		}
	}
	return out
}

func itoa(v uint) string { return strconv.FormatUint(uint64(v), 10) }

func denied(code int) bool  { return code == http.StatusForbidden || code == http.StatusUnauthorized }
func allowed(code int) bool { return !denied(code) }

// Je Ressource und Aktion: nur read -> 403 bei schreibenden Aktionen, nur eine
// andere Aktion -> 403, ohne Recht 403, fremde Seite 403 (Mandantentrennung);
// create/update/delete lassen das Lesen zu (Implikation).
func TestRoutePermissions_RechtepruefungJeRessourceUndAktion(t *testing.T) {
	for key, info := range guardedRoutes() {
		parts := strings.SplitN(key, " ", 2)
		method, route := parts[0], parts[1]
		t.Run(key, func(t *testing.T) {
			w := newPermWorld(t)
			pathA := ssoScopePath(route, w.a)
			pathB := ssoScopePath(route, w.b)

			if info.Mode == modeGlobal {
				// Auch Vollzugriff auf Seite A macht niemanden zum Plattform-Admin.
				assert.True(t, denied(w.code(method, pathA, w.user(allExcept()))), "Vollzugriff einer Seitenrolle auf globaler Route")
				return
			}

			res := info.Resources
			grant := func(actions ...string) *string {
				if info.Mode == modeAny || info.Mode == modeKind {
					// ein Recht an der ersten Ressource genügt (bei Kind: die erste = incidents)
					return listOf(permsFor(res[:1], actions...)...)
				}
				return listOf(permsFor(res, actions...)...)
			}

			// Ohne Rolle auf der Seite.
			assert.True(t, denied(w.code(method, pathA, w.user(nil))), "ohne Rolle")
			// Rolle, aber ohne Recht an dieser Ressource.
			noRight := w.user(allExcept(res...))
			if info.Mode == modePageRole {
				assert.True(t, allowed(w.code(method, pathA, noRight)), "jede Seitenrolle darf lesen")
			} else {
				assert.True(t, denied(w.code(method, pathA, noRight)), "Rolle ohne Recht an der Ressource")
			}
			// Bei „alle“ genügt eine der beiden Ressourcen nicht.
			if info.Mode == modeAll {
				assert.True(t, denied(w.code(method, pathA, w.user(listOf(permsFor(res[:1], info.Action)...)))), "nur eine der nötigen Ressourcen")
			}
			// Eine andere Aktion derselben Ressource reicht nicht.
			if info.Mode != modePageRole {
				for _, other := range []string{permissions.Create, permissions.Update, permissions.Delete} {
					if other == info.Action || (info.Action == permissions.Read) {
						continue
					}
					assert.True(t, denied(w.code(method, pathA, w.user(grant(other)))), "nur %s statt %s", other, info.Action)
				}
				if info.Action != permissions.Read {
					assert.True(t, denied(w.code(method, pathA, w.user(grant(permissions.Read)))), "nur read bei %s", info.Action)
				}
			}
			// Fremde Seite: Recht gilt nur auf Seite A.
			full := grant(info.Action)
			if info.Mode == modeAll {
				full = listOf(permsFor(res, info.Action)...)
			}
			assert.True(t, denied(w.code(method, pathB, w.user(full))), "Mandantentrennung: Recht von Seite A auf Seite B")
			// Erlaubt mit genau dem Recht; Lesen auch durch create/update/delete.
			if info.Mode != modePageRole {
				assert.True(t, allowed(w.code(method, pathA, w.user(full))), "mit %s", info.Action)
			}
			if info.Action == permissions.Read && info.Mode != modePageRole {
				for _, impl := range []string{permissions.Create, permissions.Update, permissions.Delete} {
					assert.True(t, allowed(w.code(method, pathA, w.user(grant(impl)))), "read durch %s impliziert", impl)
				}
			}
		})
	}
}

// Wartungen teilen sich die Routen mit Vorfällen: die Ressource folgt der Art
// des Datensatzes.
func TestRoutePermissions_WartungFolgtDerArtDesDatensatzes(t *testing.T) {
	post := func(w *permWorld, token, body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/pages/"+itoa(w.a.page.ID)+"/incidents", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Auth-Token", token)
		rec := httptest.NewRecorder()
		w.r.ServeHTTP(rec, req)
		return rec.Code
	}
	w := newPermWorld(t)
	onlyIncidents := w.user(listOf("incidents:create"))
	onlyMaintenance := w.user(listOf("maintenance:create"))
	assert.True(t, allowed(post(w, onlyIncidents, `{"IsMaintenance":false}`)), "Vorfall mit incidents:create")
	assert.Equal(t, http.StatusForbidden, post(w, onlyIncidents, `{"IsMaintenance":true}`), "Wartung ohne maintenance:create")
	assert.True(t, allowed(post(w, onlyMaintenance, `{"IsMaintenance":true}`)), "Wartung mit maintenance:create")
	assert.Equal(t, http.StatusForbidden, post(w, onlyMaintenance, `{}`), "Vorfall ohne incidents:create")

	// Bestehende Wartung: Ändern/Löschen verlangt maintenance, nicht incidents.
	maint := models.Incident{StatusPageID: w.a.page.ID, Title: "W", Message: "m", Status: "scheduled", IsMaintenance: true}
	require.NoError(t, w.db.Create(&maint).Error)
	path := "/api/pages/" + itoa(w.a.page.ID) + "/incidents/" + itoa(maint.ID)
	assert.Equal(t, http.StatusForbidden, w.code(http.MethodPut, path, w.user(listOf("incidents:update"))), "Wartung ändern mit incidents:update")
	assert.True(t, allowed(w.code(http.MethodPut, path, w.user(listOf("maintenance:update")))), "Wartung ändern mit maintenance:update")
	assert.Equal(t, http.StatusForbidden, w.code(http.MethodDelete, path, w.user(listOf("incidents:delete"))), "Wartung löschen mit incidents:delete")
	assert.Equal(t, http.StatusForbidden, w.code(http.MethodPost, path+"/updates", w.user(listOf("incidents:create"))), "Timeline einer Wartung")
	assert.True(t, allowed(w.code(http.MethodPost, path+"/updates", w.user(listOf("maintenance:create")))))
}

// Der Import legt eigene Komponenten an und verlangt beide Rechte.
func TestRoutePermissions_ImportBrauchtQuellenUndKomponentenRecht(t *testing.T) {
	w := newPermWorld(t)
	path := "/api/pages/" + itoa(w.a.page.ID) + "/external-sources/999/import"
	assert.Equal(t, http.StatusForbidden, w.code(http.MethodPost, path, w.user(listOf("external_sources:create"))))
	assert.Equal(t, http.StatusForbidden, w.code(http.MethodPost, path, w.user(listOf("components:create"))))
	assert.True(t, allowed(w.code(http.MethodPost, path, w.user(listOf("external_sources:create", "components:create")))))
}

// Kompatibilität: Eine Rolle mit dem Altwert write (Alt-Rolle,
// Alt-Token) zählt weiter als create+update+delete+read - ausschließlich beim
// Prüfen; gespeichert wird er nie mehr (siehe handlers/role_test.go).
func TestRoutePermissions_AltwertWriteGiltWeiterBeimPruefen(t *testing.T) {
	w := newPermWorld(t)
	tok := w.user(listOf("components:write"))
	base := "/api/pages/" + itoa(w.a.page.ID) + "/components"
	assert.True(t, allowed(w.code(http.MethodPost, base, tok)), "create")
	assert.True(t, allowed(w.code(http.MethodPut, base+"/"+itoa(w.a.component), tok)), "update")
	assert.True(t, allowed(w.code(http.MethodGet, "/api/components/"+itoa(w.a.component)+"/translations/all", tok)), "read")
	assert.Equal(t, http.StatusForbidden, w.code(http.MethodPost, "/api/pages/"+itoa(w.a.page.ID)+"/notices", tok), "andere Ressource")
	assert.True(t, allowed(w.code(http.MethodDelete, base+"/"+itoa(w.a.component), tok)), "delete")
}
