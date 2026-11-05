package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Feldlängen (Name 60, Beschreibung 240) und Sichtbarkeit.

type visEnv struct {
	t    *testing.T
	db   *gorm.DB
	r    http.Handler
	page models.StatusPage
	base string
}

func newVisEnv(t *testing.T) *visEnv {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	h.Now = func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }
	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) {
		if v := c.GetHeader("X-Test-User"); v != "" {
			id, _ := strconv.ParseUint(v, 10, 32)
			c.Set("userID", uint(id))
		}
		c.Next()
	})
	r.POST("/api/pages/:page_id/components", h.CreateComponentHandler)
	r.PUT("/api/pages/:page_id/components/:id", h.UpdateComponentHandler)
	r.GET("/api/pages/:page_id/components", h.GetComponentsHandler)
	r.GET("/api/pages/:page_id/incidents", middleware.OptionalAuthMiddleware(), h.GetIncidentsHandler)
	r.GET("/api/pages/:page_id/incidents/:id", middleware.OptionalAuthMiddleware(), h.GetIncidentDetailHandler)
	r.GET("/api/components/:component_id/translations", middleware.OptionalAuthMiddleware(), h.GetComponentTranslationsHandler)
	r.POST("/api/pages/:page_id/components/:id/archive", h.ArchiveComponentHandler)
	r.POST("/api/pages/:page_id/components/:id/restore", h.RestoreComponentHandler)
	r.POST("/api/webhooks/components/:component_id/status", h.ComponentStatusWebhookHandler)
	g := r.Group("/api/public")
	g.Use(middleware.OptionalAuthMiddleware())
	g.GET("/pages/:slug/dashboard", h.GetPublicDashboardHandler)
	g.GET("/pages/:slug/availability", h.GetPublicAvailabilityHandler)
	e := &visEnv{t: t, db: db, r: r}
	e.page = models.StatusPage{Name: "Vis", Slug: "vis-580", WebhookTokenHash: models.HashToken("tok")}
	require.NoError(t, db.Create(&e.page).Error)
	require.NoError(t, db.Model(&e.page).Update("created_at", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)).Error)
	e.base = "/api/pages/" + strconv.Itoa(int(e.page.ID)) + "/components"
	return e
}

func (e *visEnv) comp(name string, parent *uint, visible bool, status string) models.Component {
	c := models.Component{StatusPageID: e.page.ID, Name: name, Status: status, ParentID: parent}
	require.NoError(e.t, e.db.Create(&c).Error)
	if !visible {
		require.NoError(e.t, e.db.Model(&c).Update("visible", false).Error)
	}
	return c
}

func (e *visEnv) do(method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	e.t.Helper()
	w := doTemplateRequest(e.r, method, path, body)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w, m
}

func (e *visEnv) get(path string) *httptest.ResponseRecorder {
	e.t.Helper()
	return doTemplateRequest(e.r, "GET", path, nil)
}

func TestComponentLaengen_Create(t *testing.T) {
	e := newVisEnv(t)
	count := func() int64 {
		var n int64
		e.db.Model(&models.Component{}).Where("status_page_id = ?", e.page.ID).Count(&n)
		return n
	}

	w, m := e.do("POST", e.base, map[string]any{"name": strings.Repeat("a", 61)})
	requireStatusCode(t, w, http.StatusBadRequest)
	assert.Equal(t, "name_too_long", m["code"])

	w, m = e.do("POST", e.base, map[string]any{"name": "ok", "description": strings.Repeat("b", 241)})
	requireStatusCode(t, w, http.StatusBadRequest)
	assert.Equal(t, "description_too_long", m["code"])
	assert.Zero(t, count(), "nichts darf angelegt werden")

	// Genau an der Grenze; Umlaute zählen als je ein Zeichen (nicht als Bytes).
	w, _ = e.do("POST", e.base, map[string]any{"name": strings.Repeat("ä", 60), "description": strings.Repeat("ö", 240)})
	requireStatusCode(t, w, http.StatusCreated)
	assert.EqualValues(t, 1, count())
}

