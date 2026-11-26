package handlers

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("kaputt") }
func (failingBody) Close() error             { return nil }

// The kind of the record (incident/maintenance) determines the resource;
// unreadable or too large bodies abort (separate test).
func TestIncidentResourceFromBody(t *testing.T) {
	h := &Handler{}
	resolve := h.IncidentResourceFromBody()
	ctx := func(body io.ReadCloser) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		c.Request.Body = body
		return c
	}

	assert.Equal(t, resourceMaintenance, resolve(ctx(io.NopCloser(strings.NewReader(`{"IsMaintenance":true}`)))))
	assert.Equal(t, resourceIncidents, resolve(ctx(io.NopCloser(strings.NewReader(`{"IsMaintenance":false}`)))))

	// The body stays readable for the handler.
	c := ctx(io.NopCloser(strings.NewReader(`{"IsMaintenance":true}`)))
	resolve(c)
	again, _ := io.ReadAll(c.Request.Body)
	assert.JSONEq(t, `{"IsMaintenance":true}`, string(again))

	// Invalid IDs fall back to "incidents".
	c = ctx(nil)
	c.Params = gin.Params{{Key: "id", Value: "abc"}, {Key: "translation_id", Value: "abc"}}
	assert.Equal(t, resourceIncidents, h.IncidentResourceByParam("id")(c))
	assert.Equal(t, resourceIncidents, h.IncidentResourceByTranslation()(c))
}

// Translation -> incident -> kind: a maintenance passes "maintenance" on to its translations,
// unknown translations and incidents of another page fall back to "incidents".
func TestIncidentResourceByTranslation_LiestArtAusDerDatenbank(t *testing.T) {
	db := testutils.SetupTestDB()
	page := models.StatusPage{Name: "P", Slug: "p-kind"}
	other := models.StatusPage{Name: "Q", Slug: "q-kind"}
	assert.NoError(t, db.Create(&page).Error)
	assert.NoError(t, db.Create(&other).Error)
	maint := models.Incident{StatusPageID: page.ID, Title: "W", Message: "m", Status: "scheduled", IsMaintenance: true}
	assert.NoError(t, db.Create(&maint).Error)
	tr := models.IncidentTranslation{IncidentID: maint.ID, LanguageCode: "en", Title: "T", Message: "M"}
	assert.NoError(t, db.Create(&tr).Error)
	h := NewHandler(db)

	ctx := func(params ...gin.Param) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Params = params
		return c
	}
	id := strconv.Itoa(int(tr.ID))
	assert.Equal(t, resourceMaintenance, h.IncidentResourceByTranslation()(ctx(gin.Param{Key: "translation_id", Value: id}, gin.Param{Key: "page_id", Value: strconv.Itoa(int(page.ID))})))
	assert.Equal(t, resourceIncidents, h.IncidentResourceByTranslation()(ctx(gin.Param{Key: "translation_id", Value: "9999"})))
	assert.Equal(t, resourceIncidents, h.IncidentResourceByTranslation()(ctx(gin.Param{Key: "translation_id", Value: id}, gin.Param{Key: "page_id", Value: strconv.Itoa(int(other.ID))})), "fremde Seite verrät die Art nicht")
}

// The kind detection itself aborts on read/parse errors and too large
// bodies (400/413) and never silently falls back to "incidents".
func TestIncidentResourceFromBody_BrichtBeiFehlernAb(t *testing.T) {
	h := &Handler{}
	run := func(body io.ReadCloser) (*gin.Context, *httptest.ResponseRecorder) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		c.Request.Body = body
		h.IncidentResourceFromBody()(c)
		return c, rec
	}
	big := strings.NewReader(`{"IsMaintenance":true,"Padding":"` + strings.Repeat("x", maxKindBody) + `"}`)
	for name, tc := range map[string]struct {
		body   io.ReadCloser
		status int
		code   string
	}{
		"zu gross":   {io.NopCloser(big), http.StatusRequestEntityTooLarge, "request_too_large"},
		"kein json":  {io.NopCloser(strings.NewReader("kein json")), http.StatusBadRequest, "invalid_request"},
		"lesefehler": {failingBody{}, http.StatusBadRequest, "invalid_request"},
		"kein body":  {nil, http.StatusBadRequest, "invalid_request"},
	} {
		c, rec := run(tc.body)
		assert.True(t, c.IsAborted(), name)
		assert.Equal(t, tc.status, rec.Code, name)
		assert.Contains(t, rec.Body.String(), tc.code, name)
	}
}

// A database error in the kind detection by ID aborts with 500
// instead of silently assuming "incidents"; "not found" is left to the handler (404).
func TestIncidentResourceByParam_DBFehlerBrichtAb(t *testing.T) {
	db := testutils.SetupTestDB()
	h := &Handler{DB: db}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Params = gin.Params{{Key: "id", Value: "9999"}}
	assert.Equal(t, resourceIncidents, h.IncidentResourceByParam("id")(c))
	assert.False(t, c.IsAborted(), "unbekannter Datensatz: der Handler antwortet mit 404")

	sqlDB, _ := db.DB()
	_ = sqlDB.Close()
	c, rec = func() (*gin.Context, *httptest.ResponseRecorder) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Params = gin.Params{{Key: "id", Value: "9999"}, {Key: "translation_id", Value: "9999"}}
		return c, rec
	}()
	h.IncidentResourceByParam("id")(c)
	assert.True(t, c.IsAborted())
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "server_error")

	c, rec = func() (*gin.Context, *httptest.ResponseRecorder) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Params = gin.Params{{Key: "translation_id", Value: "9999"}}
		return c, rec
	}()
	h.IncidentResourceByTranslation()(c)
	assert.True(t, c.IsAborted())
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
