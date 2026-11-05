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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Verweist ein Update auf eine Komponente einer anderen Seite, antwortet der
// Handler mit 404 und darf am Vorfall nichts verändert haben (keine halb
// geschriebene Änderung vor der Fehlerantwort).
func TestUpdateIncident_ForeignComponentChangesNothing(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.PUT("/api/pages/:page_id/incidents/:id", h.UpdateIncidentHandler)

	pageA := models.StatusPage{Name: "A", Slug: "atomic-a"}
	pageB := models.StatusPage{Name: "B", Slug: "atomic-b"}
	require.NoError(t, db.Create(&pageA).Error)
	require.NoError(t, db.Create(&pageB).Error)
	own := models.Component{StatusPageID: pageA.ID, Name: "eigene"}
	foreign := models.Component{StatusPageID: pageB.ID, Name: "fremde"}
	require.NoError(t, db.Create(&own).Error)
	require.NoError(t, db.Create(&foreign).Error)
	inc := models.Incident{StatusPageID: pageA.ID, Title: "Original", Message: "m", Status: "investigating", Severity: "degraded_performance"}
	require.NoError(t, db.Create(&inc).Error)
	require.NoError(t, db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: own.ID}).Error)

	put := func(body map[string]any) int {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("PUT", "/api/pages/"+strconv.Itoa(int(pageA.ID))+"/incidents/"+strconv.Itoa(int(inc.ID)), bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	assert.Equal(t, http.StatusNotFound, put(map[string]any{"title": "Geändert", "message": "m", "status": "investigating", "ComponentIDs": []uint{foreign.ID}}))
	var got models.Incident
	require.NoError(t, db.First(&got, inc.ID).Error)
	assert.Equal(t, "Original", got.Title, "bei fehlgeschlagener Prüfung bleibt der Vorfall unverändert")

	// Gegenprobe: eigene Komponente funktioniert und ändert den Titel.
	assert.Equal(t, http.StatusOK, put(map[string]any{"title": "Geändert", "message": "m", "status": "investigating", "ComponentIDs": []uint{own.ID}}))
	require.NoError(t, db.First(&got, inc.ID).Error)
	assert.Equal(t, "Geändert", got.Title)
}
