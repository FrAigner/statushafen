package handlers_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Kennung (Slug) für Komponenten und Gruppen. Anlegen erzeugt sie
// aus dem Namen, sie ist je Statusseite eindeutig, formgeprüft und nach dem
// Anlegen unveränderlich.
func TestComponentSlug(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) {
		if v := c.GetHeader("X-Test-User"); v != "" {
			id, _ := strconv.ParseUint(v, 10, 32)
			c.Set("userID", uint(id))
		}
		c.Next()
	})
	r.GET("/api/pages/:page_id/components", h.GetComponentsHandler)
	r.POST("/api/pages/:page_id/components", h.CreateComponentHandler)
	r.PUT("/api/pages/:page_id/components/:id", h.UpdateComponentHandler)
	r.DELETE("/api/pages/:page_id/components/:id", h.DeleteComponentHandler)

	page := models.StatusPage{Name: "Kennung", Slug: "kennung-577"}
	other := models.StatusPage{Name: "Fremd", Slug: "kennung-577-fremd"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&other).Error)

	base := func(p uint) string { return "/api/pages/" + strconv.Itoa(int(p)) + "/components" }
	create := func(p uint, payload map[string]interface{}) (models.Component, int, string) {
		w := doTemplateRequest(r, "POST", base(p), payload)
		var c models.Component
		_ = json.Unmarshal(w.Body.Bytes(), &c)
		return c, w.Code, w.Body.String()
	}
	slugOf := func(p uint, name string) string {
		c, code, body := create(p, map[string]interface{}{"name": name})
		require.Equal(t, http.StatusCreated, code, body)
		return c.Slug
	}

	t.Run("Automatisch aus dem Namen, Umlaute transliteriert, Kollision zählt hoch", func(t *testing.T) {
		assert.Equal(t, "mail-server", slugOf(page.ID, "Mail Server"))
		assert.Equal(t, "mail-server-2", slugOf(page.ID, "Mail Server"))
		assert.Equal(t, "mail-server-3", slugOf(page.ID, "mail-server"))
		assert.Equal(t, "aerger-ueber-grosse-oel", slugOf(page.ID, "Ärger über große Öl!"))
		assert.Equal(t, "komponente", slugOf(page.ID, "!!!"))
	})

	t.Run("Gleiche Kennung auf einer anderen Statusseite ist erlaubt", func(t *testing.T) {
		assert.Equal(t, "mail-server", slugOf(other.ID, "Mail Server"))
	})

	t.Run("Gruppe bekommt ebenfalls eine Kennung", func(t *testing.T) {
		c, code, body := create(page.ID, map[string]interface{}{"name": "Kern", "is_group": true})
		require.Equal(t, http.StatusCreated, code, body)
		assert.Equal(t, "kern", c.Slug)
		var stored models.Component
		require.NoError(t, db.First(&stored, c.ID).Error)
		assert.Equal(t, "kern", stored.Slug)
	})

	t.Run("Lange Namen werden auf 60 Zeichen gekürzt, auch mit Zähler", func(t *testing.T) {
		long := strings.Repeat("a", 60)
		first := slugOf(page.ID, long)
		second := slugOf(page.ID, long)
		assert.LessOrEqual(t, len(first), 60)
		assert.LessOrEqual(t, len(second), 60)
		assert.True(t, models.ValidComponentSlug(first))
		assert.True(t, models.ValidComponentSlug(second))
		assert.Equal(t, strings.Repeat("a", 58)+"-2", second)
	})

	t.Run("Eigene Kennung wird übernommen", func(t *testing.T) {
		c, code, body := create(page.ID, map[string]interface{}{"name": "Egal", "slug": "mein-slug-1"})
		require.Equal(t, http.StatusCreated, code, body)
		assert.Equal(t, "mein-slug-1", c.Slug)
	})

	t.Run("Ungültige Kennung gibt 400 slug_invalid und legt nichts an", func(t *testing.T) {
		var before int64
		db.Model(&models.Component{}).Count(&before)
		for _, bad := range []string{"Gross", "mit leer", "-vorn", "hinten-", "dop--pelt", "ümlaut", "a_b", strings.Repeat("a", 61)} {
			_, code, body := create(page.ID, map[string]interface{}{"name": "X", "slug": bad})
			assert.Equal(t, http.StatusBadRequest, code, bad)
			assert.Contains(t, body, `"code":"slug_invalid"`, bad)
		}
		var after int64
		db.Model(&models.Component{}).Count(&after)
		assert.Equal(t, before, after)
		_, code, _ := create(page.ID, map[string]interface{}{"name": "X", "slug": strings.Repeat("b", 60)})
		assert.Equal(t, http.StatusCreated, code, "genau 60 Zeichen sind erlaubt")
	})

	t.Run("Vergebene Kennung gibt 409 slug_taken", func(t *testing.T) {
		_, code, body := create(page.ID, map[string]interface{}{"name": "Doppelt", "slug": "mein-slug-1"})
		assert.Equal(t, http.StatusConflict, code)
		assert.Contains(t, body, `"code":"slug_taken"`)
	})

	t.Run("Kennung einer gelöschten Komponente wird wieder frei", func(t *testing.T) {
		c, _, _ := create(page.ID, map[string]interface{}{"name": "Weg", "slug": "weg-577"})
		requireStatusCode(t, doTemplateRequest(r, "DELETE", base(page.ID)+"/"+strconv.Itoa(int(c.ID)), nil), http.StatusOK)
		again, code, body := create(page.ID, map[string]interface{}{"name": "Weg", "slug": "weg-577"})
		require.Equal(t, http.StatusCreated, code, body)
		assert.Equal(t, "weg-577", again.Slug)
	})

	t.Run("GET liefert die Kennung", func(t *testing.T) {
		w := doTemplateRequest(r, "GET", base(page.ID), nil)
		requireStatusCode(t, w, http.StatusOK)
		assert.Contains(t, w.Body.String(), `"Slug":"mail-server"`)
	})

	t.Run("Update: geänderte Kennung gibt 400 slug_immutable, gleiche Kennung ist ok", func(t *testing.T) {
		c, _, _ := create(page.ID, map[string]interface{}{"name": "Fest", "slug": "fest-577"})
		path := base(page.ID) + "/" + strconv.Itoa(int(c.ID))
		w := doTemplateRequest(r, "PUT", path, map[string]interface{}{"name": "Fest", "status": "operational", "slug": "anders"})
		requireStatusCode(t, w, http.StatusBadRequest)
		requireBodyContains(t, w, `"code":"slug_immutable"`)
		var stored models.Component
		require.NoError(t, db.First(&stored, c.ID).Error)
		assert.Equal(t, "fest-577", stored.Slug)

		w = doTemplateRequest(r, "PUT", path, map[string]interface{}{"name": "Umbenannt", "status": "operational", "slug": "fest-577"})
		requireStatusCode(t, w, http.StatusOK)
		require.NoError(t, db.First(&stored, c.ID).Error)
		assert.Equal(t, "Umbenannt", stored.Name)
		assert.Equal(t, "fest-577", stored.Slug, "Umbenennen ändert die Kennung nicht")

		// Ohne slug im Body bleibt sie ebenfalls.
		w = doTemplateRequest(r, "PUT", path, map[string]interface{}{"name": "Nochmal", "status": "operational"})
		requireStatusCode(t, w, http.StatusOK)
		require.NoError(t, db.First(&stored, c.ID).Error)
		assert.Equal(t, "fest-577", stored.Slug)
	})

	t.Run("Update über eine fremde Statusseite gibt 404", func(t *testing.T) {
		c, _, _ := create(page.ID, map[string]interface{}{"name": "Meine", "slug": "meine-577"})
		w := doTemplateRequest(r, "PUT", base(other.ID)+"/"+strconv.Itoa(int(c.ID)), map[string]interface{}{"name": "X", "status": "operational", "slug": "meine-577"})
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Anlegen mit fremdem Tag gibt 403, Kennung bleibt frei", func(t *testing.T) {
		usTag := models.Tag{Name: "US", StatusPageID: &page.ID}
		deTag := models.Tag{Name: "DE", StatusPageID: &page.ID}
		require.NoError(t, db.Create(&usTag).Error)
		require.NoError(t, db.Create(&deTag).Error)
		user := models.User{Email: "slug-de@example.com"}
		require.NoError(t, db.Create(&user).Error)
		require.NoError(t, db.Create(&models.UserTag{UserID: user.ID, TagID: deTag.ID}).Error)
		req, _ := http.NewRequest("POST", base(page.ID), strings.NewReader(`{"name":"US","slug":"us-577","tag_ids":[`+strconv.Itoa(int(usTag.ID))+`]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", strconv.Itoa(int(user.ID)))
		requireStatusCode(t, serve(r, req), http.StatusForbidden)
		var n int64
		db.Model(&models.Component{}).Where("slug = ?", "us-577").Count(&n)
		assert.Zero(t, n)
	})
}

// Unique-Race: schiebt eine parallele Anfrage zwischen
// Vorabprüfung und INSERT dieselbe Kennung ein, antwortet der Handler mit 409
// slug_taken statt 500. Bei automatischer Kennung wird stattdessen wiederholt.
func TestComponentSlug_UniqueRace(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/pages/:page_id/components", h.CreateComponentHandler)
	page := models.StatusPage{Name: "Race", Slug: "race-577"}
	require.NoError(t, db.Create(&page).Error)
	path := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/components"

	// Vor dem ersten INSERT einer Komponente legt der "Wettbewerber" dieselbe Kennung an.
	var done int
	inject := func(slug string, times int) {
		done = 0
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("race577", func(tx *gorm.DB) {
			if comp, ok := tx.Statement.Dest.(*models.Component); ok && comp.Slug == slug && done < times {
				done++
				tx.Session(&gorm.Session{NewDB: true}).Exec(
					"INSERT INTO components (created_at, updated_at, status_page_id, name, slug, status, visible) VALUES (datetime('now'), datetime('now'), ?, 'Wettbewerber', ?, 'operational', 1)",
					page.ID, slug)
			}
		}))
	}
	t.Run("Vorgegebene Kennung: 409 slug_taken", func(t *testing.T) {
		inject("race-fest", 1)
		defer func() { _ = db.Callback().Create().Remove("race577") }()
		w := doTemplateRequest(r, "POST", path, map[string]interface{}{"name": "A", "slug": "race-fest"})
		requireStatusCode(t, w, http.StatusConflict)
		requireBodyContains(t, w, `"code":"slug_taken"`)
	})
	t.Run("Automatische Kennung: Wiederholung führt zum Erfolg", func(t *testing.T) {
		// BeforeCreate (Kennung berechnen) läuft vor "gorm:create"-Callbacks der
		// Anwendung nicht zwingend; der Wettbewerber legt "wett" erst an, wenn die
		// Komponente diese Kennung schon trägt.
		inject("wett", 1)
		defer func() { _ = db.Callback().Create().Remove("race577") }()
		w := doTemplateRequest(r, "POST", path, map[string]interface{}{"name": "Wett"})
		requireStatusCode(t, w, http.StatusCreated)
		var c models.Component
		_ = json.Unmarshal(w.Body.Bytes(), &c)
		assert.Equal(t, "wett", c.Slug)
		assert.Equal(t, 1, done, "genau ein Versuch scheiterte am Konflikt")
	})
	t.Run("Dauerhafter Konflikt bei automatischer Kennung: 409 nach den Versuchen", func(t *testing.T) {
		inject("dauer", 99)
		defer func() { _ = db.Callback().Create().Remove("race577") }()
		w := doTemplateRequest(r, "POST", path, map[string]interface{}{"name": "Dauer"})
		requireStatusCode(t, w, http.StatusConflict)
		requireBodyContains(t, w, `"code":"slug_taken"`)
	})
}

// Lesefehler bei der Eindeutigkeitsprüfung: 500 mit Code, nichts wird angelegt.
func TestComponentSlug_PruefungDatenbankfehler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/pages/:page_id/components", h.CreateComponentHandler)
	page := models.StatusPage{Name: "Fehler", Slug: "fehler-577"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Exec("ALTER TABLE components RENAME COLUMN slug TO slug_x").Error)
	w := doTemplateRequest(r, "POST", "/api/pages/"+strconv.Itoa(int(page.ID))+"/components", map[string]interface{}{"name": "A", "slug": "a"})
	requireStatusCode(t, w, http.StatusInternalServerError)
	requireBodyContains(t, w, `"code":"server_error"`)
}
