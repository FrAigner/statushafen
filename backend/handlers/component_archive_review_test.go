package handlers_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Review-Nachbesserung.

func doTemplateRequestAs(r http.Handler, userID uint, method, path string, payload any) *httptest.ResponseRecorder {
	var buf *bytes.Buffer
	if payload != nil {
		b, _ := json.Marshal(payload)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req, _ := http.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", strconv.Itoa(int(userID)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func (e *visEnv) archiveRaw(ids ...uint) {
	e.t.Helper()
	require.NoError(e.t, e.db.Model(&models.Component{}).Where("id IN ?", ids).Update("archived_at", time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)).Error)
}

func (e *visEnv) doAs(userID uint, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	e.t.Helper()
	w := doTemplateRequestAs(e.r, userID, method, path, body)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w, m
}

func (e *visEnv) globalAdmin(email string) models.User {
	u := models.User{Email: email, IsGlobalAdmin: true}
	require.NoError(e.t, e.db.Create(&u).Error)
	return u
}

// 1) Die Kaskade prüft die Tag-Rechte für JEDE betroffene Komponente.
func TestComponentArchiveReview_KaskadePrueftTagRechteJederKomponente(t *testing.T) {
	e := newVisEnv(t)
	group := e.comp("Gruppe", nil, true, "group")
	child := e.comp("US-Kind", &group.ID, true, "operational")
	tagUS := models.Tag{Name: "US", StatusPageID: &e.page.ID}
	tagDE := models.Tag{Name: "DE", StatusPageID: &e.page.ID}
	require.NoError(t, e.db.Create(&tagUS).Error)
	require.NoError(t, e.db.Create(&tagDE).Error)
	require.NoError(t, e.db.Create(&models.ComponentTag{ComponentID: child.ID, TagID: tagUS.ID}).Error)
	user := models.User{Email: "kaskade-de@example.com"}
	require.NoError(t, e.db.Create(&user).Error)
	require.NoError(t, e.db.Create(&models.UserTag{UserID: user.ID, TagID: tagDE.ID}).Error)

	// Archivieren: Wurzel ist ohne Tag erlaubt, das US-Kind nicht -> 403, nichts ändert sich.
	w, _ := e.doAs(user.ID, "POST", e.compURL(group, "/archive"), nil)
	requireStatusCode(t, w, http.StatusForbidden)
	assert.Nil(t, e.reload(group).ArchivedAt)
	assert.Nil(t, e.reload(child).ArchivedAt)
	assert.Zero(t, e.auditCount("component.archived"))

	// Wiederherstellen: gleiche Prüfung über die Kaskade.
	e.archiveRaw(group.ID, child.ID)
	w, _ = e.doAs(user.ID, "POST", e.compURL(group, "/restore"), nil)
	requireStatusCode(t, w, http.StatusForbidden)
	assert.NotNil(t, e.reload(group).ArchivedAt)
	assert.NotNil(t, e.reload(child).ArchivedAt)
	assert.Zero(t, e.auditCount("component.restored"))
}

// 2) Anonyme Vorfall-Routen nennen archivierte und unsichtbare Komponenten nicht.
func TestComponentArchiveReview_AnonymeVorfallRoutenOhneVersteckteKomponenten(t *testing.T) {
	e := newVisEnv(t)
	archived := e.comp("Archiviert-Geheim", nil, true, "operational")
	hidden := e.comp("Unsichtbar-Geheim", nil, false, "operational")
	shown := e.comp("Sichtbar-Offen", nil, true, "operational")
	e.archiveRaw(archived.ID)
	inc := models.Incident{StatusPageID: e.page.ID, Title: "Vorfall", Message: "m", Status: "investigating"}
	require.NoError(t, e.db.Create(&inc).Error)
	for _, c := range []models.Component{archived, hidden, shown} {
		require.NoError(t, e.db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: c.ID}).Error)
	}
	pageBase := "/api/pages/" + strconv.Itoa(int(e.page.ID)) + "/incidents"

	for _, path := range []string{pageBase, pageBase + "/" + strconv.Itoa(int(inc.ID))} {
		w := e.get(path)
		requireStatusCode(t, w, http.StatusOK)
		assert.NotContains(t, w.Body.String(), "Archiviert-Geheim", path)
		assert.NotContains(t, w.Body.String(), "Unsichtbar-Geheim", path)
		assert.Contains(t, w.Body.String(), "Sichtbar-Offen", path)
	}

	// Verwalter sehen alle Komponenten des Vorfalls.
	admin := e.globalAdmin("inc-admin@example.com")
	for _, path := range []string{pageBase, pageBase + "/" + strconv.Itoa(int(inc.ID))} {
		w, _ := e.doAs(admin.ID, "GET", path, nil)
		requireStatusCode(t, w, http.StatusOK)
		assert.Contains(t, w.Body.String(), "Archiviert-Geheim", path)
		assert.Contains(t, w.Body.String(), "Unsichtbar-Geheim", path)
	}
}

