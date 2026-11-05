package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fehlerpfade, die über die echte Routenkette schwer zu erreichen
// sind (fehlende Pfadparameter, ungültige IDs, Datenbankfehler), werden mit
// direkt registrierten Handlern geprüft. Jede Antwort trägt `code` und den
// Rückfalltext `error`, aber nie den rohen Fehlertext.

type abdeckungCase struct {
	name, method, url string
	body              any
	status            int
	code              string
	failOp, failTable string // optional: DB-Operation, die vorher scheitert
	token             string // optional: Anmeldung statt Global Admin
	prepare           func()
}

func TestFehlercodes_Abdeckung(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	g := r.Group("/t", middleware.AuthMiddleware())

	// Pfadparameter fehlen (Route ohne :param) -> parameter_missing.
	g.GET("/np/comptrans", h.GetComponentTranslationsHandler)
	g.GET("/np/noticetrans", h.GetGeneralNoticeTranslationsHandler)
	g.GET("/np/inctrans", h.GetIncidentTranslationsHandler)
	g.PATCH("/np/transconfirm", h.ConfirmIncidentTranslationHandler)
	g.GET("/np/tpl", h.GetIncidentTemplatesHandler)
	g.GET("/np/subs", h.GetSubscribersHandler)
	g.POST("/np/subs", h.CreateSubscriberAdminHandler)
	g.PUT("/np/subs/:id", h.UpdateSubscriberHandler)
	g.DELETE("/np/subs/:id", h.DeleteSubscriberHandler)
	// Seiten-ID keine Zahl.
	g.PUT("/pg/:page_id/subs/:id", h.UpdateSubscriberHandler)
	g.DELETE("/pg/:page_id/subs/:id", h.DeleteSubscriberHandler)
	g.GET("/pg/:page_id/notices", h.GetActiveNoticesHandler)
	g.GET("/pg/:page_id/inc/:id", h.GetIncidentDetailHandler)
	g.POST("/pg/:page_id/inc/:id/updates", h.AddIncidentUpdateHandler)
	g.PUT("/pg/:page_id/inc/:id/pm", h.UpsertPostMortemHandler)
	g.POST("/pg/:page_id/tpl", h.CreateIncidentTemplateHandler)
	g.PUT("/pg/:page_id/domain", h.SetCustomDomainHandler)
	g.DELETE("/pg/:page_id/domain", h.DeleteCustomDomainHandler)
	g.POST("/pg/:page_id/domain/verify", h.VerifyCustomDomainHandler)
	g.GET("/pg/:page_id/sso", h.GetSSO)
	g.PUT("/pg/:page_id/sso", h.UpdateSSO)
	g.DELETE("/pg/:page_id/sso", h.DeleteSSO)
	g.PUT("/pg/:page_id/langs", h.PutPageLanguagesHandler)
	g.PUT("/pg/:page_id/uio", h.PutPageUiOverrideHandler)
	g.PUT("/pg/:page_id", h.UpdateStatusPageHandler)
	g.POST("/pg/:page_id/users", h.AddStatusPageUserHandler)
	g.POST("/pg", h.CreateStatusPageHandler)
	g.POST("/pg/:page_id/webhook-token", h.GenerateWebhookTokenHandler)
	// Übersetzungen (Komponente, Ankündigung, Vorfall).
	for _, p := range []struct {
		name, param string
		get, create gin.HandlerFunc
		confirm     gin.HandlerFunc
		del         gin.HandlerFunc
	}{
		{"comp", "component_id", h.GetComponentTranslationsHandler, h.CreateComponentTranslationHandler, h.ConfirmComponentTranslationHandler, h.DeleteComponentTranslationHandler},
		{"notice", "notice_id", h.GetGeneralNoticeTranslationsHandler, h.CreateGeneralNoticeTranslationHandler, h.ConfirmGeneralNoticeTranslationHandler, h.DeleteGeneralNoticeTranslationHandler},
		{"inc", "incident_id", h.GetIncidentTranslationsHandler, h.CreateIncidentTranslationHandler, h.ConfirmIncidentTranslationHandler, h.DeleteIncidentTranslationHandler},
	} {
		g.GET("/tr/"+p.name+"/:"+p.param, p.get)
		g.POST("/tr/"+p.name+"/:"+p.param, p.create)
		g.PATCH("/trx/"+p.name+"/:translation_id/confirm", p.confirm)
		g.DELETE("/trx/"+p.name+"/:translation_id", p.del)
	}
	g.POST("/roles", h.CreateRoleHandler)
	g.PUT("/roles/:id", h.UpdateRoleHandler)
	g.GET("/overview", h.GetOverviewHandler)
	g.GET("/audit", h.GetAuditLogsHandler)
	g.PUT("/uio", h.PutGlobalUiOverrideHandler)
	g.DELETE("/uio", h.DeleteGlobalUiOverrideHandler)
	g.GET("/uio", h.GetGlobalUiOverridesHandler)
	g.GET("/pg/:page_id/uio", h.GetPageUiOverridesHandler)
	g.DELETE("/pg/:page_id/uio", h.DeletePageUiOverrideHandler)
	g.POST("/tags", h.CreateTagHandler)
	g.PUT("/tags/:id", h.UpdateTagHandler)
	g.DELETE("/pg/:page_id", h.DeleteStatusPageHandler)
	g.GET("/me/pages", h.GetMyPagesHandler)
	g.GET("/dash/:slug", h.GetPublicDashboardHandler)
	r.POST("/t/unsub", h.UnsubscribeHandler)
	r.POST("/t/order-confirm", h.ConfirmOrderHandler)
	r.GET("/t/oauth/:provider/callback", h.OAuthCallbackHandler)

	page := models.StatusPage{Name: "P", Slug: "abd-p"}
	require.NoError(t, db.Create(&page).Error)
	pid := strconv.Itoa(int(page.ID))
	comp := models.Component{StatusPageID: page.ID, Name: "K", Status: "operational"}
	require.NoError(t, db.Create(&comp).Error)
	cid := strconv.Itoa(int(comp.ID))
	notice := models.GeneralNotice{StatusPageID: page.ID, Title: "N", Message: "m", Level: "info", IsActive: true}
	require.NoError(t, db.Create(&notice).Error)
	nid := strconv.Itoa(int(notice.ID))
	orphan := models.GeneralNotice{StatusPageID: 987654, Title: "O", Message: "m", Level: "info"}
	require.NoError(t, db.Create(&orphan).Error)
	oid := strconv.Itoa(int(orphan.ID))
	inc := models.Incident{StatusPageID: page.ID, Title: "I", Message: "m", Status: "investigating"}
	require.NoError(t, db.Create(&inc).Error)
	iid := strconv.Itoa(int(inc.ID))
	ct := models.ComponentTranslation{ComponentID: comp.ID, LanguageCode: "en", Title: "C", Status: "draft"}
	nt := models.GeneralNoticeTranslation{GeneralNoticeID: notice.ID, LanguageCode: "en", Title: "N", Message: "m", Status: "draft"}
	it := models.IncidentTranslation{IncidentID: inc.ID, LanguageCode: "en", Title: "I", Status: "draft"}
	require.NoError(t, db.Create(&ct).Error)
	require.NoError(t, db.Create(&nt).Error)
	require.NoError(t, db.Create(&it).Error)
	ctid, ntid, itid := strconv.Itoa(int(ct.ID)), strconv.Itoa(int(nt.ID)), strconv.Itoa(int(it.ID))
	sub := models.Subscriber{StatusPageID: page.ID, Email: "s@example.com", Channel: "email", SubscribedComponentIDs: "all", Status: "active"}
	require.NoError(t, db.Create(&sub).Error)
	unsubTok, _ := middleware.GenerateSubscriberToken(sub.ID, page.ID)
	require.NoError(t, db.Create(&models.StatusPageSSOConfig{StatusPageID: page.ID}).Error)
	gaUser := models.User{Email: "ga-abd@example.com", IsGlobalAdmin: true}
	require.NoError(t, db.Create(&gaUser).Error)
	tok, _ := middleware.GenerateToken(gaUser.ID, gaUser.Email, "", "", true)
	// Token eines Nutzers, der danach gelöscht wird -> GetMyPages: "User not found".
	ghost := models.User{Email: "ghost-abd@example.com"}
	require.NoError(t, db.Create(&ghost).Error)
	ghostTok, _ := middleware.GenerateToken(ghost.ID, ghost.Email, "", "", false)
	require.NoError(t, db.Unscoped().Delete(&ghost).Error)
	// Bestellung, deren Wunsch-Slug inzwischen von einer Seite belegt ist.
	future := time.Now().Add(time.Hour)
	require.NoError(t, db.Create(&models.Role{Name: "Page Admin", Permissions: "pages:write"}).Error)
	require.NoError(t, db.Create(&models.PageOrder{CompanyName: "Z", Email: "z-abd@example.com", Slug: "abd-p", Status: "pending_confirmation", ConfirmTokenHash: models.HashToken("abd-tok"), ConfirmTokenExpiry: &future}).Error)

	// Nutzer mit Tag-Einschränkung sieht Vorfälle fremder Tags nicht (403).
	tagA, tagB := models.Tag{Name: "abd-a"}, models.Tag{Name: "abd-b"}
	require.NoError(t, db.Create(&tagA).Error)
	require.NoError(t, db.Create(&tagB).Error)
	require.NoError(t, db.Create(&models.IncidentTag{IncidentID: inc.ID, TagID: tagA.ID}).Error)
	restricted := models.User{Email: "tagged-abd@example.com"}
	require.NoError(t, db.Create(&restricted).Error)
	require.NoError(t, db.Create(&models.UserTag{UserID: restricted.ID, TagID: tagB.ID}).Error)
	restrictedTok, _ := middleware.GenerateToken(restricted.ID, restricted.Email, "", "", false)

	trBody := map[string]any{"language_code": "de", "title": "T", "message": "m", "description": "d", "source": "manual"}
	cases := []abdeckungCase{
		// parameter_missing / invalid_id
		{name: "Vorfall-Update: Tag-Einschränkung", method: "POST", url: "/t/pg/" + pid + "/inc/" + iid + "/updates", body: map[string]any{"status": "investigating", "message": "m"}, status: 403, code: "forbidden", token: restrictedTok},
		{name: "Postmortem: Tag-Einschränkung", method: "PUT", url: "/t/pg/" + pid + "/inc/" + iid + "/pm", body: map[string]any{"summary": "s"}, status: 403, code: "forbidden", token: restrictedTok},
		{name: "Ankündigungs-Übersetzungen: Ankündigung unbekannt", method: "GET", url: "/t/tr/notice/99999", status: 404, code: "notice_not_found"},
		{name: "Komponenten-Übersetzung ohne component_id", method: "GET", url: "/t/np/comptrans", status: 400, code: "parameter_missing"},
		{name: "Ankündigungs-Übersetzung ohne notice_id", method: "GET", url: "/t/np/noticetrans", status: 400, code: "parameter_missing"},
		{name: "Vorfall-Übersetzung ohne incident_id", method: "GET", url: "/t/np/inctrans", status: 400, code: "parameter_missing"},
		{name: "Übersetzung bestätigen ohne translation_id", method: "PATCH", url: "/t/np/transconfirm", status: 400, code: "parameter_missing"},
		{name: "Vorlagen ohne page_id", method: "GET", url: "/t/np/tpl", status: 400, code: "parameter_missing"},
		{name: "Abonnenten ohne page_id", method: "GET", url: "/t/np/subs", status: 400, code: "parameter_missing"},
		{name: "Abonnent anlegen ohne page_id", method: "POST", url: "/t/np/subs", body: map[string]any{}, status: 400, code: "parameter_missing"},
		{name: "Abonnent ändern ohne page_id", method: "PUT", url: "/t/np/subs/1", body: map[string]any{}, status: 400, code: "parameter_missing"},
		{name: "Abonnent löschen ohne page_id", method: "DELETE", url: "/t/np/subs/1", status: 400, code: "parameter_missing"},
		{name: "Abonnent ändern: page_id keine Zahl", method: "PUT", url: "/t/pg/abc/subs/1", body: map[string]any{}, status: 400, code: "invalid_id"},
		{name: "Abonnent löschen: page_id keine Zahl", method: "DELETE", url: "/t/pg/abc/subs/1", status: 400, code: "invalid_id"},
		{name: "Vorfall: Seite keine Zahl", method: "GET", url: "/t/pg/abc/inc/1", status: 400, code: "invalid_id"},
		{name: "Vorfall: ID keine Zahl", method: "GET", url: "/t/pg/" + pid + "/inc/abc", status: 400, code: "invalid_id"},
		{name: "Vorfall-Update: ID keine Zahl", method: "POST", url: "/t/pg/" + pid + "/inc/abc/updates", body: map[string]any{}, status: 400, code: "invalid_id"},
		{name: "Postmortem: ID keine Zahl", method: "PUT", url: "/t/pg/" + pid + "/inc/abc/pm", body: map[string]any{}, status: 400, code: "invalid_id"},
		{name: "Vorfall-Update: Vorfall unbekannt", method: "POST", url: "/t/pg/" + pid + "/inc/99999/updates", body: map[string]any{}, status: 404, code: "incident_not_found"},
		{name: "Postmortem: Vorfall unbekannt", method: "PUT", url: "/t/pg/" + pid + "/inc/99999/pm", body: map[string]any{}, status: 404, code: "incident_not_found"},
		{name: "Vorfall: unbekannt", method: "GET", url: "/t/pg/" + pid + "/inc/99999", status: 404, code: "incident_not_found"},
		{name: "Vorfall-Update: Body ungültig", method: "POST", url: "/t/pg/" + pid + "/inc/" + iid + "/updates", body: map[string]any{}, status: 400, code: "invalid_request"},
		{name: "Postmortem: Body ungültig", method: "PUT", url: "/t/pg/" + pid + "/inc/" + iid + "/pm", body: "kein json", status: 400, code: "invalid_request"},
		{name: "Vorlage anlegen: Body ungültig", method: "POST", url: "/t/pg/" + pid + "/tpl", body: map[string]any{}, status: 400, code: "invalid_request"},
		{name: "Rolle ändern: Body ungültig", method: "PUT", url: "/t/roles/1", body: map[string]any{}, status: 400, code: "invalid_request", prepare: func() { _ = db.Create(&models.Role{Name: "Rx"}) }},
		{name: "Override setzen: Body ungültig", method: "PUT", url: "/t/uio", body: "kein json", status: 400, code: "invalid_request"},
		{name: "Seiten-Sprachen: leer", method: "PUT", url: "/t/pg/" + pid + "/langs", body: map[string]any{"codes": []string{}, "default": "de"}, status: 400, code: "language_none_enabled"},
		{name: "Tag anlegen: Name leer", method: "POST", url: "/t/tags", body: map[string]any{"name": "  "}, status: 400, code: "name_required"},
		{name: "Tag anlegen: Seite unbekannt", method: "POST", url: "/t/tags", body: map[string]any{"name": "t", "status_page_id": 99999}, status: 400, code: "page_not_found"},
		{name: "Tag ändern: Name leer", method: "PUT", url: "/t/tags/1", body: map[string]any{"name": " "}, status: 400, code: "name_required", prepare: func() { _ = db.Create(&models.Tag{Name: "vorhanden-abd"}) }},
		{name: "Seite ändern: Slug ungültig", method: "PUT", url: "/t/pg/" + pid, body: map[string]any{"Name": "P", "Slug": "Ungültig!", "PageLayout": "standard"}, status: 400, code: "slug_invalid"},
		{name: "Seite ändern: Branding ungültig", method: "PUT", url: "/t/pg/" + pid, body: map[string]any{"Name": "P", "Slug": "abd-p", "PrimaryColor": "rot", "PageLayout": "standard"}, status: 400, code: "invalid_request"},
		{name: "Seite ändern: Absender ungültig", method: "PUT", url: "/t/pg/" + pid, body: map[string]any{"Name": "P", "Slug": "abd-p", "NotificationFromAddress": "kein-absender", "PageLayout": "standard"}, status: 400, code: "invalid_request"},
		{name: "Seite anlegen: Absender ungültig", method: "POST", url: "/t/pg", body: map[string]any{"Name": "Neu", "Slug": "abd-neu", "NotificationFromAddress": "kein-absender"}, status: 400, code: "invalid_request"},
		{name: "SSO lesen: ID keine Zahl", method: "GET", url: "/t/pg/abc/sso", status: 400, code: "invalid_id"},
		{name: "SSO lesen: Seite unbekannt", method: "GET", url: "/t/pg/99999/sso", status: 404, code: "page_not_found"},
		{name: "SSO ändern: ID keine Zahl", method: "PUT", url: "/t/pg/abc/sso", body: map[string]any{}, status: 400, code: "invalid_id"},
		{name: "SSO ändern: Seite unbekannt", method: "PUT", url: "/t/pg/99999/sso", body: map[string]any{}, status: 404, code: "page_not_found"},
		{name: "SSO ändern: Body ungültig", method: "PUT", url: "/t/pg/" + pid + "/sso", body: "kein json", status: 400, code: "invalid_request"},
		{name: "SSO löschen: ID keine Zahl", method: "DELETE", url: "/t/pg/abc/sso", status: 400, code: "invalid_id"},
		{name: "SSO löschen: Seite unbekannt", method: "DELETE", url: "/t/pg/99999/sso", status: 404, code: "page_not_found"},
		{name: "Eigene Domain setzen: ID keine Zahl", method: "PUT", url: "/t/pg/abc/domain", body: map[string]any{}, status: 400, code: "invalid_id"},
		{name: "Eigene Domain setzen: Seite unbekannt", method: "PUT", url: "/t/pg/99999/domain", body: map[string]any{}, status: 404, code: "page_not_found"},
		{name: "Eigene Domain setzen: Body ungültig", method: "PUT", url: "/t/pg/" + pid + "/domain", body: "kein json", status: 400, code: "invalid_request"},
		{name: "Dashboard: Seite unbekannt", method: "GET", url: "/t/dash/gibt-es-nicht", status: 404, code: "page_not_found"},
		{name: "Seite als Nutzer ohne Konto", method: "GET", url: "/t/me/pages", status: 401, code: "unauthorized"},
		{name: "Bestellung bestätigen: Slug inzwischen belegt", method: "POST", url: "/t/order-confirm", body: map[string]any{"token": "abd-tok"}, status: 409, code: "slug_taken"},
		{name: "Abbestellen: Löschen scheitert", method: "POST", url: "/t/unsub?token=" + unsubTok, status: 500, code: "server_error", failOp: "delete", failTable: "subscribers"},
		// 500: Datenbankfehler
		{name: "Audit-Log lesen scheitert", method: "GET", url: "/t/audit", status: 500, code: "server_error", failOp: "query", failTable: "audit_logs"},
		{name: "Übersicht lesen scheitert", method: "GET", url: "/t/overview", status: 500, code: "server_error", failOp: "query", failTable: "status_pages"},
		{name: "Aktive Ankündigungen lesen scheitert", method: "GET", url: "/t/pg/" + pid + "/notices", status: 500, code: "server_error", failOp: "query", failTable: "general_notices"},
		{name: "Komponenten-Übersetzungen lesen scheitert", method: "GET", url: "/t/tr/comp/" + cid, status: 500, code: "server_error", failOp: "query", failTable: "component_translations"},
		{name: "Ankündigungs-Übersetzungen lesen scheitert", method: "GET", url: "/t/tr/notice/" + nid, status: 500, code: "server_error", failOp: "query", failTable: "general_notice_translations"},
		{name: "Vorfall-Übersetzungen lesen scheitert", method: "GET", url: "/t/tr/inc/" + iid, status: 500, code: "server_error", failOp: "query", failTable: "incident_translations"},
		{name: "Ankündigungs-Übersetzungen: Seite fehlt", method: "GET", url: "/t/tr/notice/" + oid, status: 404, code: "notice_not_found"},
		{name: "Komponenten-Übersetzung anlegen scheitert", method: "POST", url: "/t/tr/comp/" + cid, body: trBody, status: 500, code: "server_error", failOp: "create", failTable: "component_translations"},
		{name: "Ankündigungs-Übersetzung anlegen scheitert", method: "POST", url: "/t/tr/notice/" + nid, body: trBody, status: 500, code: "server_error", failOp: "create", failTable: "general_notice_translations"},
		{name: "Vorfall-Übersetzung anlegen scheitert", method: "POST", url: "/t/tr/inc/" + iid, body: trBody, status: 500, code: "server_error", failOp: "create", failTable: "incident_translations"},
		{name: "Komponenten-Übersetzung bestätigen scheitert", method: "PATCH", url: "/t/trx/comp/" + ctid + "/confirm", status: 500, code: "server_error", failOp: "update", failTable: "component_translations"},
		{name: "Ankündigungs-Übersetzung bestätigen scheitert", method: "PATCH", url: "/t/trx/notice/" + ntid + "/confirm", status: 500, code: "server_error", failOp: "update", failTable: "general_notice_translations"},
		{name: "Vorfall-Übersetzung bestätigen scheitert", method: "PATCH", url: "/t/trx/inc/" + itid + "/confirm", status: 500, code: "server_error", failOp: "update", failTable: "incident_translations"},
		{name: "Komponenten-Übersetzung löschen scheitert", method: "DELETE", url: "/t/trx/comp/" + ctid, status: 500, code: "server_error", failOp: "delete", failTable: "component_translations"},
		{name: "Ankündigungs-Übersetzung löschen scheitert", method: "DELETE", url: "/t/trx/notice/" + ntid, status: 500, code: "server_error", failOp: "delete", failTable: "general_notice_translations"},
		{name: "Vorfall-Übersetzung löschen scheitert", method: "DELETE", url: "/t/trx/inc/" + itid, status: 500, code: "server_error", failOp: "delete", failTable: "incident_translations"},
		{name: "Vorfall-Update anlegen scheitert", method: "POST", url: "/t/pg/" + pid + "/inc/" + iid + "/updates", body: map[string]any{"status": "investigating", "message": "m"}, status: 500, code: "server_error", failOp: "create", failTable: "incident_updates"},
		{name: "Postmortem speichern scheitert", method: "PUT", url: "/t/pg/" + pid + "/inc/" + iid + "/pm", body: map[string]any{"summary": "s"}, status: 500, code: "server_error", failOp: "create", failTable: "post_mortems"},
		{name: "Vorlage anlegen scheitert", method: "POST", url: "/t/pg/" + pid + "/tpl", body: map[string]any{"Name": "V", "Title": "T", "Message": "m"}, status: 500, code: "server_error", failOp: "create", failTable: "incident_templates"},
		{name: "Rolle anlegen scheitert", method: "POST", url: "/t/roles", body: map[string]any{"name": "NeuAbd"}, status: 500, code: "server_error", failOp: "create", failTable: "roles"},
		{name: "Rolle ändern scheitert", method: "PUT", url: "/t/roles/1", body: map[string]any{"name": "Geändert"}, status: 500, code: "server_error", failOp: "update", failTable: "roles"},
		{name: "Override anlegen scheitert", method: "PUT", url: "/t/uio", body: map[string]any{"language_code": "de", "key": "a.b", "value": "x"}, status: 500, code: "server_error", failOp: "create", failTable: "ui_translation_overrides"},
		{name: "Override ändern scheitert", method: "PUT", url: "/t/uio", body: map[string]any{"language_code": "de", "key": "a.c", "value": "y"}, status: 500, code: "server_error", failOp: "update", failTable: "ui_translation_overrides",
			prepare: func() {
				_ = db.Create(&models.UiTranslationOverride{LanguageCode: "de", Key: "a.c", Value: "alt"})
			}},
		{name: "Override löschen scheitert", method: "DELETE", url: "/t/uio?language_code=de&key=a.c", status: 500, code: "server_error", failOp: "delete", failTable: "ui_translation_overrides"},
		{name: "Overrides lesen scheitert", method: "GET", url: "/t/uio?language_code=de", status: 500, code: "server_error", failOp: "query", failTable: "ui_translation_overrides"},
		{name: "Overrides ohne Sprache", method: "GET", url: "/t/uio", status: 400, code: "override_language_required"},
		{name: "Override löschen ohne Angaben", method: "DELETE", url: "/t/uio", status: 400, code: "override_fields_required"},
		{name: "Seite anlegen scheitert", method: "POST", url: "/t/pg", body: map[string]any{"Name": "Neu2", "Slug": "abd-neu2"}, status: 500, code: "server_error", failOp: "create", failTable: "status_pages"},
		{name: "Seite ändern scheitert", method: "PUT", url: "/t/pg/" + pid, body: map[string]any{"Name": "P", "Slug": "abd-p", "PageLayout": "standard"}, status: 500, code: "server_error", failOp: "update", failTable: "status_pages"},
		{name: "SSO löschen scheitert", method: "DELETE", url: "/t/pg/" + pid + "/sso", status: 500, code: "server_error", failOp: "delete", failTable: "status_page_sso_configs"},
		{name: "Seitenrolle prüfen scheitert", method: "POST", url: "/t/pg/" + pid + "/users", body: map[string]any{"user_id": gaUser.ID, "role_id": 1}, status: 500, code: "server_error", failOp: "query", failTable: "user_status_page_roles"},
		{name: "Seitenrolle anlegen scheitert", method: "POST", url: "/t/pg/" + pid + "/users", body: map[string]any{"user_id": gaUser.ID, "role_id": 1}, status: 500, code: "server_error", failOp: "create", failTable: "user_status_page_roles"},
		{name: "Seitenrolle ändern scheitert", method: "POST", url: "/t/pg/" + pid + "/users", body: map[string]any{"user_id": gaUser.ID, "role_id": 1}, status: 500, code: "server_error", failOp: "update", failTable: "user_status_page_roles",
			prepare: func() {
				_ = db.Create(&models.UserStatusPageRole{UserID: gaUser.ID, StatusPageID: page.ID, RoleID: 1})
			}},
		{name: "Eigene Domain speichern scheitert", method: "PUT", url: "/t/pg/" + pid + "/domain", body: map[string]any{"domain": "status.example.org"}, status: 500, code: "server_error", failOp: "update", failTable: "status_pages"},
		{name: "Eigene Domain entfernen scheitert", method: "DELETE", url: "/t/pg/" + pid + "/domain", status: 500, code: "server_error", failOp: "update", failTable: "status_pages"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.prepare != nil {
				tc.prepare()
			}
			if tc.failOp != "" {
				failDB(t, db, tc.failOp, tc.failTable, 0)
			}
			token := tok
			if tc.token != "" {
				token = tc.token
			}
			if tc.name == "Seite als Nutzer ohne Konto" {
				token = ghostTok
			}
			status, out := doJSON(t, r, tc.method, tc.url, token, tc.body)
			assert.Equal(t, tc.status, status, "%v", out)
			assert.Equal(t, tc.code, out["code"], "%v", out)
			msg, _ := out["error"].(string)
			assert.NotEmpty(t, msg)
			assert.NotContains(t, msg, "injizierter Fehler")
		})
	}
}

// OAuth-Callback mit gültigem State, aber unbekanntem Provider.
func TestFehlercodes_OAuthUnbekannterProvider(t *testing.T) {
	h := handlers.NewHandler(testutils.SetupTestDB())
	r := testutils.SetupRouter()
	r.GET("/oauth/:provider/callback", h.OAuthCallbackHandler)
	req := httptest.NewRequest(http.MethodGet, "/oauth/bogus/callback?code=x&state=abc", nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "abc"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"provider_unknown"`)
}
