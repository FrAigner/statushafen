package handlers_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"gorm.io/gorm"

	"github.com/fraigner/statushafen/models"
)

// Übersetzungen von Vorlagen (Sprachumschalter im Vorlagen-Formular).
type tmplTransFixture struct {
	db        *gorm.DB
	r         http.Handler
	pageID    uint
	otherPage uint
	tmpl      models.IncidentTemplate
}

func (f tmplTransFixture) base(pageID uint, tmplID uint) string {
	return "/api/pages/" + strconv.Itoa(int(pageID)) + "/incident-templates/" + strconv.Itoa(int(tmplID)) + "/translations"
}

func newTmplTransFixture(t *testing.T) tmplTransFixture {
	t.Helper()
	db, r, h := newTestSetup()
	r.GET("/api/pages/:page_id/incident-templates/:id/translations", h.GetIncidentTemplateTranslationsHandler)
	r.PUT("/api/pages/:page_id/incident-templates/:id/translations/:language_code", h.SaveIncidentTemplateTranslationHandler)
	r.DELETE("/api/pages/:page_id/incident-templates/:id", h.DeleteIncidentTemplateHandler)
	page := newTestPage(db)
	other := models.StatusPage{Name: "Andere", Slug: "andere-674"}
	db.Create(&other)
	for _, code := range []string{"de", "en", "fr"} {
		db.FirstOrCreate(&models.Language{Code: code, Name: code, IsActive: true}, models.Language{Code: code})
	}
	db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true})
	db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "en"})
	tmpl := models.IncidentTemplate{StatusPageID: page.ID, Name: "DB", Kind: "incident", Title: "Titel", Message: "Text"}
	db.Create(&tmpl)
	return tmplTransFixture{db: db, r: r, pageID: page.ID, otherPage: other.ID, tmpl: tmpl}
}

func putTmplTrans(f tmplTransFixture, url, status string) int {
	body, _ := json.Marshal(map[string]string{"title": "Title", "message": "Text", "status": status})
	return doTranslationRequest(f.r, "PUT", url, body).Code
}

func TestSaveIncidentTemplateTranslationHandler_CreatesThenUpdatesAndLists(t *testing.T) {
	f := newTmplTransFixture(t)
	url := f.base(f.pageID, f.tmpl.ID) + "/en"
	if c := putTmplTrans(f, url, "draft"); c != http.StatusOK {
		t.Fatalf("anlegen: erwartet 200, war %d", c)
	}
	if c := putTmplTrans(f, url, "confirmed"); c != http.StatusOK {
		t.Fatalf("ändern: erwartet 200, war %d", c)
	}
	w := doTranslationRequest(f.r, "GET", f.base(f.pageID, f.tmpl.ID), nil)
	var rows []models.IncidentTemplateTranslation
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil || w.Code != http.StatusOK {
		t.Fatalf("Liste: %d %s", w.Code, w.Body.String())
	}
	if len(rows) != 1 || rows[0].Status != "confirmed" || rows[0].LanguageCode != "en" {
		t.Fatalf("erwartet genau eine bestätigte en-Übersetzung, war %+v", rows)
	}
}

func TestIncidentTemplateTranslationHandlers_RejectBadInput(t *testing.T) {
	f := newTmplTransFixture(t)
	base := f.base(f.pageID, f.tmpl.ID)
	for _, code := range []string{"de", "fr"} { // Standardsprache bzw. nicht aktiv
		if c := putTmplTrans(f, base+"/"+code, "draft"); c != http.StatusBadRequest {
			t.Errorf("%s: erwartet 400, war %d", code, c)
		}
	}
	if c := putTmplTrans(f, base+"/en", "published"); c != http.StatusBadRequest {
		t.Errorf("ungültiger Stand: erwartet 400, war %d", c)
	}
	if c := putTmplTrans(f, f.base(f.pageID, 999)+"/en", "draft"); c != http.StatusNotFound {
		t.Errorf("unbekannte Vorlage: erwartet 404, war %d", c)
	}
	// Mandantentrennung: Vorlage von Seite A über Seite B ist nicht erreichbar.
	if c := putTmplTrans(f, f.base(f.otherPage, f.tmpl.ID)+"/en", "draft"); c != http.StatusNotFound {
		t.Errorf("fremde Seite (PUT): erwartet 404, war %d", c)
	}
	if c := doTranslationRequest(f.r, "GET", f.base(f.otherPage, f.tmpl.ID), nil).Code; c != http.StatusNotFound {
		t.Errorf("fremde Seite (GET): erwartet 404, war %d", c)
	}
	if c := doTranslationRequest(f.r, "GET", "/api/pages/"+strconv.Itoa(int(f.pageID))+"/incident-templates/abc/translations", nil).Code; c != http.StatusBadRequest {
		t.Errorf("ungültige ID: erwartet 400, war %d", c)
	}
	if c := doTranslationRequest(f.r, "GET", "/api/pages/abc/incident-templates/1/translations", nil).Code; c != http.StatusBadRequest {
		t.Errorf("ungültige Seite: erwartet 400, war %d", c)
	}
}

// Das Löschen einer Vorlage nimmt ihre Übersetzungen mit (Soft-Delete löst das FK-Cascade nicht aus).
func TestDeleteIncidentTemplateHandler_RemovesTranslations(t *testing.T) {
	f := newTmplTransFixture(t)
	if c := putTmplTrans(f, f.base(f.pageID, f.tmpl.ID)+"/en", "draft"); c != http.StatusOK {
		t.Fatalf("anlegen: %d", c)
	}
	path := "/api/pages/" + strconv.Itoa(int(f.pageID)) + "/incident-templates/" + strconv.Itoa(int(f.tmpl.ID))
	if c := doTranslationRequest(f.r, "DELETE", path, nil).Code; c != http.StatusOK {
		t.Fatalf("löschen: %d", c)
	}
	var n int64
	f.db.Model(&models.IncidentTemplateTranslation{}).Where("incident_template_id = ?", f.tmpl.ID).Count(&n)
	if n != 0 {
		t.Fatalf("erwartet 0 Übersetzungen nach dem Löschen, waren %d", n)
	}
}

func TestIncidentTemplateTranslationHandlers_DatabaseErrorsReturn500(t *testing.T) {
	f := newTmplTransFixture(t)
	url := f.base(f.pageID, f.tmpl.ID)
	f.db.Exec("CREATE TRIGGER no_insert BEFORE INSERT ON incident_template_translations BEGIN SELECT RAISE(ABORT, 'gesperrt'); END")
	if c := putTmplTrans(f, url+"/en", "draft"); c != http.StatusInternalServerError {
		t.Errorf("Speichern: erwartet 500, war %d", c)
	}
	f.db.Exec("DROP TRIGGER no_insert")
	if err := f.db.Migrator().DropTable(&models.IncidentTemplateTranslation{}); err != nil {
		t.Fatal(err)
	}
	if c := putTmplTrans(f, url+"/en", "draft"); c != http.StatusInternalServerError {
		t.Errorf("Lesen (PUT): erwartet 500, war %d", c)
	}
	if c := doTranslationRequest(f.r, "GET", url, nil).Code; c != http.StatusInternalServerError {
		t.Errorf("Liste: erwartet 500, war %d", c)
	}
}
