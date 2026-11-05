package handlers_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Eine leere Beschreibung im PUT leert die bestehende; fehlt das
// Feld, bleibt sie unverändert. Die Komponente einer fremden Seite bleibt
// unberührt (404).
func TestUpdateComponentHandler_DescriptionClear(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.PUT("/api/pages/:page_id/components/:id", h.UpdateComponentHandler)

	page := models.StatusPage{Name: "Beschr", Slug: "beschr-479"}
	other := models.StatusPage{Name: "Fremd", Slug: "fremd-479"}
	db.Create(&page)
	db.Create(&other)
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational", Description: "alt"}
	db.Create(&comp)
	url := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/components/" + strconv.Itoa(int(comp.ID))
	desc := func() string {
		var c models.Component
		db.First(&c, comp.ID)
		return c.Description
	}

	w := doTemplateRequest(r, "PUT", url, map[string]string{"name": "API", "status": "operational"})
	requireStatusCode(t, w, http.StatusOK)
	if desc() != "alt" {
		t.Errorf("fehlendes Feld darf die Beschreibung nicht ändern, ist %q", desc())
	}

	w = doTemplateRequest(r, "PUT", url, map[string]string{"name": "API", "status": "operational", "description": "neu"})
	requireStatusCode(t, w, http.StatusOK)
	if desc() != "neu" {
		t.Errorf("Beschreibung nicht gesetzt: %q", desc())
	}

	w = doTemplateRequest(r, "PUT", url, map[string]string{"name": "API", "status": "operational", "description": ""})
	requireStatusCode(t, w, http.StatusOK)
	if desc() != "" {
		t.Errorf("leerer String muss die Beschreibung leeren, ist %q", desc())
	}

	db.Model(&comp).Update("description", "bleibt")
	foreign := "/api/pages/" + strconv.Itoa(int(other.ID)) + "/components/" + strconv.Itoa(int(comp.ID))
	w = doTemplateRequest(r, "PUT", foreign, map[string]string{"name": "API", "status": "operational", "description": ""})
	requireStatusCode(t, w, http.StatusNotFound)
	if desc() != "bleibt" {
		t.Errorf("fremde Seite darf nichts ändern, ist %q", desc())
	}
}
