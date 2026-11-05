package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AddIncidentUpdateHandler übernahm den Status ungeprüft, und
// "behoben" war im Backend nur resolved, im Frontend resolved + completed.
func TestIncidentStatusValidationAndResolvedRule(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	g := r.Group("/api/pages/:page_id/incidents")
	g.Use(middleware.AuthMiddleware(), middleware.RequirePermission("incidents:write"))
	g.POST("", h.CreateIncidentHandler)
	g.PUT("/:id", h.UpdateIncidentHandler)
	g.POST("/:id/updates", h.AddIncidentUpdateHandler)

	page := models.StatusPage{Name: "P", Slug: "p-525"}
	other := models.StatusPage{Name: "O", Slug: "o-525"}
	db.Create(&page)
	db.Create(&other)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	db.Create(&comp)
	admin := models.User{Email: "a525@example.com", IsGlobalAdmin: true}
	db.Create(&admin)
	tok, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)
	viewer := models.User{Email: "v525@example.com"}
	db.Create(&viewer)
	vtok, _ := middleware.GenerateToken(viewer.ID, viewer.Email, "", "", false)

	do := func(method, url, token string, payload any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest(method, url, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	pid := strconv.Itoa(int(page.ID))
	newInc := func(status string, maint bool) models.Incident {
		inc := models.Incident{StatusPageID: page.ID, Title: "T", Message: "m", Status: status, IsMaintenance: maint}
		db.Create(&inc)
		db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID})
		return inc
	}
	incURL := func(id uint) string { return "/api/pages/" + pid + "/incidents/" + strconv.Itoa(int(id)) }

	t.Run("Update mit unbekanntem Status -> 400, Vorfall unverändert", func(t *testing.T) {
		inc := newInc("investigating", false)
		for _, bad := range []string{"bogus", "completed", "scheduled", "in_progress"} {
			w := do("POST", incURL(inc.ID)+"/updates", tok, map[string]string{"status": bad, "message": "x"})
			assert.Equal(t, http.StatusBadRequest, w.Code, bad)
		}
		var fresh models.Incident
		db.First(&fresh, inc.ID)
		assert.Equal(t, "investigating", fresh.Status)
		var n int64
		db.Model(&models.IncidentUpdate{}).Where("incident_id = ?", inc.ID).Count(&n)
		assert.Zero(t, n)
	})

	t.Run("Wartung: scheduled/in_progress/resolved ok, investigating und completed 400", func(t *testing.T) {
		inc := newInc("scheduled", true)
		assert.Equal(t, http.StatusBadRequest, do("POST", incURL(inc.ID)+"/updates", tok, map[string]string{"status": "investigating", "message": "x"}).Code)
		assert.Equal(t, http.StatusBadRequest, do("POST", incURL(inc.ID)+"/updates", tok, map[string]string{"status": "completed", "message": "x"}).Code)
		assert.Equal(t, http.StatusCreated, do("POST", incURL(inc.ID)+"/updates", tok, map[string]string{"status": "in_progress", "message": "x"}).Code)
	})

	t.Run("resolved schließt den Vorfall und setzt ResolvedAt; Wiederöffnen leert es", func(t *testing.T) {
		inc := newInc("investigating", false)
		require.Equal(t, http.StatusCreated, do("POST", incURL(inc.ID)+"/updates", tok, map[string]string{"status": "resolved", "message": "behoben"}).Code)
		var fresh models.Incident
		db.First(&fresh, inc.ID)
		assert.Equal(t, "resolved", fresh.Status)
		assert.NotNil(t, fresh.ResolvedAt)
		require.Equal(t, http.StatusCreated, do("POST", incURL(inc.ID)+"/updates", tok, map[string]string{"status": "monitoring", "message": "doch nicht"}).Code)
		var reopened models.Incident
		db.First(&reopened, inc.ID)
		assert.Nil(t, reopened.ResolvedAt)
	})

	t.Run("Bearbeiten und Anlegen prüfen den Status ebenfalls", func(t *testing.T) {
		inc := newInc("investigating", false)
		w := do("PUT", incURL(inc.ID), tok, map[string]any{"message": "m", "status": "bogus", "ComponentIDs": []uint{comp.ID}})
		assert.Equal(t, http.StatusBadRequest, w.Code)
		w = do("POST", "/api/pages/"+pid+"/incidents", tok, map[string]any{"Title": "t", "Message": "m", "Status": "bogus", "ComponentIDs": []uint{comp.ID}})
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Altwert completed ist kein gültiger Status mehr", func(t *testing.T) {
		inc := newInc("investigating", false)
		w := do("PUT", incURL(inc.ID), tok, map[string]any{"message": "neu", "status": "completed", "ComponentIDs": []uint{comp.ID}})
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("completed zählt nicht mehr als behoben (Migration stellt ihn um): Komponente bleibt überlagert", func(t *testing.T) {
		pg := models.StatusPage{Name: "C", Slug: "c-525"}
		db.Create(&pg)
		c1 := models.Component{StatusPageID: pg.ID, Name: "c1", Status: "operational"}
		db.Create(&c1)
		inc := models.Incident{StatusPageID: pg.ID, Title: "T", Message: "m", Status: "completed"}
		db.Create(&inc)
		db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: c1.ID})
		assert.Equal(t, map[uint]string{c1.ID: "degraded_performance"}, componentstatus.OpenIncidentComponentIDs(db, pg.ID))
	})

	t.Run("fremde Statusseite -> 404, ohne Recht -> 403, ungültige Seite -> 400", func(t *testing.T) {
		inc := newInc("investigating", false)
		oid := strconv.Itoa(int(other.ID))
		w := do("POST", "/api/pages/"+oid+"/incidents/"+strconv.Itoa(int(inc.ID))+"/updates", tok, map[string]string{"status": "resolved", "message": "x"})
		assert.Equal(t, http.StatusNotFound, w.Code)
		w = do("POST", incURL(inc.ID)+"/updates", vtok, map[string]string{"status": "resolved", "message": "x"})
		assert.Equal(t, http.StatusForbidden, w.Code)
		w = do("POST", "/api/pages/abc/incidents/"+strconv.Itoa(int(inc.ID))+"/updates", tok, map[string]string{"status": "resolved", "message": "x"})
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// Gruppen verlieren ihren Status nie, auch nicht über Webhook
// oder Wartungs-Statusschreiber.
func TestGroupStatusProtectedFromWriters(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/webhooks/components/:component_id/status", h.ComponentStatusWebhookHandler)
	g := r.Group("/api/pages/:page_id/incidents")
	g.Use(middleware.AuthMiddleware(), middleware.RequirePermission("incidents:write"))
	g.POST("", h.CreateIncidentHandler)

	page := models.StatusPage{Name: "G", Slug: "g-530", WebhookTokenHash: models.HashToken("tok-530")}
	db.Create(&page)
	group := models.Component{StatusPageID: page.ID, Name: "Gruppe", Status: "group"}
	db.Create(&group)
	child := models.Component{StatusPageID: page.ID, Name: "Kind", Status: "operational", ParentID: &group.ID}
	db.Create(&child)

	post := func(id uint, token, status string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]string{"status": status})
		req, _ := http.NewRequest("POST", "/api/webhooks/components/"+strconv.Itoa(int(id))+"/status", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("X-Webhook-Token", token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	statusOf := func(id uint) string {
		var c models.Component
		db.First(&c, id)
		return c.Status
	}

	t.Run("Webhook auf Gruppe -> 409 component_is_group, Status bleibt group", func(t *testing.T) {
		w := post(group.ID, "tok-530", "major_outage")
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "component_is_group")
		assert.Equal(t, "group", statusOf(group.ID))
	})
	t.Run("Webhook auf Gruppe ohne Token bleibt 401", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, post(group.ID, "", "major_outage").Code)
	})
	t.Run("Webhook auf Kind funktioniert weiter", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, post(child.ID, "tok-530", "major_outage").Code)
		assert.Equal(t, "major_outage", statusOf(child.ID))
	})
	t.Run("Laufende Wartung auf Gruppe überschreibt den Gruppenstatus nicht", func(t *testing.T) {
		admin := models.User{Email: "a530@example.com", IsGlobalAdmin: true}
		db.Create(&admin)
		tok, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)
		start := time.Now().Add(-time.Hour)
		end := time.Now().Add(time.Hour)
		b, _ := json.Marshal(map[string]any{"Title": "W", "Message": "m", "IsMaintenance": true, "Status": "in_progress",
			"ScheduledStartAt": start, "ScheduledEndAt": end, "ComponentIDs": []uint{group.ID, child.ID}})
		req, _ := http.NewRequest("POST", "/api/pages/"+strconv.Itoa(int(page.ID))+"/incidents", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		assert.Equal(t, "group", statusOf(group.ID))
		assert.Equal(t, "under_maintenance", statusOf(child.ID))
	})
}
