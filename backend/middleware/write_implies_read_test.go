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

// "<Bereich>:write" schließt "<Bereich>:read" ein, aber nicht
// umgekehrt und nicht über Bereichsgrenzen hinweg. Mandantentrennung bleibt
// bestehen (fremde Seite: 403).
func TestRequirePermission_WriteImpliesRead(t *testing.T) {
	db := testutils.SetupTestDB()
	writeOnly := models.Role{Name: "NurSchreiben", Permissions: "components:write"}
	readOnly := models.Role{Name: "NurLesen", Permissions: "components:read"}
	db.Create(&writeOnly)
	db.Create(&readOnly)
	writer := models.User{Email: "w@test.com"}
	reader := models.User{Email: "r@test.com"}
	db.Create(&writer)
	db.Create(&reader)
	own := models.StatusPage{Name: "Own", Slug: "own"}
	foreign := models.StatusPage{Name: "Foreign", Slug: "foreign"}
	db.Create(&own)
	db.Create(&foreign)
	db.Create(&models.UserStatusPageRole{UserID: writer.ID, StatusPageID: own.ID, RoleID: writeOnly.ID})
	db.Create(&models.UserStatusPageRole{UserID: reader.ID, StatusPageID: own.ID, RoleID: readOnly.ID})

	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) {
		uid, _ := strconv.Atoi(c.GetHeader("X-Mock-User-ID"))
		c.Set("userID", uint(uid))
		c.Next()
	})
	ok := func(c *gin.Context) { c.String(http.StatusOK, "ok") }
	r.GET("/read/:page_id", middleware.RequirePermission("components:read"), ok)
	r.GET("/write/:page_id", middleware.RequirePermission("components:write"), ok)
	r.GET("/other/:page_id", middleware.RequirePermission("incidents:read"), ok)

	id := func(v uint) string { return strconv.Itoa(int(v)) }
	tests := []struct {
		name, user, path string
		want             int
	}{
		{"write erlaubt read", id(writer.ID), "/read/" + id(own.ID), http.StatusOK},
		{"write erlaubt write", id(writer.ID), "/write/" + id(own.ID), http.StatusOK},
		{"read erlaubt kein write", id(reader.ID), "/write/" + id(own.ID), http.StatusForbidden},
		{"read erlaubt read", id(reader.ID), "/read/" + id(own.ID), http.StatusOK},
		{"write gilt nicht für anderen Bereich", id(writer.ID), "/other/" + id(own.ID), http.StatusForbidden},
		{"write gilt nicht auf fremder Seite", id(writer.ID), "/read/" + id(foreign.ID), http.StatusForbidden},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("X-Mock-User-ID", tc.user)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, w.Code, tc.want)
		}
	}
}