// 3) Übersetzungen archivierter/unsichtbarer Komponenten sind anonym nicht abrufbar.
func TestComponentArchiveReview_UebersetzungenNurFuerBerechtigte(t *testing.T) {
	e := newVisEnv(t)
	archived := e.comp("Arch", nil, true, "operational")
	hidden := e.comp("Hid", nil, false, "operational")
	shown := e.comp("Shown", nil, true, "operational")
	e.archiveRaw(archived.ID)
	for _, c := range []models.Component{archived, hidden, shown} {
		require.NoError(t, e.db.Create(&models.ComponentTranslation{ComponentID: c.ID, LanguageCode: "en", Title: c.Name + " EN", Description: "d", Status: "confirmed", Source: "manual"}).Error)
	}
	path := func(c models.Component) string { return "/api/components/" + strconv.Itoa(int(c.ID)) + "/translations" }

	requireStatusCode(t, e.get(path(archived)), http.StatusNotFound)
	requireStatusCode(t, e.get(path(hidden)), http.StatusNotFound)
	w := e.get(path(shown))
	requireStatusCode(t, w, http.StatusOK)
	assert.Contains(t, w.Body.String(), "Shown EN")

	admin := e.globalAdmin("tr-admin@example.com")
	w, _ = e.doAs(admin.ID, "GET", path(archived), nil)
	requireStatusCode(t, w, http.StatusOK)
	assert.Contains(t, w.Body.String(), "Arch EN")
}

// 4) Statusschreiben überschreibt eine zwischenzeitliche Archivierung nicht.
func TestComponentArchiveReview_StatusSchreibenBehaeltArchivierung(t *testing.T) {
	cases := map[string]func(e *visEnv, c models.Component) *httptest.ResponseRecorder{
		"PUT Status": func(e *visEnv, c models.Component) *httptest.ResponseRecorder {
			w, _ := e.do("PUT", e.compURL(c, ""), map[string]any{"status": "major_outage"})
			return w
		},
		"Webhook": func(e *visEnv, c models.Component) *httptest.ResponseRecorder {
			return doWebhookStatusPost(e.r, strconv.Itoa(int(c.ID)), "major_outage", "tok")
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			e := newVisEnv(t)
			comp := e.comp("Rennen", nil, true, "operational")
			armed := true
			require.NoError(t, e.db.Callback().Query().After("gorm:query").Register("test:archive-race", func(d *gorm.DB) {
				if armed && d.Statement.Table == "components" && d.Error == nil {
					armed = false
					require.NoError(t, d.Session(&gorm.Session{NewDB: true}).Exec("UPDATE components SET archived_at = ? WHERE id = ?", time.Now(), comp.ID).Error)
				}
			}))
			w := call(e, comp)
			requireStatusCode(t, w, http.StatusOK)
			got := e.reload(comp)
			assert.Equal(t, "major_outage", got.Status)
			assert.NotNil(t, got.ArchivedAt, "zwischenzeitliche Archivierung darf nicht überschrieben werden")
		})
	}

	t.Run("Gruppe umbenennen", func(t *testing.T) {
		e := newVisEnv(t)
		group := e.comp("Gruppe", nil, true, "group")
		armed := true
		require.NoError(t, e.db.Callback().Query().After("gorm:query").Register("test:archive-race", func(d *gorm.DB) {
			if armed && d.Statement.Table == "components" && d.Error == nil {
				armed = false
				require.NoError(t, d.Session(&gorm.Session{NewDB: true}).Exec("UPDATE components SET archived_at = ? WHERE id = ?", time.Now(), group.ID).Error)
			}
		}))
		w, _ := e.do("PUT", e.compURL(group, ""), map[string]any{"name": "Neu"})
		requireStatusCode(t, w, http.StatusOK)
		got := e.reload(group)
		assert.Equal(t, "Neu", got.Name)
		assert.NotNil(t, got.ArchivedAt)
	})
}

