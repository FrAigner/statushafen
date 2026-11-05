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
	"gorm.io/gorm"
)

// Ein JWT aus einem Seiten-SSO-Login gilt nur für die Seite,
// deren IdP es ausgestellt hat. Der Test prüft JEDE Route des echten Routers;
// eine neue Route muss hier einer Kategorie zugeordnet werden.
type ssoRouteKind int

const (
	// ssoPageRoute: verlangt Rechte auf einer Seite (RequirePermission/
	// RequirePageRole, auch über die *PageScope-Middlewares).
	ssoPageRoute ssoRouteKind = iota
	// ssoPageRead: liest Daten einer (hier privaten) Seite mit optionaler
	// Anmeldung; eine fremde Seite bleibt verborgen (403 bzw. 404).
	ssoPageRead
	// ssoGlobalRoute: RequireGlobalAdmin - nie aus einer Seiten-SSO-Sitzung.
	ssoGlobalRoute
	// ssoUnchanged: eigenes Konto, öffentliche Routen und Routen ohne
	// Rechteprüfung - Antwort wie mit einem Token ohne Seitenbezug.
	ssoUnchanged
)

var ssoRouteKinds = map[string]ssoRouteKind{
	"GET /healthz":                                       ssoUnchanged,
	"GET /api/auth/oidc/login":                           ssoUnchanged,
	"GET /api/auth/oidc/callback":                        ssoUnchanged,
	"GET /api/auth/saml/login":                           ssoUnchanged,
	"GET /api/auth/sso/login":                            ssoUnchanged,
	"GET /api/auth/saml/:page_id/metadata":               ssoUnchanged,
	"POST /api/auth/saml/:page_id/acs":                   ssoUnchanged,
	"GET /api/auth/:provider":                            ssoUnchanged,
	"GET /api/auth/:provider/callback":                   ssoUnchanged,
	"POST /api/auth/sso-exchange":                        ssoUnchanged,
	"POST /api/login":                                    ssoUnchanged,
	"POST /api/subscribe":                                ssoUnchanged,
	"POST /api/subscribe/test-webhook":                   ssoUnchanged,
	"GET /api/public/pages":                              ssoUnchanged,
	"GET /api/public/ui-overrides":                       ssoUnchanged,
	"POST /api/webhooks/components/:component_id/status": ssoUnchanged,
	"POST /api/webhooks/component-slugs/:slug/status":    ssoUnchanged,
	"GET /api/public/resolve-domain":                     ssoUnchanged,
	"GET /api/public/subscribers/unsubscribe":            ssoUnchanged,
	"POST /api/public/subscribers/unsubscribe":           ssoUnchanged,
	"POST /api/public/subscribers/confirm":               ssoUnchanged,
	"GET /api/public/orders/enabled":                     ssoUnchanged,
	"POST /api/public/orders":                            ssoUnchanged,
	"POST /api/public/orders/confirm":                    ssoUnchanged,
	"POST /api/public/orders/set-password":               ssoUnchanged,
	"GET /api/internal/custom-domain-check":              ssoUnchanged,
	"GET /api/me":                                        ssoUnchanged,
	"PATCH /api/me":                                      ssoUnchanged,
	"DELETE /api/me":                                     ssoUnchanged,
	"GET /api/me/export":                                 ssoUnchanged,
	"GET /api/me/sso":                                    ssoUnchanged,
	"DELETE /api/me/sso/:id":                             ssoUnchanged,
	"POST /api/auth/oidc/link":                           ssoUnchanged,
	"POST /api/auth/saml/link":                           ssoUnchanged,
	"GET /api/me/subscription":                           ssoUnchanged,
	"PUT /api/me/subscription":                           ssoUnchanged,
	"DELETE /api/me/subscription":                        ssoUnchanged,
	// Sprachkatalog lesen: jeder eingeloggte Nutzer, keine Rechte nötig.
	"GET /api/languages": ssoUnchanged,
	// Liefert ohnehin nur die SSO-Seite (siehe handlers/sso_page_scope_internal_test.go).
	"GET /api/me/pages": ssoUnchanged,
	// eigene Einladung; Seiten-SSO-Sitzungen lehnt der Handler beim Annehmen ab (409).
	"POST /api/invitations/preview": ssoUnchanged,
	"POST /api/invitations/accept":  ssoUnchanged,
	"POST /api/invitations/decline": ssoUnchanged,

	"GET /api/pages/slug/:slug":                                              ssoPageRead,
	"GET /api/public/pages/:slug/dashboard":                                  ssoPageRead,
	"GET /api/public/pages/:slug/sso":                                        ssoPageRead,
	"GET /api/public/pages/:slug/availability":                               ssoPageRead,
	"GET /api/public/pages/:slug/translations":                               ssoPageRead,
	"GET /api/public/pages/:slug/incidents/:incident_id/update-translations": ssoPageRead,
	"GET /api/pages/:page_id/components":                                     ssoPageRead,
	"GET /api/pages/:page_id/incidents":                                      ssoPageRead,
	"GET /api/pages/:page_id/incidents/:id":                                  ssoPageRead,
	"GET /api/pages/:page_id/notices":                                        ssoPageRead,
	"GET /api/incidents/:incident_id/translations":                           ssoPageRead,
	"GET /api/components/:component_id/translations":                         ssoPageRead,
	"GET /api/notices/:notice_id/translations":                               ssoPageRead,

	"GET /api/pages":                      ssoGlobalRoute,
	"POST /api/admin/pages":               ssoGlobalRoute,
	"POST /api/languages":                 ssoGlobalRoute,
	"PUT /api/languages/:id":              ssoGlobalRoute,
	"DELETE /api/languages/:id":           ssoGlobalRoute,
	"GET /api/admin/ui-overrides":         ssoGlobalRoute,
	"PUT /api/admin/ui-overrides":         ssoGlobalRoute,
	"DELETE /api/admin/ui-overrides":      ssoGlobalRoute,
	"GET /api/users":                      ssoGlobalRoute,
	"POST /api/users":                     ssoGlobalRoute,
	"PUT /api/users/:id":                  ssoGlobalRoute,
	"DELETE /api/users/:id":               ssoGlobalRoute,
	"GET /api/roles":                      ssoGlobalRoute,
	"POST /api/roles":                     ssoGlobalRoute,
	"PUT /api/roles/:id":                  ssoGlobalRoute,
	"DELETE /api/roles/:id":               ssoGlobalRoute,
	"GET /api/tags":                       ssoGlobalRoute,
	"POST /api/tags":                      ssoGlobalRoute,
	"PUT /api/tags/:id":                   ssoGlobalRoute,
	"DELETE /api/tags/:id":                ssoGlobalRoute,
	"GET /api/settings":                   ssoGlobalRoute,
	"GET /api/settings/translation":       ssoGlobalRoute,
	"PUT /api/settings/translation":       ssoGlobalRoute,
	"POST /api/settings/translation/test": ssoGlobalRoute,
	"PUT /api/settings":                   ssoGlobalRoute,
	"GET /api/admin/overview":             ssoGlobalRoute,
	"GET /api/admin/monitoring":           ssoGlobalRoute,
	"GET /api/admin/audit":                ssoGlobalRoute,

	"GET /api/admin/pages/:page_id/users":                                                  ssoPageRoute,
	"POST /api/admin/pages/:page_id/users":                                                 ssoPageRoute,
	"DELETE /api/admin/pages/:page_id/users/:uid":                                          ssoPageRoute,
	"GET /api/admin/pages/:page_id/invitations":                                            ssoPageRoute,
	"DELETE /api/admin/pages/:page_id/invitations/:id":                                     ssoPageRoute,
	"PUT /api/admin/pages/:page_id":                                                        ssoPageRoute,
	"DELETE /api/admin/pages/:page_id":                                                     ssoPageRoute,
	"POST /api/admin/pages/:page_id/webhook-token":                                         ssoPageRoute,
	"DELETE /api/admin/pages/:page_id/webhook-token":                                       ssoPageRoute,
	"PUT /api/admin/pages/:page_id/custom-domain":                                          ssoPageRoute,
	"DELETE /api/admin/pages/:page_id/custom-domain":                                       ssoPageRoute,
	"POST /api/admin/pages/:page_id/custom-domain/verify":                                  ssoPageRoute,
	"GET /api/admin/pages/:page_id/sso":                                                    ssoPageRoute,
	"PUT /api/admin/pages/:page_id/sso":                                                    ssoPageRoute,
	"DELETE /api/admin/pages/:page_id/sso":                                                 ssoPageRoute,
	"POST /api/pages/:page_id/translate":                                                   ssoPageRoute,
	"POST /api/admin/pages/:page_id/sso/test":                                              ssoPageRoute,
	"POST /api/admin/pages/:page_id/sso/saml-test":                                         ssoPageRoute,
	"POST /api/pages/:page_id/components":                                                  ssoPageRoute,
	"PUT /api/pages/:page_id/components/:id":                                               ssoPageRoute,
	"DELETE /api/pages/:page_id/components/:id":                                            ssoPageRoute,
	"POST /api/pages/:page_id/components/:id/archive":                                      ssoPageRoute,
	"POST /api/pages/:page_id/components/:id/restore":                                      ssoPageRoute,
	"POST /api/pages/:page_id/incidents":                                                   ssoPageRoute,
	"PUT /api/pages/:page_id/incidents/:id":                                                ssoPageRoute,
	"DELETE /api/pages/:page_id/incidents/:id":                                             ssoPageRoute,
	"POST /api/pages/:page_id/incidents/:id/updates":                                       ssoPageRoute,
	"GET /api/pages/:page_id/incidents/:id/updates/:update_id/translations":                ssoPageRoute,
	"PUT /api/pages/:page_id/incidents/:id/updates/:update_id/translations/:language_code": ssoPageRoute,
	"PUT /api/pages/:page_id/incidents/:id/postmortem":                                     ssoPageRoute,
	"POST /api/incidents/:incident_id/translations":                                        ssoPageRoute,
	"PUT /api/notices/:notice_id/translations/:language_code":                              ssoPageRoute,
	"PUT /api/components/:component_id/translations/:language_code":                        ssoPageRoute,
	"PUT /api/incidents/:incident_id/translations/:language_code":                          ssoPageRoute,
	"PATCH /api/incident-translations/:translation_id/confirm":                             ssoPageRoute,
	"DELETE /api/incident-translations/:translation_id":                                    ssoPageRoute,
	"POST /api/components/:component_id/translations":                                      ssoPageRoute,
	"PATCH /api/component-translations/:translation_id/confirm":                            ssoPageRoute,
	"DELETE /api/component-translations/:translation_id":                                   ssoPageRoute,
	"POST /api/notices/:notice_id/translations":                                            ssoPageRoute,
	"PATCH /api/notice-translations/:translation_id/confirm":                               ssoPageRoute,
	"DELETE /api/notice-translations/:translation_id":                                      ssoPageRoute,
	"GET /api/incidents/:incident_id/translations/all":                                     ssoPageRoute,
	"GET /api/components/:component_id/translations/all":                                   ssoPageRoute,
	"GET /api/notices/:notice_id/translations/all":                                         ssoPageRoute,
	"GET /api/pages/:page_id/notices/all":                                                  ssoPageRoute,
	"GET /api/pages/:page_id/tags":                                                         ssoPageRoute,
	"POST /api/pages/:page_id/notices":                                                     ssoPageRoute,
	"PUT /api/pages/:page_id/notices/:id":                                                  ssoPageRoute,
	"DELETE /api/pages/:page_id/notices/:id":                                               ssoPageRoute,
	// externe Statusseiten.
	"GET /api/pages/:page_id/external-sources":                ssoPageRoute,
	"POST /api/pages/:page_id/external-sources":               ssoPageRoute,
	"GET /api/pages/:page_id/external-sources/:id":            ssoPageRoute,
	"PUT /api/pages/:page_id/external-sources/:id":            ssoPageRoute,
	"DELETE /api/pages/:page_id/external-sources/:id":         ssoPageRoute,
	"POST /api/pages/:page_id/external-sources/:id/fetch":     ssoPageRoute,
	"GET /api/pages/:page_id/external-sources/:id/components": ssoPageRoute,
	"POST /api/pages/:page_id/external-sources/:id/import":    ssoPageRoute,
	"GET /api/pages/:page_id/external-incidents":              ssoPageRoute,
	"GET /api/pages/:page_id/external-incidents/:id":          ssoPageRoute,
	"GET /api/pages/:page_id/external-outages":                ssoPageRoute,
	// Nachrichtencenter - Seiten-Routen prüfen Rolle und SSO-Seite selbst,
	// systemweite verlangen Global Admin ohne Seiten-SSO.
	"GET /api/pages/:page_id/admin-notifications":                                       ssoPageRoute,
	"GET /api/pages/:page_id/admin-notifications/unread-count":                          ssoPageRoute,
	"POST /api/pages/:page_id/admin-notifications/read-all":                             ssoPageRoute,
	"POST /api/pages/:page_id/admin-notifications/:id/read":                             ssoPageRoute,
	"GET /api/admin-notifications":                                                      ssoGlobalRoute,
	"GET /api/admin-notifications/unread-count":                                         ssoGlobalRoute,
	"POST /api/admin-notifications/read-all":                                            ssoGlobalRoute,
	"POST /api/admin-notifications/:id/read":                                            ssoGlobalRoute,
	"GET /api/pages/:page_id/external-links":                                            ssoPageRoute,
	"POST /api/pages/:page_id/external-links":                                           ssoPageRoute,
	"DELETE /api/pages/:page_id/external-links/:id":                                     ssoPageRoute,
	"GET /api/pages/:page_id/incident-templates":                                        ssoPageRoute,
	"POST /api/pages/:page_id/incident-templates":                                       ssoPageRoute,
	"PUT /api/pages/:page_id/incident-templates/:id":                                    ssoPageRoute,
	"DELETE /api/pages/:page_id/incident-templates/:id":                                 ssoPageRoute,
	"GET /api/pages/:page_id/incident-templates/:id/translations":                       ssoPageRoute,
	"PUT /api/pages/:page_id/incident-templates/:id/translations/:language_code":        ssoPageRoute,
	"GET /api/pages/:page_id/notification-templates":                                    ssoPageRoute,
	"PUT /api/pages/:page_id/notification-templates/:kind/:audience/:language":          ssoPageRoute,
	"DELETE /api/pages/:page_id/notification-templates/:kind/:audience/:language":       ssoPageRoute,
	"POST /api/pages/:page_id/notification-templates/:kind/:audience/:language/preview": ssoPageRoute,
	"GET /api/pages/:page_id/languages":                                                 ssoPageRoute,
	"PUT /api/pages/:page_id/languages":                                                 ssoPageRoute,
	"GET /api/pages/:page_id/ui-overrides":                                              ssoPageRoute,
	"PUT /api/pages/:page_id/ui-overrides":                                              ssoPageRoute,
	"DELETE /api/pages/:page_id/ui-overrides":                                           ssoPageRoute,
	"GET /api/pages/:page_id/subscribers":                                               ssoPageRoute,
	"POST /api/pages/:page_id/subscribers":                                              ssoPageRoute,
	"PUT /api/pages/:page_id/subscribers/:id":                                           ssoPageRoute,
	"DELETE /api/pages/:page_id/subscribers/:id":                                        ssoPageRoute,
}

