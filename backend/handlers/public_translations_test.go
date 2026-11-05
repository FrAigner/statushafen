package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/middleware"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// gebündelter öffentlicher Übersetzungs-Endpunkt.

type ptResp struct {
	Language        string                       `json:"language"`
	DefaultLanguage string                       `json:"default_language"`
	Components      map[string]map[string]string `json:"components"`
	Incidents       map[string]map[string]string `json:"incidents"`
	Notices         map[string]map[string]string `json:"notices"`
}

type ptEnv struct {
	t   *testing.T
	db  *gorm.DB
	r   http.Handler
	now time.Time
}

func newPTEnv(t *testing.T) *ptEnv {
	t.Helper()
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	e := &ptEnv{t: t, db: db, now: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
	h.Now = func() time.Time { return e.now }
	r := testutils.SetupRouter()
	g := r.Group("/api/public")
	g.Use(middleware.OptionalAuthMiddleware())
	g.GET("/pages/:slug/translations", h.GetPublicTranslationsHandler)
	e.r = r
	return e
}

func (e *ptEnv) get(path string, headers ...string) (*httptest.ResponseRecorder, ptResp) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	var resp ptResp
	if w.Code == http.StatusOK {
		require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &resp))
	}
	return w, resp
}

// page legt eine Seite mit den Sprachen de (Standard) und en/fr an.
func (e *ptEnv) page(slug string, private bool) models.StatusPage {
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

func (e *ptEnv) comp(pageID uint, name string, visible bool, parent *uint) models.Component {
	c := models.Component{StatusPageID: pageID, Name: name, Slug: name, Status: "operational", Visible: true, ParentID: parent}
	require.NoError(e.t, e.db.Create(&c).Error)
	if !visible {
		require.NoError(e.t, e.db.Model(&c).Update("visible", false).Error)
		c.Visible = false
	}
	return c
}

func (e *ptEnv) compTr(id uint, lang, title, status string) {
	require.NoError(e.t, e.db.Create(&models.ComponentTranslation{ComponentID: id, LanguageCode: lang, Title: title, Description: title + " d", Status: status, Source: "manual"}).Error)
}

func TestPublicTranslations_NurBestaetigteUndRueckfall(t *testing.T) {
	e := newPTEnv(t)
	p := e.page("pt", false)
	conf := e.comp(p.ID, "conf", true, nil)
	draft := e.comp(p.ID, "draft", true, nil)
	onlyDe := e.comp(p.ID, "onlyde", true, nil)
	none := e.comp(p.ID, "none", true, nil)
	e.compTr(conf.ID, "en", "Conf EN", "confirmed")
	e.compTr(draft.ID, "en", "Entwurf EN", "draft")
	e.compTr(onlyDe.ID, "de", "Nur DE", "confirmed")

	w, resp := e.get("/api/public/pages/pt/translations?lang=en")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "en", resp.Language)
	assert.Equal(t, "de", resp.DefaultLanguage)
	// bestätigt in der gewünschten Sprache
	assert.Equal(t, "Conf EN", resp.Components[itoa(conf.ID)]["title"])
	assert.Equal(t, "Conf EN d", resp.Components[itoa(conf.ID)]["text"])
	// Entwurf nie, auch nicht als Rückfall auf Original-Auswahl
	assert.NotContains(t, resp.Components, itoa(draft.ID))
	assert.NotContains(t, w.Body.String(), "Entwurf EN")
	// Rückfall: gewünschte Sprache fehlt -> Standardsprache der Seite
	assert.Equal(t, "Nur DE", resp.Components[itoa(onlyDe.ID)]["title"])
	// weder noch -> kein Eintrag (Frontend zeigt das Original)
	assert.NotContains(t, resp.Components, itoa(none.ID))

	// Ohne lang: Standardsprache der Seite
	_, def := e.get("/api/public/pages/pt/translations")
	assert.Equal(t, "de", def.Language)
	assert.Equal(t, "Nur DE", def.Components[itoa(onlyDe.ID)]["title"])
	assert.NotContains(t, def.Components, itoa(conf.ID))
}

