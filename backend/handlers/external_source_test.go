package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fraigner/statushafen/externalstatus"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Admin-API externer Statusseiten. Zwei Statusseiten über Kreuz,
// damit jeder Handler gegen die Mandantengrenze geprüft wird.

type extFakeProvider struct {
	mu   sync.Mutex
	sum  *externalstatus.Summary
	err  error
	reqs []externalstatus.FetchRequest
	// Feeds je Art (incident/maintenance); feedErr lässt sie scheitern.
	feeds   map[string][]externalstatus.Incident
	feedErr error
}

// FetchIncidents implementiert externalstatus.IncidentProvider.
func (p *extFakeProvider) FetchIncidents(_ context.Context, req externalstatus.IncidentRequest) (*externalstatus.IncidentFeed, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.feedErr != nil {
		return nil, p.feedErr
	}
	return &externalstatus.IncidentFeed{Items: p.feeds[req.Kind]}, nil
}

func (p *extFakeProvider) FetchSummary(_ context.Context, req externalstatus.FetchRequest) (*externalstatus.Summary, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reqs = append(p.reqs, req)
	return p.sum, p.err
}

type extListener struct {
	mu      sync.Mutex
	changes []externalstatus.StatusChange
}

func (l *extListener) EffectiveStatusChanged(_ context.Context, ch externalstatus.StatusChange) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.changes = append(l.changes, ch)
}

type extFixture struct {
	t                      *testing.T
	db                     *gorm.DB
	h                      *handlers.Handler
	r                      *gin.Engine
	provider               *extFakeProvider
	listener               *extListener
	clock                  time.Time // Fake-Uhr des Dienstes; Null = echte Zeit
	page, other            models.StatusPage
	adminTok, pageAdminTok string
	viewerTok, otherTok    string
	comp, otherComp, group models.Component
	child                  models.Component
}

func newExtFixture(t *testing.T) *extFixture {
	t.Helper()
	f := &extFixture{t: t, db: testutils.SetupTestDB(), provider: &extFakeProvider{}, listener: &extListener{}}
	f.h = handlers.NewHandler(f.db)
	f.h.ExternalStatus = externalstatus.NewService(f.db, externalstatus.Options{
		Providers: map[string]externalstatus.Provider{externalstatus.ProviderSummaryV2: f.provider},
		Listener:  f.listener,
		Outage:    f.h,
		Incidents: f.h,
		Jitter:    func(time.Duration) time.Duration { return 0 },
		Now: func() time.Time {
			if f.clock.IsZero() {
				return time.Now()
			}
			return f.clock
		},
	})
	f.r = testutils.SetupRouter()
	auth := f.r.Group("/api")
	auth.Use(middleware.AuthMiddleware())
	// Ressource external_sources; die echte Zuordnung je Aktion prüft main_route_permissions_test.go.
	read, write := middleware.RequirePermission("external_sources:read"), middleware.RequirePermission("external_sources:update")
	src := auth.Group("/pages/:page_id/external-sources")
	src.GET("", read, f.h.GetExternalSourcesHandler)
	src.POST("", write, f.h.CreateExternalSourceHandler)
	src.GET("/:id", read, f.h.GetExternalSourceHandler)
	src.PUT("/:id", write, f.h.UpdateExternalSourceHandler)
	src.DELETE("/:id", write, f.h.DeleteExternalSourceHandler)
	src.POST("/:id/fetch", write, f.h.FetchExternalSourceHandler)
	src.GET("/:id/components", read, f.h.GetExternalComponentsHandler)
	src.POST("/:id/import", write, f.h.ImportExternalComponentsHandler)
	auth.GET("/pages/:page_id/external-outages", read, f.h.GetExternalOutagesHandler)
	auth.GET("/pages/:page_id/external-incidents", read, f.h.GetExternalIncidentsHandler)
	auth.GET("/pages/:page_id/external-incidents/:id", read, f.h.GetExternalIncidentHandler)
	links := auth.Group("/pages/:page_id/external-links")
	links.GET("", read, f.h.GetExternalLinksHandler)
	links.POST("", write, f.h.CreateExternalLinkHandler)
	links.DELETE("/:id", write, f.h.DeleteExternalLinkHandler)
	f.r.GET("/api/pages/:page_id/components", middleware.OptionalAuthMiddleware(), f.h.GetComponentsHandler)
	f.r.DELETE("/api/pages/:page_id/components/:id", f.h.DeleteComponentHandler)
	f.r.GET("/api/public/pages/:slug/dashboard", f.h.GetPublicDashboardHandler)
	f.r.GET("/api/admin/overview", f.h.GetOverviewHandler)

	f.page = models.StatusPage{Name: "Eigene", Slug: "eigene"}
	f.other = models.StatusPage{Name: "Fremde", Slug: "fremde"}
	require.NoError(t, f.db.Create(&f.page).Error)
	require.NoError(t, f.db.Create(&f.other).Error)
	f.group = models.Component{StatusPageID: f.page.ID, Name: "Gruppe", Status: "group"}
	require.NoError(t, f.db.Create(&f.group).Error)
	f.comp = models.Component{StatusPageID: f.page.ID, Name: "API", Status: "operational"}
	f.child = models.Component{StatusPageID: f.page.ID, Name: "Kind", Status: "operational", ParentID: &f.group.ID}
	f.otherComp = models.Component{StatusPageID: f.other.ID, Name: "Fremd-API", Status: "operational"}
	require.NoError(t, f.db.Create(&f.comp).Error)
	require.NoError(t, f.db.Create(&f.child).Error)
	require.NoError(t, f.db.Create(&f.otherComp).Error)

	rw := models.Role{Name: "Page Admin", Permissions: "components:read,components:update,external_sources:read,external_sources:update"}
	ro := models.Role{Name: "Viewer", Permissions: "components:read,external_sources:read"}
	require.NoError(t, f.db.Create(&rw).Error)
	require.NoError(t, f.db.Create(&ro).Error)
	f.adminTok = f.user("admin@example.com", true, nil)
	f.pageAdminTok = f.user("pa@example.com", false, map[uint]uint{f.page.ID: rw.ID})
	f.viewerTok = f.user("viewer@example.com", false, map[uint]uint{f.page.ID: ro.ID})
	f.otherTok = f.user("other@example.com", false, map[uint]uint{f.other.ID: rw.ID})
	return f
}

