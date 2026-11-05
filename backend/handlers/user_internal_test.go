package handlers

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Scheitert das Anlegen einer Seitenrolle, wird das ganze Konto zurückgerollt
// (kein halb angelegter Nutzer).
func TestCreateUserHandler_PageRoleErrorRollsBackUser(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "P", Slug: "p-user-tx"}
	role := models.Role{Name: "Editor-tx", Permissions: "incidents:write"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&role).Error)
	remove := injectDBError(t, db, "create", "user_status_page_roles", 0)

	body := `{"email":"neu-tx@example.com","password":"geheim-12345","page_roles":[{"status_page_id":` +
		strconv.Itoa(int(page.ID)) + `,"role_id":` + strconv.Itoa(int(role.ID)) + `}]}`
	c, w := pageTestContext(http.MethodPost, body, uint(1))
	h.CreateUserHandler(c)
	remove()

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	var n int64
	require.NoError(t, db.Model(&models.User{}).Where("email = ?", "neu-tx@example.com").Count(&n).Error)
	assert.Zero(t, n)
}

// Eine Seitenrolle mit role_id 0 bedeutet "keine Rolle": Beim Ändern wird die
// bisherige Rolle auf dieser Seite entfernt und keine neue angelegt.
func TestUpdateUserHandler_RoleZeroRemovesPageRole(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "P", Slug: "p-user-zero"}
	role := models.Role{Name: "Editor-zero", Permissions: "incidents:write"}
	user := models.User{Email: "zero@example.com"}
	require.NoError(t, db.Create(&page).Error)
	require.NoError(t, db.Create(&role).Error)
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&models.UserStatusPageRole{UserID: user.ID, StatusPageID: page.ID, RoleID: role.ID}).Error)

	body := `{"email":"zero@example.com","page_roles":[{"status_page_id":` + strconv.Itoa(int(page.ID)) + `,"role_id":0}]}`
	c, w := pageTestContext(http.MethodPut, body, uint(user.ID+100))
	c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(int(user.ID))}}
	h.UpdateUserHandler(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var n int64
	require.NoError(t, db.Model(&models.UserStatusPageRole{}).Where("user_id = ?", user.ID).Count(&n).Error)
	assert.Zero(t, n, "Rolle entfernt, keine neue angelegt")
}
