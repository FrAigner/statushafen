package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fehlerantworten der Middleware tragen einen Fehlercode, den das
// Frontend unter api.errors.<code> übersetzt.

type midCase struct {
	name   string
	path   string
	set    func(c *gin.Context)
	status int
	code   string
}

func runMiddlewareCases(t *testing.T, route string, mw gin.HandlerFunc, cases []midCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.GET(route, func(c *gin.Context) {
				if tc.set != nil {
					tc.set(c)
				}
			}, mw, func(c *gin.Context) { c.Status(http.StatusOK) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			assert.Equal(t, tc.status, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"code":"`+tc.code+`"`)
		})
	}
}

func TestRequireGlobalAdmin_Fehlercodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutils.SetupTestDB()
	user := models.User{Email: "plain-634@example.com"}
	require.NoError(t, db.Create(&user).Error)

	runMiddlewareCases(t, "/x", middleware.RequireGlobalAdmin(), []midCase{
		{"ohne Sitzung", "/x", nil, http.StatusUnauthorized, "unauthorized"},
		{"Sitzung mit falschem Typ", "/x", func(c *gin.Context) { c.Set("userID", "kein-uint") }, http.StatusInternalServerError, "server_error"},
		{"Nutzer existiert nicht mehr", "/x", func(c *gin.Context) { c.Set("userID", uint(999999)) }, http.StatusUnauthorized, "unauthorized"},
		{"kein Global Admin", "/x", func(c *gin.Context) { c.Set("userID", user.ID) }, http.StatusForbidden, "forbidden"},
	})
}

func TestRequirePermission_Fehlercodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutils.SetupTestDB()
	user := models.User{Email: "plain2-634@example.com"}
	require.NoError(t, db.Create(&user).Error)
	asUser := func(c *gin.Context) { c.Set("userID", user.ID) }

	runMiddlewareCases(t, "/p/:page_id", middleware.RequirePermission("components:read"), []midCase{
		{"ohne Sitzung", "/p/1", nil, http.StatusUnauthorized, "unauthorized"},
		{"Sitzung mit falschem Typ", "/p/1", func(c *gin.Context) { c.Set("userID", "kein-uint") }, http.StatusInternalServerError, "server_error"},
		{"page_id keine Zahl", "/p/abc", asUser, http.StatusBadRequest, "invalid_id"},
		{"keine Rolle auf der Seite", "/p/1", asUser, http.StatusForbidden, "forbidden"},
	})
	runMiddlewareCases(t, "/p/:page_id", middleware.RequireAnyPermission("components:read", "incidents:read"), []midCase{
		{"RequireAnyPermission: keine Rolle auf der Seite", "/p/1", asUser, http.StatusForbidden, "forbidden"},
	})
}