func (f *extFixture) user(email string, global bool, roles map[uint]uint) string {
	f.t.Helper()
	u := models.User{Email: email, IsGlobalAdmin: global}
	require.NoError(f.t, f.db.Create(&u).Error)
	for pageID, roleID := range roles {
		require.NoError(f.t, f.db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: pageID, RoleID: roleID}).Error)
	}
	tok, err := middleware.GenerateToken(u.ID, u.Email, "", "", global)
	require.NoError(f.t, err)
	return tok
}

func (f *extFixture) do(method, path, tok string, body any) *httptest.ResponseRecorder {
	f.t.Helper()
	var buf *bytes.Reader
	switch b := body.(type) {
	case nil:
		buf = bytes.NewReader(nil)
	case string:
		buf = bytes.NewReader([]byte(b))
	default:
		raw, err := json.Marshal(b)
		require.NoError(f.t, err)
		buf = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

func pid(p models.StatusPage) string { return strconv.FormatUint(uint64(p.ID), 10) }
func uid(id uint) string             { return strconv.FormatUint(uint64(id), 10) }

func (f *extFixture) sourcesPath(p models.StatusPage) string {
	return "/api/pages/" + pid(p) + "/external-sources"
}

func (f *extFixture) linksPath(p models.StatusPage) string {
	return "/api/pages/" + pid(p) + "/external-links"
}

// createSource legt per API eine Quelle an und gibt ihre ID zurück.
func (f *extFixture) createSource(p models.StatusPage, tok, base string) uint {
	f.t.Helper()
	w := f.do(http.MethodPost, f.sourcesPath(p), tok, map[string]any{"name": "Example Cloud", "base_url": base})
	require.Equal(f.t, http.StatusCreated, w.Code, w.Body.String())
	var resp struct{ ID uint }
	require.NoError(f.t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.ID
}

// extComp legt direkt eine externe Komponente der Quelle an.
func (f *extFixture) extComp(srcID uint, externalID, name, status string) models.ExternalComponent {
	f.t.Helper()
	ec := models.ExternalComponent{ExternalSourceID: srcID, ExternalID: externalID, Name: name, Status: status, RawStatus: status,
		GroupName: "Group A", GroupExternalID: "g1", URL: "https://status.example.com", LastSeenAt: time.Now()}
	require.NoError(f.t, f.db.Create(&ec).Error)
	return ec
}

func (f *extFixture) linkVia(p models.StatusPage, tok string, compID, extID uint) *httptest.ResponseRecorder {
	return f.do(http.MethodPost, f.linksPath(p), tok, map[string]any{"component_id": compID, "external_component_id": extID})
}

func (f *extFixture) auditActions() []string {
	var logs []models.AuditLog
	f.db.Order("id").Find(&logs)
	out := make([]string, 0, len(logs))
	for _, l := range logs {
		out = append(out, l.Action)
	}
	return out
}

// extFailOn lässt GORM-Operationen op auf table scheitern (Fehlerpfade → 500).
func extFailOn(t *testing.T, db *gorm.DB, op, table string) {
	t.Helper()
	name := "test:extfail:" + op + ":" + table
	hook := func(tx *gorm.DB) {
		if tx.Statement.Table == table {
			_ = tx.AddError(errors.New("injizierter Fehler"))
		}
	}
	var err error
	switch op {
	case "create":
		err = db.Callback().Create().Before("gorm:create").Register(name, hook)
	case "update":
		err = db.Callback().Update().Before("gorm:update").Register(name, hook)
	case "delete":
		err = db.Callback().Delete().Before("gorm:delete").Register(name, hook)
	case "row":
		err = db.Callback().Row().Before("gorm:row").Register(name, hook)
	default:
		err = db.Callback().Query().Before("gorm:query").Register(name, hook)
	}
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Callback().Create().Remove(name)
		_ = db.Callback().Update().Remove(name)
		_ = db.Callback().Delete().Remove(name)
		_ = db.Callback().Query().Remove(name)
		_ = db.Callback().Row().Remove(name)
	})
}

func TestExternalSources_CRUD(t *testing.T) {
	f := newExtFixture(t)
	w := f.do(http.MethodPost, f.sourcesPath(f.page), f.pageAdminTok, map[string]any{"name": " Example Cloud  ", "base_url": "https://Status.Example.Com/"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	assert.Equal(t, "Example Cloud", created["name"])
	assert.Equal(t, "https://status.example.com", created["base_url"])
	assert.Equal(t, "summaryv2", created["provider"])
	assert.EqualValues(t, 120, created["poll_interval_seconds"])
	assert.Equal(t, true, created["active"])
	assert.Nil(t, created["last_fetch_ok"])
	assert.EqualValues(t, 0, created["component_count"])
	assert.NotContains(t, created, "ETag", "Validatoren sind intern")
	id := uint(created["ID"].(float64))

	// Doppelte Basis-URL auf derselben Seite → 409, auf einer anderen Seite erlaubt.
	w = f.do(http.MethodPost, f.sourcesPath(f.page), f.pageAdminTok, map[string]any{"name": "x", "base_url": "https://status.example.com"})
	assert.Equal(t, http.StatusConflict, w.Code)
	otherID := f.createSource(f.other, f.otherTok, "https://status.example.com")

	f.extComp(id, "vm", "Service 03", "operational")
	w = f.do(http.MethodGet, f.sourcesPath(f.page), f.viewerTok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 1, "nur Quellen der eigenen Seite")
	assert.EqualValues(t, 1, list[0]["component_count"])
	assert.EqualValues(t, 0, list[0]["link_count"])

	w = f.do(http.MethodGet, f.sourcesPath(f.page)+"/"+uid(id), f.viewerTok, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	// Update: Intervall, aktiv, neue Basis-URL setzt Validatoren zurück.
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", id).Updates(map[string]any{"e_tag": "alt", "last_modified": "gestern"}).Error)
	w = f.do(http.MethodPut, f.sourcesPath(f.page)+"/"+uid(id), f.pageAdminTok, map[string]any{"poll_interval_seconds": 300, "active": false, "base_url": "https://www.githubstatus.com", "name": "GitHub"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var src models.ExternalSource
	require.NoError(t, f.db.First(&src, id).Error)
	assert.Equal(t, 300, src.PollIntervalSeconds)
	assert.False(t, src.Active)
	assert.Equal(t, "GitHub", src.Name)
	assert.Equal(t, "https://www.githubstatus.com", src.BaseURL)
	assert.Empty(t, src.ETag)
	assert.Empty(t, src.LastModified)

	// Gleiche URL wie vorher: Validatoren bleiben.
	require.NoError(t, f.db.Model(&models.ExternalSource{}).Where("id = ?", id).Update("e_tag", "neu").Error)
	w = f.do(http.MethodPut, f.sourcesPath(f.page)+"/"+uid(id), f.pageAdminTok, map[string]any{"base_url": "https://www.githubstatus.com/"})
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, f.db.First(&src, id).Error)
	assert.Equal(t, "neu", src.ETag)

	// Update auf eine schon vorhandene Basis-URL derselben Seite → 409.
	secondID := f.createSource(f.page, f.pageAdminTok, "https://www.cloudflarestatus.com")
	w = f.do(http.MethodPut, f.sourcesPath(f.page)+"/"+uid(secondID), f.pageAdminTok, map[string]any{"base_url": "https://www.githubstatus.com"})
	assert.Equal(t, http.StatusConflict, w.Code)

	// Löschen: Quelle, Komponenten und Kopplungen weg; fremde Quelle bleibt.
	w = f.linkVia(f.page, f.pageAdminTok, f.comp.ID, f.extComp(id, "db", "DB", "operational").ID)
	require.Equal(t, http.StatusCreated, w.Code)
	w = f.do(http.MethodDelete, f.sourcesPath(f.page)+"/"+uid(id), f.pageAdminTok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var n int64
	f.db.Model(&models.ExternalComponent{}).Where("external_source_id = ?", id).Count(&n)
	assert.Zero(t, n)
	f.db.Model(&models.ExternalComponentLink{}).Where("component_id = ?", f.comp.ID).Count(&n)
	assert.Zero(t, n)
	assert.Equal(t, http.StatusNotFound, f.do(http.MethodGet, f.sourcesPath(f.page)+"/"+uid(id), f.adminTok, nil).Code)
	assert.Equal(t, http.StatusOK, f.do(http.MethodGet, f.sourcesPath(f.other)+"/"+uid(otherID), f.otherTok, nil).Code)

	assert.Subset(t, f.auditActions(), []string{"external_source.created", "external_source.updated", "external_source.deleted", "external_link.created"})
}

func TestExternalSources_Validierung(t *testing.T) {
	f := newExtFixture(t)
	path := f.sourcesPath(f.page)
	bad := []any{
		"kein json",
		map[string]any{"name": "x"},
		map[string]any{"base_url": "https://example.com"},
		map[string]any{"name": " ", "base_url": "https://example.com"},
		map[string]any{"name": strings.Repeat("x", 201), "base_url": "https://example.com"},
		map[string]any{"name": "x", "base_url": "http://example.com"},
		map[string]any{"name": "x", "base_url": "https://example.com:8443"},
		map[string]any{"name": "x", "base_url": "https://u:p@example.com"},
		map[string]any{"name": "x", "base_url": "https://example.com", "provider": "statusio"},
		map[string]any{"name": "x", "base_url": "https://example.com", "poll_interval_seconds": 59},
		map[string]any{"name": "x", "base_url": "https://example.com", "poll_interval_seconds": 86401},
	}
	for i, b := range bad {
		w := f.do(http.MethodPost, path, f.pageAdminTok, b)
		assert.Equal(t, http.StatusBadRequest, w.Code, "Fall %d: %s", i, w.Body.String())
	}
	w := f.do(http.MethodPost, path, f.pageAdminTok, map[string]any{"name": "x", "base_url": "https://example.com", "provider": "statusio"})
	assert.Contains(t, w.Body.String(), "summaryv2", "nennt die unterstützten Provider")

	w = f.do(http.MethodPost, path, f.pageAdminTok, map[string]any{"name": "x", "base_url": "https://example.com", "poll_interval_seconds": 60, "active": false, "provider": "summaryv2"})
	require.Equal(t, http.StatusCreated, w.Code, "Grenzwert 60 s und explizit inaktiv")
	assert.Contains(t, w.Body.String(), `"active":false`)
	var created struct{ ID uint }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	one := path + "/" + uid(created.ID)

	assert.Equal(t, http.StatusBadRequest, f.do(http.MethodPut, one, f.pageAdminTok, "kein json").Code)
	assert.Equal(t, http.StatusBadRequest, f.do(http.MethodPut, one, f.pageAdminTok, map[string]any{"poll_interval_seconds": 1}).Code)
	assert.Equal(t, http.StatusOK, f.do(http.MethodPut, one, f.pageAdminTok, map[string]any{"poll_interval_seconds": 86400}).Code)
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		assert.Equal(t, http.StatusBadRequest, f.do(m, path+"/abc", f.pageAdminTok, map[string]any{}).Code, m)
	}
	assert.Equal(t, http.StatusBadRequest, f.do(http.MethodPost, path+"/abc/fetch", f.pageAdminTok, nil).Code)
	assert.Equal(t, http.StatusBadRequest, f.do(http.MethodGet, path+"/abc/components", f.pageAdminTok, nil).Code)
}

func TestExternalSources_Berechtigungen(t *testing.T) {
	f := newExtFixture(t)
	id := f.createSource(f.page, f.adminTok, "https://status.example.com")
	path := f.sourcesPath(f.page)
	one := path + "/" + uid(id)

	assert.Equal(t, http.StatusUnauthorized, f.do(http.MethodGet, path, "", nil).Code)
	assert.Equal(t, http.StatusOK, f.do(http.MethodGet, path, f.viewerTok, nil).Code, "Lesen mit external_sources:read")
	assert.Equal(t, http.StatusOK, f.do(http.MethodGet, one+"/components", f.viewerTok, nil).Code)
	assert.Equal(t, http.StatusOK, f.do(http.MethodGet, f.linksPath(f.page), f.viewerTok, nil).Code)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, path}, {http.MethodPut, one}, {http.MethodDelete, one}, {http.MethodPost, one + "/fetch"},
		{http.MethodPost, f.linksPath(f.page)}, {http.MethodDelete, f.linksPath(f.page) + "/1"},
	} {
		assert.Equal(t, http.StatusForbidden, f.do(tc.method, tc.path, f.viewerTok, map[string]any{}).Code, "Viewer: %s %s", tc.method, tc.path)
		assert.Equal(t, http.StatusForbidden, f.do(tc.method, tc.path, f.otherTok, map[string]any{}).Code, "fremde Seite: %s %s", tc.method, tc.path)
	}
	assert.Equal(t, http.StatusForbidden, f.do(http.MethodGet, path, f.otherTok, nil).Code, "Rolle nur auf der anderen Seite")
}

// Über Kreuz: Die Admin der anderen Seite adressiert Quelle, Komponenten und
// Kopplungen von Seite 1 über die URL ihrer eigenen Seite → überall 404.
func TestExternalSources_MandantentrennungUeberKreuz(t *testing.T) {
	f := newExtFixture(t)
	id := f.createSource(f.page, f.pageAdminTok, "https://status.example.com")
	ec := f.extComp(id, "vm", "VM", "major_outage")
	w := f.linkVia(f.page, f.pageAdminTok, f.comp.ID, ec.ID)
	require.Equal(t, http.StatusCreated, w.Code)
	var link struct {
		ID uint `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &link))

	otherSrc := f.createSource(f.other, f.otherTok, "https://status.example.com")
	otherEC := f.extComp(otherSrc, "vm", "Fremd-VM", "operational")

	cross := f.sourcesPath(f.other) + "/" + uid(id)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, cross}, {http.MethodPut, cross}, {http.MethodDelete, cross},
		{http.MethodPost, cross + "/fetch"}, {http.MethodGet, cross + "/components"},
		{http.MethodDelete, f.linksPath(f.other) + "/" + uid(link.ID)},
	} {
		assert.Equal(t, http.StatusNotFound, f.do(tc.method, tc.path, f.otherTok, map[string]any{"name": "gekapert"}).Code, "%s %s", tc.method, tc.path)
	}
	// Koppeln über Kreuz: fremde eigene Komponente bzw. fremde externe Komponente.
	assert.Equal(t, http.StatusNotFound, f.linkVia(f.other, f.otherTok, f.comp.ID, otherEC.ID).Code, "eigene Komponente einer fremden Seite")
	assert.Equal(t, http.StatusNotFound, f.linkVia(f.other, f.otherTok, f.otherComp.ID, ec.ID).Code, "externe Komponente einer fremden Quelle")
	assert.Equal(t, http.StatusNotFound, f.linkVia(f.page, f.adminTok, f.comp.ID, otherEC.ID).Code, "auch der Global Admin koppelt nicht über Kreuz")

	// Die Liste der anderen Seite enthält nichts von Seite 1.
	w = f.do(http.MethodGet, f.linksPath(f.other), f.otherTok, nil)
	assert.Equal(t, "[]", strings.TrimSpace(w.Body.String()))
	var src models.ExternalSource
	require.NoError(t, f.db.First(&src, id).Error)
	assert.Equal(t, "Example Cloud", src.Name, "nicht verändert")
	var n int64
	f.db.Model(&models.ExternalComponentLink{}).Count(&n)
	assert.EqualValues(t, 1, n, "Kopplung von Seite 1 unverändert")
}

func TestExternalSources_Fetch(t *testing.T) {
	f := newExtFixture(t)
	id := f.createSource(f.page, f.pageAdminTok, "https://status.example.com")
	f.provider.sum = &externalstatus.Summary{ETag: "e1", PageURL: "https://status.example.com", Components: []externalstatus.Component{
		{ExternalID: "g1", Name: "Group A", IsGroup: true, Status: "operational", RawStatus: "operational"},
		{ExternalID: "vm", Name: "Service 03", GroupExternalID: "g1", GroupName: "Group A", Status: "partial_outage", RawStatus: "partial_outage"},
	}}
	w := f.do(http.MethodPost, f.sourcesPath(f.page)+"/"+uid(id)+"/fetch", f.pageAdminTok, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Result externalstatus.FetchResult `json:"result"`
		Source map[string]any             `json:"source"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Result.OK)
	assert.Equal(t, 2, resp.Result.Created)
	assert.EqualValues(t, 2, resp.Source["component_count"])
	assert.Equal(t, true, resp.Source["last_fetch_ok"])
	assert.Equal(t, "https://status.example.com", f.provider.reqs[0].BaseURL)

	f.provider.sum, f.provider.err = nil, errors.New("HTTP 503 von https://status.example.com/api/v2/summary.json")
	w = f.do(http.MethodPost, f.sourcesPath(f.page)+"/"+uid(id)+"/fetch", f.pageAdminTok, nil)
	require.Equal(t, http.StatusOK, w.Code, "Fehler der Fremdseite ist ein Ergebnis, kein 5xx")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.Result.OK)
	assert.Contains(t, resp.Result.Error, "HTTP 503")
	assert.Equal(t, false, resp.Source["last_fetch_ok"])
	assert.Contains(t, resp.Source["last_error"], "HTTP 503")
	var logs []models.AuditLog
	f.db.Where("action = ?", "external_source.fetched").Order("id").Find(&logs)
	require.Len(t, logs, 2)
	assert.Equal(t, "ok", logs[0].Details)
	assert.Contains(t, logs[1].Details, "HTTP 503")
}

func TestExternalComponents_SucheUndFilter(t *testing.T) {
	f := newExtFixture(t)
	id := f.createSource(f.page, f.pageAdminTok, "https://status.example.com")
	g := models.ExternalComponent{ExternalSourceID: id, ExternalID: "g1", Name: "Group A", IsGroup: true, Status: "operational", LastSeenAt: time.Now()}
	require.NoError(t, f.db.Create(&g).Error)
	vm := f.extComp(id, "vm", "Service 03", "partial_outage")
	f.extComp(id, "k8s", "Service 02", "operational")
	gone := f.extComp(id, "alt", "Altes Produkt", "operational")
	require.NoError(t, f.db.Model(&gone).Update("vanished", true).Error)
	other := models.ExternalComponent{ExternalSourceID: id, ExternalID: "db", Name: "Service 04", GroupExternalID: "g2", GroupName: "Group B", Status: "operational", LastSeenAt: time.Now()}
	require.NoError(t, f.db.Create(&other).Error)
	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.pageAdminTok, f.comp.ID, vm.ID).Code)

	base := f.sourcesPath(f.page) + "/" + uid(id) + "/components"
	names := func(query string) []string {
		w := f.do(http.MethodGet, base+query, f.viewerTok, nil)
		require.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			Groups     []map[string]any `json:"groups"`
			Components []struct {
				Name               string `json:"name"`
				LinkedComponentIDs []uint `json:"linked_component_ids"`
			} `json:"components"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		out := []string{}
		for _, c := range resp.Components {
			out = append(out, c.Name)
			if c.Name == "Service 03" {
				assert.Equal(t, []uint{f.comp.ID}, c.LinkedComponentIDs)
			} else {
				assert.NotNil(t, c.LinkedComponentIDs, "immer eine Liste, nie null")
			}
		}
		if query == "" {
			require.Len(t, resp.Groups, 1)
			assert.Equal(t, "Group A", resp.Groups[0]["name"])
		}
		return out
	}
	assert.ElementsMatch(t, []string{"Group A", "Service 03", "Service 02", "Service 04"}, names(""), "verschwundene ausgeblendet")
	assert.Contains(t, names("?include_vanished=true"), "Altes Produkt")
	assert.ElementsMatch(t, []string{"Service 03"}, names("?q=SERVICE%2003"))
	assert.ElementsMatch(t, []string{"Service 04"}, names("?q=group%20b"), "Suche auch im Gruppennamen")
	assert.ElementsMatch(t, []string{"Group A", "Service 03", "Service 02"}, names("?group=g1"), "Gruppe samt Kindern")
	assert.ElementsMatch(t, []string{"Service 03"}, names("?status=partial_outage"))
	assert.Empty(t, names("?q=altes"), "Suche respektiert den Verschwunden-Filter")
}

func TestExternalLinks_KoppelnUndEntkoppeln(t *testing.T) {
	f := newExtFixture(t)
	id := f.createSource(f.page, f.pageAdminTok, "https://status.example.com")
	ec := f.extComp(id, "vm", "Service 03", "major_outage")

	w := f.linkVia(f.page, f.pageAdminTok, f.comp.ID, ec.ID)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var link map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &link))
	assert.Equal(t, "API", link["component_name"])
	assert.Equal(t, "Service 03", link["external_component_name"])
	assert.Equal(t, "Example Cloud", link["source_name"])
	assert.Equal(t, "Group A", link["group_name"])
	assert.Equal(t, "major_outage", link["status"])
	require.Len(t, f.listener.changes, 1, "Kopplung ändert den effektiven Status → Ereignis")
	assert.Equal(t, externalstatus.StatusChange{StatusPageID: f.page.ID, ComponentID: f.comp.ID, Previous: "operational",
		Current: "major_outage", Cause: externalstatus.CauseLink, SourceID: id}, f.listener.changes[0])

	// Idempotent: zweites POST → 200, dieselbe ID, keine zweite Zeile, kein Ereignis.
	w = f.linkVia(f.page, f.pageAdminTok, f.comp.ID, ec.ID)
	require.Equal(t, http.StatusOK, w.Code)
	var again map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &again))
	assert.Equal(t, link["id"], again["id"])
	var n int64
	f.db.Model(&models.ExternalComponentLink{}).Count(&n)
	assert.EqualValues(t, 1, n)
	assert.Len(t, f.listener.changes, 1)

	w = f.do(http.MethodGet, f.linksPath(f.page), f.viewerTok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 1)
	assert.Equal(t, link["id"], list[0]["id"])

	assert.Equal(t, http.StatusBadRequest, f.linkVia(f.page, f.pageAdminTok, f.group.ID, ec.ID).Code, "reine Gruppe")
	assert.Equal(t, http.StatusBadRequest, f.do(http.MethodPost, f.linksPath(f.page), f.pageAdminTok, map[string]any{"component_id": f.comp.ID}).Code)
	assert.Equal(t, http.StatusNotFound, f.linkVia(f.page, f.pageAdminTok, 99999, ec.ID).Code)
	assert.Equal(t, http.StatusNotFound, f.linkVia(f.page, f.pageAdminTok, f.comp.ID, 99999).Code)

	linkID := uid(uint(link["id"].(float64)))
	assert.Equal(t, http.StatusBadRequest, f.do(http.MethodDelete, f.linksPath(f.page)+"/abc", f.pageAdminTok, nil).Code)
	w = f.do(http.MethodDelete, f.linksPath(f.page)+"/"+linkID, f.pageAdminTok, nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, f.listener.changes, 2)
	assert.Equal(t, externalstatus.CauseUnlink, f.listener.changes[1].Cause)
	assert.Equal(t, "operational", f.listener.changes[1].Current)
	assert.Equal(t, http.StatusNotFound, f.do(http.MethodDelete, f.linksPath(f.page)+"/"+linkID, f.pageAdminTok, nil).Code)

	// Nach dem Entkoppeln erneut koppelbar (Soft-Delete blockiert nicht).
	assert.Equal(t, http.StatusCreated, f.linkVia(f.page, f.pageAdminTok, f.comp.ID, ec.ID).Code)
	assert.Subset(t, f.auditActions(), []string{"external_link.created", "external_link.deleted"})
}

// Tag-Prüfung wie beim Bearbeiten einer Komponente: Ein Seiten-Admin, dessen
// Tags die Komponente nicht abdecken, darf sie weder koppeln noch entkoppeln.
func TestExternalLinks_TagZugriff(t *testing.T) {
	f := newExtFixture(t)
	id := f.createSource(f.page, f.adminTok, "https://status.example.com")
	ec := f.extComp(id, "vm", "VM", "operational")
	tagDE := models.Tag{Name: "DE", StatusPageID: &f.page.ID}
	tagAT := models.Tag{Name: "AT", StatusPageID: &f.page.ID}
	require.NoError(t, f.db.Create(&tagDE).Error)
	require.NoError(t, f.db.Create(&tagAT).Error)
	require.NoError(t, f.db.Create(&models.ComponentTag{ComponentID: f.comp.ID, TagID: tagDE.ID}).Error)
	var pa models.User
	require.NoError(t, f.db.Where("email = ?", "pa@example.com").First(&pa).Error)
	require.NoError(t, f.db.Create(&models.UserTag{UserID: pa.ID, TagID: tagAT.ID}).Error)

	assert.Equal(t, http.StatusForbidden, f.linkVia(f.page, f.pageAdminTok, f.comp.ID, ec.ID).Code)
	w := f.linkVia(f.page, f.adminTok, f.comp.ID, ec.ID)
	require.Equal(t, http.StatusCreated, w.Code)
	var link struct {
		ID uint `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &link))
	assert.Equal(t, http.StatusForbidden, f.do(http.MethodDelete, f.linksPath(f.page)+"/"+uid(link.ID), f.pageAdminTok, nil).Code)
	assert.Equal(t, http.StatusOK, f.do(http.MethodDelete, f.linksPath(f.page)+"/"+uid(link.ID), f.adminTok, nil).Code)
}

