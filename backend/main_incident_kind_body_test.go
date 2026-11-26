package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The kind detection (incident/maintenance) read only the first 1 MiB of the
// body and silently fell back to "incidents" for larger bodies. Anyone who only
// had incidents:create still created a maintenance with an inflated body.

const maintenanceBodyFmt = `{"Title":"W","Message":"m","ComponentIDs":[%d],"IsMaintenance":true,` +
	`"ScheduledStartAt":"2099-01-01T10:00:00Z","ScheduledEndAt":"2099-01-01T11:00:00Z"%s}`

func maintenanceBody(comp uint, pad int) string {
	extra := ""
	if pad > 0 {
		extra = `,"Padding":"` + strings.Repeat("x", pad) + `"`
	}
	return strings.Replace(strings.Replace(maintenanceBodyFmt, "%d", itoa(comp), 1), "%s", extra, 1)
}

func (w *permWorld) postIncident(token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/pages/"+itoa(w.a.page.ID)+"/incidents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Token", token)
	rec := httptest.NewRecorder()
	w.r.ServeHTTP(rec, req)
	return rec
}

func (w *permWorld) maintenanceCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, w.db.Model(&models.Incident{}).Where("status_page_id = ? AND is_maintenance = ?", w.a.page.ID, true).Count(&n).Error)
	return n
}

func TestIncidentCreate_GrosserBodyUmgehtDieWartungsPruefungNicht(t *testing.T) {
	w := newPermWorld(t)
	onlyIncidents := w.user(listOf("incidents:create"))

	// Above the old kind-detection limit (1 MiB) and just above the new one (64 KiB).
	for _, pad := range []int{(1 << 20) + 10, 70 << 10} {
		rec := w.postIncident(onlyIncidents, maintenanceBody(w.a.component, pad))
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, "pad=%d", pad)
		assert.Contains(t, rec.Body.String(), `"request_too_large"`)
	}
	assert.Zero(t, w.maintenanceCount(t), "keine Wartung ohne maintenance:create")
}

func TestIncidentCreate_KaputterBodyBrichtMit400Ab(t *testing.T) {
	w := newPermWorld(t)
	rec := w.postIncident(w.user(listOf("incidents:create")), `{"IsMaintenance":true,`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"invalid_request"`)
}

func TestIncidentCreate_BeideRechteLegenWeiterhinWartungAn(t *testing.T) {
	w := newPermWorld(t)
	both := w.user(listOf("incidents:create", "maintenance:create"))
	rec := w.postIncident(both, maintenanceBody(w.a.component, 0))
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.EqualValues(t, 1, w.maintenanceCount(t))
}

func TestIncidentCreate_NurIncidentsRechtLegtWeiterhinVorfallAn(t *testing.T) {
	w := newPermWorld(t)
	body := `{"Title":"V","Message":"m","Status":"investigating","ComponentIDs":[` + itoa(w.a.component) + `]}`
	rec := w.postIncident(w.user(listOf("incidents:create")), body)
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Zero(t, w.maintenanceCount(t))
}

func TestIncidentCreate_NurIncidentsRechtKannKeineWartungAnlegen(t *testing.T) {
	w := newPermWorld(t)
	rec := w.postIncident(w.user(listOf("incidents:create")), maintenanceBody(w.a.component, 0))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Zero(t, w.maintenanceCount(t))
}
