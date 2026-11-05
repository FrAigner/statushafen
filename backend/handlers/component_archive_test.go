package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Komponenten archivieren und wiederherstellen. Nutzt die
// Testumgebung der Sichtbarkeit, weil archivierte Komponenten
// öffentlich wie unsichtbare behandelt werden.

func (e *visEnv) compURL(c models.Component, suffix string) string {
	return e.base + "/" + strconv.Itoa(int(c.ID)) + suffix
}

func (e *visEnv) reload(c models.Component) models.Component {
	e.t.Helper()
	var out models.Component
	require.NoError(e.t, e.db.First(&out, c.ID).Error)
	return out
}

func (e *visEnv) auditCount(action string) int64 {
	var n int64
	e.db.Model(&models.AuditLog{}).Where("action = ?", action).Count(&n)
	return n
}

func TestComponentArchive_ArchiviertUndStelltWiederHer(t *testing.T) {
	e := newVisEnv(t)
	comp := e.comp("Alt", nil, true, "operational")

	w, m := e.do("POST", e.compURL(comp, "/archive"), nil)
	requireStatusCode(t, w, http.StatusOK)
	assert.NotNil(t, m["ArchivedAt"])
	require.NotNil(t, e.reload(comp).ArchivedAt)
	assert.EqualValues(t, 1, e.auditCount("component.archived"))

	// idempotent: zweites Archivieren behält den ersten Zeitpunkt, kein zweiter Audit-Eintrag
	first := *e.reload(comp).ArchivedAt
	w, _ = e.do("POST", e.compURL(comp, "/archive"), nil)
	requireStatusCode(t, w, http.StatusOK)
	assert.True(t, first.Equal(*e.reload(comp).ArchivedAt))
	assert.EqualValues(t, 1, e.auditCount("component.archived"))

	w, m = e.do("POST", e.compURL(comp, "/restore"), nil)
	requireStatusCode(t, w, http.StatusOK)
	assert.Nil(t, m["ArchivedAt"])
	assert.Nil(t, e.reload(comp).ArchivedAt)
	assert.EqualValues(t, 1, e.auditCount("component.restored"))

	// Wiederherstellen einer aktiven Komponente ist ebenfalls folgenlos.
	w, _ = e.do("POST", e.compURL(comp, "/restore"), nil)
	requireStatusCode(t, w, http.StatusOK)
	assert.EqualValues(t, 1, e.auditCount("component.restored"))
}

func TestComponentArchive_FehlerpfadeUndMandantentrennung(t *testing.T) {
	e := newVisEnv(t)
	comp := e.comp("A", nil, true, "operational")

	for _, action := range []string{"archive", "restore"} {
		w, _ := e.do("POST", e.base+"/abc/"+action, nil)
		requireStatusCode(t, w, http.StatusBadRequest)
		w, _ = e.do("POST", e.base+"/99999/"+action, nil)
		requireStatusCode(t, w, http.StatusNotFound)
		w, _ = e.do("POST", "/api/pages/xyz/components/1/"+action, nil)
		requireStatusCode(t, w, http.StatusBadRequest)
	}

	other := models.StatusPage{Name: "Fremd", Slug: "fremd-578"}
	require.NoError(t, e.db.Create(&other).Error)
	foreign := "/api/pages/" + strconv.Itoa(int(other.ID)) + "/components/" + strconv.Itoa(int(comp.ID))
	w, _ := e.do("POST", foreign+"/archive", nil)
	requireStatusCode(t, w, http.StatusNotFound)
	assert.Nil(t, e.reload(comp).ArchivedAt, "fremde Seite darf nichts archivieren")

	require.NoError(t, e.db.Model(&comp).Update("archived_at", time.Now()).Error)
	w, _ = e.do("POST", foreign+"/restore", nil)
	requireStatusCode(t, w, http.StatusNotFound)
	assert.NotNil(t, e.reload(comp).ArchivedAt, "fremde Seite darf nichts wiederherstellen")
}

