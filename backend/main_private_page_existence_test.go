package main

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Ob eine Statusseite privat ist oder nicht existiert, darf ein Besucher ohne
// Berechtigung nicht erfahren: Beides antwortet mit demselben 404 samt gleichem
// Body. Der Test schickt an jede anonym erreichbare Route zur Seite oder ihren
// Unterobjekten (Dashboard, Verfügbarkeit, Übersetzungen, Komponenten, Vorfälle,
// Hinweise, SSO-Auskunft) dieselbe Anfrage einmal für eine private und einmal
// für eine nicht vorhandene Seite bzw. ID und vergleicht Status und Body.

type existenceFixture struct {
	db  *gorm.DB
	r   http.Handler
	pub struct{ page, incident, component, notice uint }
	prv struct{ page, incident, component, notice uint }

	publicSlug, privateSlug string
	memberToken             string
	noRoleToken             string
	ghostToken              string
	adminToken              string
}

const (
	missingSlug = "gibt-es-nicht-ex"
	missingID   = 987654
)

func setupExistenceFixture(t *testing.T) existenceFixture {
	t.Helper()
	db := testutils.SetupTestDB()
	f := existenceFixture{db: db, r: newRouter(handlers.NewHandler(db)), publicSlug: "offen-ex", privateSlug: "geheim-ex"}

	seed := func(slug string, private bool) (page, incident, component, notice uint) {
		p := models.StatusPage{Name: "Seite " + slug, Slug: slug, IsPrivate: private}
		require.NoError(t, db.Create(&p).Error)
		c := models.Component{StatusPageID: p.ID, Name: "Dienst"}
		require.NoError(t, db.Create(&c).Error)
		i := models.Incident{StatusPageID: p.ID, Title: "Störung", Status: "investigating"}
		require.NoError(t, db.Create(&i).Error)
		n := models.GeneralNotice{StatusPageID: p.ID, Title: "Hinweis", IsActive: true}
		require.NoError(t, db.Create(&n).Error)
		return p.ID, i.ID, c.ID, n.ID
	}
	f.pub.page, f.pub.incident, f.pub.component, f.pub.notice = seed(f.publicSlug, false)
	f.prv.page, f.prv.incident, f.prv.component, f.prv.notice = seed(f.privateSlug, true)

	viewer := models.Role{Name: "Lesen Existenz", Permissions: "components:read,incidents:read,notices:read"}
	require.NoError(t, db.Create(&viewer).Error)
	token := func(email string, admin bool) (models.User, string) {
		u := models.User{Email: email, IsGlobalAdmin: admin}
		require.NoError(t, db.Create(&u).Error)
		tok, err := middleware.GenerateToken(u.ID, u.Email, "", "", admin)
		require.NoError(t, err)
		return u, tok
	}
	member, memberTok := token("mitglied-ex@example.com", false)
	require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: member.ID, StatusPageID: f.prv.page, RoleID: viewer.ID}).Error)
	f.memberToken = memberTok
	_, f.noRoleToken = token("ohne-rolle-ex@example.com", false)
	_, f.adminToken = token("admin-ex@example.com", true)
	ghost, ghostTok := token("geist-ex@example.com", false)
	require.NoError(t, db.Unscoped().Delete(&ghost).Error)
	f.ghostToken = ghostTok
	return f
}

func (f existenceFixture) get(path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("X-Auth-Token", token)
	}
	req.RemoteAddr = "192.0.2.90:7000"
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

// pathPair liefert dieselbe Route einmal für die private Seite und einmal für
// ein nicht vorhandenes Ziel.
type pathPair struct{ name, private, missing, public string }

