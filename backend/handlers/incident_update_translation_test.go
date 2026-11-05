package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Übersetzungen der Updates eines Vorfalls.

type iutEnv struct {
	t    *testing.T
	db   *gorm.DB
	r    *gin.Engine
	page models.StatusPage
	inc  models.Incident
	upd  models.IncidentUpdate
}

func newIUTEnv(t *testing.T) *iutEnv {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) {
		if v := c.GetHeader("X-Test-User"); v != "" {
			id, _ := strconv.ParseUint(v, 10, 32)
			c.Set("userID", uint(id))
		}
		c.Next()
	})
	base := "/api/pages/:page_id/incidents/:id/updates/:update_id/translations"
	r.GET(base, h.GetIncidentUpdateTranslationsHandler)
	r.PUT(base+"/:language_code", h.SaveIncidentUpdateTranslationHandler)
	g := r.Group("/api/public")
	g.Use(middleware.OptionalAuthMiddleware())
	g.GET("/pages/:slug/incidents/:incident_id/update-translations", h.GetPublicIncidentUpdateTranslationsHandler)

	e := &iutEnv{t: t, db: db, r: r}
	e.page = e.newPage("iut", false)
	e.inc = models.Incident{StatusPageID: e.page.ID, Title: "Störung", Message: "Erstmeldung", Status: "investigating"}
	require.NoError(t, db.Create(&e.inc).Error)
	e.upd = e.newUpdate(e.inc.ID, "Wir arbeiten daran")
	return e
}

func (e *iutEnv) newPage(slug string, private bool) models.StatusPage {
	p := models.StatusPage{Name: slug, Slug: slug, IsPrivate: private}
	require.NoError(e.t, e.db.Create(&p).Error)
	for _, code := range []string{"de", "en", "fr"} {
		require.NoError(e.t, e.db.Where("code = ?", code).FirstOrCreate(&models.Language{Code: code, Name: code, IsActive: true}).Error)
	}
	for _, l := range []models.StatusPageLanguage{{StatusPageID: p.ID, LanguageCode: "de", IsDefault: true}, {StatusPageID: p.ID, LanguageCode: "en"}, {StatusPageID: p.ID, LanguageCode: "fr"}} {
		require.NoError(e.t, e.db.Create(&l).Error)
	}
	return p
}

func (e *iutEnv) newUpdate(incID uint, msg string) models.IncidentUpdate {
	u := models.IncidentUpdate{IncidentID: incID, Status: "monitoring", Message: msg}
	require.NoError(e.t, e.db.Create(&u).Error)
	return u
}

func (e *iutEnv) path(pageID, incID, updID uint, suffix string) string {
	return "/api/pages/" + strconv.Itoa(int(pageID)) + "/incidents/" + strconv.Itoa(int(incID)) +
		"/updates/" + strconv.Itoa(int(updID)) + "/translations" + suffix
}

func (e *iutEnv) adminPath(suffix string) string {
	return e.path(e.page.ID, e.inc.ID, e.upd.ID, suffix)
}

func (e *iutEnv) do(method, path string, payload any, headers ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	req, _ := http.NewRequest(method, path, nil)
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	return serve(e.r, req)
}

func (e *iutEnv) save(code, msg, status string) *httptest.ResponseRecorder {
	return e.do("PUT", e.adminPath("/"+code), map[string]string{"message": msg, "status": status})
}

func (e *iutEnv) publicGet(slug string, incID uint, query string) (*httptest.ResponseRecorder, publicUpdatesResp) {
	e.t.Helper()
	w := e.do("GET", "/api/public/pages/"+slug+"/incidents/"+strconv.Itoa(int(incID))+"/update-translations"+query, nil)
	var resp publicUpdatesResp
	if w.Code == http.StatusOK {
		require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &resp))
	}
	return w, resp
}

type publicUpdatesResp struct {
	Language        string            `json:"language"`
	DefaultLanguage string            `json:"default_language"`
	Updates         map[string]string `json:"updates"`
}

func codeFrom(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct{ Code string }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out.Code
}