func TestComponentArchive_GruppeNimmtMitgliederMit(t *testing.T) {
	e := newVisEnv(t)
	group := e.comp("Gruppe", nil, true, "group")
	child := e.comp("Kind", &group.ID, true, "operational")
	grandchild := e.comp("Enkel", &child.ID, true, "operational")
	// Bereits vorher einzeln archiviert: behält seinen eigenen Zeitpunkt.
	early := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	own := e.comp("Schon weg", &group.ID, true, "operational")
	require.NoError(t, e.db.Model(&own).Update("archived_at", early).Error)
	other := e.comp("Nebenan", nil, true, "operational")

	w, _ := e.do("POST", e.compURL(group, "/archive"), nil)
	requireStatusCode(t, w, http.StatusOK)
	for _, c := range []models.Component{group, child, grandchild} {
		assert.NotNil(t, e.reload(c).ArchivedAt, c.Name)
	}
	assert.True(t, early.Equal(*e.reload(own).ArchivedAt), "früherer Zeitpunkt bleibt")
	assert.Nil(t, e.reload(other).ArchivedAt)

	// Kind einzeln wiederherstellen geht nicht, solange die Gruppe archiviert ist.
	w, m := e.do("POST", e.compURL(child, "/restore"), nil)
	requireStatusCode(t, w, http.StatusConflict)
	assert.Equal(t, "parent_archived", m["code"])
	assert.NotNil(t, e.reload(child).ArchivedAt)

	// Gruppe wiederherstellen holt nur die mit archivierten Mitglieder zurück.
	w, _ = e.do("POST", e.compURL(group, "/restore"), nil)
	requireStatusCode(t, w, http.StatusOK)
	for _, c := range []models.Component{group, child, grandchild} {
		assert.Nil(t, e.reload(c).ArchivedAt, c.Name)
	}
	assert.NotNil(t, e.reload(own).ArchivedAt, "einzeln archiviertes Mitglied bleibt archiviert")

	// Ein Mitglied lässt sich einzeln archivieren und (bei aktiver Gruppe) zurückholen.
	w, _ = e.do("POST", e.compURL(child, "/archive"), nil)
	requireStatusCode(t, w, http.StatusOK)
	assert.Nil(t, e.reload(group).ArchivedAt)
	assert.NotNil(t, e.reload(grandchild).ArchivedAt, "Enkel folgt dem Kind")
	w, _ = e.do("POST", e.compURL(child, "/restore"), nil)
	requireStatusCode(t, w, http.StatusOK)
	assert.Nil(t, e.reload(child).ArchivedAt)
	assert.Nil(t, e.reload(grandchild).ArchivedAt)
}

func TestComponentArchive_TagZugriff(t *testing.T) {
	e := newVisEnv(t)
	comp := e.comp("Getaggt", nil, true, "operational")
	tag := models.Tag{Name: "US", StatusPageID: &e.page.ID}
	own := models.Tag{Name: "DE", StatusPageID: &e.page.ID}
	require.NoError(t, e.db.Create(&tag).Error)
	require.NoError(t, e.db.Create(&own).Error)
	require.NoError(t, e.db.Create(&models.ComponentTag{ComponentID: comp.ID, TagID: tag.ID}).Error)
	user := models.User{Email: "archiv-de@example.com"}
	require.NoError(t, e.db.Create(&user).Error)
	require.NoError(t, e.db.Create(&models.UserTag{UserID: user.ID, TagID: own.ID}).Error)
	asUser := func(path string) *httptest.ResponseRecorder {
		req, _ := http.NewRequest("POST", path, nil)
		req.Header.Set("X-Test-User", strconv.Itoa(int(user.ID)))
		w := httptest.NewRecorder()
		e.r.ServeHTTP(w, req)
		return w
	}

	requireStatusCode(t, asUser(e.compURL(comp, "/archive")), http.StatusForbidden)
	assert.Nil(t, e.reload(comp).ArchivedAt)
	assert.Zero(t, e.auditCount("component.archived"))

	require.NoError(t, e.db.Model(&comp).Update("archived_at", time.Now()).Error)
	requireStatusCode(t, asUser(e.compURL(comp, "/restore")), http.StatusForbidden)
	assert.NotNil(t, e.reload(comp).ArchivedAt)
}

func TestComponentArchive_DatenbankfehlerGibt500(t *testing.T) {
	e := newVisEnv(t)
	comp := e.comp("A", nil, true, "operational")
	require.NoError(t, e.db.Callback().Update().Before("gorm:update").Register("test:fail578", func(d *gorm.DB) {
		_ = d.AddError(errors.New("boom"))
	}))
	w, m := e.do("POST", e.compURL(comp, "/archive"), nil)
	requireStatusCode(t, w, http.StatusInternalServerError)
	assert.Equal(t, "server_error", m["code"])
	assert.Zero(t, e.auditCount("component.archived"))

	require.NoError(t, e.db.Exec("UPDATE components SET archived_at = ? WHERE id = ?", time.Now(), comp.ID).Error)
	w, m = e.do("POST", e.compURL(comp, "/restore"), nil)
	requireStatusCode(t, w, http.StatusInternalServerError)
	assert.Equal(t, "server_error", m["code"])
	assert.Zero(t, e.auditCount("component.restored"))
}

