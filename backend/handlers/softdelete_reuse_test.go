package handlers_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/database"
	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Nach dem Löschen eines Tags oder einer Statusseite ließ sich
// derselbe Name bzw. Slug nicht wieder anlegen (500), weil die Unique-Indizes
// auch soft-gelöschte Zeilen erfassten. Die Tests laufen mit den echten
// Produktions-Indizes (database.CreateSoftDeleteUniqueIndexes), da
// testutils.SetupTestDB nur AutoMigrate kennt.
func setupSoftDeleteReuseRouter(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()
	db := testutils.SetupTestDB()
	database.CreateSoftDeleteUniqueIndexes(db)
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/api/tags", h.CreateTagHandler)
	r.PUT("/api/tags/:id", h.UpdateTagHandler)
	r.DELETE("/api/tags/:id", h.DeleteTagHandler)
	r.POST("/api/admin/pages", h.CreateStatusPageHandler)
	r.PUT("/api/admin/pages/:page_id", h.UpdateStatusPageHandler)
	r.DELETE("/api/admin/pages/:page_id", h.DeleteStatusPageHandler)
	return db, r
}

func decodeID(t *testing.T, body []byte) uint {
	t.Helper()
	var obj struct{ ID uint }
	require.NoError(t, json.Unmarshal(body, &obj))
	require.NotZero(t, obj.ID)
	return obj.ID
}

func requireErrorCode(t *testing.T, body []byte, want string) {
	t.Helper()
	var resp map[string]string
	require.NoError(t, json.Unmarshal(body, &resp))
	require.Equal(t, want, resp["code"])
	require.NotEmpty(t, resp["error"])
}

func TestSoftDeleteReuse_TagName(t *testing.T) {
	db, r := setupSoftDeleteReuseRouter(t)
	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	require.NoError(t, db.Create(&page).Error)

	scopes := map[string]map[string]interface{}{
		"global":      {"name": "UI-Test"},
		"statusseite": {"name": "UI-Test", "status_page_id": page.ID},
	}
	for name, payload := range scopes {
		t.Run(name, func(t *testing.T) {
			w := doTagRequest(r, http.MethodPost, "/api/tags", payload)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			id := decodeID(t, w.Body.Bytes())

			// Aktiver Name kollidiert: 409 mit Fehlercode statt 500.
			w = doTagRequest(r, http.MethodPost, "/api/tags", payload)
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			requireErrorCode(t, w.Body.Bytes(), "tag_name_taken")

			w = doTagRequest(r, http.MethodDelete, "/api/tags/"+strconv.FormatUint(uint64(id), 10), nil)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			// Kern: gleicher Name nach dem Löschen wieder anlegbar.
			w = doTagRequest(r, http.MethodPost, "/api/tags", payload)
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			require.NotEqual(t, id, decodeID(t, w.Body.Bytes()))
		})
	}

	t.Run("Umbenennen auf den Namen eines gelöschten Tags", func(t *testing.T) {
		w := doTagRequest(r, http.MethodPost, "/api/tags", map[string]interface{}{"name": "alt"})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		altID := decodeID(t, w.Body.Bytes())
		w = doTagRequest(r, http.MethodPost, "/api/tags", map[string]interface{}{"name": "neu"})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		neuID := decodeID(t, w.Body.Bytes())

		w = doTagRequest(r, http.MethodPut, "/api/tags/"+strconv.FormatUint(uint64(neuID), 10), map[string]interface{}{"name": "ALT"})
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		requireErrorCode(t, w.Body.Bytes(), "tag_name_taken")

		w = doTagRequest(r, http.MethodDelete, "/api/tags/"+strconv.FormatUint(uint64(altID), 10), nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		w = doTagRequest(r, http.MethodPut, "/api/tags/"+strconv.FormatUint(uint64(neuID), 10), map[string]interface{}{"name": "ALT"})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})
}

func TestSoftDeleteReuse_StatusPageSlug(t *testing.T) {
	_, r := setupSoftDeleteReuseRouter(t)
	payload := map[string]interface{}{"Name": "Statusseite", "Slug": "statusseite"}

	w := doTagRequest(r, http.MethodPost, "/api/admin/pages", payload)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	id := decodeID(t, w.Body.Bytes())

	w = doTagRequest(r, http.MethodPost, "/api/admin/pages", payload)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "slug_taken")

	w = doTagRequest(r, http.MethodDelete, "/api/admin/pages/"+strconv.FormatUint(uint64(id), 10), nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Kern: gleicher Slug nach dem Löschen wieder anlegbar.
	w = doTagRequest(r, http.MethodPost, "/api/admin/pages", payload)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	neuID := decodeID(t, w.Body.Bytes())
	require.NotEqual(t, id, neuID)

	// Ändern auf einen aktiven fremden Slug: 409 mit Code.
	w = doTagRequest(r, http.MethodPost, "/api/admin/pages", map[string]interface{}{"Name": "Andere", "Slug": "andere"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	andereID := decodeID(t, w.Body.Bytes())
	w = doTagRequest(r, http.MethodPut, "/api/admin/pages/"+strconv.FormatUint(uint64(andereID), 10), payload)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "slug_taken")
}

// Die Systemseite darf nicht gelöscht werden. Vorher brach ein
// Löschen den nächsten Backend-Start ab (SeedSystemStatusPage → Fatal).
func TestDeleteStatusPageHandler_SystemPageProtected(t *testing.T) {
	db, r := setupSoftDeleteReuseRouter(t)
	database.SeedSystemStatusPage(db)
	var system models.StatusPage
	require.NoError(t, db.Where("slug = ?", database.SystemStatusPageSlug).First(&system).Error)

	w := doTagRequest(r, http.MethodDelete, "/api/admin/pages/"+strconv.FormatUint(uint64(system.ID), 10), nil)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	requireErrorCode(t, w.Body.Bytes(), "system_page_protected")

	var count int64
	require.NoError(t, db.Model(&models.StatusPage{}).Where("id = ?", system.ID).Count(&count).Error)
	require.EqualValues(t, 1, count, "Systemseite muss bestehen bleiben")
}