func TestPublicTranslations_ErsteSpracheVorStandardsprache(t *testing.T) {
	e := newPTEnv(t)
	p := e.page("pt", false)
	c := e.comp(p.ID, "c", true, nil)
	e.compTr(c.ID, "de", "DE", "confirmed")
	e.compTr(c.ID, "en", "EN", "confirmed")
	_, resp := e.get("/api/public/pages/pt/translations?lang=en")
	assert.Equal(t, "EN", resp.Components[itoa(c.ID)]["title"])
	_, resp = e.get("/api/public/pages/pt/translations?lang=fr")
	assert.Equal(t, "DE", resp.Components[itoa(c.ID)]["title"], "fr fehlt: Rückfall auf Standardsprache, nicht auf en")
}

func TestPublicTranslations_UnsichtbareKomponentenUndGruppenKinder(t *testing.T) {
	e := newPTEnv(t)
	p := e.page("pt", false)
	hiddenGroup := e.comp(p.ID, "hg", false, nil)
	child := e.comp(p.ID, "child", true, &hiddenGroup.ID)
	hidden := e.comp(p.ID, "hidden", false, nil)
	shown := e.comp(p.ID, "shown", true, nil)
	for _, c := range []models.Component{hiddenGroup, child, hidden, shown} {
		e.compTr(c.ID, "en", c.Name+" EN", "confirmed")
	}
	_, resp := e.get("/api/public/pages/pt/translations?lang=en")
	assert.Contains(t, resp.Components, itoa(shown.ID))
	assert.NotContains(t, resp.Components, itoa(hidden.ID))
	assert.NotContains(t, resp.Components, itoa(hiddenGroup.ID))
	assert.NotContains(t, resp.Components, itoa(child.ID), "Mitglieder einer unsichtbaren Gruppe bleiben verborgen")
}

// Review: archivierte Komponenten und ihre Gruppen-Nachfahren
// fehlen im öffentlichen Übersetzungs-Bündel (archived_at muss geladen werden).
func TestPublicTranslations_ArchivierteKomponentenUndGruppenKinder(t *testing.T) {
	e := newPTEnv(t)
	p := e.page("pt", false)
	group := e.comp(p.ID, "ag", true, nil)
	child := e.comp(p.ID, "achild", true, &group.ID)
	archived := e.comp(p.ID, "arch", true, nil)
	shown := e.comp(p.ID, "shown", true, nil)
	require.NoError(t, e.db.Model(&models.Component{}).Where("id IN ?", []uint{group.ID, archived.ID}).Update("archived_at", e.now).Error)
	for _, c := range []models.Component{group, child, archived, shown} {
		e.compTr(c.ID, "en", c.Name+" EN", "confirmed")
	}
	_, resp := e.get("/api/public/pages/pt/translations?lang=en")
	assert.Contains(t, resp.Components, itoa(shown.ID))
	assert.NotContains(t, resp.Components, itoa(archived.ID))
	assert.NotContains(t, resp.Components, itoa(group.ID))
	assert.NotContains(t, resp.Components, itoa(child.ID), "Mitglieder einer archivierten Gruppe bleiben verborgen")
}

func TestPublicTranslations_VorfaelleUndAnkuendigungen(t *testing.T) {
	e := newPTEnv(t)
	p := e.page("pt", false)
	inc := models.Incident{StatusPageID: p.ID, Title: "T", Message: "M", Status: "investigating"}
	require.NoError(t, e.db.Create(&inc).Error)
	require.NoError(t, e.db.Create(&models.IncidentTranslation{IncidentID: inc.ID, LanguageCode: "en", Title: "Title EN", Message: "Msg EN", Status: "confirmed", Source: "manual"}).Error)
	incDraft := models.Incident{StatusPageID: p.ID, Title: "T2", Message: "M2", Status: "investigating"}
	require.NoError(t, e.db.Create(&incDraft).Error)
	require.NoError(t, e.db.Create(&models.IncidentTranslation{IncidentID: incDraft.ID, LanguageCode: "en", Title: "geheim", Message: "geheim", Status: "draft", Source: "manual"}).Error)

	active := models.GeneralNotice{StatusPageID: p.ID, Title: "N", Message: "M", Level: "info", IsActive: true}
	require.NoError(t, e.db.Create(&active).Error)
	inactive := models.GeneralNotice{StatusPageID: p.ID, Title: "N2", Message: "M", Level: "info", IsActive: false}
	require.NoError(t, e.db.Create(&inactive).Error)
	past := e.now.Add(-time.Hour)
	ended := models.GeneralNotice{StatusPageID: p.ID, Title: "N3", Message: "M", Level: "info", IsActive: true, EndAt: &past}
	require.NoError(t, e.db.Create(&ended).Error)
	for _, n := range []models.GeneralNotice{active, inactive, ended} {
		require.NoError(t, e.db.Create(&models.GeneralNoticeTranslation{GeneralNoticeID: n.ID, LanguageCode: "en", Title: n.Title + " EN", Message: "m EN", Status: "confirmed", Source: "manual"}).Error)
	}

	w, resp := e.get("/api/public/pages/pt/translations?lang=en")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Title EN", resp.Incidents[itoa(inc.ID)]["title"])
	assert.Equal(t, "Msg EN", resp.Incidents[itoa(inc.ID)]["text"])
	assert.NotContains(t, resp.Incidents, itoa(incDraft.ID))
	assert.NotContains(t, w.Body.String(), "geheim")
	assert.Equal(t, "N EN", resp.Notices[itoa(active.ID)]["title"])
	assert.NotContains(t, resp.Notices, itoa(inactive.ID))
	assert.NotContains(t, resp.Notices, itoa(ended.ID))
}