func TestSaveIncidentUpdateTranslation_AnlegenDannAendern(t *testing.T) {
	e := newIUTEnv(t)
	w := e.save("EN", "We are working on it", "draft") // Großschreibung wird normalisiert
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = e.save("en", "We are fixing it", "confirmed")
	require.Equal(t, http.StatusOK, w.Code)

	var rows []models.IncidentUpdateTranslation
	require.NoError(t, e.db.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, "en", rows[0].LanguageCode)
	assert.Equal(t, "We are fixing it", rows[0].Message)
	assert.Equal(t, "confirmed", rows[0].Status)
	assert.Equal(t, "manual", rows[0].Source)

	// Admin-Liste enthält Entwürfe und Bestätigte, nach Sprache sortiert.
	require.Equal(t, http.StatusOK, e.save("fr", "On s'en occupe", "draft").Code)
	w = e.do("GET", e.adminPath(""), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var list []map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 2)
	assert.Equal(t, "en", list[0]["LanguageCode"])
	assert.Equal(t, "confirmed", list[0]["Status"])
	assert.Equal(t, "fr", list[1]["LanguageCode"])
	assert.Equal(t, "draft", list[1]["Status"])
}

func TestSaveIncidentUpdateTranslation_Eingabepruefung(t *testing.T) {
	e := newIUTEnv(t)
	for _, code := range []string{"de", "es"} { // Standardsprache bzw. nicht aktiv
		w := e.save(code, "x", "draft")
		assert.Equal(t, http.StatusBadRequest, w.Code, code)
		assert.Equal(t, "language_not_available", codeFrom(t, w), code)
	}
	assert.Equal(t, "invalid_request", codeFrom(t, e.save("en", "x", "published")))
	assert.Equal(t, "invalid_request", codeFrom(t, e.save("en", "", "draft")))

	cases := []struct {
		name, path string
		status     int
		code       string
	}{
		{"Seite ungültig", "/api/pages/x/incidents/1/updates/1/translations", 400, "invalid_id"},
		{"Vorfall ungültig", "/api/pages/1/incidents/x/updates/1/translations", 400, "invalid_id"},
		{"Update ungültig", "/api/pages/1/incidents/1/updates/x/translations", 400, "invalid_id"},
		{"Vorfall unbekannt", e.path(e.page.ID, 9999, e.upd.ID, ""), 404, "incident_not_found"},
		{"Update unbekannt", e.path(e.page.ID, e.inc.ID, 9999, ""), 404, "update_not_found"},
	}
	for _, c := range cases {
		for _, method := range []string{"GET", "PUT"} {
			p := c.path
			if method == "PUT" {
				p += "/en"
			}
			w := e.do(method, p, map[string]string{"message": "x", "status": "draft"})
			assert.Equal(t, c.status, w.Code, c.name+" "+method)
			assert.Equal(t, c.code, codeFrom(t, w), c.name+" "+method)
		}
	}
}

func TestSaveIncidentUpdateTranslation_Mandantentrennung(t *testing.T) {
	e := newIUTEnv(t)
	other := e.newPage("iut-other", false)
	// Vorfall einer fremden Seite über die eigene Seiten-ID: 404.
	w := e.do("PUT", e.path(other.ID, e.inc.ID, e.upd.ID, "/en"), map[string]string{"message": "x", "status": "draft"})
	assert.Equal(t, http.StatusNotFound, w.Code)
	// Update eines anderen Vorfalls derselben Seite: 404.
	inc2 := models.Incident{StatusPageID: e.page.ID, Title: "Zwei", Status: "investigating"}
	require.NoError(t, e.db.Create(&inc2).Error)
	upd2 := e.newUpdate(inc2.ID, "anderes Update")
	w = e.do("PUT", e.path(e.page.ID, e.inc.ID, upd2.ID, "/en"), map[string]string{"message": "x", "status": "draft"})
	assert.Equal(t, "update_not_found", codeFrom(t, w))
	var n int64
	e.db.Model(&models.IncidentUpdateTranslation{}).Count(&n)
	assert.Zero(t, n)
}

