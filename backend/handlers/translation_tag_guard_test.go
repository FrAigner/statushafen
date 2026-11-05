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
	"github.com/stretchr/testify/require"
)

// TestTranslationTagGuards prüft die Wächter einzeln: kaputte ID (400),
// unbekannte Ressource (404), fremder Tag (403) und die Gegenprobe mit
// passendem Tag bzw. ohne Tag (200). Die Verdrahtung an den echten Routen
// prüft main_translation_tag_access_test.go.
func TestTranslationTagGuards(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)

	page := models.StatusPage{Name: "Wächter", Slug: "tag-guard"}
	require.NoError(t, db.Create(&page).Error)
	eu := models.Tag{Name: "eu"}
	us := models.Tag{Name: "us"}
	require.NoError(t, db.Create(&eu).Error)
	require.NoError(t, db.Create(&us).Error)
	user := models.User{Email: "guard@example.com"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&models.UserTag{UserID: user.ID, TagID: eu.ID}).Error)

	inc := models.Incident{StatusPageID: page.ID, Title: "I", Message: "m", Status: "investigating"}
	comp := models.Component{StatusPageID: page.ID, Name: "C"}
	require.NoError(t, db.Create(&inc).Error)
	require.NoError(t, db.Create(&comp).Error)
	require.NoError(t, db.Create(&models.IncidentTag{IncidentID: inc.ID, TagID: us.ID}).Error)
	require.NoError(t, db.Create(&models.ComponentTag{ComponentID: comp.ID, TagID: us.ID}).Error)
	incTr := models.IncidentTranslation{IncidentID: inc.ID, LanguageCode: "fr", Title: "T", Message: "M"}
	compTr := models.ComponentTranslation{ComponentID: comp.ID, LanguageCode: "fr", Title: "T", Description: "D"}
	require.NoError(t, db.Create(&incTr).Error)
	require.NoError(t, db.Create(&compTr).Error)

	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) { c.Set("userID", user.ID) })
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	r.GET("/incident/:incident_id", h.IncidentTagGuard(), ok)
	r.GET("/component/:component_id", h.ComponentTagGuard(), ok)
	r.GET("/incident-tr/:translation_id", h.IncidentTranslationTagGuard(), ok)
	r.GET("/component-tr/:translation_id", h.ComponentTranslationTagGuard(), ok)

	id := func(v uint) string { return strconv.FormatUint(uint64(v), 10) }
	get := func(path string) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w.Code
	}

	for _, c := range []struct {
		prefix string
		known  uint
	}{
		{"/incident/", inc.ID}, {"/component/", comp.ID},
		{"/incident-tr/", incTr.ID}, {"/component-tr/", compTr.ID},
	} {
		assert.Equal(t, http.StatusForbidden, get(c.prefix+id(c.known)), c.prefix+" fremder Tag")
		assert.Equal(t, http.StatusNotFound, get(c.prefix+"999999"), c.prefix+" unbekannt")
		assert.Equal(t, http.StatusBadRequest, get(c.prefix+"abc"), c.prefix+" ungültige ID")
	}

	// Gegenprobe: derselbe Nutzer darf die Ressource, sobald sie seinen Tag trägt.
	require.NoError(t, db.Create(&models.IncidentTag{IncidentID: inc.ID, TagID: eu.ID}).Error)
	require.NoError(t, db.Create(&models.ComponentTag{ComponentID: comp.ID, TagID: eu.ID}).Error)
	for _, p := range []string{"/incident/" + id(inc.ID), "/component/" + id(comp.ID), "/incident-tr/" + id(incTr.ID), "/component-tr/" + id(compTr.ID)} {
		assert.Equal(t, http.StatusOK, get(p), p+" passender Tag")
	}
}