func TestPublicTranslations_Mandantentrennung(t *testing.T) {
	e := newPTEnv(t)
	a := e.page("seite-a", false)
	// zweite Seite: Sprachen wurden schon angelegt, nur Zeilen für die Seite
	b := models.StatusPage{Name: "b", Slug: "seite-b"}
	require.NoError(t, e.db.Create(&b).Error)
	require.NoError(t, e.db.Create(&models.StatusPageLanguage{StatusPageID: b.ID, LanguageCode: "de", IsDefault: true}).Error)
	require.NoError(t, e.db.Create(&models.StatusPageLanguage{StatusPageID: b.ID, LanguageCode: "en"}).Error)
	ca := e.comp(a.ID, "ca", true, nil)
	cb := e.comp(b.ID, "cb", true, nil)
	e.compTr(ca.ID, "en", "A EN", "confirmed")
	e.compTr(cb.ID, "en", "B GEHEIM", "confirmed")
	w, resp := e.get("/api/public/pages/seite-a/translations?lang=en")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, resp.Components, itoa(ca.ID))
	assert.NotContains(t, resp.Components, itoa(cb.ID))
	assert.NotContains(t, w.Body.String(), "GEHEIM")
}

func TestPublicTranslations_Validierung404(t *testing.T) {
	e := newPTEnv(t)
	e.page("pt", false)
	w, _ := e.get("/api/public/pages/gibt-es-nicht/translations?lang=en")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "page_not_found")
	// Groß-/Kleinschreibung egal (Codes sind klein gespeichert)
	w, resp := e.get("/api/public/pages/pt/translations?lang=EN")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "en", resp.Language)
	// Sprache nicht bei der Seite aktiv
	w, _ = e.get("/api/public/pages/pt/translations?lang=es")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_request")
	// Ungültige Form
	for _, bad := range []string{"%27%3B--", "de%00", "1234567890", "e"} {
		w, _ = e.get("/api/public/pages/pt/translations?lang=" + bad)
		assert.Equal(t, http.StatusBadRequest, w.Code, bad)
	}
}