// ssoScopePage: IDs der Inhalte einer Testseite für die Pfad-Parameter.
type ssoScopePage struct {
	page                              models.StatusPage
	component, incident, notice       uint
	incidentTr, componentTr, noticeTr uint
}

// ssoScopeWorld: V hat auf den privaten Seiten A und B je eine Rolle mit
// allen Seitenrechten und ist inzwischen Global Admin. scoped ist ein
// Seiten-SSO-Token für A, plain ein normales Token desselben Nutzers.
type ssoScopeWorld struct {
	r             *gin.Engine
	a, b          ssoScopePage
	scoped, plain string
}

func seedSSOScopePage(db *gorm.DB, slug string, roleID, userID uint) ssoScopePage {
	p := ssoScopePage{page: models.StatusPage{Name: slug, Slug: slug, IsPrivate: true}}
	db.Create(&p.page)
	db.Create(&models.UserStatusPageRole{UserID: userID, StatusPageID: p.page.ID, RoleID: roleID})
	comp := models.Component{StatusPageID: p.page.ID, Name: "C"}
	db.Create(&comp)
	inc := models.Incident{StatusPageID: p.page.ID, Title: "I", Message: "m", Status: "investigating"}
	db.Create(&inc)
	n := models.GeneralNotice{StatusPageID: p.page.ID, Title: "N", Message: "m", Level: "info", IsActive: true}
	db.Create(&n)
	it := models.IncidentTranslation{IncidentID: inc.ID, LanguageCode: "es", Title: "T", Message: "M"}
	ct := models.ComponentTranslation{ComponentID: comp.ID, LanguageCode: "es", Title: "T", Description: "D"}
	nt := models.GeneralNoticeTranslation{GeneralNoticeID: n.ID, LanguageCode: "es", Title: "T", Message: "M"}
	db.Create(&it)
	db.Create(&ct)
	db.Create(&nt)
	p.component, p.incident, p.notice = comp.ID, inc.ID, n.ID
	p.incidentTr, p.componentTr, p.noticeTr = it.ID, ct.ID, nt.ID
	return p
}

