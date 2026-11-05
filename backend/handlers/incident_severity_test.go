package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Schweregrad je Vorfall. Anlegen/Ändern validieren ihn, die
// 90-Tage-Verfügbarkeit gewichtet damit (Wartungen zählen weiter nicht).

func TestIncidentSeverity_AnlegenUndAendern(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/pages/:page_id/incidents", h.CreateIncidentHandler)
	r.PUT("/api/pages/:page_id/incidents/:id", h.UpdateIncidentHandler)
	page := models.StatusPage{Name: "Sev", Slug: "sev666"}
	require.NoError(t, db.Create(&page).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API"}
	require.NoError(t, db.Create(&comp).Error)
	base := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/incidents"

	do := func(method, url string, payload map[string]any) (int, map[string]any) {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(method, url, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := serve(r, req)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	create := func(extra map[string]any) (int, map[string]any) {
		p := map[string]any{"Title": "T", "Message": "M", "Status": "investigating", "ComponentIDs": []uint{comp.ID}}
		for k, v := range extra {
			p[k] = v
		}
		return do(http.MethodPost, base, p)
	}

	code, out := create(nil)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "degraded_performance", out["Severity"], "ohne Angabe: Default")

	code, out = create(map[string]any{"Severity": "major_outage"})
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "major_outage", out["Severity"])
	id := strconv.Itoa(int(out["ID"].(float64)))

	code, _ = create(map[string]any{"Severity": "operational"})
	assert.Equal(t, http.StatusBadRequest, code, "nur die drei Beeinträchtigungsstufen")

	upd := func(extra map[string]any) (int, map[string]any) {
		p := map[string]any{"message": "m", "status": "investigating", "ComponentIDs": []uint{comp.ID}}
		for k, v := range extra {
			p[k] = v
		}
		return do(http.MethodPut, base+"/"+id, p)
	}
	code, out = upd(map[string]any{"Severity": "partial_outage"})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "partial_outage", out["Severity"], "nachträglich änderbar")
	code, out = upd(nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "partial_outage", out["Severity"], "ohne Feld unverändert")
	code, _ = upd(map[string]any{"Severity": "kaputt"})
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestIncidentSeverity_WartungBekommtDefault(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := gin.New()
	actAsGlobalAdmin(r, db)
	r.POST("/api/pages/:page_id/incidents", h.CreateIncidentHandler)
	page := models.StatusPage{Name: "SevW", Slug: "sev666w"}
	require.NoError(t, db.Create(&page).Error)
	comp := models.Component{StatusPageID: page.ID, Name: "API"}
	require.NoError(t, db.Create(&comp).Error)
	body, _ := json.Marshal(map[string]any{"Title": "W", "Message": "M", "IsMaintenance": true, "Severity": "major_outage",
		"ScheduledStartAt": "2030-01-01T10:00:00Z", "ScheduledEndAt": "2030-01-01T11:00:00Z", "ComponentIDs": []uint{comp.ID}})
	req, _ := http.NewRequest(http.MethodPost, "/api/pages/"+strconv.Itoa(int(page.ID))+"/incidents", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := serve(r, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var inc models.Incident
	require.NoError(t, db.Where("is_maintenance = ?", true).First(&inc).Error)
	assert.Equal(t, "degraded_performance", inc.Severity, "Schweregrad gilt nur für Vorfälle")
}

func TestAvailability_EigenerVorfallNachSchweregrad(t *testing.T) {
	e := newAvailEnv(t)
	p := e.page("sev", false)
	a := e.comp(p.ID, "A", "operational", nil)
	inc := e.incident(p.ID, "resolved", *utc(20, 0), utc(20, 12), a.ID)
	cases := []struct {
		severity, status string
		weight           float64
	}{
		{"partial_outage", "partial_outage", 0.6},
		{"major_outage", "major_outage", 1.0},
		{"degraded_performance", "degraded_performance", 0.3},
		{"", "degraded_performance", 0.3}, // Altdaten ohne Wert
		{"unbekannt", "degraded_performance", 0.3},
	}
	for _, tc := range cases {
		require.NoError(t, e.db.Model(&inc).UpdateColumn("severity", tc.severity).Error)
		e.now = e.now.Add(2 * time.Minute) // Cache (60 s) verfallen lassen
		_, resp := e.get("/api/public/pages/sev/availability")
		d := day(t, resp.Days, "2026-10-20")
		assert.InDelta(t, 100-100*tc.weight*12/24, *d.Availability, 1e-9, "severity=%q", tc.severity)
		assert.Equal(t, tc.status, d.Status, "severity=%q", tc.severity)
	}
}
