package handlers_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Titel und Seitennamen sind auf 200 Zeichen begrenzt (Code title_too_long
// beziehungsweise page_name_too_long). Ohne Grenze wurden Titel bis zum
// Body-Limit gespeichert und sprengten die Admin-Tabellen. Die Grenze zählt
// Zeichen, nicht Bytes, und gilt an allen Stellen, an denen ein Titel
// gespeichert wird.
const (
	titleAtLimit   = 200
	titleOverLimit = 201
)

func TestTitleLength_AllEntryPoints(t *testing.T) {
	h, r, adminTok, _, page, _ := newCodeEnvRouter(t)
	engine := r.(*gin.Engine)
	auth := engine.Group("/api", middleware.AuthMiddleware())
	auth.POST("/pages/:page_id/notices", h.CreateNoticeHandler)
	auth.PUT("/pages/:page_id/notices/:id", h.UpdateNoticeHandler)
	auth.POST("/pages/:page_id/incident-templates", h.CreateIncidentTemplateHandler)
	auth.PUT("/pages/:page_id/incident-templates/:id", h.UpdateIncidentTemplateHandler)
	auth.POST("/incidents/:incident_id/translations", h.CreateIncidentTranslationHandler)
	auth.PUT("/incidents/:incident_id/translations/:language_code", h.SaveIncidentTranslationHandler)
	auth.POST("/notices/:notice_id/translations", h.CreateGeneralNoticeTranslationHandler)
	auth.PUT("/notices/:notice_id/translations/:language_code", h.SaveGeneralNoticeTranslationHandler)
	auth.PUT("/pages/:page_id/incident-templates/:id/translations/:language_code", h.SaveIncidentTemplateTranslationHandler)

	db := h.DB
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	inc := models.Incident{StatusPageID: page.ID, Title: "T", Message: "m", Status: "investigating"}
	require.NoError(t, db.Create(&inc).Error)
	require.NoError(t, db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID}).Error)
	notice := models.GeneralNotice{StatusPageID: page.ID, Title: "N", Message: "n", Level: "info", IsActive: true}
	require.NoError(t, db.Create(&notice).Error)
	tmpl := models.IncidentTemplate{StatusPageID: page.ID, Name: "V", Kind: "incident", Title: "T", Message: "m"}
	require.NoError(t, db.Create(&tmpl).Error)

	id := func(n uint) string { return strconv.Itoa(int(n)) }
	pid := "/api/pages/" + id(page.ID)
	type entry struct {
		name, method, url string
		body              func(title string) map[string]any
	}
	entries := []entry{
		{"Vorfall anlegen", "POST", pid + "/incidents", func(s string) map[string]any {
			return map[string]any{"Title": s, "Message": "m", "Status": "investigating", "ComponentIDs": []uint{comp.ID}}
		}},
		{"Vorfall ändern", "PUT", pid + "/incidents/" + id(inc.ID), func(s string) map[string]any {
			return map[string]any{"title": s, "message": "m", "status": "investigating", "ComponentIDs": []uint{comp.ID}}
		}},
		{"Ankündigung anlegen", "POST", pid + "/notices", func(s string) map[string]any {
			return map[string]any{"Title": s, "Message": "m", "Level": "info"}
		}},
		{"Ankündigung ändern", "PUT", pid + "/notices/" + id(notice.ID), func(s string) map[string]any {
			return map[string]any{"Title": s, "Message": "m", "Level": "info"}
		}},
		{"Vorlage anlegen", "POST", pid + "/incident-templates", func(s string) map[string]any {
			return map[string]any{"Name": "V2", "Title": s, "Message": "m"}
		}},
		{"Vorlage ändern", "PUT", pid + "/incident-templates/" + id(tmpl.ID), func(s string) map[string]any {
			return map[string]any{"Name": "V", "Title": s, "Message": "m"}
		}},
		{"Vorlage anlegen (Name)", "POST", pid + "/incident-templates", func(s string) map[string]any {
			return map[string]any{"Name": s, "Title": "T", "Message": "m"}
		}},
		{"Vorlage ändern (Name)", "PUT", pid + "/incident-templates/" + id(tmpl.ID), func(s string) map[string]any {
			return map[string]any{"Name": s, "Title": "T", "Message": "m"}
		}},
		{"Vorfall-Übersetzung anlegen", "POST", "/api/incidents/" + id(inc.ID) + "/translations", func(s string) map[string]any {
			return map[string]any{"language_code": "fr", "title": s, "message": "m", "source": "manual"}
		}},
		{"Vorfall-Übersetzung speichern", "PUT", "/api/incidents/" + id(inc.ID) + "/translations/fr", func(s string) map[string]any {
			return map[string]any{"title": s, "message": "m", "status": "draft"}
		}},
		{"Ankündigungs-Übersetzung anlegen", "POST", "/api/notices/" + id(notice.ID) + "/translations", func(s string) map[string]any {
			return map[string]any{"language_code": "fr", "title": s, "message": "m", "source": "manual"}
		}},
		{"Ankündigungs-Übersetzung speichern", "PUT", "/api/notices/" + id(notice.ID) + "/translations/fr", func(s string) map[string]any {
			return map[string]any{"title": s, "message": "m", "status": "draft"}
		}},
		{"Vorlagen-Übersetzung speichern", "PUT", pid + "/incident-templates/" + id(tmpl.ID) + "/translations/fr", func(s string) map[string]any {
			return map[string]any{"title": s, "message": "m", "status": "draft"}
		}},
	}
	for _, e := range entries {
		t.Run(e.name, func(t *testing.T) {
			status, out := doJSON(t, r, e.method, e.url, adminTok, e.body(strings.Repeat("ä", titleOverLimit)))
			assert.Equal(t, http.StatusBadRequest, status, "%v", out)
			assert.Equal(t, "title_too_long", out["code"], "%v", out)
			assert.Contains(t, out["error"], "200")
			// An der Grenze (200 Zeichen, auch Mehrbyte-Zeichen) wird nicht wegen der Länge abgelehnt.
			status, out = doJSON(t, r, e.method, e.url, adminTok, e.body(strings.Repeat("ä", titleAtLimit)))
			assert.NotEqual(t, "title_too_long", out["code"], "%v", out)
			assert.Less(t, status, 500)
		})
	}
}

