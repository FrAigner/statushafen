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
	"github.com/fraigner/statushafen/permissions"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Der Altwert "<ressource>:write" wird nie mehr gespeichert. Ein
// noch gecachtes Alt-Frontend schickt ihn beim Speichern einer Rolle; der
// Server macht daraus create+read+update+delete.
func TestRoleHandlers_WriteWirdBeimSpeichernZuCrud(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/roles", h.CreateRoleHandler)
	r.PUT("/api/roles/:id", h.UpdateRoleHandler)

	send := func(method, path string, perms string) models.Role {
		body, _ := json.Marshal(map[string]string{"name": "Alt-Frontend", "permissions": perms})
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Less(t, w.Code, 300, w.Body.String())
		var role models.Role
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &role))
		return role
	}

	created := send(http.MethodPost, "/api/roles", "components:write,admin_dashboard")
	assert.False(t, permissions.HasLegacyWrite(created.Permissions), created.Permissions)
	for _, act := range permissions.Actions {
		assert.Contains(t, created.Permissions, permissions.Perm("components", act))
	}
	assert.Contains(t, created.Permissions, "admin_dashboard")

	updated := send(http.MethodPut, "/api/roles/"+strconv.Itoa(int(created.ID)), "incidents:write,notices:read")
	assert.False(t, permissions.HasLegacyWrite(updated.Permissions), updated.Permissions)
	assert.Contains(t, updated.Permissions, "incidents:delete")

	var stored models.Role
	require.NoError(t, db.First(&stored, created.ID).Error)
	assert.Equal(t, updated.Permissions, stored.Permissions, "gespeichert, nicht nur in der Antwort")

	// Create/Update/Delete setzen Read automatisch.
	only := send(http.MethodPut, "/api/roles/"+strconv.Itoa(int(created.ID)), "notices:update")
	assert.Equal(t, "notices:update,notices:read", only.Permissions)
}
