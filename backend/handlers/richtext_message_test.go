package handlers_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/richtext"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Formatierte Nachrichten. Dieser Test deckt die Längengrenze
// (5.000 Zeichen, Code message_too_long) an allen Stellen ab, an denen eine
// Nachricht gespeichert wird.

const (
	msgAtLimit   = 5000
	msgOverLimit = 5001
)

func TestMessageLength_AllEntryPoints(t *testing.T) {
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
	auth.PUT("/pages/:page_id/incidents/:id/updates/:update_id/translations/:language_code", h.SaveIncidentUpdateTranslationHandler)
	auth.PUT("/pages/:page_id/incident-templates/:id/translations/:language_code", h.SaveIncidentTemplateTranslationHandler)

	db := h.DB
	comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	inc := models.Incident{StatusPageID: page.ID, Title: "T", Message: "m", Status: "investigating"}
	require.NoError(t, db.Create(&inc).Error)
	require.NoError(t, db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID}).Error)
	upd := models.IncidentUpdate{IncidentID: inc.ID, Status: "investigating", Message: "u"}
	require.NoError(t, db.Create(&upd).Error)
	notice := models.GeneralNotice{StatusPageID: page.ID, Title: "N", Message: "n", Level: "info", IsActive: true}
	require.NoError(t, db.Create(&notice).Error)
	tmpl := models.IncidentTemplate{StatusPageID: page.ID, Name: "V", Kind: "incident", Title: "T", Message: "m"}
	require.NoError(t, db.Create(&tmpl).Error)

	id := func(n uint) string { return strconv.Itoa(int(n)) }
	pid := "/api/pages/" + id(page.ID)
	type entry struct {
		name, method, url string
		body              func(msg string) map[string]any
	}
	entries := []entry{
		{"Vorfall anlegen", "POST", pid + "/incidents", func(m string) map[string]any {
			return map[string]any{"Title": "T", "Message": m, "Status": "investigating", "ComponentIDs": []uint{comp.ID}}
		}},
		{"Vorfall ändern", "PUT", pid + "/incidents/" + id(inc.ID), func(m string) map[string]any {
			return map[string]any{"message": m, "status": "investigating", "ComponentIDs": []uint{comp.ID}}
		}},
		{"Vorfall-Update", "POST", pid + "/incidents/" + id(inc.ID) + "/updates", func(m string) map[string]any {
			return map[string]any{"status": "investigating", "message": m}
		}},
		{"Ankündigung anlegen", "POST", pid + "/notices", func(m string) map[string]any {
			return map[string]any{"Title": "N", "Message": m, "Level": "info"}
		}},
		{"Ankündigung ändern", "PUT", pid + "/notices/" + id(notice.ID), func(m string) map[string]any {
			return map[string]any{"Title": "N", "Message": m, "Level": "info"}
		}},
		{"Vorlage anlegen", "POST", pid + "/incident-templates", func(m string) map[string]any {
			return map[string]any{"Name": "V2", "Title": "T", "Message": m}
		}},
		{"Vorlage ändern", "PUT", pid + "/incident-templates/" + id(tmpl.ID), func(m string) map[string]any {
			return map[string]any{"Name": "V", "Title": "T", "Message": m}
		}},
		{"Vorfall-Übersetzung anlegen", "POST", "/api/incidents/" + id(inc.ID) + "/translations", func(m string) map[string]any {
			return map[string]any{"language_code": "fr", "title": "T", "message": m, "source": "manual"}
		}},
		{"Vorfall-Übersetzung speichern", "PUT", "/api/incidents/" + id(inc.ID) + "/translations/fr", func(m string) map[string]any {
			return map[string]any{"title": "T", "message": m, "status": "draft"}
		}},
		{"Ankündigungs-Übersetzung anlegen", "POST", "/api/notices/" + id(notice.ID) + "/translations", func(m string) map[string]any {
			return map[string]any{"language_code": "fr", "title": "T", "message": m, "source": "manual"}
		}},
		{"Ankündigungs-Übersetzung speichern", "PUT", "/api/notices/" + id(notice.ID) + "/translations/fr", func(m string) map[string]any {
			return map[string]any{"title": "T", "message": m, "status": "draft"}
		}},
		{"Update-Übersetzung speichern", "PUT", pid + "/incidents/" + id(inc.ID) + "/updates/" + id(upd.ID) + "/translations/fr", func(m string) map[string]any {
			return map[string]any{"message": m, "status": "draft"}
		}},
		{"Vorlagen-Übersetzung speichern", "PUT", pid + "/incident-templates/" + id(tmpl.ID) + "/translations/fr", func(m string) map[string]any {
			return map[string]any{"title": "T", "message": m, "status": "draft"}
		}},
	}
	for _, e := range entries {
		t.Run(e.name, func(t *testing.T) {
			status, out := doJSON(t, r, e.method, e.url, adminTok, e.body(strings.Repeat("ä", msgOverLimit)))
			assert.Equal(t, http.StatusBadRequest, status, "%v", out)
			assert.Equal(t, "message_too_long", out["code"], "%v", out)
			// An der Grenze (5.000 Zeichen, auch Mehrbyte-Zeichen) wird nicht wegen der Länge abgelehnt.
			status, out = doJSON(t, r, e.method, e.url, adminTok, e.body(strings.Repeat("ä", msgAtLimit)))
			assert.NotEqual(t, "message_too_long", out["code"], "%v", out)
			assert.Less(t, status, 500)
		})
	}
}

func TestMessageLength_ConstantMatchesFrontendAndDocs(t *testing.T) {
	assert.Equal(t, 5000, richtext.MaxLen)
}

func TestRequireMessageLength_ErrorTextNamesLimit(t *testing.T) {
	_, r, adminTok, _, page, _ := newCodeEnvRouter(t)
	status, out := doJSON(t, r, "POST", "/api/pages/"+strconv.Itoa(int(page.ID))+"/incidents", adminTok,
		map[string]any{"Title": "T", "Message": strings.Repeat("x", msgOverLimit), "ComponentIDs": []uint{1}})
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, out["error"], "5000")
}
