package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// GET /components ersetzt den Status einer Gruppe durch den
// schlechtesten Kindstatus. Der Admin schickte diesen Wert beim Speichern
// zurück, und der Update-Handler machte aus der Gruppe eine normale
// Komponente mit festem Status. Jetzt kennzeichnet GET Gruppen per IsGroup,
// und das Update behält bei Gruppen den Status "group".
func TestUpdateComponentHandler_GroupKeepsGroupStatus(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/:page_id/components", h.GetComponentsHandler)
	r.PUT("/api/pages/:page_id/components/:id", h.UpdateComponentHandler)

	page := models.StatusPage{Name: "Gruppen-Seite", Slug: "grp-528"}
	db.Create(&page)
	base := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/components"

	newGroup := func(name string) models.Component {
		grp := models.Component{StatusPageID: page.ID, Name: name, Status: "group"}
		db.Create(&grp)
		db.Create(&models.Component{StatusPageID: page.ID, ParentID: &grp.ID, Name: name + " Kind", Status: "major_outage"})
		return grp
	}
	reload := func(id uint) models.Component {
		var c models.Component
		db.First(&c, id)
		return c
	}

	t.Run("GET kennzeichnet Gruppen per IsGroup, normale Komponenten nicht", func(t *testing.T) {
		grp := newGroup("Kern")
		w := doTemplateRequest(r, "GET", base, nil)
		requireStatusCode(t, w, http.StatusOK)
		var tree []models.Component
		if err := json.Unmarshal(w.Body.Bytes(), &tree); err != nil {
			t.Fatalf("Antwort nicht lesbar: %v", err)
		}
		for _, n := range tree {
			if n.ID != grp.ID {
				continue
			}
			if !n.IsGroup || n.Status != "major_outage" {
				t.Errorf("Gruppe: IsGroup=%v Status=%q, erwartet true/major_outage", n.IsGroup, n.Status)
			}
			if len(n.Children) != 1 || n.Children[0].IsGroup {
				t.Errorf("Kind falsch gekennzeichnet: %+v", n.Children)
			}
			return
		}
		t.Fatalf("Gruppe %d fehlt in der Antwort", grp.ID)
	})

	t.Run("Update mit aggregiertem Kindstatus behält Gruppe", func(t *testing.T) {
		grp := newGroup("Dienste")
		w := doTemplateRequest(r, "PUT", base+"/"+strconv.Itoa(int(grp.ID)),
			map[string]string{"name": "Dienste neu", "status": "major_outage"})
		requireStatusCode(t, w, http.StatusOK)
		got := reload(grp.ID)
		if got.Status != "group" || got.Name != "Dienste neu" {
			t.Errorf("Status=%q Name=%q, erwartet group/Dienste neu", got.Status, got.Name)
		}
		var resp models.Component
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if !resp.IsGroup {
			t.Errorf("Antwort ohne IsGroup: %s", w.Body.String())
		}
	})

	// status ist nicht mehr binding:"required"; der Bind-Fehlerzweig greift
	// deshalb nur noch bei kaputtem JSON.
	t.Run("Update mit ungültigem JSON gibt 400 und lässt die Gruppe unverändert", func(t *testing.T) {
		grp := newGroup("Kaputt")
		req, _ := http.NewRequest("PUT", base+"/"+strconv.Itoa(int(grp.ID)), strings.NewReader("{kein json"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusBadRequest)
		requireBodyContains(t, w, "Invalid request fields")
		if got := reload(grp.ID); got.Status != "group" || got.Name != "Kaputt" {
			t.Errorf("Gruppe verändert: %+v", got)
		}
	})

	t.Run("Update ohne Status ist bei Gruppen erlaubt", func(t *testing.T) {
		grp := newGroup("Datenbanken")
		w := doTemplateRequest(r, "PUT", base+"/"+strconv.Itoa(int(grp.ID)),
			map[string]string{"description": "Primär und Cache"})
		requireStatusCode(t, w, http.StatusOK)
		got := reload(grp.ID)
		if got.Status != "group" || got.Description != "Primär und Cache" {
			t.Errorf("Status=%q Beschreibung=%q, erwartet group/Primär und Cache", got.Status, got.Description)
		}
	})
}
