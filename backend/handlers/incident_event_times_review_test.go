package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Statuswechsel über die
// Detailseite und geleerte Zeitfelder im Bearbeiten-Formular.
func TestIncidentEventTimesReview(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.PUT("/api/pages/:page_id/incidents/:id", h.UpdateIncidentHandler)
	r.POST("/api/pages/:page_id/incidents/:id/updates", h.AddIncidentUpdateHandler)

	page := models.StatusPage{Name: "Review", Slug: "review-zeiten"}
	db.Create(&page)
	url := func(inc models.Incident) string {
		return "/api/pages/" + strconv.Itoa(int(page.ID)) + "/incidents/" + strconv.Itoa(int(inc.ID))
	}
	start := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	newIncident := func(status string, withEnd bool) models.Incident {
		inc := models.Incident{StatusPageID: page.ID, Title: "T", Message: "m", Status: status, StartedAt: &start}
		if withEnd {
			inc.ResolvedAt = &end
		}
		db.Create(&inc)
		return inc
	}
	send := func(method, u string, payload string) int {
		req, _ := http.NewRequest(method, u, bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		return serve(r, req).Code
	}
	reload := func(inc models.Incident) models.Incident {
		var out models.Incident
		db.First(&out, inc.ID)
		return out
	}

	t.Run("Befund 1: Statusmeldung investigating leert ResolvedAt, danach geht PUT", func(t *testing.T) {
		inc := newIncident("resolved", true)
		if code := send("POST", url(inc)+"/updates", `{"status":"investigating","message":"wieder offen"}`); code != http.StatusCreated {
			t.Fatalf("updates: %d", code)
		}
		after := reload(inc)
		if after.Status != "investigating" || after.ResolvedAt != nil {
			t.Fatalf("Status %s, ResolvedAt %v", after.Status, after.ResolvedAt)
		}
		if code := send("PUT", url(inc), `{"message":"m","title":"neu","status":"investigating"}`); code != http.StatusOK {
			t.Errorf("PUT: erwartet 200, bekam %d", code)
		}
	})

	t.Run("Befund 1: Statusmeldung resolved lässt vorhandenes ResolvedAt unverändert", func(t *testing.T) {
		inc := newIncident("resolved", true)
		if code := send("POST", url(inc)+"/updates", `{"status":"resolved","message":"weiter gelöst"}`); code != http.StatusCreated {
			t.Fatalf("updates: %d", code)
		}
		after := reload(inc)
		if after.ResolvedAt == nil || !after.ResolvedAt.Equal(end) {
			t.Errorf("ResolvedAt verändert: %v", after.ResolvedAt)
		}
	})

	t.Run("Befund 1: Wartung behält Zeiten bei Statusmeldung", func(t *testing.T) {
		inc := models.Incident{StatusPageID: page.ID, Title: "W", Message: "m", Status: "scheduled", IsMaintenance: true, ResolvedAt: &end}
		db.Create(&inc)
		if code := send("POST", url(inc)+"/updates", `{"status":"in_progress","message":"läuft"}`); code != http.StatusCreated {
			t.Fatalf("updates: %d", code)
		}
		if after := reload(inc); after.ResolvedAt == nil {
			t.Errorf("ResolvedAt einer Wartung wurde geleert")
		}
	})

	t.Run("Befund 2: null setzt StartedAt und ResolvedAt zurück, fehlendes Feld nicht", func(t *testing.T) {
		inc := newIncident("resolved", true)
		if code := send("PUT", url(inc), `{"message":"m","status":"resolved"}`); code != http.StatusOK {
			t.Fatalf("PUT ohne Felder: %d", code)
		}
		if after := reload(inc); after.StartedAt == nil || after.ResolvedAt == nil {
			t.Fatalf("fehlende Felder haben Werte verändert: %v %v", after.StartedAt, after.ResolvedAt)
		}
		if code := send("PUT", url(inc), `{"message":"m","status":"resolved","StartedAt":null,"ResolvedAt":null}`); code != http.StatusOK {
			t.Fatalf("PUT mit null: %d", code)
		}
		after := reload(inc)
		if after.StartedAt != nil || after.ResolvedAt != nil {
			t.Errorf("null wurde ignoriert: %v %v", after.StartedAt, after.ResolvedAt)
		}
	})

	t.Run("Befund 2: ungültiger Zeitwert gibt 400", func(t *testing.T) {
		inc := newIncident("investigating", false)
		if code := send("PUT", url(inc), `{"message":"m","status":"investigating","StartedAt":"kein-datum"}`); code != http.StatusBadRequest {
			t.Errorf("erwartet 400, bekam %d", code)
		}
	})

	t.Run("Befund 2: Wartung ignoriert StartedAt null", func(t *testing.T) {
		inc := models.Incident{StatusPageID: page.ID, Title: "W2", Message: "m", Status: "scheduled", IsMaintenance: true, StartedAt: &start}
		db.Create(&inc)
		if code := send("PUT", url(inc), `{"message":"m","status":"scheduled","StartedAt":null}`); code != http.StatusOK {
			t.Fatalf("PUT: %d", code)
		}
		if after := reload(inc); after.StartedAt == nil {
			t.Errorf("StartedAt einer Wartung wurde geleert")
		}
	})
}

func TestOptionalTimeUnmarshal(t *testing.T) {
	var v struct {
		A handlers.OptionalTime `json:"A"`
		B handlers.OptionalTime `json:"B"`
	}
	if err := json.Unmarshal([]byte(`{"A":null}`), &v); err != nil {
		t.Fatal(err)
	}
	if !v.A.Set || v.A.Time != nil || v.B.Set {
		t.Errorf("A=%+v B=%+v", v.A, v.B)
	}
}