// 5) Update-API umgeht die Archiv-Regeln nicht.
func TestComponentArchiveReview_UpdateUmgehtArchivRegelnNicht(t *testing.T) {
	e := newVisEnv(t)
	oldGroup := e.comp("Alte Gruppe", nil, true, "group")
	member := e.comp("Mitglied", &oldGroup.ID, true, "operational")
	e.archiveRaw(oldGroup.ID, member.ID)
	active := e.comp("Aktiv", nil, true, "operational")

	// Aktive Komponente in eine archivierte Gruppe verschieben: abgelehnt.
	w, m := e.do("PUT", e.compURL(active, ""), map[string]any{"status": "operational", "parent_id": oldGroup.ID})
	requireStatusCode(t, w, http.StatusBadRequest)
	assert.Equal(t, "parent_archived", m["code"])
	assert.Nil(t, e.reload(active).ParentID)

	// Archiviertes Mitglied herauslösen (parent_id 0) oder verschieben: abgelehnt (409).
	other := e.comp("Andere Gruppe", nil, true, "group")
	for _, pid := range []uint{0, other.ID} {
		w, m = e.do("PUT", e.compURL(member, ""), map[string]any{"status": "operational", "parent_id": pid})
		requireStatusCode(t, w, http.StatusConflict)
		assert.Equal(t, "component_archived", m["code"])
		require.NotNil(t, e.reload(member).ParentID)
		assert.Equal(t, oldGroup.ID, *e.reload(member).ParentID)
	}

	// Unverändertes parent_id (das Formular schickt es mit) und Umbenennen bleiben möglich.
	w, _ = e.do("PUT", e.compURL(member, ""), map[string]any{"status": "operational", "name": "Neu", "parent_id": oldGroup.ID})
	requireStatusCode(t, w, http.StatusOK)
	w, _ = e.do("PUT", e.compURL(oldGroup, ""), map[string]any{"name": "Gruppe neu", "parent_id": 0})
	requireStatusCode(t, w, http.StatusOK)
}

// 5b) Restore prüft die ganze Vorfahrenkette.
func TestComponentArchiveReview_RestorePrueftVorfahrenkette(t *testing.T) {
	e := newVisEnv(t)
	top := e.comp("Oben", nil, true, "group")
	mid := e.comp("Mitte", &top.ID, true, "group")
	leaf := e.comp("Blatt", &mid.ID, true, "operational")
	e.archiveRaw(top.ID, leaf.ID) // Mitte ist (inkonsistent) aktiv, Oben archiviert

	w, m := e.do("POST", e.compURL(leaf, "/restore"), nil)
	requireStatusCode(t, w, http.StatusConflict)
	assert.Equal(t, "parent_archived", m["code"])
	assert.NotNil(t, e.reload(leaf).ArchivedAt)
}

// 6a) Archivierte Kinder zählen nicht in den Gruppenstatus der aktiven Ansicht.
func TestComponentArchiveReview_GruppenstatusOhneArchivierteKinder(t *testing.T) {
	e := newVisEnv(t)
	group := e.comp("Gruppe", nil, true, "group")
	e.comp("Gesund", &group.ID, true, "operational")
	bad := e.comp("Alt-Defekt", &group.ID, true, "major_outage")
	e.archiveRaw(bad.ID)
	admin := e.globalAdmin("gs-admin@example.com")

	w, _ := e.doAs(admin.ID, "GET", e.base, nil)
	requireStatusCode(t, w, http.StatusOK)
	var tree []models.Component
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tree))
	require.Len(t, tree, 1)
	assert.Equal(t, "operational", tree[0].Status)
	assert.Len(t, tree[0].Children, 2, "archivierte Kinder bleiben für das Segment Archiviert in der Liste")

	// Ist die Gruppe selbst archiviert, bleibt der Aggregatstatus über alle Kinder.
	e.archiveRaw(group.ID)
	w, _ = e.doAs(admin.ID, "GET", e.base, nil)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tree))
	assert.Equal(t, "major_outage", tree[0].Status)
}