// Eine Quelle zu löschen entfernt die Kopplungen all ihrer Komponenten. Das ist nur
// erlaubt, wenn der Nutzer auf jede davon Tag-Zugriff hat; sonst wäre der Tag-Schutz
// des einzelnen Entkoppelns über das Löschen der Quelle zu umgehen.
func TestDeleteExternalSource_TagZugriff(t *testing.T) {
	f := newExtFixture(t)
	eu := models.Tag{Name: "eu", StatusPageID: &f.page.ID}
	us := models.Tag{Name: "us", StatusPageID: &f.page.ID}
	require.NoError(t, f.db.Create(&eu).Error)
	require.NoError(t, f.db.Create(&us).Error)
	euComp := models.Component{StatusPageID: f.page.ID, Name: "EU-API", Status: "operational"}
	require.NoError(t, f.db.Create(&euComp).Error)
	require.NoError(t, f.db.Create(&models.ComponentTag{ComponentID: euComp.ID, TagID: eu.ID}).Error)
	require.NoError(t, f.db.Create(&models.ComponentTag{ComponentID: f.comp.ID, TagID: us.ID}).Error)
	var pa models.User
	require.NoError(t, f.db.Where("email = ?", "pa@example.com").First(&pa).Error)
	require.NoError(t, f.db.Create(&models.UserTag{UserID: pa.ID, TagID: eu.ID}).Error)

	linkCount := func(srcID uint) (n int64) {
		f.db.Model(&models.ExternalComponentLink{}).
			Joins("JOIN external_components ON external_components.id = external_component_links.external_component_id").
			Where("external_components.external_source_id = ?", srcID).Count(&n)
		return n
	}
	mixed := f.createSource(f.page, f.adminTok, "https://status.example.com")
	mixedEu := f.extComp(mixed, "a", "A", "operational")
	mixedUs := f.extComp(mixed, "b", "B", "operational")
	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.adminTok, euComp.ID, mixedEu.ID).Code)
	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.adminTok, f.comp.ID, mixedUs.ID).Code)
	euOnly := f.createSource(f.page, f.adminTok, "https://status.example.org")
	euOnlyComp := f.extComp(euOnly, "c", "C", "operational")
	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.adminTok, euComp.ID, euOnlyComp.ID).Code)
	unlinked := f.createSource(f.page, f.adminTok, "https://unlinked.example.org")

	t.Run("Quelle mit fremd getaggter Komponente: 403, nichts gelöscht", func(t *testing.T) {
		w := f.do(http.MethodDelete, f.sourcesPath(f.page)+"/"+uid(mixed), f.pageAdminTok, nil)
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"code":"forbidden"`)
		var n int64
		f.db.Model(&models.ExternalSource{}).Where("id = ?", mixed).Count(&n)
		assert.EqualValues(t, 1, n, "Quelle bleibt")
		assert.EqualValues(t, 2, linkCount(mixed), "alle Kopplungen bleiben")
		assert.NotContains(t, f.auditActions(), "external_source.deleted")
	})
	t.Run("Gegenprobe: nur eigene Tags und unverknüpfte Quelle: 200", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, f.do(http.MethodDelete, f.sourcesPath(f.page)+"/"+uid(euOnly), f.pageAdminTok, nil).Code)
		assert.EqualValues(t, 0, linkCount(euOnly))
		assert.Equal(t, http.StatusOK, f.do(http.MethodDelete, f.sourcesPath(f.page)+"/"+uid(unlinked), f.pageAdminTok, nil).Code)
	})
	t.Run("Global Admin darf die gemischte Quelle löschen", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, f.do(http.MethodDelete, f.sourcesPath(f.page)+"/"+uid(mixed), f.adminTok, nil).Code)
		assert.EqualValues(t, 0, linkCount(mixed))
	})
}

// Schlägt die Abfrage der verknüpften Komponenten fehl, wird nichts gelöscht (fail closed).
func TestDeleteExternalSource_TagPruefungDBFehler(t *testing.T) {
	f := newExtFixture(t)
	id := f.createSource(f.page, f.adminTok, "https://status.example.com")
	ec := f.extComp(id, "a", "A", "operational")
	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.adminTok, f.comp.ID, ec.ID).Code)
	extFailOn(t, f.db, "query", "external_component_links")
	w := f.do(http.MethodDelete, f.sourcesPath(f.page)+"/"+uid(id), f.adminTok, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	var n int64
	f.db.Model(&models.ExternalSource{}).Where("id = ?", id).Count(&n)
	assert.EqualValues(t, 1, n)
}

type treeNode struct {
	ID              uint
	Name            string
	Status          string
	EffectiveStatus string                     `json:"effective_status"`
	ExternalSources []models.ExternalStatusRef `json:"external_sources"`
	Children        []treeNode
}

func findNode(nodes []treeNode, name string) *treeNode {
	for i := range nodes {
		if nodes[i].Name == name {
			return &nodes[i]
		}
		if n := findNode(nodes[i].Children, name); n != nil {
			return n
		}
	}
	return nil
}

func TestEffectiveStatus_InKomponentenbaumUndDashboard(t *testing.T) {
	f := newExtFixture(t)
	id := f.createSource(f.page, f.adminTok, "https://status.example.com")
	outage := f.extComp(id, "vm", "Service 03", "major_outage")
	maint := f.extComp(id, "db", "Service 04", "under_maintenance")
	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.adminTok, f.comp.ID, outage.ID).Code)
	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.adminTok, f.child.ID, maint.ID).Code)

	w := f.do(http.MethodGet, "/api/pages/"+pid(f.page)+"/components", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var tree []treeNode
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tree))
	api := findNode(tree, "API")
	require.NotNil(t, api)
	assert.Equal(t, "operational", api.Status, "gespeicherter Status bleibt")
	assert.Equal(t, "major_outage", api.EffectiveStatus)
	require.Len(t, api.ExternalSources, 1)
	assert.Equal(t, models.ExternalStatusRef{ExternalComponentID: outage.ID, SourceID: id, SourceName: "Example Cloud", Name: "Service 03",
		GroupName: "Group A", Status: "major_outage", URL: "https://status.example.com"}, api.ExternalSources[0])
	grp := findNode(tree, "Gruppe")
	require.NotNil(t, grp)
	assert.Equal(t, "operational", grp.Status)
	assert.Equal(t, "under_maintenance", grp.EffectiveStatus, "Gruppe aggregiert den effektiven Status der Kinder")
	assert.Equal(t, "under_maintenance", findNode(tree, "Kind").EffectiveStatus)
	assert.NotContains(t, w.Body.String(), `"external_sources":null`)

	// Dashboard: Status behält seine Bedeutung (gespeichert + offene Vorfälle).
	inc := models.Incident{StatusPageID: f.page.ID, Title: "t", Message: "m", Status: "investigating"}
	require.NoError(t, f.db.Create(&inc).Error)
	require.NoError(t, f.db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: f.child.ID}).Error)
	w = f.do(http.MethodGet, "/api/public/pages/eigene/dashboard", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var dash struct {
		Components []treeNode `json:"components"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &dash))
	assert.Equal(t, "operational", findNode(dash.Components, "API").Status)
	assert.Equal(t, "major_outage", findNode(dash.Components, "API").EffectiveStatus)
	kind := findNode(dash.Components, "Kind")
	assert.Equal(t, "degraded_performance", kind.Status, "Vorfall-Überlagerung wie bisher")
	assert.Equal(t, "degraded_performance", kind.EffectiveStatus, "Störung schlägt Wartung")

	// Inaktive Quelle zählt nicht mehr.
	require.Equal(t, http.StatusOK, f.do(http.MethodPut, f.sourcesPath(f.page)+"/"+uid(id), f.adminTok, map[string]any{"active": false}).Code)
	w = f.do(http.MethodGet, "/api/pages/"+pid(f.page)+"/components", "", nil)
	var after []treeNode
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &after))
	assert.Equal(t, "operational", findNode(after, "API").EffectiveStatus)
	assert.Empty(t, findNode(after, "API").ExternalSources)
	causes := []string{}
	for _, ch := range f.listener.changes {
		causes = append(causes, ch.Cause)
	}
	assert.Contains(t, causes, externalstatus.CauseSource, "Deaktivieren ist ein Ereignis")
}