func newSSOScopeWorld(t *testing.T) ssoScopeWorld {
	t.Helper()
	db := testutils.SetupTestDB()
	role := models.Role{Name: "Alles", Permissions: permissions.FullAccess()}
	db.Create(&role)
	v := models.User{Email: "v@example.com", IsGlobalAdmin: true, Role: role}
	db.Create(&v)
	w := ssoScopeWorld{r: newRouter(handlers.NewHandler(db))}
	w.a = seedSSOScopePage(db, "sso-a", role.ID, v.ID)
	w.b = seedSSOScopePage(db, "sso-b", role.ID, v.ID)
	w.scoped, _ = middleware.GeneratePageSSOToken(v, "V", models.SSOProtocolOIDC, w.a.page.ID)
	w.plain, _ = middleware.GenerateToken(v.ID, v.Email, role.Name, role.Permissions, true)
	return w
}

// ssoScopePath setzt die Parameter einer Route für die Zielseite p ein.
func ssoScopePath(path string, p ssoScopePage) string {
	id := func(v uint) string { return strconv.FormatUint(uint64(v), 10) }
	itemID := "999"
	switch {
	case strings.Contains(path, "/incidents/:id"):
		itemID = id(p.incident)
	case strings.Contains(path, "/components/:id"):
		itemID = id(p.component)
	case strings.Contains(path, "/notices/:id"):
		itemID = id(p.notice)
	}
	trID := map[string]string{
		"incident": id(p.incidentTr), "component": id(p.componentTr), "notice": id(p.noticeTr),
	}[strings.TrimPrefix(strings.SplitN(path, "-translations/", 2)[0], "/api/")]
	return strings.NewReplacer(
		":page_id", id(p.page.ID),
		":incident_id", id(p.incident),
		":component_id", id(p.component),
		":notice_id", id(p.notice),
		":translation_id", trID,
		":slug", p.page.Slug,
		":provider", "google",
		":kind", "incident_created",
		":audience", "staff",
		":language", "en",
		":uid", "999",
		":id", itemID,
	).Replace(path)
}

