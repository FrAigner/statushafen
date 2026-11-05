package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func TestComponentHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/pages/:page_id/components", h.GetComponentsHandler)
	r.POST("/api/pages/:page_id/components", h.CreateComponentHandler)
	r.PUT("/api/pages/:page_id/components/:id", h.UpdateComponentHandler)
	r.DELETE("/api/pages/:page_id/components/:id", h.DeleteComponentHandler)

	// Create test pages
	page1 := models.StatusPage{Name: "Page 1", Slug: "p1"}
	db.Create(&page1)
	page2 := models.StatusPage{Name: "Page 2", Slug: "p2"}
	db.Create(&page2)

	p1 := strconv.Itoa(int(page1.ID))
	p2 := strconv.Itoa(int(page2.ID))

	var createdComponentID uint

	t.Run("Create Component for Page 1", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":        "Web App",
			"description": "Main Web Application",
			"status":      "operational",
			"country":     "global",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/pages/"+p1+"/components", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusCreated)

		var comp models.Component
		_ = json.Unmarshal(w.Body.Bytes(), &comp)
		createdComponentID = comp.ID

		if comp.Name != "Web App" || comp.StatusPageID != page1.ID {
			t.Errorf("Component created incorrectly")
		}
	})

	t.Run("Create Component for Page 2", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":    "Database",
			"status":  "operational",
			"country": "global",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/pages/"+p2+"/components", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusCreated)
	})

	t.Run("Get Components (Tenant Separation)", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/pages/"+p1+"/components", nil)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var components []models.Component
		_ = json.Unmarshal(w.Body.Bytes(), &components)

		if len(components) != 1 {
			t.Errorf("Expected 1 component for page 1, got %d", len(components))
		}
		if components[0].Name != "Web App" {
			t.Errorf("Expected Web App component")
		}
	})

	t.Run("Update Component", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":   "Updated Web App",
			"status": "major_outage",
		}
		body, _ := json.Marshal(payload)
		url := "/api/pages/" + p1 + "/components/" + strconv.Itoa(int(createdComponentID))
		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var comp models.Component
		db.First(&comp, createdComponentID)
		if comp.Name != "Updated Web App" || comp.Status != "major_outage" {
			t.Errorf("Component not updated correctly")
		}
	})

	// Seiten-Rollen (hier ohne Login = kein Global Admin) vergeben
	// nur seiteneigene Tags; globale Tags siehe page_tags_test.go.
	t.Run("Update Component mit tag_ids weist seiteneigene Tags zu", func(t *testing.T) {
		globalTag := models.Tag{Name: "global-tag", StatusPageID: &page1.ID}
		db.Create(&globalTag)

		payload := map[string]interface{}{
			"name":    "Updated Web App",
			"status":  "operational",
			"tag_ids": []uint{globalTag.ID},
		}
		body, _ := json.Marshal(payload)
		url := "/api/pages/" + p1 + "/components/" + strconv.Itoa(int(createdComponentID))
		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var tagCount int64
		db.Model(&models.ComponentTag{}).Where("component_id = ? AND tag_id = ?", createdComponentID, globalTag.ID).Count(&tagCount)
		if tagCount != 1 {
			t.Errorf("expected component to be tagged with global tag, got count %d", tagCount)
		}
	})

	t.Run("Update Component mit unbekannter tag_id schlägt fehl", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":    "Updated Web App",
			"status":  "operational",
			"tag_ids": []uint{999999},
		}
		body, _ := json.Marshal(payload)
		url := "/api/pages/" + p1 + "/components/" + strconv.Itoa(int(createdComponentID))
		req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Delete Component", func(t *testing.T) {
		url := "/api/pages/" + p1 + "/components/" + strconv.Itoa(int(createdComponentID))
		req, _ := http.NewRequest("DELETE", url, nil)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var count int64
		db.Model(&models.Component{}).Where("id = ?", createdComponentID).Count(&count)
		if count != 0 {
			t.Errorf("Component was not deleted")
		}
	})

	// Regressionstest (ersetzt den früheren Country-Default-Test):
	// ein fehlendes "tag_ids" im Request darf das Anlegen nicht verhindern -
	// anders als beim alten Country-Feld (das implizit auf "global" fiel)
	// bedeutet das aber KEINE automatische globale Tag-Zuweisung mehr,
	// sondern eine Komponente ganz ohne Tag (fail-closed).
	t.Run("Create Component ohne tag_ids legt Komponente ohne Tags an", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":   "No Tags Component",
			"status": "operational",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/pages/"+p1+"/components", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusCreated)

		var comp models.Component
		_ = json.Unmarshal(w.Body.Bytes(), &comp)

		var tagCount int64
		db.Model(&models.ComponentTag{}).Where("component_id = ?", comp.ID).Count(&tagCount)
		if tagCount != 0 {
			t.Errorf("Expected no tags assigned when tag_ids is omitted, got %d", tagCount)
		}
	})
}