func (f existenceFixture) pairs() []pathPair {
	id := func(n uint) string { return strconv.FormatUint(uint64(n), 10) }
	var out []pathPair
	for _, route := range []string{"dashboard", "availability", "translations", "sso"} {
		out = append(out, pathPair{"/api/public/pages/:slug/" + route,
			"/api/public/pages/" + f.privateSlug + "/" + route,
			"/api/public/pages/" + missingSlug + "/" + route,
			"/api/public/pages/" + f.publicSlug + "/" + route})
	}
	out = append(out, pathPair{"/api/pages/slug/:slug",
		"/api/pages/slug/" + f.privateSlug, "/api/pages/slug/" + missingSlug, "/api/pages/slug/" + f.publicSlug})
	out = append(out, pathPair{"/api/public/pages/:slug/incidents/:incident_id/update-translations",
		"/api/public/pages/" + f.privateSlug + "/incidents/" + id(f.prv.incident) + "/update-translations",
		"/api/public/pages/" + missingSlug + "/incidents/" + id(f.prv.incident) + "/update-translations",
		"/api/public/pages/" + f.publicSlug + "/incidents/" + id(f.pub.incident) + "/update-translations"})
	for _, route := range []string{"components", "incidents", "notices"} {
		out = append(out, pathPair{"/api/pages/:page_id/" + route,
			"/api/pages/" + id(f.prv.page) + "/" + route,
			"/api/pages/" + strconv.Itoa(missingID) + "/" + route,
			"/api/pages/" + id(f.pub.page) + "/" + route})
	}
	out = append(out, pathPair{"/api/pages/:page_id/incidents/:id",
		"/api/pages/" + id(f.prv.page) + "/incidents/" + id(f.prv.incident),
		"/api/pages/" + strconv.Itoa(missingID) + "/incidents/" + id(f.prv.incident),
		"/api/pages/" + id(f.pub.page) + "/incidents/" + id(f.pub.incident)})
	out = append(out,
		pathPair{"/api/incidents/:incident_id/translations", "/api/incidents/" + id(f.prv.incident) + "/translations",
			"/api/incidents/" + strconv.Itoa(missingID) + "/translations", "/api/incidents/" + id(f.pub.incident) + "/translations"},
		pathPair{"/api/components/:component_id/translations", "/api/components/" + id(f.prv.component) + "/translations",
			"/api/components/" + strconv.Itoa(missingID) + "/translations", "/api/components/" + id(f.pub.component) + "/translations"},
		pathPair{"/api/notices/:notice_id/translations", "/api/notices/" + id(f.prv.notice) + "/translations",
			"/api/notices/" + strconv.Itoa(missingID) + "/translations", "/api/notices/" + id(f.pub.notice) + "/translations"})
	return out
}

// Angriff: Für jeden Aufrufer ohne Berechtigung ist die private Seite von der
// fehlenden nicht zu unterscheiden.
func TestRouter_PrivatePageLooksLikeMissingPageForUnauthorizedCallers(t *testing.T) {
	f := setupExistenceFixture(t)
	callers := map[string]string{
		"anonym":                        "",
		"angemeldet ohne Rolle":         f.noRoleToken,
		"Token eines gelöschten Kontos": f.ghostToken,
	}
	for who, token := range callers {
		for _, p := range f.pairs() {
			t.Run(who+" "+p.name, func(t *testing.T) {
				private, missing := f.get(p.private, token), f.get(p.missing, token)
				assert.Equal(t, http.StatusNotFound, missing.Code, "Vergleichswert: %s", missing.Body.String())
				assert.Equal(t, missing.Code, private.Code, "privat: %s", private.Body.String())
				assert.JSONEq(t, missing.Body.String(), private.Body.String(), "gleicher Body für privat und fehlend")
			})
		}
	}
}

// Gegenprobe: Dieselben Routen liefern Mitgliedern und Global Admins die Inhalte
// der privaten Seite und allen die der öffentlichen.
func TestRouter_PrivatePageContentStillReachableForMembersAndAdmins(t *testing.T) {
	f := setupExistenceFixture(t)
	for _, p := range f.pairs() {
		t.Run(p.name, func(t *testing.T) {
			assert.Equal(t, http.StatusOK, f.get(p.public, "").Code, "öffentliche Seite anonym")
			assert.Equal(t, http.StatusOK, f.get(p.private, f.memberToken).Code, "Mitglied")
			assert.Equal(t, http.StatusOK, f.get(p.private, f.adminToken).Code, "Global Admin")
		})
	}
}

// Der SSO-Hinweis nennt nur Berechtigten Seiten-ID und SSO-Art einer privaten
// Seite; öffentliche Seiten liefern ihn wie bisher an alle.
func TestRouter_SSOInfoOfPrivatePageOnlyForMembers(t *testing.T) {
	f := setupExistenceFixture(t)
	private := "/api/public/pages/" + f.privateSlug + "/sso"
	w := f.get(private, "")
	assert.NotContains(t, w.Body.String(), "page_id")
	assert.NotContains(t, w.Body.String(), "oidc_enabled")

	member := f.get(private, f.memberToken)
	require.Equal(t, http.StatusOK, member.Code)
	assert.Contains(t, member.Body.String(), `"page_id":`+strconv.Itoa(int(f.prv.page)))
	pub := f.get("/api/public/pages/"+f.publicSlug+"/sso", "")
	assert.Contains(t, pub.Body.String(), `"page_id":`+strconv.Itoa(int(f.pub.page)))
}

