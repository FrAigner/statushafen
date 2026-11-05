package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIncidentDetail_AuthorEmailOnlyForEditors:
// das öffentliche GET /api/pages/:page_id/incidents/:id lieferte
// updates[].author_email an jeden Besucher und machte so die E-Mail-Adressen
// des Teams öffentlich. Sie bleibt nur für Nutzer mit incidents:write auf
// genau dieser Seite (Admin-Ansicht AdminIncidentDetail.vue) sichtbar -
// dieselbe Grenze wie für Post-Mortem-Entwürfe.
func TestIncidentDetail_AuthorEmailOnlyForEditors(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/:page_id/incidents/:id", middleware.OptionalAuthMiddleware(), h.GetIncidentDetailHandler)

	page := models.StatusPage{Name: "Author Page", Slug: "author-page"}
	require.NoError(t, db.Create(&page).Error)
	inc := models.Incident{StatusPageID: page.ID, Title: "Outage", Message: "m", Status: "investigating"}
	require.NoError(t, db.Create(&inc).Error)
	require.NoError(t, db.Create(&models.IncidentUpdate{IncidentID: inc.ID, Status: "identified", Message: "gefunden", AuthorEmail: "oncall@team.example"}).Error)

	writer := models.Role{Name: "Author Writer", Permissions: "incidents:write"}
	reader := models.Role{Name: "Author Reader", Permissions: "incidents:read"}
	db.Create(&writer)
	db.Create(&reader)
	editor := models.User{Email: "editor@team.example"}
	viewer := models.User{Email: "viewer@team.example"}
	db.Create(&editor)
	db.Create(&viewer)
	db.Create(&models.UserStatusPageRole{UserID: editor.ID, StatusPageID: page.ID, RoleID: writer.ID})
	db.Create(&models.UserStatusPageRole{UserID: viewer.ID, StatusPageID: page.ID, RoleID: reader.ID})
	editorToken, _ := middleware.GenerateToken(editor.ID, editor.Email, "", "", false)
	viewerToken, _ := middleware.GenerateToken(viewer.ID, viewer.Email, "", "", false)

	path := "/api/pages/" + strconv.Itoa(int(page.ID)) + "/incidents/" + strconv.Itoa(int(inc.ID))
	firstUpdate := func(t *testing.T, token string) map[string]interface{} {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp struct {
			Updates []map[string]interface{} `json:"updates"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Updates, 1)
		assert.Equal(t, "gefunden", resp.Updates[0]["message"])
		return resp.Updates[0]
	}

	t.Run("anonym: keine author_email", func(t *testing.T) {
		assert.NotContains(t, firstUpdate(t, ""), "author_email")
	})
	t.Run("Rolle ohne incidents:write: keine author_email", func(t *testing.T) {
		assert.NotContains(t, firstUpdate(t, viewerToken), "author_email")
	})
	t.Run("incidents:write auf der Seite: author_email sichtbar", func(t *testing.T) {
		assert.Equal(t, "oncall@team.example", firstUpdate(t, editorToken)["author_email"])
	})
}
