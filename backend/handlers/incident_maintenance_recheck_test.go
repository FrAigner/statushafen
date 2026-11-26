package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// CreateIncidentHandler itself checks after binding whether the
// user has maintenance:create if the body sets IsMaintenance. Here the handler
// is attached to the router WITHOUT the upstream kind detection: a failure there
// must never lead to a maintenance without permission.
func TestCreateIncidentHandler_WartungPrueftRechtErneut(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	page := models.StatusPage{Name: "P", Slug: "p-745"}
	db.Create(&page)
	comp := models.Component{StatusPageID: page.ID, Name: "C"}
	db.Create(&comp)
	mk := func(email, perms string) uint {
		u := models.User{Email: email}
		db.Create(&u)
		role := models.Role{Name: email, Permissions: perms}
		db.Create(&role)
		db.Create(&models.UserStatusPageRole{UserID: u.ID, StatusPageID: page.ID, RoleID: role.ID})
		return u.ID
	}
	onlyInc := mk("inc@example.com", "incidents:create")
	both := mk("both@example.com", "incidents:create,maintenance:create")

	r := testutils.SetupRouter()
	r.POST("/p/:page_id/incidents", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.GetHeader("X-User"))
		c.Set("userID", uint(id))
		c.Next()
	}, h.CreateIncidentHandler)

	body := `{"Title":"W","Message":"m","ComponentIDs":[` + strconv.Itoa(int(comp.ID)) + `],"IsMaintenance":true,` +
		`"ScheduledStartAt":"2099-01-01T10:00:00Z","ScheduledEndAt":"2099-01-01T11:00:00Z"}`
	post := func(user uint) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/p/"+strconv.Itoa(int(page.ID))+"/incidents", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User", strconv.Itoa(int(user)))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	count := func() (n int64) {
		db.Model(&models.Incident{}).Where("is_maintenance = ?", true).Count(&n)
		return
	}

	rec := post(onlyInc)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"forbidden"`)
	assert.Zero(t, count())

	assert.Equal(t, http.StatusCreated, post(both).Code)
	assert.EqualValues(t, 1, count())
}

// A too large body is also rejected by the handler itself with 413.
func TestCreateIncidentHandler_ZuGrosserBody413(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.POST("/p/:page_id/incidents", h.CreateIncidentHandler)
	big := `{"Title":"x","Padding":"` + strings.Repeat("x", 70<<10) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/p/1/incidents", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Contains(t, rec.Body.String(), `"request_too_large"`)
}

// actAsGlobalAdmin creates a global admin and lets all requests of the
// router run as this user. Handler tests without upstream
// authentication need this for maintenances.
func actAsGlobalAdmin(r gin.IRoutes, db *gorm.DB) {
	admin := models.User{Email: "admin-745@example.com", IsGlobalAdmin: true}
	db.Create(&admin)
	r.Use(func(c *gin.Context) {
		c.Set("userID", admin.ID)
		c.Next()
	})
}