func TestComponentLaengen_Update(t *testing.T) {
	e := newVisEnv(t)
	long := strings.Repeat("x", 300)
	// Bestandsdaten dürfen länger sein: direkt in die DB geschrieben.
	comp := e.comp(strings.Repeat("n", 70), nil, true, "operational")
	require.NoError(t, e.db.Model(&comp).Update("description", long).Error)
	url := e.base + "/" + strconv.Itoa(int(comp.ID))

	// Quick-Status schickt nur den Status: Bestand bleibt unberührt und stört nicht.
	w, _ := e.do("PUT", url, map[string]any{"status": "major_outage"})
	requireStatusCode(t, w, http.StatusOK)
	var fresh models.Component
	e.db.First(&fresh, comp.ID)
	assert.Equal(t, long, fresh.Description)
	assert.Len(t, []rune(fresh.Name), 70)

	// Beim nächsten Speichern der langen Felder wird geprüft.
	w, m := e.do("PUT", url, map[string]any{"name": strings.Repeat("n", 61), "status": "operational"})
	requireStatusCode(t, w, http.StatusBadRequest)
	assert.Equal(t, "name_too_long", m["code"])
	w, m = e.do("PUT", url, map[string]any{"status": "operational", "description": long})
	requireStatusCode(t, w, http.StatusBadRequest)
	assert.Equal(t, "description_too_long", m["code"])
	e.db.First(&fresh, comp.ID)
	assert.Equal(t, "major_outage", fresh.Status, "abgelehntes Speichern ändert nichts")

	w, _ = e.do("PUT", url, map[string]any{"name": strings.Repeat("n", 60), "status": "operational", "description": strings.Repeat("d", 240)})
	requireStatusCode(t, w, http.StatusOK)

	// Mandantentrennung: fremde Seite bleibt 404, auch mit zu langem Namen.
	other := models.StatusPage{Name: "Fremd", Slug: "fremd-580"}
	require.NoError(t, e.db.Create(&other).Error)
	foreign := "/api/pages/" + strconv.Itoa(int(other.ID)) + "/components/" + strconv.Itoa(int(comp.ID))
	w, _ = e.do("PUT", foreign, map[string]any{"name": strings.Repeat("n", 61), "status": "operational"})
	requireStatusCode(t, w, http.StatusNotFound)
}

func TestComponentVisible_CreateUpdate(t *testing.T) {
	e := newVisEnv(t)

	w, m := e.do("POST", e.base, map[string]any{"name": "Standard"})
	requireStatusCode(t, w, http.StatusCreated)
	assert.Equal(t, true, m["Visible"], "ohne Angabe sichtbar")

	w, m = e.do("POST", e.base, map[string]any{"name": "Versteckt", "visible": false})
	requireStatusCode(t, w, http.StatusCreated)
	assert.Equal(t, false, m["Visible"])
	var stored models.Component
	require.NoError(t, e.db.First(&stored, uint(m["ID"].(float64))).Error)
	assert.False(t, stored.Visible, "false muss trotz GORM-Default true gespeichert sein")

	url := e.base + "/" + strconv.Itoa(int(stored.ID))
	// Quick-Status ohne visible lässt das Feld unverändert.
	w, _ = e.do("PUT", url, map[string]any{"status": "degraded_performance"})
	requireStatusCode(t, w, http.StatusOK)
	e.db.First(&stored, stored.ID)
	assert.False(t, stored.Visible)

	w, _ = e.do("PUT", url, map[string]any{"status": "operational", "visible": true})
	requireStatusCode(t, w, http.StatusOK)
	e.db.First(&stored, stored.ID)
	assert.True(t, stored.Visible)

	w, _ = e.do("PUT", url, map[string]any{"status": "operational", "visible": false})
	requireStatusCode(t, w, http.StatusOK)
	e.db.First(&stored, stored.ID)
	assert.False(t, stored.Visible)
}

