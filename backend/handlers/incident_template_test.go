package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// doTemplateRequest runs a request against the test router and
// JSON-encodes payload (if set) as the body.
func doTemplateRequest(r http.Handler, method, path string, payload interface{}) *httptest.ResponseRecorder {
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = bytes.NewBuffer(b)
	}
	req, _ := http.NewRequest(method, path, body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestIncidentTemplateHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/pages/:page_id/incident-templates", h.GetIncidentTemplatesHandler)
	r.POST("/api/pages/:page_id/incident-templates", h.CreateIncidentTemplateHandler)
	r.PUT("/api/pages/:page_id/incident-templates/:id", h.UpdateIncidentTemplateHandler)
	r.DELETE("/api/pages/:page_id/incident-templates/:id", h.DeleteIncidentTemplateHandler)

	// For the "create template -> create incident from it" verification step from
	// (no FK dependency) also register the incident routes.
	r.POST("/api/pages/:page_id/incidents", h.CreateIncidentHandler)

	page := models.StatusPage{Name: "Template Page", Slug: "tpl-page"}
	db.Create(&page)

	comp := models.Component{StatusPageID: page.ID, Name: "Database"}
	db.Create(&comp)

	pid := strconv.Itoa(int(page.ID))
	var createdTemplateID uint

	t.Run("Create incident template", func(t *testing.T) {
		w := doTemplateRequest(r, "POST", "/api/pages/"+pid+"/incident-templates", map[string]interface{}{
			"Name":          "DB-Wartung",
			"Kind":          "maintenance",
			"Title":         "Geplante Datenbank-Wartung",
			"Message":       "Wir führen ein Wartungsfenster für die Datenbank durch.",
			"DefaultStatus": "scheduled",
			"ComponentIDs":  strconv.Itoa(int(comp.ID)),
		})

		requireStatusCode(t, w, http.StatusCreated)

		var tmpl models.IncidentTemplate
		_ = json.Unmarshal(w.Body.Bytes(), &tmpl)
		createdTemplateID = tmpl.ID

		if tmpl.Name != "DB-Wartung" || tmpl.StatusPageID != page.ID {
			t.Errorf("Template created incorrectly: %+v", tmpl)
		}
	})

	t.Run("Fetch template and verify fields are correctly available", func(t *testing.T) {
		w := doTemplateRequest(r, "GET", "/api/pages/"+pid+"/incident-templates", nil)

		requireStatusCode(t, w, http.StatusOK)

		var templates []models.IncidentTemplate
		_ = json.Unmarshal(w.Body.Bytes(), &templates)

		if len(templates) != 1 {
			t.Fatalf("Expected 1 template, got %d", len(templates))
		}

		got := templates[0]
		if got.ID != createdTemplateID {
			t.Errorf("Expected template ID %d, got %d", createdTemplateID, got.ID)
		}
		if got.Kind != "maintenance" {
			t.Errorf("Expected Kind 'maintenance', got %q", got.Kind)
		}
		if got.Title != "Geplante Datenbank-Wartung" {
			t.Errorf("Expected Title to be prefilled correctly, got %q", got.Title)
		}
		if got.Message != "Wir führen ein Wartungsfenster für die Datenbank durch." {
			t.Errorf("Expected Message to be prefilled correctly, got %q", got.Message)
		}
		if got.DefaultStatus != "scheduled" {
			t.Errorf("Expected DefaultStatus 'scheduled', got %q", got.DefaultStatus)
		}
		if got.ComponentIDs != strconv.Itoa(int(comp.ID)) {
			t.Errorf("Expected ComponentIDs %q, got %q", strconv.Itoa(int(comp.ID)), got.ComponentIDs)
		}
	})

	t.Run("Create incident template ohne Kind fällt auf 'incident' zurück", func(t *testing.T) {
		w := doTemplateRequest(r, "POST", "/api/pages/"+pid+"/incident-templates", map[string]interface{}{
			"Name":    "Latenz-Vorlage",
			"Title":   "Erhöhte Latenz",
			"Message": "Wir untersuchen erhöhte Antwortzeiten.",
		})

		requireStatusCode(t, w, http.StatusCreated)

		var tmpl models.IncidentTemplate
		_ = json.Unmarshal(w.Body.Bytes(), &tmpl)
		if tmpl.Kind != "incident" {
			t.Errorf("Expected Kind to default to 'incident', got %q", tmpl.Kind)
		}
	})

	// Verification: deleting a template must not affect incidents
	// already created with it, since there is no FK relation -
	// the field values were only copied when the incident was created.
	t.Run("Deleting a template does not affect incidents created from it", func(t *testing.T) {
		w := doTemplateRequest(r, "POST", "/api/pages/"+pid+"/incidents", map[string]interface{}{
			"Title":        "Geplante Datenbank-Wartung",
			"Message":      "Wir führen ein Wartungsfenster für die Datenbank durch.",
			"Status":       "investigating",
			"ComponentIDs": []uint{comp.ID},
		})
		requireStatusCode(t, w, http.StatusCreated)

		var createdIncident models.Incident
		_ = json.Unmarshal(w.Body.Bytes(), &createdIncident)

		// Delete the template now
		delW := doTemplateRequest(r, "DELETE", "/api/pages/"+pid+"/incident-templates/"+strconv.Itoa(int(createdTemplateID)), nil)
		requireStatusCode(t, delW, http.StatusOK)

		var tmplCount int64
		db.Model(&models.IncidentTemplate{}).Where("id = ?", createdTemplateID).Count(&tmplCount)
		if tmplCount != 0 {
			t.Errorf("Expected template to be deleted, still found %d row(s)", tmplCount)
		}

		// The previously created incident must be unchanged and still findable
		// (no FK/CASCADE relation with the deleted template).
		var incAfterDelete models.Incident
		if err := db.Where("id = ?", createdIncident.ID).First(&incAfterDelete).Error; err != nil {
			t.Fatalf("Expected incident to still exist after template deletion, got error: %v", err)
		}
		if incAfterDelete.Title != "Geplante Datenbank-Wartung" {
			t.Errorf("Incident fields changed unexpectedly after template deletion: %+v", incAfterDelete)
		}
	})
}