// 6b) Admin-Übersicht und effektive Status zählen archivierte Komponenten nicht.
func TestComponentArchiveReview_UebersichtOhneArchivierte(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	g := r.Group("/api")
	g.Use(middleware.AuthMiddleware())
	g.GET("/admin/overview", middleware.RequireGlobalAdmin(), h.GetOverviewHandler)
	admin := models.User{Email: "ov578@example.com", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&admin).Error)
	tok, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)
	p := models.StatusPage{Name: "P", Slug: "p578"}
	require.NoError(t, db.Create(&p).Error)
	require.NoError(t, db.Create(&models.Component{StatusPageID: p.ID, Name: "Ok", Status: "operational"}).Error)
	old := models.Component{StatusPageID: p.ID, Name: "Alt", Status: "major_outage"}
	require.NoError(t, db.Create(&old).Error)
	require.NoError(t, db.Model(&old).Update("archived_at", time.Now()).Error)

	req, _ := http.NewRequest("GET", "/api/admin/overview", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"worst_status":"operational"`)
	assert.Contains(t, w.Body.String(), `"components":1`)
}

// failNthComponentQuery lässt die n-te Abfrage auf components fehlschlagen.
func failNthComponentQuery(t *testing.T, db *gorm.DB, n int) {
	t.Helper()
	count := 0
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail-nth-comp", func(d *gorm.DB) {
		if d.Statement.Table == "components" {
			count++
			if count == n {
				_ = d.AddError(errors.New("boom"))
			}
		}
	}))
}

func TestComponentArchiveReview_LesefehlerGeben500(t *testing.T) {
	t.Run("Archivieren", func(t *testing.T) {
		e := newVisEnv(t)
		comp := e.comp("A", nil, true, "operational")
		failNthComponentQuery(t, e.db, 2)
		w, _ := e.do("POST", e.compURL(comp, "/archive"), nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
		assert.Nil(t, e.reload(comp).ArchivedAt)
	})
	t.Run("Wiederherstellen", func(t *testing.T) {
		e := newVisEnv(t)
		comp := e.comp("A", nil, true, "operational")
		e.archiveRaw(comp.ID)
		failNthComponentQuery(t, e.db, 2)
		w, _ := e.do("POST", e.compURL(comp, "/restore"), nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
		assert.NotNil(t, e.reload(comp).ArchivedAt)
	})
	t.Run("Übersetzungen", func(t *testing.T) {
		e := newVisEnv(t)
		comp := e.comp("A", nil, true, "operational")
		failNthComponentQuery(t, e.db, 2)
		w := e.get("/api/components/" + strconv.Itoa(int(comp.ID)) + "/translations")
		requireStatusCode(t, w, http.StatusInternalServerError)
	})
	t.Run("Vorfälle: fail-closed ohne Komponenten", func(t *testing.T) {
		e := newVisEnv(t)
		comp := e.comp("Geheim-Fail", nil, true, "operational")
		inc := models.Incident{StatusPageID: e.page.ID, Title: "V", Message: "m", Status: "investigating"}
		require.NoError(t, e.db.Create(&inc).Error)
		require.NoError(t, e.db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID}).Error)
		// Die Vorfall-Abfrage lädt Komponenten per Preload (1.), die Sichtbarkeitsprüfung danach (2.).
		failNthComponentQuery(t, e.db, 2)
		w := e.get("/api/pages/" + strconv.Itoa(int(e.page.ID)) + "/incidents")
		requireStatusCode(t, w, http.StatusOK)
		assert.NotContains(t, w.Body.String(), "Geheim-Fail")
	})
}

// Ein Vorfahr, der nicht (mehr) existiert, blockiert das Wiederherstellen nicht.
func TestComponentArchiveReview_RestoreMitFehlendemElternknoten(t *testing.T) {
	e := newVisEnv(t)
	gone := uint(99999)
	comp := e.comp("Waise", &gone, true, "operational")
	e.archiveRaw(comp.ID)
	w, _ := e.do("POST", e.compURL(comp, "/restore"), nil)
	requireStatusCode(t, w, http.StatusOK)
	assert.Nil(t, e.reload(comp).ArchivedAt)
}