func TestComponentVisible_AdminSiehtAlles_WebhookFunktioniert(t *testing.T) {
	e := newVisEnv(t)
	hidden := e.comp("Intern", nil, false, "operational")
	e.comp("Offen", nil, true, "operational")

	listAs := func(userID uint) []models.Component {
		req, _ := http.NewRequest("GET", e.base, nil)
		if userID != 0 {
			req.Header.Set("X-Test-User", strconv.Itoa(int(userID)))
		}
		w := httptest.NewRecorder()
		e.r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusOK)
		var tree []models.Component
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tree))
		return tree
	}
	admin := models.User{Email: "vis-admin@example.com", IsGlobalAdmin: true}
	require.NoError(t, e.db.Create(&admin).Error)
	editorRole := models.Role{Name: "Redakteur-580", Permissions: "components:read,components:write"}
	require.NoError(t, e.db.Create(&editorRole).Error)
	editor := models.User{Email: "vis-editor@example.com"}
	require.NoError(t, e.db.Create(&editor).Error)
	require.NoError(t, e.db.Create(&models.UserStatusPageRole{UserID: editor.ID, StatusPageID: e.page.ID, RoleID: editorRole.ID}).Error)
	plainRole := models.Role{Name: "Leser-580", Permissions: "notices:read"}
	require.NoError(t, e.db.Create(&plainRole).Error)
	reader := models.User{Email: "vis-reader@example.com"}
	require.NoError(t, e.db.Create(&reader).Error)
	require.NoError(t, e.db.Create(&models.UserStatusPageRole{UserID: reader.ID, StatusPageID: e.page.ID, RoleID: plainRole.ID}).Error)

	// Die Route ist anonym lesbar: Anonyme und Leser ohne Komponentenrecht
	// bekommen den gefilterten Baum, Verwalter sehen alles mit Markierung.
	assert.Len(t, listAs(0), 1, "anonym: unsichtbare Komponente fehlt")
	assert.Len(t, listAs(reader.ID), 1, "Rolle ohne Komponentenrecht: fehlt")
	for _, uid := range []uint{admin.ID, editor.ID} {
		tree := listAs(uid)
		require.Len(t, tree, 2)
		var vis []bool
		for _, c := range tree {
			vis = append(vis, c.Visible)
		}
		assert.ElementsMatch(t, []bool{true, false}, vis, "Admin sieht die Komponente samt Markierung")
	}

	wh := doWebhookStatusPost(e.r, strconv.Itoa(int(hidden.ID)), "major_outage", "tok")
	requireStatusCode(t, wh, http.StatusOK)
	var fresh models.Component
	e.db.First(&fresh, hidden.ID)
	assert.Equal(t, "major_outage", fresh.Status)
	assert.False(t, fresh.Visible, "Webhook darf die Sichtbarkeit nicht ändern")
}

type dashResp struct {
	Components []struct {
		Name string
	} `json:"components"`
	Incidents []struct {
		Title      string
		Components []struct{ Name string }
	} `json:"incidents"`
}

