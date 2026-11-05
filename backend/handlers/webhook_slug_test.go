package handlers_test

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Monitoring-Webhook per Kennung (Slug). Die Seite kommt aus dem
// Token, die Kennung wird nur dort aufgelöst; Speichern, Historie und Audit
// laufen über denselben Pfad wie der ID-Weg.

func slugWebhookPost(r http.Handler, slug, status, token string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"status": status})
	req, _ := http.NewRequest("POST", "/api/webhooks/component-slugs/"+slug+"/status", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Webhook-Token", token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func slugWebhookCode(w *httptest.ResponseRecorder) string {
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	s, _ := m["code"].(string)
	return s
}

func TestComponentStatusWebhookBySlug(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/webhooks/component-slugs/:slug/status", h.ComponentStatusWebhookBySlugHandler)

	pageA := models.StatusPage{Name: "A", Slug: "seite-a", WebhookTokenHash: models.HashToken("token-a")}
	pageB := models.StatusPage{Name: "B", Slug: "seite-b", WebhookTokenHash: models.HashToken("token-b")}
	require.NoError(t, db.Create(&pageA).Error)
	require.NoError(t, db.Create(&pageB).Error)
	compA := models.Component{StatusPageID: pageA.ID, Name: "API", Slug: "api", Status: "operational"}
	compB := models.Component{StatusPageID: pageB.ID, Name: "API", Slug: "api", Status: "operational"}
	onlyB := models.Component{StatusPageID: pageB.ID, Name: "Nur B", Slug: "nur-b", Status: "operational"}
	group := models.Component{StatusPageID: pageA.ID, Name: "Gruppe", Slug: "gruppe", Status: "group"}
	archived := models.Component{StatusPageID: pageA.ID, Name: "Alt", Slug: "alt", Status: "operational"}
	gone := models.Component{StatusPageID: pageA.ID, Name: "Weg", Slug: "weg", Status: "operational"}
	for _, c := range []*models.Component{&compA, &compB, &onlyB, &group, &archived, &gone} {
		require.NoError(t, db.Create(c).Error)
	}
	now := time.Now()
	require.NoError(t, db.Model(&archived).Update("archived_at", now).Error)
	require.NoError(t, db.Delete(&gone).Error)

	statusOf := func(id uint) string {
		var c models.Component
		require.NoError(t, db.Unscoped().First(&c, id).Error)
		return c.Status
	}
	changes := func(id uint) int64 {
		var n int64
		db.Model(&models.ComponentStatusChange{}).Where("component_id = ?", id).Count(&n)
		return n
	}

	t.Run("Erfolg: Status gesetzt, genau ein Historieneintrag mit Quelle webhook, Audit", func(t *testing.T) {
		w := slugWebhookPost(r, "api", "major_outage", "token-a")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, "major_outage", statusOf(compA.ID))
		assert.Equal(t, "operational", statusOf(compB.ID), "gleiche Kennung auf anderer Seite bleibt unberührt")
		hist := histOf(t, db, compA.ID)
		require.Len(t, hist, 1)
		assert.Equal(t, models.StatusSourceWebhook, hist[0].Source)
		var n int64
		db.Model(&models.AuditLog{}).Where("action = ?", "component.status_webhook_updated").Count(&n)
		assert.Equal(t, int64(1), n)
	})

	t.Run("Token als Query-Parameter", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/api/webhooks/component-slugs/api/status?token=token-a", bytes.NewBufferString(`{"status":"operational"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "operational", statusOf(compA.ID))
	})

	t.Run("Unbekannte Kennung -> 404", func(t *testing.T) {
		w := slugWebhookPost(r, "gibt-es-nicht", "major_outage", "token-a")
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, "component_not_found", slugWebhookCode(w))
	})

	t.Run("Kennung einer fremden Seite -> 404 wie unbekannt, Status unverändert", func(t *testing.T) {
		w := slugWebhookPost(r, "nur-b", "major_outage", "token-a")
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Equal(t, "component_not_found", slugWebhookCode(w))
		assert.Equal(t, "operational", statusOf(onlyB.ID))
	})

	t.Run("Token fehlt, falsch oder widerrufen -> 401, auch bei unbekannter Kennung", func(t *testing.T) {
		for _, slug := range []string{"api", "gibt-es-nicht", "nur-b"} {
			for _, tok := range []string{"", "falsch"} {
				w := slugWebhookPost(r, slug, "major_outage", tok)
				assert.Equal(t, http.StatusUnauthorized, w.Code, slug+"/"+tok)
				assert.Equal(t, "webhook_token_invalid", slugWebhookCode(w))
			}
		}
		require.NoError(t, db.Model(&pageA).Update("webhook_token", "").Error)
		assert.Equal(t, http.StatusUnauthorized, slugWebhookPost(r, "api", "major_outage", "").Code)
		require.NoError(t, db.Model(&pageA).Update("webhook_token", models.HashToken("token-a")).Error)
		assert.Equal(t, "operational", statusOf(compA.ID))
	})

	t.Run("Gruppe -> 409", func(t *testing.T) {
		w := slugWebhookPost(r, "gruppe", "major_outage", "token-a")
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Equal(t, "component_is_group", slugWebhookCode(w))
	})

	t.Run("Archivierte Komponente bleibt ansprechbar", func(t *testing.T) {
		w := slugWebhookPost(r, "alt", "degraded_performance", "token-a")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "degraded_performance", statusOf(archived.ID))
		var fresh models.Component
		require.NoError(t, db.First(&fresh, archived.ID).Error)
		assert.NotNil(t, fresh.ArchivedAt, "bleibt archiviert")
	})

	t.Run("Gelöschte Komponente -> 404", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, slugWebhookPost(r, "weg", "major_outage", "token-a").Code)
	})

	t.Run("Ungültiger Status -> 400 ohne Historie; Wiederholung ohne Wechsel schreibt keinen Eintrag", func(t *testing.T) {
		before := changes(compA.ID)
		w := slugWebhookPost(r, "api", "kaputt", "token-a")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "invalid_component_status", slugWebhookCode(w))
		assert.Equal(t, http.StatusBadRequest, func() int {
			req, _ := http.NewRequest("POST", "/api/webhooks/component-slugs/api/status", bytes.NewBufferString(`{}`))
			req.Header.Set("X-Webhook-Token", "token-a")
			ww := httptest.NewRecorder()
			r.ServeHTTP(ww, req)
			return ww.Code
		}())
		assert.Equal(t, before, changes(compA.ID))
		require.Equal(t, http.StatusOK, slugWebhookPost(r, "api", "major_outage", "token-a").Code)
		after := changes(compA.ID)
		require.Equal(t, http.StatusOK, slugWebhookPost(r, "api", "major_outage", "token-a").Code)
		assert.Equal(t, after, changes(compA.ID))
	})
}

