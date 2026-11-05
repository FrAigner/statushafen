package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func TestIncidentHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	actAsGlobalAdmin(r, db)

	r.GET("/api/pages/:page_id/incidents", h.GetIncidentsHandler)
	r.POST("/api/pages/:page_id/incidents", h.CreateIncidentHandler)
	r.PUT("/api/pages/:page_id/incidents/:id", h.UpdateIncidentHandler)
	r.DELETE("/api/pages/:page_id/incidents/:id", h.DeleteIncidentHandler)

	// Create test page
	page := models.StatusPage{Name: "Incident Page", Slug: "inc-page"}
	db.Create(&page)

	// Create test component
	comp := models.Component{StatusPageID: page.ID, Name: "Test API"}
	db.Create(&comp)

	pid := strconv.Itoa(int(page.ID))
	var createdIncidentID uint

	t.Run("Create Incident", func(t *testing.T) {
		payload := map[string]interface{}{
			"Title":        "API Down",
			"Message":      "We are investigating an issue.",
			"Status":       "investigating",
			"ComponentIDs": []uint{comp.ID},
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/pages/"+pid+"/incidents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusCreated)

		var dbInc models.Incident
		db.Where("title = ?", "API Down").First(&dbInc)
		createdIncidentID = dbInc.ID

		var link models.IncidentComponent
		if err := db.Where("incident_id = ? AND component_id = ?", dbInc.ID, comp.ID).First(&link).Error; err != nil {
			t.Errorf("Expected IncidentComponent link to component, got error: %v", err)
		}
		if dbInc.Title != "API Down" {
			t.Errorf("Incident created incorrectly")
		}
	})

	t.Run("Create Incident mit mehreren Komponenten", func(t *testing.T) {
		comp2 := models.Component{StatusPageID: page.ID, Name: "Test DB"}
		db.Create(&comp2)

		payload := map[string]interface{}{
			"Title":        "Multi-Component Outage",
			"Message":      "Betrifft mehrere Services.",
			"Status":       "investigating",
			"ComponentIDs": []uint{comp.ID, comp2.ID},
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/pages/"+pid+"/incidents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusCreated)

		var resp struct {
			ID         uint `json:"ID"`
			Components []struct {
				ID uint `json:"ID"`
			} `json:"Components"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Response nicht parsebar: %v", err)
		}
		if len(resp.Components) != 2 {
			t.Fatalf("Expected 2 components in response, got %d", len(resp.Components))
		}

		var count int64
		db.Model(&models.IncidentComponent{}).Where("incident_id = ?", resp.ID).Count(&count)
		if count != 2 {
			t.Errorf("Expected 2 IncidentComponent rows, got %d", count)
		}
	})

	t.Run("Create Wartungsfenster ohne Start-/Endzeit wird abgelehnt", func(t *testing.T) {
		payload := map[string]interface{}{
			"Title":         "Wartung ohne Zeit",
			"Message":       "m",
			"ComponentIDs":  []uint{comp.ID},
			"IsMaintenance": true,
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/pages/"+pid+"/incidents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Create Wartungsfenster mit gültigen Zeiten bekommt Status scheduled", func(t *testing.T) {
		start := time.Now().Add(1 * time.Hour)
		end := time.Now().Add(3 * time.Hour)
		payload := map[string]interface{}{
			"Title":               "Geplante Wartung",
			"Message":             "m",
			"ComponentIDs":        []uint{comp.ID},
			"IsMaintenance":       true,
			"ScheduledStartAt":    start,
			"ScheduledEndAt":      end,
			"ReminderLeadMinutes": 60,
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/pages/"+pid+"/incidents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusCreated)

		var resp struct {
			ID     uint   `json:"ID"`
			Status string `json:"Status"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Status != "scheduled" {
			t.Errorf("Expected status 'scheduled', got %q", resp.Status)
		}
	})

	t.Run("Create Wartungsfenster mit Ende vor Start wird abgelehnt", func(t *testing.T) {
		start := time.Now().Add(3 * time.Hour)
		end := time.Now().Add(1 * time.Hour)
		payload := map[string]interface{}{
			"Title":            "Ungültige Wartung",
			"Message":          "m",
			"ComponentIDs":     []uint{comp.ID},
			"IsMaintenance":    true,
			"ScheduledStartAt": start,
			"ScheduledEndAt":   end,
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/pages/"+pid+"/incidents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Get Incidents (Tenant Separation)", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/pages/"+pid+"/incidents", nil)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var incidents []models.Incident
		_ = json.Unmarshal(w.Body.Bytes(), &incidents)

		// 2 statt 1: "Create Incident" + "Create Incident mit mehreren Komponenten"
		// haben beide bereits einen Incident auf dieser Seite angelegt.
		if len(incidents) != 2 {
			t.Errorf("Expected 2 incidents, got %d", len(incidents))
		}
	})

	t.Run("Update Incident", func(t *testing.T) {
		payload := map[string]interface{}{
			"title":   "API Down",
			"message": "Issue resolved.",
			"status":  "resolved",
		}
		body, _ := json.Marshal(payload)
		url := "/api/pages/" + pid + "/incidents/" + strconv.Itoa(int(createdIncidentID))
		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var inc models.Incident
		db.First(&inc, createdIncidentID)
		if inc.Status != "resolved" || inc.Message != "Issue resolved." {
			t.Errorf("Incident not updated correctly")
		}
	})

	t.Run("Update Incident mit ComponentIDs ersetzt die Komponenten-Zuordnung", func(t *testing.T) {
		comp3 := models.Component{StatusPageID: page.ID, Name: "Test Cache"}
		db.Create(&comp3)

		payload := map[string]interface{}{
			"title":        "API Down",
			"message":      "Nur noch Cache betroffen.",
			"status":       "investigating",
			"ComponentIDs": []uint{comp3.ID},
		}
		body, _ := json.Marshal(payload)
		url := "/api/pages/" + pid + "/incidents/" + strconv.Itoa(int(createdIncidentID))
		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var links []models.IncidentComponent
		db.Where("incident_id = ?", createdIncidentID).Find(&links)
		if len(links) != 1 || links[0].ComponentID != comp3.ID {
			t.Errorf("expected incident to be linked only to comp3, got links: %+v", links)
		}
	})

	t.Run("Update Incident mit erneut denselben ComponentIDs bleibt konsistent", func(t *testing.T) {
		// Nach dem vorherigen Subtest ist der Incident an "Test Cache"
		// gebunden. Ein erneutes Update mit derselben ComponentID löst
		// intern ein (Soft-)Delete gefolgt von einem Create desselben
		// (incident_id, component_id)-Paares aus, das ohne den partiellen
		// Unique-Index mit dem gerade gelöschten Eintrag
		// kollidiert wäre. Vor der Transaktions-Fix verwarf
		// replaceIncidentComponents diesen Create-Fehler stillschweigend und
		// beließ den Incident ohne jede Komponenten-Zuordnung, obwohl der
		// Handler 200 OK zurückgab. Seit der Umstellung rollt die Transaktion bei einem
		// Fehler vollständig zurück (Zuordnung bleibt erhalten), aber der
		// Request selbst schlug weiterhin mit 500 fehl statt eines
		// idempotenten No-Ops - das ist behoben.
		var comp3 models.Component
		db.Where("name = ?", "Test Cache").First(&comp3)

		payload := map[string]interface{}{
			"title":        "API Down",
			"message":      "Immer noch nur Cache betroffen.",
			"status":       "investigating",
			"ComponentIDs": []uint{comp3.ID},
		}
		body, _ := json.Marshal(payload)
		url := "/api/pages/" + pid + "/incidents/" + strconv.Itoa(int(createdIncidentID))
		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var links []models.IncidentComponent
		db.Where("incident_id = ? AND component_id = ?", createdIncidentID, comp3.ID).Find(&links)
		if len(links) != 1 {
			t.Fatalf("expected incident to still be linked to Test Cache after the update (status %d), got links: %+v", w.Code, links)
		}
	})

	t.Run("Update Incident mit unbekannten ComponentIDs schlägt fehl", func(t *testing.T) {
		payload := map[string]interface{}{
			"title":        "API Down",
			"message":      "Issue resolved.",
			"status":       "resolved",
			"ComponentIDs": []uint{999999},
		}
		body, _ := json.Marshal(payload)
		url := "/api/pages/" + pid + "/incidents/" + strconv.Itoa(int(createdIncidentID))
		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Update Incident mit leerer TagIDs-Liste entfernt Tags", func(t *testing.T) {
		payload := map[string]interface{}{
			"title":   "API Down",
			"message": "Issue resolved.",
			"status":  "resolved",
			"TagIDs":  []uint{},
		}
		body, _ := json.Marshal(payload)
		url := "/api/pages/" + pid + "/incidents/" + strconv.Itoa(int(createdIncidentID))
		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)
	})

	t.Run("Delete Incident", func(t *testing.T) {
		url := "/api/pages/" + pid + "/incidents/" + strconv.Itoa(int(createdIncidentID))
		req, _ := http.NewRequest("DELETE", url, nil)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var count int64
		db.Model(&models.Incident{}).Where("id = ?", createdIncidentID).Count(&count)
		if count != 0 {
			t.Errorf("Incident was not deleted")
		}
	})
}

// TestSubscriberHasAnyComponent ist ein reiner Funktionstest der
// Filter-Logik und war zuvor als Subtest in TestIncidentHandlers versteckt;
// als eigenständiger Test unabhängig von HTTP-Router/DB-Setup.
func TestSubscriberHasAnyComponent(t *testing.T) {
	cases := []struct {
		subscribedIDs string
		targetIDs     []uint
		want          bool
	}{
		{"all", []uint{1, 2}, true},
		{"1,2", []uint{3, 4}, false},
		{"1,2", []uint{2, 5}, true},
		{"", []uint{1}, false},
	}
	for _, tc := range cases {
		if got := handlers.SubscriberHasAnyComponent(tc.subscribedIDs, tc.targetIDs); got != tc.want {
			t.Errorf("SubscriberHasAnyComponent(%q, %v) = %v, want %v", tc.subscribedIDs, tc.targetIDs, got, tc.want)
		}
	}
}
