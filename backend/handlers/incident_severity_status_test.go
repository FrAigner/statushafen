package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Der Schweregrad eines offenen Vorfalls bestimmt den
// angezeigten Komponentenstatus - dieselbe Zuordnung wie bei den
// Verfügbarkeitsbalken (partial_outage/major_outage statt pauschal
// degraded_performance). Es gilt weiter "der Schlimmste gewinnt", und ein
// schlechterer manueller Status wird nie abgemildert.
func TestIncidentSeverity_PraegtAngezeigtenKomponentenstatus(t *testing.T) {
	f := newExtFixture(t)
	openInc := func(severity string, compID uint, status string) models.Incident {
		inc := models.Incident{StatusPageID: f.page.ID, Title: "t", Message: "m", Status: status, Severity: severity}
		require.NoError(t, f.db.Create(&inc).Error)
		require.NoError(t, f.db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: compID}).Error)
		return inc
	}
	type views struct{ dashStatus, dashEff, treeEff string }
	read := func(name string) views {
		w := f.do(http.MethodGet, "/api/public/pages/eigene/dashboard", "", nil)
		require.Equal(t, http.StatusOK, w.Code)
		var dash struct {
			Components []treeNode `json:"components"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &dash))
		w = f.do(http.MethodGet, "/api/pages/"+pid(f.page)+"/components", "", nil)
		require.Equal(t, http.StatusOK, w.Code)
		var tree []treeNode
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tree))
		d := findNode(dash.Components, name)
		require.NotNil(t, d)
		return views{d.Status, d.EffectiveStatus, findNode(tree, name).EffectiveStatus}
	}

	t.Run("Schweregrad bestimmt den Status", func(t *testing.T) {
		inc := openInc("partial_outage", f.comp.ID, "investigating")
		assert.Equal(t, views{"partial_outage", "partial_outage", "partial_outage"}, read("API"))
		require.NoError(t, f.db.Model(&inc).Update("severity", "major_outage").Error)
		assert.Equal(t, views{"major_outage", "major_outage", "major_outage"}, read("API"))
		require.NoError(t, f.db.Model(&inc).Update("severity", "degraded_performance").Error)
		assert.Equal(t, views{"degraded_performance", "degraded_performance", "degraded_performance"}, read("API"))
		require.NoError(t, f.db.Model(&inc).Update("status", "resolved").Error)
		assert.Equal(t, views{"operational", "operational", "operational"}, read("API"))
	})

	t.Run("mehrere offene Vorfälle: der schlimmste Schweregrad gewinnt", func(t *testing.T) {
		openInc("degraded_performance", f.child.ID, "identified")
		openInc("major_outage", f.child.ID, "monitoring")
		openInc("partial_outage", f.child.ID, "investigating")
		v := read("Kind")
		assert.Equal(t, "major_outage", v.dashStatus)
		assert.Equal(t, "major_outage", v.treeEff)
	})

	t.Run("Altdaten ohne Schweregrad zählen wie degraded_performance", func(t *testing.T) {
		inc := openInc("", f.comp.ID, "investigating")
		assert.Equal(t, "degraded_performance", read("API").dashStatus)
		require.NoError(t, f.db.Model(&inc).Update("status", "resolved").Error)
	})

	t.Run("schlechterer manueller Status wird nicht abgemildert", func(t *testing.T) {
		require.NoError(t, f.db.Model(&models.Component{}).Where("id = ?", f.comp.ID).Update("status", "major_outage").Error)
		openInc("degraded_performance", f.comp.ID, "investigating")
		assert.Equal(t, "major_outage", read("API").dashStatus)
	})
}

// Die Übersicht (externalstatus.EffectiveStatuses) nutzt dieselbe Zuordnung.
func TestIncidentSeverity_Uebersicht(t *testing.T) {
	f := newExtFixture(t)
	inc := models.Incident{StatusPageID: f.page.ID, Title: "t", Message: "m", Status: "investigating", Severity: "major_outage"}
	require.NoError(t, f.db.Create(&inc).Error)
	require.NoError(t, f.db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: f.comp.ID}).Error)
	w := f.do(http.MethodGet, "/api/admin/overview", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		WorstStatus string `json:"worst_status"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "major_outage", resp.WorstStatus)
}