// Punkt 1: Leere (und sonst ungültige) Kennung darf nie eine
// Komponente mit leerem Slug treffen; gleiche Antwort wie unbekannte Kennung.
func TestComponentStatusWebhookBySlug_UngueltigeKennung(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/webhooks/component-slugs/:slug/status", h.ComponentStatusWebhookBySlugHandler)
	page := models.StatusPage{Name: "A", Slug: "inv-a", WebhookTokenHash: models.HashToken("tok-inv")}
	require.NoError(t, db.Create(&page).Error)
	noSlug := models.Component{StatusPageID: page.ID, Name: "Ohne", Status: "operational"}
	upper := models.Component{StatusPageID: page.ID, Name: "Gross", Slug: "api", Status: "operational"}
	require.NoError(t, db.Create(&noSlug).Error)
	require.NoError(t, db.Create(&upper).Error)
	require.NoError(t, db.Model(&noSlug).Update("slug", "").Error)

	for _, slug := range []string{"", "API", "a%20b", "-x", "x--y"} {
		w := slugWebhookPost(r, slug, "major_outage", "tok-inv")
		assert.Equal(t, http.StatusNotFound, w.Code, "slug=%q", slug)
		assert.Equal(t, "component_not_found", slugWebhookCode(w), "slug=%q", slug)
	}
	var fresh models.Component
	require.NoError(t, db.First(&fresh, noSlug.ID).Error)
	assert.Equal(t, "operational", fresh.Status)
	// Token bleibt zuerst: ungültiger Token + leere Kennung = 401.
	assert.Equal(t, http.StatusUnauthorized, slugWebhookPost(r, "", "major_outage", "falsch").Code)
}