func TestEffectiveStatus_Uebersicht(t *testing.T) {
	f := newExtFixture(t)
	id := f.createSource(f.page, f.adminTok, "https://status.example.com")
	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.adminTok, f.comp.ID, f.extComp(id, "vm", "VM", "partial_outage").ID).Code)
	w := f.do(http.MethodGet, "/api/admin/overview", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Pages []struct {
			ID         uint   `json:"id"`
			Status     string `json:"status"`
			Components int64  `json:"components"`
		} `json:"pages"`
		WorstStatus string `json:"worst_status"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "partial_outage", resp.WorstStatus)
	for _, p := range resp.Pages {
		if p.ID == f.page.ID {
			assert.Equal(t, "partial_outage", p.Status)
			assert.EqualValues(t, 2, p.Components)
		} else {
			assert.Equal(t, "operational", p.Status, "andere Seite unberührt")
		}
	}
}

func TestDeleteComponent_EntferntKopplungen(t *testing.T) {
	f := newExtFixture(t)
	id := f.createSource(f.page, f.adminTok, "https://status.example.com")
	require.Equal(t, http.StatusCreated, f.linkVia(f.page, f.adminTok, f.comp.ID, f.extComp(id, "vm", "VM", "operational").ID).Code)
	w := f.do(http.MethodDelete, "/api/pages/"+pid(f.page)+"/components/"+uid(f.comp.ID), "", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var n int64
	f.db.Model(&models.ExternalComponentLink{}).Where("component_id = ?", f.comp.ID).Count(&n)
	assert.Zero(t, n)
}

func TestExternalStatus_DBFehler(t *testing.T) {
	cases := []struct {
		name, op, table, method string
		path                    func(f *extFixture, srcID, linkID uint) string
		body                    any
	}{
		{"Quellen listen", "query", "external_sources", http.MethodGet, func(f *extFixture, _, _ uint) string { return f.sourcesPath(f.page) }, nil},
		{"Quelle anlegen", "create", "external_sources", http.MethodPost, func(f *extFixture, _, _ uint) string { return f.sourcesPath(f.page) },
			map[string]any{"name": "n", "base_url": "https://neu.example"}},
		{"Quelle ändern", "update", "external_sources", http.MethodPut, func(f *extFixture, s, _ uint) string { return f.sourcesPath(f.page) + "/" + uid(s) },
			map[string]any{"name": "neu"}},
		{"Quelle löschen: Kopplungen", "delete", "external_component_links", http.MethodDelete, func(f *extFixture, s, _ uint) string { return f.sourcesPath(f.page) + "/" + uid(s) }, nil},
		{"Quelle löschen: Komponenten", "delete", "external_components", http.MethodDelete, func(f *extFixture, s, _ uint) string { return f.sourcesPath(f.page) + "/" + uid(s) }, nil},
		{"Quelle löschen: Quelle", "delete", "external_sources", http.MethodDelete, func(f *extFixture, s, _ uint) string { return f.sourcesPath(f.page) + "/" + uid(s) }, nil},
		{"Komponenten listen", "query", "external_components", http.MethodGet, func(f *extFixture, s, _ uint) string { return f.sourcesPath(f.page) + "/" + uid(s) + "/components" }, nil},
		{"Koppeln: Antwort", "row", "l", http.MethodPost, func(f *extFixture, _, _ uint) string { return f.linksPath(f.page) }, "LINK"},
		{"Ausfälle listen", "query", "external_sources", http.MethodGet, func(f *extFixture, _, _ uint) string {
			return "/api/pages/" + pid(f.page) + "/external-outages"
		}, nil},
		{"Kopplungen listen", "row", "l", http.MethodGet, func(f *extFixture, _, _ uint) string { return f.linksPath(f.page) }, nil},
		{"Koppeln", "create", "external_component_links", http.MethodPost, func(f *extFixture, _, _ uint) string { return f.linksPath(f.page) }, "LINK"},
		{"Koppeln: Bestandsprüfung", "query", "external_component_links", http.MethodPost, func(f *extFixture, _, _ uint) string { return f.linksPath(f.page) }, "LINK"},
		{"Entkoppeln", "delete", "external_component_links", http.MethodDelete, func(f *extFixture, _, l uint) string { return f.linksPath(f.page) + "/" + uid(l) }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newExtFixture(t)
			srcID := f.createSource(f.page, f.adminTok, "https://status.example.com")
			vm := f.extComp(srcID, "vm", "VM", "operational")
			db := f.extComp(srcID, "db", "DB", "operational")
			w := f.linkVia(f.page, f.adminTok, f.comp.ID, vm.ID)
			require.Equal(t, http.StatusCreated, w.Code)
			var link struct {
				ID uint `json:"id"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &link))
			body := tc.body
			if body == "LINK" {
				body = map[string]any{"component_id": f.child.ID, "external_component_id": db.ID}
			}
			extFailOn(t, f.db, tc.op, tc.table)
			w = f.do(tc.method, tc.path(f, srcID, link.ID), f.adminTok, body)
			assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
		})
	}
}

