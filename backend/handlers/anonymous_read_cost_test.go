package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// countStatements zählt alle Abfragen und Raw-Statements der Datenbank, bis
// die zurückgegebene Funktion aufgerufen wird. Der Name hängt am Test, damit
// parallele Registrierungen sich nicht überschneiden.
func countStatements(t *testing.T, db *gorm.DB) (count *atomic.Int64) {
	t.Helper()
	count = &atomic.Int64{}
	name := "test:count_statements:" + t.Name()
	inc := func(*gorm.DB) { count.Add(1) }
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(name+":q", inc))
	require.NoError(t, db.Callback().Raw().Before("gorm:raw").Register(name+":r", inc))
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(name + ":q")
		_ = db.Callback().Raw().Remove(name + ":r")
	})
	return count
}

// seedTaggedComponents legt n Komponenten mit je zwei Tags an.
func (e *visEnv) seedTaggedComponents(n int) []models.Component {
	e.t.Helper()
	tags := []models.Tag{{Name: "eins"}, {Name: "zwei"}}
	require.NoError(e.t, e.db.Create(&tags).Error)
	comps := make([]models.Component, 0, n)
	for i := 0; i < n; i++ {
		c := e.comp(fmt.Sprintf("Dienst %03d", i), nil, true, "operational")
		for _, tg := range tags {
			require.NoError(e.t, e.db.Create(&models.ComponentTag{ComponentID: c.ID, TagID: tg.ID}).Error)
		}
		comps = append(comps, c)
	}
	return comps
}

func (e *visEnv) statementsFor(path string) int64 {
	e.t.Helper()
	count := countStatements(e.t, e.db)
	w := doTemplateRequest(e.r, http.MethodGet, path, nil)
	require.Equal(e.t, http.StatusOK, w.Code, w.Body.String())
	return count.Load()
}

// Die Zahl der Datenbankabfragen hängt nicht von der Komponentenzahl ab. Vorher
// holte die Route die Tags jeder Komponente einzeln (n + 4 Abfragen, 53 ms CPU
// je Anfrage bei 200 Komponenten), und das ohne Limit für jeden Besucher.
func TestGetComponents_StatementCountIsIndependentOfComponentCount(t *testing.T) {
	small := newVisEnv(t)
	small.seedTaggedComponents(3)
	big := newVisEnv(t)
	big.seedTaggedComponents(60)

	few := small.statementsFor(small.base)
	many := big.statementsFor(big.base)
	assert.Equal(t, few, many, "Abfragen bei 3 und bei 60 Komponenten")
	assert.LessOrEqual(t, many, int64(10))
}

func TestPublicDashboard_StatementCountIsIndependentOfComponentCount(t *testing.T) {
	small := newVisEnv(t)
	small.seedTaggedComponents(3)
	big := newVisEnv(t)
	big.seedTaggedComponents(60)

	few := small.statementsFor("/api/public/pages/" + small.page.Slug + "/dashboard")
	many := big.statementsFor("/api/public/pages/" + big.page.Slug + "/dashboard")
	assert.Equal(t, few, many, "Abfragen bei 3 und bei 60 Komponenten")
}

// Gegenprobe: Die gebündelte Abfrage ordnet die Tags weiter der richtigen
// Komponente zu und lässt gelöschte Tags und Zuordnungen aus.
func TestGetComponents_BatchedTagsStayWithTheirComponent(t *testing.T) {
	e := newVisEnv(t)
	a := e.comp("A", nil, true, "operational")
	b := e.comp("B", nil, true, "operational")
	none := e.comp("Ohne", nil, true, "operational")
	other := models.StatusPage{Name: "Andere", Slug: "andere-tags"}
	require.NoError(t, e.db.Create(&other).Error)
	foreign := models.Component{StatusPageID: other.ID, Name: "Fremd"}
	require.NoError(t, e.db.Create(&foreign).Error)

	tagA := models.Tag{Name: "nur-a"}
	tagB := models.Tag{Name: "nur-b"}
	gone := models.Tag{Name: "geloescht"}
	for _, tg := range []*models.Tag{&tagA, &tagB, &gone} {
		require.NoError(t, e.db.Create(tg).Error)
	}
	for _, ct := range []models.ComponentTag{{ComponentID: a.ID, TagID: tagA.ID}, {ComponentID: b.ID, TagID: tagB.ID},
		{ComponentID: a.ID, TagID: gone.ID}, {ComponentID: foreign.ID, TagID: tagA.ID}} {
		require.NoError(t, e.db.Create(&ct).Error)
	}
	require.NoError(t, e.db.Delete(&gone).Error)
	var dropped models.ComponentTag
	require.NoError(t, e.db.Create(&models.ComponentTag{ComponentID: b.ID, TagID: tagA.ID}).Error)
	require.NoError(t, e.db.Where("component_id = ? AND tag_id = ?", b.ID, tagA.ID).Delete(&dropped).Error)

	w := doTemplateRequest(e.r, http.MethodGet, e.base, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var tree []struct {
		ID   uint
		Name string
		Tags []struct{ Name string }
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tree))
	names := map[uint][]string{}
	for _, c := range tree {
		for _, tg := range c.Tags {
			names[c.ID] = append(names[c.ID], tg.Name)
		}
	}
	assert.Equal(t, []string{"nur-a"}, names[a.ID])
	assert.Equal(t, []string{"nur-b"}, names[b.ID])
	assert.Empty(t, names[none.ID])
	assert.Len(t, tree, 3, "nur Komponenten dieser Seite")
	assert.Contains(t, w.Body.String(), `"Tags":[]`, "ohne Tags eine leere Liste statt null")
	assert.NotContains(t, w.Body.String(), `"Tags":null`)
}

// Die gebündelte Tag-Abfrage ist auf die Komponenten der angefragten Seite
// eingeschränkt. Ohne diese Einschränkung lüde jeder anonyme Aufruf die
// Tag-Zuordnungen aller Seiten, um sie danach zu verwerfen.
func TestGetComponents_TagQueryIsRestrictedToThePage(t *testing.T) {
	e := newVisEnv(t)
	e.seedTaggedComponents(3)

	var mu sync.Mutex
	var tagQueries []string
	record := func(tx *gorm.DB) {
		if sql := tx.Statement.SQL.String(); strings.Contains(sql, "component_tags.component_id") {
			mu.Lock()
			tagQueries = append(tagQueries, sql)
			mu.Unlock()
		}
	}
	name := "test:tag_sql:" + t.Name()
	require.NoError(t, e.db.Callback().Query().After("gorm:query").Register(name+":q", record))
	require.NoError(t, e.db.Callback().Row().After("gorm:row").Register(name+":r", record))
	t.Cleanup(func() {
		_ = e.db.Callback().Query().Remove(name + ":q")
		_ = e.db.Callback().Row().Remove(name + ":r")
	})

	for _, path := range []string{e.base, "/api/public/pages/" + e.page.Slug + "/dashboard"} {
		tagQueries = nil
		w := doTemplateRequest(e.r, http.MethodGet, path, nil)
		require.Equal(t, http.StatusOK, w.Code, path)
		require.Len(t, tagQueries, 1, "%s: genau eine Abfrage der Tag-Zuordnungen", path)
		assert.Contains(t, tagQueries[0], "status_page_id = ", "%s: nur Komponenten dieser Seite", path)
		assert.Contains(t, tagQueries[0], "component_tags.deleted_at IS NULL", "%s: gelöschte Zuordnungen fehlen", path)
	}
}