// Punkt 3: Body-Größenlimit auf beiden Wegen.
func TestComponentStatusWebhook_BodyZuGross(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/webhooks/component-slugs/:slug/status", h.ComponentStatusWebhookBySlugHandler)
	r.POST("/api/webhooks/components/:component_id/status", h.ComponentStatusWebhookHandler)
	page := models.StatusPage{Name: "A", Slug: "big-a", WebhookTokenHash: models.HashToken("tok-big")}
	require.NoError(t, db.Create(&page).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Slug: "api", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	big := `{"status":"major_outage","x":"` + strings.Repeat("a", 1<<20) + `"}`
	for _, path := range []string{"/api/webhooks/component-slugs/api/status", "/api/webhooks/components/" + strconv.Itoa(int(comp.ID)) + "/status"} {
		req, _ := http.NewRequest("POST", path, strings.NewReader(big))
		req.Header.Set("X-Webhook-Token", "tok-big")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, path)
		assert.Equal(t, "request_too_large", slugWebhookCode(w), path)
	}
	var fresh models.Component
	require.NoError(t, db.First(&fresh, comp.ID).Error)
	assert.Equal(t, "operational", fresh.Status)
}

// Punkt 4: Fehlversuche (falscher Token, unbekannte Kennung)
// erzeugen keine GORM-Fehlerzeile "record not found"; das Token steht nie im Log.
func TestComponentStatusWebhookBySlug_KeinLogRauschen(t *testing.T) {
	base := testutils.SetupTestDB()
	var buf bytes.Buffer
	lg := gormlogger.New(log.New(&buf, "", 0), gormlogger.Config{LogLevel: gormlogger.Error, IgnoreRecordNotFoundError: false})
	db := base.Session(&gorm.Session{Logger: lg})
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/webhooks/component-slugs/:slug/status", h.ComponentStatusWebhookBySlugHandler)
	page := models.StatusPage{Name: "A", Slug: "log-a", WebhookTokenHash: models.HashToken("geheimes-token-xyz")}
	require.NoError(t, base.Create(&page).Error)

	require.Equal(t, http.StatusUnauthorized, slugWebhookPost(r, "api", "major_outage", "geheimes-token-abc").Code)
	require.Equal(t, http.StatusNotFound, slugWebhookPost(r, "gibt-es-nicht", "major_outage", "geheimes-token-xyz").Code)
	assert.NotContains(t, buf.String(), "record not found")
	assert.NotContains(t, buf.String(), "geheimes-token")
}

// Hinweis: setComponentStatus darf eine parallel gesetzte
// Archivierung nicht überschreiben (Omit("archived_at")).
func TestComponentStatusWebhookBySlug_ParallelArchivierungBleibt(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/webhooks/component-slugs/:slug/status", h.ComponentStatusWebhookBySlugHandler)
	page := models.StatusPage{Name: "A", Slug: "arch-a", WebhookTokenHash: models.HashToken("tok-arch")}
	require.NoError(t, db.Create(&page).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Slug: "api", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	// Archivierung zwischen Lesen der Komponente und Speichern des Status.
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:archive_after_read", func(tx *gorm.DB) {
		if tx.Statement.Table == "components" && !tx.Statement.SkipHooks {
			tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Table("components").Where("id = ?", comp.ID).Update("archived_at", time.Now())
		}
	}))
	require.Equal(t, http.StatusOK, slugWebhookPost(r, "api", "major_outage", "tok-arch").Code)
	var fresh models.Component
	require.NoError(t, db.First(&fresh, comp.ID).Error)
	assert.NotNil(t, fresh.ArchivedAt)
	assert.Equal(t, "major_outage", fresh.Status)
}