func collectNames(nodes []any, out *[]string) {
	for _, n := range nodes {
		m := n.(map[string]any)
		*out = append(*out, m["Name"].(string))
		if ch, ok := m["Children"].([]any); ok {
			collectNames(ch, out)
		}
	}
}

func TestComponentArchive_OeffentlichAusgeblendetAdminSiehtSie(t *testing.T) {
	e := newVisEnv(t)
	group := e.comp("Gruppe", nil, true, "group")
	e.comp("Kind aktiv", &group.ID, true, "operational")
	archivedChild := e.comp("Kind archiviert", &group.ID, true, "major_outage")
	archived := e.comp("Alt", nil, true, "major_outage")
	e.comp("Offen", nil, true, "operational")
	for _, c := range []models.Component{archived, archivedChild} {
		w, _ := e.do("POST", e.compURL(c, "/archive"), nil)
		requireStatusCode(t, w, http.StatusOK)
	}
	inc := models.Incident{StatusPageID: e.page.ID, Title: "Vorfall", Message: "m", Status: "investigating"}
	require.NoError(t, e.db.Create(&inc).Error)
	require.NoError(t, e.db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: archived.ID}).Error)

	// Öffentliches Dashboard: weder Baum noch Vorfall-Zuordnung zeigen den Namen.
	w := e.get("/api/public/pages/vis-580/dashboard")
	requireStatusCode(t, w, http.StatusOK)
	assert.NotContains(t, w.Body.String(), "Alt\"")
	assert.NotContains(t, w.Body.String(), "Kind archiviert")
	var resp dashResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	var shown []string
	for _, c := range resp.Components {
		shown = append(shown, c.Name)
	}
	assert.ElementsMatch(t, []string{"Gruppe", "Offen"}, shown)
	require.Len(t, resp.Incidents, 1, "der Vorfall bleibt")
	assert.Empty(t, resp.Incidents[0].Components)

	// Verfügbarkeit: archivierte Komponente taucht nicht auf.
	w = e.get("/api/public/pages/vis-580/availability?include=components")
	requireStatusCode(t, w, http.StatusOK)
	assert.NotContains(t, w.Body.String(), "Alt")

	// Anonym (öffentliche Komponentenliste): gefiltert.
	w = e.get(e.base)
	requireStatusCode(t, w, http.StatusOK)
	var tree []any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tree))
	var names []string
	collectNames(tree, &names)
	assert.ElementsMatch(t, []string{"Gruppe", "Kind aktiv", "Offen"}, names)

	// Verwalter sehen alles, archivierte mit ArchivedAt.
	admin := models.User{Email: "arch-admin@example.com", IsGlobalAdmin: true}
	require.NoError(t, e.db.Create(&admin).Error)
	req, _ := http.NewRequest("GET", e.base, nil)
	req.Header.Set("X-Test-User", strconv.Itoa(int(admin.ID)))
	rec := httptest.NewRecorder()
	e.r.ServeHTTP(rec, req)
	requireStatusCode(t, rec, http.StatusOK)
	var adminTree []any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &adminTree))
	names = nil
	collectNames(adminTree, &names)
	assert.ElementsMatch(t, []string{"Gruppe", "Kind aktiv", "Kind archiviert", "Alt", "Offen"}, names)
	assert.Contains(t, rec.Body.String(), `"ArchivedAt":"`)
}

func TestComponentArchive_WebhookUndVerlaufBleibenErhalten(t *testing.T) {
	e := newVisEnv(t)
	comp := e.comp("Alt", nil, true, "operational")
	inc := models.Incident{StatusPageID: e.page.ID, Title: "Vorfall", Message: "m", Status: "resolved"}
	require.NoError(t, e.db.Create(&inc).Error)
	require.NoError(t, e.db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID}).Error)
	w, _ := e.do("POST", e.compURL(comp, "/archive"), nil)
	requireStatusCode(t, w, http.StatusOK)

	var links int64
	e.db.Model(&models.IncidentComponent{}).Where("component_id = ?", comp.ID).Count(&links)
	assert.EqualValues(t, 1, links, "Vorfall-Zuordnung (Verlauf) bleibt erhalten")

	wh := doWebhookStatusPost(e.r, strconv.Itoa(int(comp.ID)), "degraded_performance", "tok")
	requireStatusCode(t, wh, http.StatusOK)
	assert.NotNil(t, e.reload(comp).ArchivedAt, "Webhook ändert das Archiv nicht")
}
