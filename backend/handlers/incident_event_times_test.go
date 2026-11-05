package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Ereigniszeiten (StartedAt/ResolvedAt) bei Vorfällen, Status beim
// Anlegen von Wartungen.
func TestIncidentEventTimes(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	// Wartungen prüft der Handler selbst gegen maintenance:create;
	// ohne Kopf handelt die Anfrage als Global Admin.
	admin := models.User{Email: "admin-zeiten@example.com", IsGlobalAdmin: true}
	db.Create(&admin)
	r.Use(func(c *gin.Context) {
		c.Set("userID", admin.ID)
		if v := c.GetHeader("X-Test-User"); v != "" {
			id, _ := strconv.ParseUint(v, 10, 32)
			c.Set("userID", uint(id))
		}
		c.Next()
	})
	r.GET("/api/pages/:page_id/incidents", h.GetIncidentsHandler)
	r.POST("/api/pages/:page_id/incidents", h.CreateIncidentHandler)
	r.PUT("/api/pages/:page_id/incidents/:id", h.UpdateIncidentHandler)

	page := models.StatusPage{Name: "Zeiten", Slug: "zeiten"}
	db.Create(&page)
	comp := models.Component{StatusPageID: page.ID, Name: "Zeiten API"}
	db.Create(&comp)
	pid := strconv.Itoa(int(page.ID))
	base := "/api/pages/" + pid + "/incidents"

	do := func(method, url string, payload map[string]interface{}) (int, map[string]interface{}) {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(method, url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := serve(r, req)
		var out map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	incident := func(extra map[string]interface{}) map[string]interface{} {
		p := map[string]interface{}{"Title": "T", "Message": "M", "Status": "investigating", "ComponentIDs": []uint{comp.ID}}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}
	maintenance := func(extra map[string]interface{}) map[string]interface{} {
		p := map[string]interface{}{"Title": "W", "Message": "M", "IsMaintenance": true, "ComponentIDs": []uint{comp.ID}}
		for k, v := range extra {
			p[k] = v
		}
		return p
	}
	ts := func(d time.Duration) string { return time.Now().Add(d).UTC().Format(time.RFC3339) }
	compStatus := func() string {
		var c models.Component
		db.First(&c, comp.ID)
		return c.Status
	}
	idOf := func(m map[string]interface{}) string { return strconv.Itoa(int(m["ID"].(float64))) }

	start := "2026-09-01T10:00:00Z"
	end := "2026-09-01T12:00:00Z"

	t.Run("POST speichert StartedAt/ResolvedAt und gibt sie zurück", func(t *testing.T) {
		code, out := do("POST", base, incident(map[string]interface{}{"Status": "resolved", "StartedAt": start, "ResolvedAt": end}))
		if code != http.StatusCreated {
			t.Fatalf("Status %d", code)
		}
		if out["StartedAt"] != start || out["ResolvedAt"] != end {
			t.Errorf("Zeiten nicht zurückgegeben: %v %v", out["StartedAt"], out["ResolvedAt"])
		}
		var inc models.Incident
		db.First(&inc, uint(out["ID"].(float64)))
		if inc.StartedAt == nil || inc.ResolvedAt == nil || !inc.StartedAt.Equal(time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)) {
			t.Errorf("Zeiten nicht gespeichert: %v %v", inc.StartedAt, inc.ResolvedAt)
		}
	})

	t.Run("POST ohne Zeiten lässt beide null", func(t *testing.T) {
		code, out := do("POST", base, incident(nil))
		if code != http.StatusCreated {
			t.Fatalf("Status %d", code)
		}
		if out["StartedAt"] != nil || out["ResolvedAt"] != nil {
			t.Errorf("erwartet null, bekam %v %v", out["StartedAt"], out["ResolvedAt"])
		}
	})

	t.Run("POST ResolvedAt vor StartedAt gibt 400", func(t *testing.T) {
		code, _ := do("POST", base, incident(map[string]interface{}{"Status": "resolved", "StartedAt": end, "ResolvedAt": start}))
		if code != http.StatusBadRequest {
			t.Errorf("erwartet 400, bekam %d", code)
		}
	})

	t.Run("POST ResolvedAt bei offenem Status gibt 400", func(t *testing.T) {
		code, _ := do("POST", base, incident(map[string]interface{}{"ResolvedAt": end}))
		if code != http.StatusBadRequest {
			t.Errorf("erwartet 400, bekam %d", code)
		}
	})

	t.Run("POST ResolvedAt = StartedAt ist erlaubt", func(t *testing.T) {
		code, _ := do("POST", base, incident(map[string]interface{}{"Status": "resolved", "StartedAt": start, "ResolvedAt": start}))
		if code != http.StatusCreated {
			t.Errorf("erwartet 201, bekam %d", code)
		}
	})

	t.Run("PUT ändert Zeiten, ohne Feld bleiben sie unverändert", func(t *testing.T) {
		_, created := do("POST", base, incident(map[string]interface{}{"StartedAt": start}))
		url := base + "/" + idOf(created)
		code, out := do("PUT", url, map[string]interface{}{"message": "m", "status": "resolved", "StartedAt": "2026-09-02T08:00:00Z", "ResolvedAt": "2026-09-02T09:00:00Z"})
		if code != http.StatusOK || out["StartedAt"] != "2026-09-02T08:00:00Z" || out["ResolvedAt"] != "2026-09-02T09:00:00Z" {
			t.Fatalf("PUT Zeiten: %d %v %v", code, out["StartedAt"], out["ResolvedAt"])
		}
		code, out = do("PUT", url, map[string]interface{}{"message": "m2", "status": "resolved"})
		if code != http.StatusOK || out["StartedAt"] != "2026-09-02T08:00:00Z" || out["ResolvedAt"] != "2026-09-02T09:00:00Z" {
			t.Errorf("Zeiten haben sich geändert: %d %v %v", code, out["StartedAt"], out["ResolvedAt"])
		}
	})

	t.Run("PUT zum Wiedereröffnen löscht ResolvedAt statt 400", func(t *testing.T) {
		_, created := do("POST", base, incident(map[string]interface{}{"Status": "resolved", "StartedAt": start, "ResolvedAt": end}))
		code, out := do("PUT", base+"/"+idOf(created), map[string]interface{}{"message": "m", "status": "investigating"})
		if code != http.StatusOK || out["ResolvedAt"] != nil || out["StartedAt"] != start {
			t.Errorf("Wiedereröffnen: %d StartedAt=%v ResolvedAt=%v", code, out["StartedAt"], out["ResolvedAt"])
		}
	})

	t.Run("PUT validiert Zeiten gegen den Bestand", func(t *testing.T) {
		_, created := do("POST", base, incident(map[string]interface{}{"StartedAt": start}))
		url := base + "/" + idOf(created)
		if code, _ := do("PUT", url, map[string]interface{}{"message": "m", "status": "resolved", "ResolvedAt": "2026-08-01T00:00:00Z"}); code != http.StatusBadRequest {
			t.Errorf("Ende vor Beginn: erwartet 400, bekam %d", code)
		}
		if code, _ := do("PUT", url, map[string]interface{}{"message": "m", "status": "monitoring", "ResolvedAt": end}); code != http.StatusBadRequest {
			t.Errorf("offener Status mit ResolvedAt: erwartet 400, bekam %d", code)
		}
	})

	t.Run("Nutzer ohne Tag-Zugriff erhält weiterhin 403 beim Setzen der Zeiten", func(t *testing.T) {
		deTag := models.Tag{Name: "DE-Z", StatusPageID: &page.ID}
		usTag := models.Tag{Name: "US-Z", StatusPageID: &page.ID}
		db.Create(&deTag)
		db.Create(&usTag)
		user := models.User{Email: "zeiten-de@example.com"}
		db.Create(&user)
		db.Create(&models.UserTag{UserID: user.ID, TagID: deTag.ID})
		inc := models.Incident{StatusPageID: page.ID, Title: "US", Message: "m", Status: "investigating"}
		db.Create(&inc)
		db.Create(&models.IncidentTag{IncidentID: inc.ID, TagID: usTag.ID})

		body, _ := json.Marshal(map[string]interface{}{"message": "m", "status": "resolved", "StartedAt": start})
		req, _ := http.NewRequest("PUT", base+"/"+strconv.Itoa(int(inc.ID)), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", strconv.Itoa(int(user.ID)))
		if w := serve(r, req); w.Code != http.StatusForbidden {
			t.Errorf("PUT: erwartet 403, bekam %d", w.Code)
		}
		var after models.Incident
		db.First(&after, inc.ID)
		if after.StartedAt != nil {
			t.Errorf("StartedAt trotz 403 gesetzt")
		}

		body, _ = json.Marshal(incident(map[string]interface{}{"StartedAt": start, "TagIDs": []uint{usTag.ID}}))
		req, _ = http.NewRequest("POST", base, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", strconv.Itoa(int(user.ID)))
		if w := serve(r, req); w.Code != http.StatusForbidden {
			t.Errorf("POST: erwartet 403, bekam %d", w.Code)
		}
	})

	t.Run("Liste sortiert nach StartedAt mit Fallback CreatedAt", func(t *testing.T) {
		p2 := models.StatusPage{Name: "Sortierung", Slug: "sortierung"}
		db.Create(&p2)
		c2 := models.Component{StatusPageID: p2.ID, Name: "S"}
		db.Create(&c2)
		old := time.Now().Add(-48 * time.Hour)
		recent := time.Now().Add(-1 * time.Hour)
		// Erstellt zuletzt, aber ältester Beginn -> muss hinten stehen.
		db.Create(&models.Incident{StatusPageID: p2.ID, Title: "nachgetragen", Message: "m", Status: "resolved", StartedAt: &old})
		db.Create(&models.Incident{StatusPageID: p2.ID, Title: "ohne-start", Message: "m", Status: "investigating"})
		db.Create(&models.Incident{StatusPageID: p2.ID, Title: "mit-start", Message: "m", Status: "investigating", StartedAt: &recent})
		req, _ := http.NewRequest("GET", "/api/pages/"+strconv.Itoa(int(p2.ID))+"/incidents", nil)
		w := serve(r, req)
		var list []models.Incident
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		got := []string{}
		for _, i := range list {
			got = append(got, i.Title)
		}
		want := []string{"ohne-start", "mit-start", "nachgetragen"}
		if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			t.Errorf("Reihenfolge %v, erwartet %v", got, want)
		}
	})

	t.Run("Wartung POST übernimmt gültigen Status", func(t *testing.T) {
		for _, st := range []string{"resolved", "in_progress", "scheduled"} {
			code, out := do("POST", base, maintenance(map[string]interface{}{"Status": st, "ScheduledStartAt": ts(-3 * time.Hour), "ScheduledEndAt": ts(-2 * time.Hour)}))
			if code != http.StatusCreated || out["Status"] != st {
				t.Errorf("Status %s: %d %v", st, code, out["Status"])
			}
		}
	})

	t.Run("Wartung POST ohne Status leitet ihn aus den Zeiten ab", func(t *testing.T) {
		cases := map[string][2]time.Duration{
			"resolved":    {-3 * time.Hour, -2 * time.Hour},
			"in_progress": {-1 * time.Hour, 1 * time.Hour},
			"scheduled":   {1 * time.Hour, 2 * time.Hour},
		}
		for want, d := range cases {
			code, out := do("POST", base, maintenance(map[string]interface{}{"ScheduledStartAt": ts(d[0]), "ScheduledEndAt": ts(d[1])}))
			if code != http.StatusCreated || out["Status"] != want {
				t.Errorf("erwartet %s: %d %v", want, code, out["Status"])
			}
		}
	})

	t.Run("Wartung POST mit ungültigem Status gibt 400", func(t *testing.T) {
		code, _ := do("POST", base, maintenance(map[string]interface{}{"Status": "kaputt", "ScheduledStartAt": ts(time.Hour), "ScheduledEndAt": ts(2 * time.Hour)}))
		if code != http.StatusBadRequest {
			t.Errorf("erwartet 400, bekam %d", code)
		}
	})

	t.Run("POST und PUT liefern denselben Status, Scheduler lässt resolved in Ruhe", func(t *testing.T) {
		sched := map[string]interface{}{"ScheduledStartAt": ts(-3 * time.Hour), "ScheduledEndAt": ts(-2 * time.Hour)}
		_, created := do("POST", base, maintenance(map[string]interface{}{"Status": "resolved", "ScheduledStartAt": sched["ScheduledStartAt"], "ScheduledEndAt": sched["ScheduledEndAt"]}))
		_, updated := do("PUT", base+"/"+idOf(created), map[string]interface{}{"message": "m", "status": "resolved"})
		if created["Status"] != "resolved" || updated["Status"] != created["Status"] {
			t.Errorf("POST %v vs PUT %v", created["Status"], updated["Status"])
		}
		db.Model(&models.Component{}).Where("id = ?", comp.ID).Update("status", "operational")
		h.RunMaintenanceTickForTest()
		var inc models.Incident
		db.First(&inc, uint(created["ID"].(float64)))
		if inc.Status != "resolved" || compStatus() != "operational" {
			t.Errorf("Scheduler hat angefasst: %s / %s", inc.Status, compStatus())
		}
	})

	t.Run("Komponentenstatus: nur in_progress setzt under_maintenance", func(t *testing.T) {
		db.Model(&models.Component{}).Where("id = ?", comp.ID).Update("status", "operational")
		do("POST", base, maintenance(map[string]interface{}{"Status": "resolved", "ScheduledStartAt": ts(-3 * time.Hour), "ScheduledEndAt": ts(-2 * time.Hour)}))
		do("POST", base, maintenance(map[string]interface{}{"Status": "scheduled", "ScheduledStartAt": ts(2 * time.Hour), "ScheduledEndAt": ts(3 * time.Hour)}))
		if s := compStatus(); s != "operational" {
			t.Fatalf("Komponente fälschlich auf %s", s)
		}
		do("POST", base, maintenance(map[string]interface{}{"Status": "in_progress", "ScheduledStartAt": ts(-time.Hour), "ScheduledEndAt": ts(time.Hour)}))
		if s := compStatus(); s != "under_maintenance" {
			t.Errorf("erwartet under_maintenance, bekam %s", s)
		}
	})
}
