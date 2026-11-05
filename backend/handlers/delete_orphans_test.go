package handlers_test

import (
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

// Löschen einer Rolle setzt StatusPage.DefaultRoleID auf NULL, das
// Löschen eines Benutzers durch den Admin räumt UserTag und UserSSOIdentity auf.
func TestDeleteRoleHandler_ClearsDefaultRoleID(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.DELETE("/api/roles/:id", h.DeleteRoleHandler)

	gone := models.Role{Name: "Gone", Permissions: "components:read"}
	keep := models.Role{Name: "Keep", Permissions: "components:read"}
	require.NoError(t, db.Create(&gone).Error)
	require.NoError(t, db.Create(&keep).Error)
	pGone := models.StatusPage{Name: "A", Slug: "dr-a", DefaultRoleID: &gone.ID}
	pKeep := models.StatusPage{Name: "B", Slug: "dr-b", DefaultRoleID: &keep.ID}
	require.NoError(t, db.Create(&pGone).Error)
	require.NoError(t, db.Create(&pKeep).Error)

	req, _ := http.NewRequest("DELETE", "/api/roles/"+strconv.Itoa(int(gone.ID)), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var a, b models.StatusPage
	require.NoError(t, db.First(&a, pGone.ID).Error)
	require.NoError(t, db.First(&b, pKeep.ID).Error)
	assert.Nil(t, a.DefaultRoleID, "Standard-Rolle der gelöschten Rolle muss NULL sein")
	require.NotNil(t, b.DefaultRoleID)
	assert.Equal(t, keep.ID, *b.DefaultRoleID, "andere Seiten bleiben unberührt")
}

func TestDeleteUserHandler_RemovesTagsAndSSOIdentities(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.DELETE("/api/users/:id", h.DeleteUserHandler)

	require.NoError(t, db.Create(&models.User{Email: "root@example.com", IsGlobalAdmin: true}).Error)
	victim := models.User{Email: "victim@example.com"}
	other := models.User{Email: "other@example.com"}
	require.NoError(t, db.Create(&victim).Error)
	require.NoError(t, db.Create(&other).Error)
	page := models.StatusPage{Name: "P", Slug: "du-p"}
	require.NoError(t, db.Create(&page).Error)
	tag := models.Tag{Name: "t"}
	require.NoError(t, db.Create(&tag).Error)
	for _, u := range []models.User{victim, other} {
		require.NoError(t, db.Create(&models.UserTag{UserID: u.ID, TagID: tag.ID}).Error)
		require.NoError(t, db.Create(&models.UserSSOIdentity{UserID: u.ID, StatusPageID: page.ID, Issuer: "i", Subject: "s" + strconv.Itoa(int(u.ID))}).Error)
	}

	req, _ := http.NewRequest("DELETE", "/api/users/"+strconv.Itoa(int(victim.ID)), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var tags []models.UserTag
	require.NoError(t, db.Unscoped().Find(&tags).Error)
	require.Len(t, tags, 1)
	assert.Equal(t, other.ID, tags[0].UserID)
	var ids []models.UserSSOIdentity
	require.NoError(t, db.Unscoped().Find(&ids).Error)
	require.Len(t, ids, 1)
	assert.Equal(t, other.ID, ids[0].UserID)
}

// Datenbankfehler beim Aufräumen führen zu 500, ohne dass Rolle bzw. Benutzer
// gelöscht werden (alles in einer Transaktion).
func TestDeleteHandlers_CleanupErrorsReturn500(t *testing.T) {
	t.Run("Rolle: DefaultRoleID-Bereinigung scheitert", func(t *testing.T) {
		db := testutils.SetupTestDB()
		r := testutils.SetupRouter()
		r.DELETE("/api/roles/:id", handlers.NewHandler(db).DeleteRoleHandler)
		role := models.Role{Name: "R", Permissions: "components:read"}
		require.NoError(t, db.Create(&role).Error)
		require.NoError(t, db.Exec(`DROP TABLE status_pages`).Error)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/api/roles/"+strconv.Itoa(int(role.ID)), nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		var n int64
		require.NoError(t, db.Model(&models.Role{}).Where("id = ?", role.ID).Count(&n).Error)
		assert.EqualValues(t, 1, n, "Rolle bleibt bei Fehler erhalten")
	})
	for _, table := range []string{"user_tags", "user_sso_identities"} {
		t.Run("Benutzer: Aufräumen von "+table+" scheitert", func(t *testing.T) {
			db := testutils.SetupTestDB()
			r := testutils.SetupRouter()
			r.DELETE("/api/users/:id", handlers.NewHandler(db).DeleteUserHandler)
			require.NoError(t, db.Create(&models.User{Email: "root@example.com", IsGlobalAdmin: true}).Error)
			victim := models.User{Email: "victim@example.com"}
			require.NoError(t, db.Create(&victim).Error)
			require.NoError(t, db.Exec(`DROP TABLE `+table).Error)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("DELETE", "/api/users/"+strconv.Itoa(int(victim.ID)), nil)
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusInternalServerError, w.Code)
			var n int64
			require.NoError(t, db.Model(&models.User{}).Where("id = ?", victim.ID).Count(&n).Error)
			assert.EqualValues(t, 1, n, "Benutzer bleibt bei Fehler erhalten")
		})
	}
}