func (w ssoScopeWorld) serve(method, path, token string) int {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	w.r.ServeHTTP(rec, req)
	return rec.Code
}

// serveFresh baut für jeden Aufruf eine frische Welt, damit schreibende
// Routen sich nicht gegenseitig beeinflussen.
func serveFresh(t *testing.T, method, route string, target func(ssoScopeWorld) ssoScopePage, token func(ssoScopeWorld) string) int {
	t.Helper()
	w := newSSOScopeWorld(t)
	return w.serve(method, ssoScopePath(route, target(w)), token(w))
}

func TestRouter_SSOSessionIsLimitedToItsPage(t *testing.T) {
	t.Setenv(handlers.MetricsBasicAuthUserEnv, "")
	pageA := func(w ssoScopeWorld) ssoScopePage { return w.a }
	pageB := func(w ssoScopeWorld) ssoScopePage { return w.b }
	scoped := func(w ssoScopeWorld) string { return w.scoped }
	plain := func(w ssoScopeWorld) string { return w.plain }

	seen := map[string]bool{}
	for _, route := range newSSOScopeWorld(t).r.Routes() {
		key := route.Method + " " + route.Path
		seen[key] = true
		kind, ok := ssoRouteKinds[key]
		if !ok {
			t.Errorf("Route %s ist keiner Kategorie zugeordnet", key)
			continue
		}
		m, p := route.Method, route.Path
		t.Run(key, func(t *testing.T) {
			switch kind {
			case ssoPageRoute:
				if got := serveFresh(t, m, p, pageB, scoped); got != http.StatusForbidden {
					t.Errorf("SSO-Token für A auf Seite B: %d, want 403", got)
				}
				if got := serveFresh(t, m, p, pageB, plain); got == http.StatusForbidden || got == http.StatusUnauthorized {
					t.Errorf("normales Token auf Seite B: %d, want erlaubt", got)
				}
				if got := serveFresh(t, m, p, pageA, scoped); got == http.StatusForbidden || got == http.StatusUnauthorized {
					t.Errorf("SSO-Token für A auf Seite A: %d, want erlaubt", got)
				}
			case ssoPageRead:
				if got := serveFresh(t, m, p, pageB, scoped); got != http.StatusForbidden && got != http.StatusNotFound {
					t.Errorf("SSO-Token für A liest Seite B: %d, want 403/404", got)
				}
				if got := serveFresh(t, m, p, pageB, plain); got != http.StatusOK {
					t.Errorf("normales Token liest Seite B: %d, want 200", got)
				}
				if got := serveFresh(t, m, p, pageA, scoped); got != http.StatusOK {
					t.Errorf("SSO-Token für A liest Seite A: %d, want 200", got)
				}
			case ssoGlobalRoute:
				if got := serveFresh(t, m, p, pageA, scoped); got != http.StatusForbidden {
					t.Errorf("SSO-Token auf globaler Admin-Route: %d, want 403", got)
				}
				if got := serveFresh(t, m, p, pageA, plain); got == http.StatusForbidden || got == http.StatusUnauthorized {
					t.Errorf("normales Token (Global Admin) auf globaler Route: %d, want erlaubt", got)
				}
			case ssoUnchanged:
				if s, n := serveFresh(t, m, p, pageA, scoped), serveFresh(t, m, p, pageA, plain); s != n {
					t.Errorf("SSO-Token %d, normales Token %d - want gleich", s, n)
				}
			}
		})
	}
	for key := range ssoRouteKinds {
		if !seen[key] {
			t.Errorf("Route %s ist nicht mehr registriert", key)
		}
	}
}