func TestComponentVisible_OeffentlichesDashboard(t *testing.T) {
	e := newVisEnv(t)
	e.comp("Sichtbar", nil, true, "operational")
	hiddenComp := e.comp("Geheim", nil, false, "major_outage")
	grp := e.comp("Versteckte Gruppe", nil, false, "group")
	e.comp("Kind der versteckten Gruppe", &grp.ID, true, "major_outage")
	visGrp := e.comp("Offene Gruppe", nil, true, "group")
	e.comp("Kind offen", &visGrp.ID, true, "operational")
	e.comp("Kind versteckt", &visGrp.ID, false, "major_outage")

	shown := e.comp("Gezeigt", nil, true, "operational")
	inc := models.Incident{StatusPageID: e.page.ID, Title: "Vorfall", Message: "m", Status: "investigating"}
	require.NoError(t, e.db.Create(&inc).Error)
	for _, c := range []models.Component{hiddenComp, shown} {
		require.NoError(t, e.db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: c.ID}).Error)
	}

	w := e.get("/api/public/pages/vis-580/dashboard")
	requireStatusCode(t, w, http.StatusOK)
	body := w.Body.String()
	for _, secret := range []string{"Geheim", "Versteckte Gruppe", "Kind der versteckten Gruppe", "Kind versteckt"} {
		assert.NotContains(t, body, secret)
	}
	var resp dashResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	names := []string{}
	for _, c := range resp.Components {
		names = append(names, c.Name)
	}
	assert.ElementsMatch(t, []string{"Sichtbar", "Offene Gruppe", "Gezeigt"}, names)
	require.Len(t, resp.Incidents, 1, "der Vorfall selbst bleibt sichtbar")
	require.Len(t, resp.Incidents[0].Components, 1)
	assert.Equal(t, "Gezeigt", resp.Incidents[0].Components[0].Name)
}

func TestComponentVisible_Verfuegbarkeit(t *testing.T) {
	e := newVisEnv(t)
	e.comp("Web", nil, true, "operational")
	hid := e.comp("Intern", nil, false, "operational")
	start := time.Date(2026, 10, 20, 6, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 20, 18, 0, 0, 0, time.UTC)
	inc := models.Incident{StatusPageID: e.page.ID, Title: "t", Message: "m", Status: "resolved", StartedAt: &start, ResolvedAt: &end}
	require.NoError(t, e.db.Create(&inc).Error)
	require.NoError(t, e.db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: hid.ID}).Error)

	w := e.get("/api/public/pages/vis-580/availability?include=components")
	requireStatusCode(t, w, http.StatusOK)
	assert.NotContains(t, w.Body.String(), "Intern")
	var resp availResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Components, 1)
	assert.Equal(t, "Web", resp.Components[0].Name)
	d := day(t, resp.Days, "2026-10-20")
	assert.Equal(t, "operational", d.Status, "Ausfall einer unsichtbaren Komponente zählt nicht in die öffentliche Verfügbarkeit")
	assert.Zero(t, d.DowntimeSeconds)
}

// Meldungen externer Quellen an einer unsichtbaren Komponente zählen nicht in
// die öffentliche Verfügbarkeit der Seite.
func TestComponentVisible_VerfuegbarkeitIgnoriertExterneMeldungenUnsichtbarer(t *testing.T) {
	x := newExtAvail(t)
	x.add(models.ExternalIncidentKindIncident, "major", utc(20, 6), utc(20, 12), true)
	_, before := x.get("/api/public/pages/ext/availability")
	assert.Equal(t, "partial_outage", day(t, before.Days, "2026-10-20").Status, "Gegenprobe: sichtbar zählt (impact major)")

	require.NoError(t, x.db.Model(&x.comp).Update("visible", false).Error)
	x.now = x.now.Add(2 * time.Minute) // Cache (60 s) umgehen
	_, after := x.get("/api/public/pages/ext/availability")
	assert.Equal(t, "operational", day(t, after.Days, "2026-10-20").Status)
}

// Schlägt das Nachziehen von visible=false fehl, scheitert das Anlegen
// komplett (Transaktion), statt eine sichtbare Komponente zu hinterlassen.
func TestComponentVisible_CreateUnsichtbarFehlerRollbackt(t *testing.T) {
	e := newVisEnv(t)
	require.NoError(t, e.db.Callback().Update().Before("gorm:update").Register("test:fail", func(d *gorm.DB) {
		_ = d.AddError(errors.New("boom"))
	}))
	w, _ := e.do("POST", e.base, map[string]any{"name": "Versteckt", "visible": false})
	requireStatusCode(t, w, http.StatusInternalServerError)
	var n int64
	e.db.Model(&models.Component{}).Where("status_page_id = ?", e.page.ID).Count(&n)
	assert.Zero(t, n)
}