// Seitenname: gleiche Grenze wie die Titel, eigener Code.
func TestPageNameLength_CreateAndUpdate(t *testing.T) {
	h, r, adminTok, _, page, _ := newCodeEnvRouter(t)
	engine := r.(*gin.Engine)
	adm := engine.Group("/api/admin", middleware.AuthMiddleware(), middleware.RequireGlobalAdmin())
	adm.POST("/pages", h.CreateStatusPageHandler)
	adm.PUT("/pages/:page_id", h.UpdateStatusPageHandler)

	status, out := doJSON(t, r, "POST", "/api/admin/pages", adminTok, map[string]any{"Name": strings.Repeat("ä", titleOverLimit), "Slug": "lang-name"})
	assert.Equal(t, http.StatusBadRequest, status, "%v", out)
	assert.Equal(t, "page_name_too_long", out["code"], "%v", out)
	status, out = doJSON(t, r, "POST", "/api/admin/pages", adminTok, map[string]any{"Name": strings.Repeat("ä", titleAtLimit), "Slug": "grenz-name"})
	assert.Equal(t, http.StatusCreated, status, "%v", out)

	status, out = doJSON(t, r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID)), adminTok, map[string]any{"Name": strings.Repeat("ä", titleOverLimit), "Slug": "p-613"})
	assert.Equal(t, http.StatusBadRequest, status, "%v", out)
	assert.Equal(t, "page_name_too_long", out["code"], "%v", out)
	status, out = doJSON(t, r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID)), adminTok, map[string]any{"Name": strings.Repeat("ä", titleAtLimit), "Slug": "p-613"})
	assert.Equal(t, http.StatusOK, status, "%v", out)
}
