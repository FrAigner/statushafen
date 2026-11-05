package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Die Admin-Übersicht überlagert den Komponentenstatus mit offenen
// Vorfällen (wie das öffentliche Dashboard), der gespeicherte Status bleibt.
func TestOverview_DerivesStatusFromOpenIncidents(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	g := r.Group("/api")
	g.Use(middleware.AuthMiddleware())
	g.GET("/admin/overview", middleware.RequireGlobalAdmin(), h.GetOverviewHandler)
	admin := models.User{Email: "ov392@example.com", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&admin).Error)
	tok, _ := middleware.GenerateToken(admin.ID, admin.Email, "", "", true)

	get := func() string {
		req, _ := http.NewRequest("GET", "/api/admin/overview", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		return w.Body.String()
	}

	p := models.StatusPage{Name: "P", Slug: "p"}
	require.NoError(t, db.Create(&p).Error)
	x := models.Component{StatusPageID: p.ID, Name: "X", Status: "operational"}
	require.NoError(t, db.Create(&x).Error)
	assert.Contains(t, get(), `"worst_status":"operational"`)

	inc := models.Incident{StatusPageID: p.ID, Title: "i", Message: "m", Status: "investigating"}
	require.NoError(t, db.Create(&inc).Error)
	require.NoError(t, db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: x.ID}).Error)
	body := get()
	assert.Contains(t, body, `"status":"degraded_performance"`)
	assert.Contains(t, body, `"worst_status":"degraded_performance"`)

	// Gelöst: zurück auf den gespeicherten Status.
	require.NoError(t, db.Model(&inc).Update("status", "resolved").Error)
	assert.Contains(t, get(), `"worst_status":"operational"`)
}