// Fällt die Berechnung des effektiven Status aus, laufen Aktion und Antwort
// trotzdem: kein Ereignis, Komponentenbaum mit eigenem Status.
func TestExternalStatus_EffektiverStatusNichtBerechenbar(t *testing.T) {
	f := newExtFixture(t)
	srcID := f.createSource(f.page, f.adminTok, "https://status.example.com")
	vm := f.extComp(srcID, "vm", "VM", "major_outage")
	w := f.linkVia(f.page, f.adminTok, f.comp.ID, vm.ID)
	require.Equal(t, http.StatusCreated, w.Code)
	var link struct {
		ID uint `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &link))
	require.Len(t, f.listener.changes, 1)

	g := newExtFixture(t)
	require.NoError(t, g.db.Migrator().DropTable(&models.ExternalComponentLink{}))
	w = g.do(http.MethodGet, "/api/pages/"+pid(g.page)+"/components", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var tree []treeNode
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tree))
	assert.Equal(t, "operational", findNode(tree, "API").EffectiveStatus)

	extFailOn(t, f.db, "query", "components")
	w = f.do(http.MethodDelete, f.linksPath(f.page)+"/"+uid(link.ID), f.adminTok, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Len(t, f.listener.changes, 1, "ohne Vorher-Stand kein Ereignis")
}

// Global Admins überspringen in RequirePermission das page_id-Parsing; die
// Handler lehnen eine ungültige page_id deshalb selbst mit 400 ab.
func TestExternalStatus_UngueltigePageID(t *testing.T) {
	f := newExtFixture(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/pages/abc/external-sources"},
		{http.MethodPost, "/api/pages/abc/external-sources"},
		{http.MethodGet, "/api/pages/abc/external-sources/1"},
		{http.MethodGet, "/api/pages/abc/external-links"},
		{http.MethodGet, "/api/pages/abc/external-outages"},
		{http.MethodPost, "/api/pages/abc/external-links"},
		{http.MethodDelete, "/api/pages/abc/external-links/1"},
	} {
		w := f.do(tc.method, tc.path, f.adminTok, map[string]any{"name": "x", "base_url": "https://example.com", "component_id": 1, "external_component_id": 1})
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s %s", tc.method, tc.path)
	}
}
