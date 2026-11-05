package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// pageTestContext liefert Kontext und Recorder für einen direkten Handleraufruf.
func pageTestContext(method, body string, userID any) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if userID != nil {
		c.Set("userID", userID)
	}
	return c, w
}

func TestValidateBranding_SecondaryColor(t *testing.T) {
	msg, ok := validateBranding(&models.StatusPage{SecondaryColor: "rot"})
	assert.False(t, ok)
	assert.Contains(t, msg, "SecondaryColor")

	_, ok = validateBranding(&models.StatusPage{PrimaryColor: "#112233", SecondaryColor: "#445566", PageLayout: "standard"})
	assert.True(t, ok)
}

func TestFrontendURLHost_FallsBackToLocalhost(t *testing.T) {
	t.Setenv("FRONTEND_URL", "https://status.example.com:8443")
	assert.Equal(t, "status.example.com", frontendURLHost())

	for _, bad := range []string{"::kein-url", "/nur/ein/pfad"} {
		t.Setenv("FRONTEND_URL", bad)
		assert.Equal(t, "localhost", frontendURLHost(), bad)
	}
}

func TestUniqueSlug_EmptyBaseUsesDefaultAndCounts(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	assert.Equal(t, "statusseite", h.uniqueSlug(""))
	require.NoError(t, db.Create(&models.StatusPage{Name: "A", Slug: "statusseite"}).Error)
	assert.Equal(t, "statusseite-2", h.uniqueSlug(""))
}

// Nutzer-IDs im Kontext, die keine uint sind oder zu keinem Konto gehören,
// geben nie Zugriff auf eine private Seite (fail closed).
func TestPrivatePageAccess_InvalidIdentityIsDenied(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "Privat", Slug: "privat-ident", IsPrivate: true}
	require.NoError(t, db.Create(&page).Error)

	for name, id := range map[string]any{"falscher Typ": "7", "unbekanntes Konto": uint(9999)} {
		t.Run(name, func(t *testing.T) {
			c, _ := pageTestContext(http.MethodGet, "", id)
			status, _, ok := h.checkPrivatePageAccess(c, page)
			assert.False(t, ok)
			assert.Equal(t, http.StatusUnauthorized, status)

			c, _ = pageTestContext(http.MethodGet, "", id)
			assert.False(t, h.userHasRoleOnPrivatePage(c, page.ID))
			c, _ = pageTestContext(http.MethodGet, "", id)
			assert.False(t, h.userHasPermissionOnPrivatePage(c, page.ID, "pages:read"))
		})
	}
}

// Scheitert das Anlegen der Auto-Join-Rolle (kein Unique-Konflikt), bekommt der
// Nutzer 500 statt Zugriff.
func TestPrivatePageAccess_AutoJoinCreateErrorIs500(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	role := models.Role{Name: "Viewer-AJ", Permissions: "pages:read"}
	require.NoError(t, db.Create(&role).Error)
	page := models.StatusPage{Name: "Privat", Slug: "privat-aj", IsPrivate: true, DefaultRoleID: &role.ID, AutoJoinEmailDomains: "example.com"}
	require.NoError(t, db.Create(&page).Error)
	user := models.User{Email: "aj@example.com"}
	require.NoError(t, db.Create(&user).Error)
	injectDBError(t, db, "create", "user_status_page_roles", 0)

	c, _ := pageTestContext(http.MethodGet, "", user.ID)
	status, msg, ok := h.checkPrivatePageAccess(c, page)

	assert.False(t, ok)
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.NotContains(t, msg, "injizierter")
}

func TestStatusPageHandlers_InvalidBodyAndDBErrors(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "Seite", Slug: "seite-fehler"}
	require.NoError(t, db.Create(&page).Error)
	pid := strconv.Itoa(int(page.ID))

	t.Run("Anlegen mit kaputtem Body", func(t *testing.T) {
		c, w := pageTestContext(http.MethodPost, "{", uint(1))
		h.CreateStatusPageHandler(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), `"code":"invalid_request"`)
	})
	t.Run("Ändern mit kaputtem Body", func(t *testing.T) {
		c, w := pageTestContext(http.MethodPut, "{", uint(1))
		c.Params = gin.Params{{Key: "page_id", Value: pid}}
		h.UpdateStatusPageHandler(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), `"code":"invalid_request"`)
	})
	t.Run("Anlegen mit ungültiger Sekundärfarbe", func(t *testing.T) {
		c, w := pageTestContext(http.MethodPost, `{"Name":"Neu","SecondaryColor":"rot"}`, uint(1))
		h.CreateStatusPageHandler(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "SecondaryColor")
	})
	t.Run("Anlegen mit ungültigem Slug", func(t *testing.T) {
		c, w := pageTestContext(http.MethodPost, `{"Name":"Neu","Slug":"Ungültig Slug!"}`, uint(1))
		h.CreateStatusPageHandler(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), `"code":"slug_invalid"`)
	})
	t.Run("Anlegen: Standardsprachen scheitern, Seite entsteht trotzdem", func(t *testing.T) {
		injectDBError(t, db, "create", "status_page_languages", 0)
		c, w := pageTestContext(http.MethodPost, `{"Name":"Mit Sprachfehler","Slug":"mit-sprachfehler"}`, uint(1))
		h.CreateStatusPageHandler(c)
		assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	})
	t.Run("Liste lesen scheitert", func(t *testing.T) {
		remove := injectDBError(t, db, "query", "status_pages", 0)
		defer remove()
		c, w := pageTestContext(http.MethodGet, "", uint(1))
		h.GetStatusPagesHandler(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), `"code":"server_error"`)
		assert.NotContains(t, w.Body.String(), "injizierter")
	})
	t.Run("Löschen scheitert", func(t *testing.T) {
		injectDBError(t, db, "delete", "status_pages", 0)
		c, w := pageTestContext(http.MethodDelete, "", uint(1))
		c.Params = gin.Params{{Key: "page_id", Value: pid}}
		h.DeleteStatusPageHandler(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), `"code":"server_error"`)
	})
}