func TestPublicTranslations_PrivateSeite(t *testing.T) {
	e := newPTEnv(t)
	p := e.page("intern", true)
	c := e.comp(p.ID, "c", true, nil)
	e.compTr(c.ID, "en", "Intern EN", "confirmed")
	role := models.Role{Name: "Viewer 646", Permissions: "components:read"}
	require.NoError(t, e.db.Create(&role).Error)
	member := models.User{Email: "member-646@example.com"}
	require.NoError(t, e.db.Create(&member).Error)
	require.NoError(t, e.db.Create(&models.UserStatusPageRole{UserID: member.ID, StatusPageID: p.ID, RoleID: role.ID}).Error)
	stranger := models.User{Email: "stranger-646@example.com"}
	require.NoError(t, e.db.Create(&stranger).Error)
	bearer := func(u models.User) string {
		tok, err := middleware.GenerateToken(u.ID, u.Email, "", "", false)
		require.NoError(t, err)
		return "Bearer " + tok
	}

	w, _ := e.get("/api/public/pages/intern/translations?lang=en")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NotContains(t, w.Body.String(), "Intern EN")
	w, _ = e.get("/api/public/pages/intern/translations?lang=en", "Authorization", bearer(stranger))
	assert.NotEqual(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "Intern EN")
	w, resp := e.get("/api/public/pages/intern/translations?lang=en", "Authorization", bearer(member))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "Intern EN", resp.Components[itoa(c.ID)]["title"])

	// Subscriber-Token der richtigen Seite gewährt Zugriff, einer anderen Seite nicht.
	sub := models.Subscriber{StatusPageID: p.ID, Email: "sub-646@example.com", Channel: "email", Status: models.SubscriberStatusActive, CreatedByUserID: &member.ID}
	require.NoError(t, e.db.Create(&sub).Error)
	tok, err := middleware.GenerateSubscriberToken(sub.ID, p.ID)
	require.NoError(t, err)
	w, _ = e.get("/api/public/pages/intern/translations?lang=en&token=" + tok)
	assert.Equal(t, http.StatusOK, w.Code)
	other := models.StatusPage{Name: "o", Slug: "o", IsPrivate: true}
	require.NoError(t, e.db.Create(&other).Error)
	tok2, err := middleware.GenerateSubscriberToken(sub.ID, other.ID)
	require.NoError(t, err)
	w, _ = e.get("/api/public/pages/intern/translations?lang=en&token=" + tok2)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestPublicTranslations_CacheBisTTLDanachNeu(t *testing.T) {
	e := newPTEnv(t)
	p := e.page("pt", false)
	c := e.comp(p.ID, "c", true, nil)
	e.compTr(c.ID, "en", "Alt", "confirmed")
	_, r1 := e.get("/api/public/pages/pt/translations?lang=en")
	require.Equal(t, "Alt", r1.Components[itoa(c.ID)]["title"])

	require.NoError(t, e.db.Model(&models.ComponentTranslation{}).Where("component_id = ?", c.ID).Update("title", "Neu").Error)
	e.now = e.now.Add(59 * time.Second)
	_, r2 := e.get("/api/public/pages/pt/translations?lang=en")
	assert.Equal(t, "Alt", r2.Components[itoa(c.ID)]["title"], "innerhalb von 60 s aus dem Cache")
	e.now = e.now.Add(2 * time.Second)
	_, r3 := e.get("/api/public/pages/pt/translations?lang=en")
	assert.Equal(t, "Neu", r3.Components[itoa(c.ID)]["title"], "nach 60 s neu geladen")
	// Uhr läuft rückwärts: Eintrag gilt nicht
	e.now = e.now.Add(-time.Hour)
	require.NoError(t, e.db.Model(&models.ComponentTranslation{}).Where("component_id = ?", c.ID).Update("title", "Zurück").Error)
	_, r4 := e.get("/api/public/pages/pt/translations?lang=en")
	assert.Equal(t, "Zurück", r4.Components[itoa(c.ID)]["title"])
}

func TestPublicTranslations_SeiteOhneStandardsprache(t *testing.T) {
	e := newPTEnv(t)
	p := models.StatusPage{Name: "x", Slug: "ohne"}
	require.NoError(t, e.db.Create(&p).Error)
	require.NoError(t, e.db.Create(&models.StatusPageLanguage{StatusPageID: p.ID, LanguageCode: "en"}).Error)
	w, resp := e.get("/api/public/pages/ohne/translations")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", resp.Language)
	assert.Empty(t, resp.Components)
	assert.NotNil(t, resp.Components, "leere Maps statt null")
}

// Datenbankfehler je Abfrage ergeben 500 mit Code, nie Teildaten.
func TestPublicTranslations_Datenbankfehler500(t *testing.T) {
	for _, table := range []string{"status_page_languages", "components", "incidents", "general_notices", "component_translations", "incident_translations", "general_notice_translations"} {
		t.Run(table, func(t *testing.T) {
			e := newPTEnv(t)
			p := e.page("pt", false)
			c := e.comp(p.ID, "c", true, nil)
			inc := models.Incident{StatusPageID: p.ID, Title: "T", Message: "M", Status: "investigating"}
			require.NoError(t, e.db.Create(&inc).Error)
			n := models.GeneralNotice{StatusPageID: p.ID, Title: "N", Message: "M", Level: "info", IsActive: true}
			require.NoError(t, e.db.Create(&n).Error)
			require.NotZero(t, c.ID)
			require.NoError(t, e.db.Exec("DROP TABLE "+table).Error)
			w, _ := e.get("/api/public/pages/pt/translations?lang=en")
			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), "server_error")
		})
	}
}
