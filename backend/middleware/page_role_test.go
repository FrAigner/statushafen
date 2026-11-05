package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
)

// TestRequirePageRole: Lesezugriff auf
// seiten-interne Konfiguration verlangt irgendeine Rolle auf genau dieser
// Seite (oder Global Admin) - die konkrete Permission ist egal, weil jede
// Rolle den Admin-Bereich der Seite öffnet, der diese Daten anzeigt.
func TestRequirePageRole(t *testing.T) {
	db := testutils.SetupTestDB()
	role := models.Role{Name: "Any", Permissions: "incidents:read"}
	db.Create(&role)
	member := models.User{Email: "member@test.com"}
	admin := models.User{Email: "admin@test.com", IsGlobalAdmin: true}
	db.Create(&member)
	db.Create(&admin)
	own := models.StatusPage{Name: "Own", Slug: "own"}
	foreign := models.StatusPage{Name: "Foreign", Slug: "foreign"}
	db.Create(&own)
	db.Create(&foreign)
	db.Create(&models.UserStatusPageRole{UserID: member.ID, StatusPageID: own.ID, RoleID: role.ID})

	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) {
		if v := c.GetHeader("X-Mock-User-ID"); v != "" {
			uid, _ := strconv.Atoi(v)
			c.Set("userID", uint(uid))
		}
		c.Next()
	})
	ok := func(c *gin.Context) { c.String(http.StatusOK, "ok") }
	r.GET("/p/:page_id", middleware.RequirePageRole(), ok)
	r.GET("/nopage", middleware.RequirePageRole(), ok)

	id := func(v uint) string { return strconv.Itoa(int(v)) }
	tests := []struct {
		name, user, path string
		want             int
	}{
		{"Rolle auf der Seite", id(member.ID), "/p/" + id(own.ID), http.StatusOK},
		{"fremde Seite", id(member.ID), "/p/" + id(foreign.ID), http.StatusForbidden},
		{"Global Admin", id(admin.ID), "/p/" + id(foreign.ID), http.StatusOK},
		{"nicht angemeldet", "", "/p/" + id(own.ID), http.StatusUnauthorized},
		{"ohne page_id", id(member.ID), "/nopage", http.StatusBadRequest},
		{"kaputte page_id", id(member.ID), "/p/abc", http.StatusBadRequest},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.user != "" {
			req.Header.Set("X-Mock-User-ID", tc.user)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("%s: got %d, want %d (%s)", tc.name, w.Code, tc.want, w.Body.String())
		}
	}
}
