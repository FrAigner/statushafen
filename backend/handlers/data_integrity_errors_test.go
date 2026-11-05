package handlers_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Scheitert das Aufräumen der Seitenrollen, wird nichts gelöscht
// (Transaktion) und die Antwort ist 500.
func TestDeleteRoleAndUserHandlers_CleanupFailureIs500(t *testing.T) {
	e := newUserGuardEnv(t)
	h := handlers.NewHandler(e.db)
	e.r.DELETE("/api/roles/:id", h.DeleteRoleHandler)
	me := e.user(t, "me@example.com", true)
	victim := e.user(t, "victim@example.com", false)
	role := models.Role{Name: "R", Permissions: "x"}
	require.NoError(t, e.db.Create(&role).Error)
	require.NoError(t, e.db.Exec(`DROP TABLE user_status_page_roles`).Error)

	w := e.do(t, http.MethodDelete, "/api/roles/"+strconv.FormatUint(uint64(role.ID), 10), 0, nil)
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	var n int64
	require.NoError(t, e.db.Model(&models.Role{}).Where("id = ?", role.ID).Count(&n).Error)
	assert.EqualValues(t, 1, n)

	w = e.do(t, http.MethodDelete, userPath(victim), me.ID, nil)
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.True(t, userExists(e.db, victim.ID))
}