func TestSaveIncidentUpdateTranslation_TagZugriff(t *testing.T) {
	e := newIUTEnv(t)
	deTag := models.Tag{Name: "DE", StatusPageID: &e.page.ID}
	usTag := models.Tag{Name: "US", StatusPageID: &e.page.ID}
	require.NoError(t, e.db.Create(&deTag).Error)
	require.NoError(t, e.db.Create(&usTag).Error)
	user := models.User{Email: "iut-de@example.com"}
	require.NoError(t, e.db.Create(&user).Error)
	require.NoError(t, e.db.Create(&models.UserTag{UserID: user.ID, TagID: deTag.ID}).Error)
	require.NoError(t, e.db.Create(&models.IncidentTag{IncidentID: e.inc.ID, TagID: usTag.ID}).Error)

	uid := strconv.Itoa(int(user.ID))
	body := map[string]string{"message": "x", "status": "draft"}
	for _, method := range []string{"GET", "PUT"} {
		p := e.adminPath("")
		if method == "PUT" {
			p += "/en"
		}
		w := e.do(method, p, body, "X-Test-User", uid)
		assert.Equal(t, http.StatusForbidden, w.Code, method)
		assert.Equal(t, "forbidden", codeFrom(t, w), method)
	}
	// Mit passendem Tag am Vorfall ist der Zugriff frei.
	require.NoError(t, e.db.Create(&models.IncidentTag{IncidentID: e.inc.ID, TagID: deTag.ID}).Error)
	assert.Equal(t, http.StatusOK, e.do("PUT", e.adminPath("/en"), body, "X-Test-User", uid).Code)
}

func TestIncidentUpdateTranslation_Datenbankfehler500(t *testing.T) {
	e := newIUTEnv(t)
	// Speichern scheitert (Trigger), Lesen scheitert (Tabelle weg).
	e.db.Exec("CREATE TRIGGER no_insert BEFORE INSERT ON incident_update_translations BEGIN SELECT RAISE(ABORT, 'gesperrt'); END")
	assert.Equal(t, http.StatusInternalServerError, e.save("en", "x", "draft").Code)
	e.db.Exec("DROP TRIGGER no_insert")
	require.NoError(t, e.db.Migrator().DropTable(&models.IncidentUpdateTranslation{}))
	assert.Equal(t, http.StatusInternalServerError, e.save("en", "x", "draft").Code)
	assert.Equal(t, http.StatusInternalServerError, e.do("GET", e.adminPath(""), nil).Code)
}

func TestPublicIncidentUpdateTranslations_NurBestaetigteUndRueckfall(t *testing.T) {
	e := newIUTEnv(t)
	upd2 := e.newUpdate(e.inc.ID, "Zweites Update")
	upd3 := e.newUpdate(e.inc.ID, "Drittes Update")
	mk := func(u models.IncidentUpdate, lang, msg, status string) {
		require.NoError(t, e.db.Create(&models.IncidentUpdateTranslation{UpdateID: u.ID, LanguageCode: lang, Message: msg, Status: status}).Error)
	}
	mk(e.upd, "en", "first en", "confirmed")
	mk(e.upd, "fr", "premier fr", "confirmed")
	mk(upd2, "en", "second en DRAFT", "draft") // Entwurf nie
	mk(upd2, "fr", "deuxième fr", "confirmed") // nur fr bestätigt

	// en: Update 1 en, Update 2 hat keinen bestätigten en-Text und fällt auf die
	// Standardsprache de zurück - die gibt es als Übersetzung nicht, also fehlt der Eintrag.
	w, resp := e.publicGet("iut", e.inc.ID, "?lang=EN")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "en", resp.Language)
	assert.Equal(t, "de", resp.DefaultLanguage)
	assert.Equal(t, map[string]string{strconv.Itoa(int(e.upd.ID)): "first en"}, resp.Updates)

	_, resp = e.publicGet("iut", e.inc.ID, "?lang=fr")
	assert.Equal(t, "premier fr", resp.Updates[strconv.Itoa(int(e.upd.ID))])
	assert.Equal(t, "deuxième fr", resp.Updates[strconv.Itoa(int(upd2.ID))])
	assert.NotContains(t, resp.Updates, strconv.Itoa(int(upd3.ID)))

	// Ohne lang gilt die Standardsprache (keine Übersetzung dazu: leere Map, nie null).
	w, resp = e.publicGet("iut", e.inc.ID, "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "de", resp.Language)
	assert.NotNil(t, resp.Updates)
	assert.Empty(t, resp.Updates)
}