// Auto-Join per Standardrolle bleibt: Ein Konto mit passender E-Mail-Domain
// sieht die private Seite beim ersten Aufruf und bekommt die Rolle. Ein Konto
// ohne passende Domain sieht 404 wie bei einer fehlenden Seite.
func TestRouter_AutoJoinStillWorksForPrivatePage(t *testing.T) {
	f := setupExistenceFixture(t)
	role := models.Role{Name: "Auto-Join Lesen", Permissions: "components:read"}
	require.NoError(t, f.db.Create(&role).Error)
	require.NoError(t, f.db.Model(&models.StatusPage{}).Where("id = ?", f.prv.page).
		Updates(map[string]any{"default_role_id": role.ID, "auto_join_email_domains": "example.org"}).Error)
	join := models.User{Email: "neu@example.org"}
	other := models.User{Email: "fremd@example.net"}
	require.NoError(t, f.db.Create(&join).Error)
	require.NoError(t, f.db.Create(&other).Error)
	joinTok, _ := middleware.GenerateToken(join.ID, join.Email, "", "", false)
	otherTok, _ := middleware.GenerateToken(other.ID, other.Email, "", "", false)

	assert.Equal(t, http.StatusNotFound, f.get("/api/public/pages/"+f.privateSlug+"/dashboard", otherTok).Code)
	assert.Equal(t, http.StatusOK, f.get("/api/public/pages/"+f.privateSlug+"/dashboard", joinTok).Code)
	var n int64
	require.NoError(t, f.db.Model(&models.UserStatusPageRole{}).Where("user_id = ? AND status_page_id = ?", join.ID, f.prv.page).Count(&n).Error)
	assert.EqualValues(t, 1, n, "die Rolle wurde angelegt")
	assert.Equal(t, http.StatusOK, f.get("/api/pages/"+strconv.Itoa(int(f.prv.page))+"/components", joinTok).Code)
	_ = gin.Mode
}

// Strukturwächter: Jede anonym erreichbare GET-Route, die eine Seite oder ein
// Objekt über Slug oder ID anspricht, steht in pairs() und damit im Vergleich
// „privat gegen fehlend“. Eine neue Route dieser Art ohne Eintrag fällt hier
// auf, bevor sie Existenz oder Art einer privaten Seite verrät. Welche Routen
// anonym sind, ermittelt der Test am Router selbst (weder 401 noch 403 ohne
// Token). Die Routen unter /api/auth/ gehören nicht dazu: Anmelde-Einstiege
// (OAuth, OIDC, SAML) bleiben bewusst für jeden erreichbar.
func TestRouter_EveryAnonymousObjectRouteIsInTheExistenceComparison(t *testing.T) {
	f := setupExistenceFixture(t)
	covered := map[string]bool{}
	for _, p := range f.pairs() {
		covered[p.name] = true
	}

	objectParams := []string{":slug", ":page_id", ":incident_id", ":component_id", ":notice_id", ":id"}
	var anonymousObjectRoutes []string
	for _, ri := range newRouter(handlers.NewHandler(f.db)).Routes() {
		if ri.Method != http.MethodGet || strings.HasPrefix(ri.Path, "/api/auth/") {
			continue
		}
		hasObject := false
		parts := strings.Split(ri.Path, "/")
		for j, part := range parts {
			if strings.HasPrefix(part, ":") {
				hasObject = hasObject || slices.Contains(objectParams, part)
				parts[j] = "1"
			}
		}
		if !hasObject {
			continue
		}
		if w := f.get(strings.Join(parts, "/"), ""); w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
			continue // nur mit Anmeldung lesbar
		}
		anonymousObjectRoutes = append(anonymousObjectRoutes, ri.Path)
	}
	require.GreaterOrEqual(t, len(anonymousObjectRoutes), 10, "anonyme Objekt-Routen nicht gefunden: %v", anonymousObjectRoutes)
	for _, route := range anonymousObjectRoutes {
		assert.True(t, covered[route], "anonyme Route %s fehlt in pairs(): privat gegen fehlend vergleichen", route)
	}
	for name := range covered {
		assert.Contains(t, anonymousObjectRoutes, name, "pairs() nennt %s, die Route gibt es nicht (mehr) oder sie ist nicht anonym", name)
	}
}
