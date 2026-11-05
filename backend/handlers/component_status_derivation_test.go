package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Ein offener, nicht gelöster Vorfall mit zugeordneter Komponente
// muss sich auf den auf der öffentlichen Statusseite (Dashboard-Endpunkt)
// angezeigten Komponentenstatus auswirken, ohne den in der DB gespeicherten
// (manuell gesetzten) Status destruktiv zu überschreiben.
func TestComponentStatusDerivationOnPublicDashboard(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/pages/:slug/dashboard", h.GetPublicDashboardHandler)
	r.GET("/api/pages/:page_id/components", h.GetComponentsHandler)

	page := models.StatusPage{Name: "Derivation Page", Slug: "derivation"}
	db.Create(&page)

	fetchDashboardComponentStatus := func(t *testing.T, compID uint) string {
		t.Helper()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/public/pages/derivation/dashboard", nil)
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusOK)

		var resp struct {
			Components []models.Component `json:"components"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal dashboard response: %v", err)
		}
		for _, c := range resp.Components {
			if c.ID == compID {
				return c.Status
			}
		}
		t.Fatalf("component %d not found in dashboard response", compID)
		return ""
	}

	t.Run("offener Vorfall macht Komponente auf dem Dashboard beeinträchtigt", func(t *testing.T) {
		comp := models.Component{StatusPageID: page.ID, Name: "Datenbank-Cluster", Status: "operational"}
		db.Create(&comp)

		incident := models.Incident{StatusPageID: page.ID, Title: "Langsame Datenbankabfragen", Message: "Wir untersuchen", Status: "investigating"}
		db.Create(&incident)
		db.Create(&models.IncidentComponent{IncidentID: incident.ID, ComponentID: comp.ID})

		if got := fetchDashboardComponentStatus(t, comp.ID); got != "degraded_performance" {
			t.Errorf("expected degraded_performance while incident is open, got %q", got)
		}

		// Gelöster Vorfall: der angezeigte Status muss auf den manuell
		// gesetzten ("operational") zurückfallen.
		db.Model(&incident).Update("status", "resolved")
		if got := fetchDashboardComponentStatus(t, comp.ID); got != "operational" {
			t.Errorf("expected operational after incident resolved, got %q", got)
		}

		var persisted models.Component
		db.First(&persisted, comp.ID)
		if persisted.Status != "operational" {
			t.Errorf("expected stored status to remain operational throughout, got %q", persisted.Status)
		}
	})

	t.Run("manueller Status schlechter als Vorfall-Ableitung bleibt erhalten", func(t *testing.T) {
		comp := models.Component{StatusPageID: page.ID, Name: "Zahlungsabwicklung", Status: "major_outage"}
		db.Create(&comp)

		incident := models.Incident{StatusPageID: page.ID, Title: "Sporadische Fehler", Message: "Root Cause gefunden", Status: "identified"}
		db.Create(&incident)
		db.Create(&models.IncidentComponent{IncidentID: incident.ID, ComponentID: comp.ID})

		if got := fetchDashboardComponentStatus(t, comp.ID); got != "major_outage" {
			t.Errorf("expected manual major_outage to win over degraded_performance derivation, got %q", got)
		}
	})

	t.Run("Wartungsfenster-Vorfälle lösen keine Ableitung aus (eigene, bestehende Logik)", func(t *testing.T) {
		comp := models.Component{StatusPageID: page.ID, Name: "Edge-Netzwerk", Status: "operational"}
		db.Create(&comp)

		maintenance := models.Incident{StatusPageID: page.ID, Title: "Geplante Wartung", Message: "läuft", Status: "in_progress", IsMaintenance: true}
		db.Create(&maintenance)
		db.Create(&models.IncidentComponent{IncidentID: maintenance.ID, ComponentID: comp.ID})

		if got := fetchDashboardComponentStatus(t, comp.ID); got != "operational" {
			t.Errorf("expected maintenance incidents to be excluded from the status derivation, got %q", got)
		}
	})

	t.Run("Gruppen-Status aggregiert den abgeleiteten Status der Kinder", func(t *testing.T) {
		group := models.Component{StatusPageID: page.ID, Name: "Plattform", Status: "group"}
		db.Create(&group)
		child := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational", ParentID: &group.ID}
		db.Create(&child)

		incident := models.Incident{StatusPageID: page.ID, Title: "API-Fehler", Message: "wird untersucht", Status: "monitoring"}
		db.Create(&incident)
		db.Create(&models.IncidentComponent{IncidentID: incident.ID, ComponentID: child.ID})

		if got := fetchDashboardComponentStatus(t, group.ID); got != "degraded_performance" {
			t.Errorf("expected group status to reflect derived child status, got %q", got)
		}
	})

	// Regressionstest für die bewusste Design-Entscheidung:
	// der Admin-Endpunkt GetComponentsHandler liefert weiterhin
	// den unveränderten, manuell gesetzten Status - nicht den abgeleiteten -
	// damit das Admin-Formular beim Speichern keinen aus einem offenen
	// Vorfall abgeleiteten Status versehentlich als neuen manuellen Status
	// persistiert.
	t.Run("Admin-Komponentenliste zeigt weiterhin den manuell gesetzten Status, nicht den abgeleiteten", func(t *testing.T) {
		comp := models.Component{StatusPageID: page.ID, Name: "Auth-Service", Status: "operational"}
		db.Create(&comp)

		incident := models.Incident{StatusPageID: page.ID, Title: "Login-Fehler", Message: "wird untersucht", Status: "investigating"}
		db.Create(&incident)
		db.Create(&models.IncidentComponent{IncidentID: incident.ID, ComponentID: comp.ID})

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/pages/"+strconv.FormatUint(uint64(page.ID), 10)+"/components", nil)
		r.ServeHTTP(w, req)
		requireStatusCode(t, w, http.StatusOK)

		var components []models.Component
		_ = json.Unmarshal(w.Body.Bytes(), &components)

		found := false
		for _, c := range components {
			if c.ID == comp.ID {
				found = true
				if c.Status != "operational" {
					t.Errorf("expected admin endpoint to keep returning manual status operational, got %q", c.Status)
				}
			}
		}
		if !found {
			t.Fatalf("component %d not found in admin response", comp.ID)
		}
	})
}