func TestPublicIncidentUpdateTranslations_StandardspracheAlsRueckfall(t *testing.T) {
	e := newIUTEnv(t)
	// Eine Seite, deren Standardsprache Englisch ist: eine bestätigte en-Übersetzung
	// gilt auch für Besucher in fr, wenn fr nichts bestätigt hat.
	require.NoError(t, e.db.Model(&models.StatusPageLanguage{}).Where("status_page_id = ?", e.page.ID).Update("is_default", false).Error)
	require.NoError(t, e.db.Model(&models.StatusPageLanguage{}).Where("status_page_id = ? AND language_code = ?", e.page.ID, "en").Update("is_default", true).Error)
	require.NoError(t, e.db.Create(&models.IncidentUpdateTranslation{UpdateID: e.upd.ID, LanguageCode: "en", Message: "english", Status: "confirmed"}).Error)

	_, resp := e.publicGet("iut", e.inc.ID, "?lang=fr")
	assert.Equal(t, "english", resp.Updates[strconv.Itoa(int(e.upd.ID))])
}

func TestPublicIncidentUpdateTranslations_Validierung(t *testing.T) {
	e := newIUTEnv(t)
	other := e.newPage("iut-other", false)
	otherInc := models.Incident{StatusPageID: other.ID, Title: "Fremd", Status: "investigating"}
	require.NoError(t, e.db.Create(&otherInc).Error)

	w, _ := e.publicGet("gibt-es-nicht", e.inc.ID, "")
	assert.Equal(t, "page_not_found", codeFrom(t, w))
	w, _ = e.publicGet("iut", e.inc.ID, "?lang=es")
	assert.Equal(t, "invalid_request", codeFrom(t, w))
	w, _ = e.publicGet("iut", e.inc.ID, "?lang=%24%24")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w, _ = e.publicGet("iut", 9999, "")
	assert.Equal(t, "incident_not_found", codeFrom(t, w))
	// Vorfall einer anderen Seite: 404, keine Daten fremder Seiten.
	w, _ = e.publicGet("iut", otherInc.ID, "")
	assert.Equal(t, "incident_not_found", codeFrom(t, w))
	w = e.do("GET", "/api/public/pages/iut/incidents/abc/update-translations", nil)
	assert.Equal(t, "invalid_id", codeFrom(t, w))
}

func TestPublicIncidentUpdateTranslations_PrivateSeite(t *testing.T) {
	e := newIUTEnv(t)
	priv := e.newPage("iut-privat", true)
	inc := models.Incident{StatusPageID: priv.ID, Title: "Intern", Status: "investigating"}
	require.NoError(t, e.db.Create(&inc).Error)
	w, _ := e.publicGet("iut-privat", inc.ID, "")
	assert.Equal(t, http.StatusNotFound, w.Code, "404 wie bei einer fehlenden Seite")
	assert.Equal(t, "page_not_found", codeFrom(t, w))
}

func TestPublicIncidentUpdateTranslations_Datenbankfehler500(t *testing.T) {
	e := newIUTEnv(t)
	require.NoError(t, e.db.Migrator().DropTable(&models.IncidentUpdateTranslation{}))
	w, _ := e.publicGet("iut", e.inc.ID, "?lang=en")
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	require.NoError(t, e.db.Migrator().DropTable(&models.StatusPageLanguage{}))
	w, _ = e.publicGet("iut", e.inc.ID, "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
