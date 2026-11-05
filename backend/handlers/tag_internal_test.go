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

// Fehlerpfade der Tag-Verwaltung: kaputte Eingaben und Datenbankfehler führen zu
// einer Antwort mit Fehlercode, ohne rohen Fehlertext, und ändern nichts, was
// nicht schon geschrieben war.
func TestTagHandlers_ErrorPaths(t *testing.T) {
	type call func(h *Handler, c *gin.Context)
	tagID := func(id uint) gin.Params { return gin.Params{{Key: "id", Value: strconv.Itoa(int(id))}} }

	cases := []struct {
		name       string
		method     string
		body       string
		query      string
		params     func(tag models.Tag, page models.StatusPage) gin.Params
		op, table  string
		skip       int
		call       call
		wantStatus int
		wantCode   string
	}{
		{name: "Liste: ungültige Seiten-ID", method: "GET", query: "status_page_id=abc", call: (*Handler).GetTagsHandler, wantStatus: 400, wantCode: "invalid_id"},
		{name: "Liste: Lesen scheitert", method: "GET", op: "query", table: "tags", call: (*Handler).GetTagsHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Seitenliste: Lesen scheitert", method: "GET", op: "query", table: "tags", call: (*Handler).GetPageTagsHandler, wantStatus: 500, wantCode: "server_error",
			params: func(_ models.Tag, p models.StatusPage) gin.Params {
				return gin.Params{{Key: "page_id", Value: strconv.Itoa(int(p.ID))}}
			}},
		{name: "Anlegen: kaputter Body", method: "POST", body: "{", call: (*Handler).CreateTagHandler, wantStatus: 400, wantCode: "invalid_request"},
		{name: "Anlegen: Duplikatprüfung scheitert", method: "POST", body: `{"name":"neu"}`, op: "query", table: "tags", call: (*Handler).CreateTagHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Anlegen: Speichern scheitert", method: "POST", body: `{"name":"neu"}`, op: "create", table: "tags", call: (*Handler).CreateTagHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Ändern: kaputter Body", method: "PUT", body: "{", params: func(tg models.Tag, _ models.StatusPage) gin.Params { return tagID(tg.ID) }, call: (*Handler).UpdateTagHandler, wantStatus: 400, wantCode: "invalid_request"},
		{name: "Ändern: Duplikatprüfung scheitert", method: "PUT", body: `{"name":"anders"}`, op: "query", table: "tags", skip: 1,
			params: func(tg models.Tag, _ models.StatusPage) gin.Params { return tagID(tg.ID) }, call: (*Handler).UpdateTagHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Ändern: Speichern scheitert", method: "PUT", body: `{"name":"anders"}`, op: "update", table: "tags",
			params: func(tg models.Tag, _ models.StatusPage) gin.Params { return tagID(tg.ID) }, call: (*Handler).UpdateTagHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Löschen: Komponentenverknüpfung scheitert", method: "DELETE", op: "delete", table: "component_tags",
			params: func(tg models.Tag, _ models.StatusPage) gin.Params { return tagID(tg.ID) }, call: (*Handler).DeleteTagHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Löschen: Vorfallverknüpfung scheitert", method: "DELETE", op: "delete", table: "incident_tags",
			params: func(tg models.Tag, _ models.StatusPage) gin.Params { return tagID(tg.ID) }, call: (*Handler).DeleteTagHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Löschen: Nutzerverknüpfung scheitert", method: "DELETE", op: "delete", table: "user_tags",
			params: func(tg models.Tag, _ models.StatusPage) gin.Params { return tagID(tg.ID) }, call: (*Handler).DeleteTagHandler, wantStatus: 500, wantCode: "server_error"},
		{name: "Löschen: Tag löschen scheitert", method: "DELETE", op: "delete", table: "tags",
			params: func(tg models.Tag, _ models.StatusPage) gin.Params { return tagID(tg.ID) }, call: (*Handler).DeleteTagHandler, wantStatus: 500, wantCode: "server_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := testutils.SetupTestDB()
			h := NewHandler(db)
			page := models.StatusPage{Name: "P", Slug: "p-tag-err"}
			require.NoError(t, db.Create(&page).Error)
			tag := models.Tag{Name: "vorhanden", StatusPageID: &page.ID}
			require.NoError(t, db.Create(&tag).Error)
			other := models.Tag{Name: "anderer", StatusPageID: &page.ID}
			require.NoError(t, db.Create(&other).Error)

			c, w := pageTestContext(tc.method, tc.body, uint(1))
			if tc.query != "" {
				c.Request.URL.RawQuery = tc.query
			}
			if tc.params != nil {
				c.Params = tc.params(tag, page)
			}
			if tc.op != "" {
				injectDBError(t, db, tc.op, tc.table, tc.skip)
			}
			tc.call(h, c)

			assert.Equal(t, tc.wantStatus, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"code":"`+tc.wantCode+`"`)
			assert.NotContains(t, w.Body.String(), "injizierter")
		})
	}
}

// Beim Umbenennen eines globalen Tags zählt nur der globale Namensraum: Ein
// gleichnamiger seitengebundener Tag blockiert nicht, ein gleichnamiger
// globaler schon.
func TestUpdateTagHandler_GlobalScopeDuplicateCheck(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "P", Slug: "p-tag-scope2"}
	require.NoError(t, db.Create(&page).Error)
	mkTag := func(name string, pageID *uint) models.Tag {
		tg := models.Tag{Name: name, StatusPageID: pageID}
		require.NoError(t, db.Create(&tg).Error)
		return tg
	}
	g1, _ := mkTag("eu", nil), mkTag("us", nil)
	mkTag("apac", &page.ID)

	rename := func(id uint, name string) int {
		c, w := pageTestContext(http.MethodPut, `{"name":"`+name+`"}`, uint(1))
		c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(int(id))}}
		h.UpdateTagHandler(c)
		return w.Code
	}
	assert.Equal(t, http.StatusOK, rename(g1.ID, "APAC"), "seitengebundener Tag gleichen Namens blockiert nicht")
	assert.Equal(t, http.StatusConflict, rename(g1.ID, "US"), "globaler Tag gleichen Namens blockiert")
}
