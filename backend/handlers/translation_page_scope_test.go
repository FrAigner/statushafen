package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestTranslationPageScopes: jede Scope-Middleware
// leitet :page_id aus der Ressource ab (400 bei kaputter ID, 404 bei
// unbekannter/gelöschter Ressource). Die Verdrahtung mit RequirePermission
// prüft main_translation_scope_test.go über den echten Router.
func TestTranslationPageScopes(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	echo := func(c *gin.Context) { c.String(http.StatusOK, c.Param("page_id")) }

	r.GET("/incident/:incident_id", h.IncidentPageScope(), echo)
	r.GET("/component/:component_id", h.ComponentPageScope(), echo)
	r.GET("/notice/:notice_id", h.NoticePageScope(), echo)
	r.GET("/incident-tr/:translation_id", h.IncidentTranslationPageScope(), echo)
	r.GET("/component-tr/:translation_id", h.ComponentTranslationPageScope(), echo)
	r.GET("/notice-tr/:translation_id", h.NoticeTranslationPageScope(), echo)

	page := models.StatusPage{Name: "Scope", Slug: "scope"}
	db.Create(&page)
	inc := models.Incident{StatusPageID: page.ID, Title: "I", Message: "m", Status: "investigating"}
	comp := models.Component{StatusPageID: page.ID, Name: "C"}
	notice := models.GeneralNotice{StatusPageID: page.ID, Title: "N", Message: "m", Level: "info", IsActive: true}
	db.Create(&inc)
	db.Create(&comp)
	db.Create(&notice)
	incTr := models.IncidentTranslation{IncidentID: inc.ID, LanguageCode: "fr", Title: "T", Message: "M"}
	compTr := models.ComponentTranslation{ComponentID: comp.ID, LanguageCode: "fr", Title: "T", Description: "D"}
	noticeTr := models.GeneralNoticeTranslation{GeneralNoticeID: notice.ID, LanguageCode: "fr", Title: "T", Message: "M"}
	db.Create(&incTr)
	db.Create(&compTr)
	db.Create(&noticeTr)

	id := func(v uint) string { return strconv.FormatUint(uint64(v), 10) }
	want := id(page.ID)
	cases := []struct {
		prefix string
		known  uint
	}{
		{"/incident/", inc.ID},
		{"/component/", comp.ID},
		{"/notice/", notice.ID},
		{"/incident-tr/", incTr.ID},
		{"/component-tr/", compTr.ID},
		{"/notice-tr/", noticeTr.ID},
	}
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	for _, c := range cases {
		w := get(c.prefix + id(c.known))
		assert.Equal(t, http.StatusOK, w.Code, c.prefix)
		assert.Equal(t, want, w.Body.String(), c.prefix)
		assert.Equal(t, http.StatusNotFound, get(c.prefix+"999999").Code, c.prefix)
		assert.Equal(t, http.StatusBadRequest, get(c.prefix+"abc").Code, c.prefix)
	}
}
