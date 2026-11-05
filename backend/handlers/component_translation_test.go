package handlers_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"gorm.io/gorm"

	"github.com/fraigner/statushafen/models"
)

func newTestComponent(db *gorm.DB, pageID uint) models.Component {
	component := models.Component{
		StatusPageID: pageID,
		Name:         "Test Component",
		Description:  "Original description",
		Status:       "operational",
	}
	db.Create(&component)
	return component
}

func newComponentTranslation(componentID uint, lang, status string) *models.ComponentTranslation {
	return &models.ComponentTranslation{
		ComponentID:  componentID,
		LanguageCode: lang,
		Title:        "Translated Title",
		Description:  "Translated Description",
		Status:       status,
		Source:       "manual",
	}
}

func TestGetComponentTranslationsHandler(t *testing.T) {
	tests := []struct {
		name              string
		setupTranslations func(*gorm.DB, uint)
		expectedCount     int
		expectedLang      string
	}{
		{
			name: "ReturnsOnlyConfirmedTranslations",
			setupTranslations: func(db *gorm.DB, cid uint) {
				db.Create(newComponentTranslation(cid, "de", "confirmed"))
				db.Create(newComponentTranslation(cid, "fr", "draft"))
			},
			expectedCount: 1,
			expectedLang:  "de",
		},
		{
			name: "EmptyForNoConfirmed",
			setupTranslations: func(db *gorm.DB, cid uint) {
				db.Create(newComponentTranslation(cid, "de", "draft"))
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, r, h := newTestSetup()
			r.GET("/api/components/:component_id/translations", h.GetComponentTranslationsHandler)
			page := newTestPage(db)
			component := newTestComponent(db, page.ID)
			if tt.setupTranslations != nil {
				tt.setupTranslations(db, component.ID)
			}
			cid := strconv.Itoa(int(component.ID))

			w := doTranslationRequest(r, "GET", "/api/components/"+cid+"/translations", nil)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
			}

			var resp []models.ComponentTranslation
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			if len(resp) != tt.expectedCount {
				t.Errorf("Expected %d translations, got %d", tt.expectedCount, len(resp))
			}
			if tt.expectedCount > 0 && resp[0].LanguageCode != tt.expectedLang {
				t.Errorf("Expected lang %s, got %s", tt.expectedLang, resp[0].LanguageCode)
			}
		})
	}
}

func TestCreateComponentTranslationHandler_Success(t *testing.T) {
	db, r, h := newTestSetup()
	r.POST("/api/components/:component_id/translations", h.CreateComponentTranslationHandler)

	page := newTestPage(db)
	component := newTestComponent(db, page.ID)
	cid := strconv.Itoa(int(component.ID))

	payload := map[string]string{
		"language_code": "de",
		"title":         "Komponente",
		"description":   "Deutsche Beschreibung",
		"source":        "manual",
	}
	body, _ := json.Marshal(payload)

	w := doTranslationRequest(r, "POST", "/api/components/"+cid+"/translations", body)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp models.ComponentTranslation
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.ComponentID != component.ID {
		t.Errorf("Expected component_id %d, got %d", component.ID, resp.ComponentID)
	}
	if resp.Status != "draft" {
		t.Errorf("Expected status=draft, got %q", resp.Status)
	}
}

func TestCreateComponentTranslationHandler_DuplicateLanguageReturns409(t *testing.T) {
	db, r, h := newTestSetup()
	r.POST("/api/components/:component_id/translations", h.CreateComponentTranslationHandler)

	page := newTestPage(db)
	component := newTestComponent(db, page.ID)

	db.Create(newComponentTranslation(component.ID, "de", "draft"))

	cid := strconv.Itoa(int(component.ID))
	payload := map[string]string{
		"language_code": "de",
		"title":         "Different",
		"description":   "Different",
		"source":        "manual",
	}
	body, _ := json.Marshal(payload)

	w := doTranslationRequest(r, "POST", "/api/components/"+cid+"/translations", body)

	if w.Code != http.StatusConflict {
		t.Fatalf("Expected 409, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestCreateComponentTranslationHandler_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		body map[string]string
	}{
		{"MissingLanguageCode", map[string]string{"title": "Title", "description": "Desc"}},
		{"MissingTitle", map[string]string{"language_code": "de", "description": "Desc"}},
		{"MissingDescription", map[string]string{"language_code": "de", "title": "Title"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, r, h := newTestSetup()
			r.POST("/api/components/:component_id/translations", h.CreateComponentTranslationHandler)

			page := newTestPage(db)
			component := newTestComponent(db, page.ID)
			cid := strconv.Itoa(int(component.ID))

			body, _ := json.Marshal(tt.body)
			w := doTranslationRequest(r, "POST", "/api/components/"+cid+"/translations", body)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCreateComponentTranslationHandler_ComponentNotFound(t *testing.T) {
	_, r, h := newTestSetup()
	r.POST("/api/components/:component_id/translations", h.CreateComponentTranslationHandler)

	payload := map[string]string{
		"language_code": "de",
		"title":         "Title",
		"description":   "Desc",
		"source":        "manual",
	}
	body, _ := json.Marshal(payload)

	w := doTranslationRequest(r, "POST", "/api/components/99999/translations", body)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestCreateComponentTranslationHandler_InvalidID(t *testing.T) {
	_, r, h := newTestSetup()
	r.POST("/api/components/:component_id/translations", h.CreateComponentTranslationHandler)

	payload := map[string]string{
		"language_code": "de",
		"title":         "Title",
		"description":   "Desc",
		"source":        "manual",
	}
	body, _ := json.Marshal(payload)

	w := doTranslationRequest(r, "POST", "/api/components/invalid/translations", body)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestConfirmComponentTranslationHandler_Success(t *testing.T) {
	db, r, h := newTestSetup()
	r.PATCH("/api/component-translations/:translation_id/confirm", h.ConfirmComponentTranslationHandler)

	page := newTestPage(db)
	component := newTestComponent(db, page.ID)
	trans := newComponentTranslation(component.ID, "de", "draft")
	db.Create(trans)

	tid := strconv.Itoa(int(trans.ID))
	w := doTranslationRequest(r, "PATCH", "/api/component-translations/"+tid+"/confirm", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp models.ComponentTranslation
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.Status != "confirmed" {
		t.Errorf("Expected status=confirmed, got %q", resp.Status)
	}
}

func TestConfirmComponentTranslationHandler_Idempotent(t *testing.T) {
	db, r, h := newTestSetup()
	r.PATCH("/api/component-translations/:translation_id/confirm", h.ConfirmComponentTranslationHandler)

	page := newTestPage(db)
	component := newTestComponent(db, page.ID)
	trans := newComponentTranslation(component.ID, "de", "confirmed")
	db.Create(trans)

	tid := strconv.Itoa(int(trans.ID))
	w := doTranslationRequest(r, "PATCH", "/api/component-translations/"+tid+"/confirm", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 for idempotent confirm, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestConfirmComponentTranslationHandler_NotFound(t *testing.T) {
	_, r, h := newTestSetup()
	r.PATCH("/api/component-translations/:translation_id/confirm", h.ConfirmComponentTranslationHandler)

	w := doTranslationRequest(r, "PATCH", "/api/component-translations/99999/confirm", nil)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestConfirmComponentTranslationHandler_InvalidID(t *testing.T) {
	_, r, h := newTestSetup()
	r.PATCH("/api/component-translations/:translation_id/confirm", h.ConfirmComponentTranslationHandler)

	w := doTranslationRequest(r, "PATCH", "/api/component-translations/invalid/confirm", nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestDeleteComponentTranslationHandler_Success(t *testing.T) {
	db, r, h := newTestSetup()
	r.DELETE("/api/component-translations/:translation_id", h.DeleteComponentTranslationHandler)

	page := newTestPage(db)
	component := newTestComponent(db, page.ID)
	trans := newComponentTranslation(component.ID, "de", "draft")
	db.Create(trans)

	tid := strconv.Itoa(int(trans.ID))
	w := doTranslationRequest(r, "DELETE", "/api/component-translations/"+tid, nil)

	if w.Code != http.StatusNoContent {
		t.Fatalf("Expected 204, got %d. Body: %s", w.Code, w.Body.String())
	}

	var count int64
	db.Model(&models.ComponentTranslation{}).Where("id = ?", trans.ID).Count(&count)
	if count != 0 {
		t.Errorf("Expected translation to be deleted")
	}
}

func TestDeleteComponentTranslationHandler_NotFound(t *testing.T) {
	_, r, h := newTestSetup()
	r.DELETE("/api/component-translations/:translation_id", h.DeleteComponentTranslationHandler)

	w := doTranslationRequest(r, "DELETE", "/api/component-translations/99999", nil)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestDeleteComponentTranslationHandler_InvalidID(t *testing.T) {
	_, r, h := newTestSetup()
	r.DELETE("/api/component-translations/:translation_id", h.DeleteComponentTranslationHandler)

	w := doTranslationRequest(r, "DELETE", "/api/component-translations/invalid", nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// Speichern je Sprache aus dem Bearbeiten-Formular.
func saveComponentTransSetup(t *testing.T) (*gorm.DB, http.Handler, string) {
	t.Helper()
	db, r, h := newTestSetup()
	r.PUT("/api/components/:component_id/translations/:language_code", h.SaveComponentTranslationHandler)
	page := newTestPage(db)
	for _, code := range []string{"de", "en", "fr"} {
		db.FirstOrCreate(&models.Language{Code: code, Name: code, IsActive: true}, models.Language{Code: code})
	}
	db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true})
	db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "en"})
	comp := models.Component{StatusPageID: page.ID, Name: "API"}
	db.Create(&comp)
	return db, r, "/api/components/" + strconv.Itoa(int(comp.ID)) + "/translations/"
}

func putComponentTrans(r http.Handler, url string, body map[string]string) int {
	b, _ := json.Marshal(body)
	return doTranslationRequest(r, "PUT", url, b).Code
}

func TestSaveComponentTranslationHandler_CreatesThenUpdates(t *testing.T) {
	db, r, base := saveComponentTransSetup(t)
	if c := putComponentTrans(r, base+"en", map[string]string{"title": "API", "description": "Desc", "status": "draft"}); c != http.StatusOK {
		t.Fatalf("anlegen: erwartet 200, war %d", c)
	}
	if c := putComponentTrans(r, base+"en", map[string]string{"title": "API 2", "description": "Desc", "status": "confirmed"}); c != http.StatusOK {
		t.Fatalf("ändern: erwartet 200, war %d", c)
	}
	var all []models.ComponentTranslation
	db.Find(&all)
	if len(all) != 1 || all[0].Status != "confirmed" || all[0].Title != "API 2" || all[0].Source != "manual" {
		t.Fatalf("erwartet genau eine bestätigte Übersetzung, war %+v", all)
	}
}

// Die Beschreibung ist optional (Komponente ohne Beschreibung, Gruppen).
func TestSaveComponentTranslationHandler_DescriptionOptional(t *testing.T) {
	db, r, base := saveComponentTransSetup(t)
	if c := putComponentTrans(r, base+"en", map[string]string{"title": "API", "status": "confirmed"}); c != http.StatusOK {
		t.Fatalf("ohne Beschreibung: erwartet 200, war %d", c)
	}
	var tr models.ComponentTranslation
	db.First(&tr)
	if tr.Description != "" || tr.Title != "API" {
		t.Fatalf("erwartet leere Beschreibung, war %+v", tr)
	}
}

func TestSaveComponentTranslationHandler_RejectsBadInput(t *testing.T) {
	_, r, base := saveComponentTransSetup(t)
	ok := map[string]string{"title": "API", "description": "d", "status": "draft"}
	for _, code := range []string{"de", "fr"} { // Standardsprache bzw. nicht aktiv
		if c := putComponentTrans(r, base+code, ok); c != http.StatusBadRequest {
			t.Errorf("%s: erwartet 400, war %d", code, c)
		}
	}
	if c := putComponentTrans(r, base+"en", map[string]string{"title": "API", "status": "published"}); c != http.StatusBadRequest {
		t.Errorf("ungültiger Stand: erwartet 400, war %d", c)
	}
	if c := putComponentTrans(r, base+"en", map[string]string{"status": "draft"}); c != http.StatusBadRequest {
		t.Errorf("ohne Name: erwartet 400, war %d", c)
	}
	if c := putComponentTrans(r, "/api/components/999/translations/en", ok); c != http.StatusNotFound {
		t.Errorf("unbekannte Komponente: erwartet 404, war %d", c)
	}
	if c := putComponentTrans(r, "/api/components/abc/translations/en", ok); c != http.StatusBadRequest {
		t.Errorf("ungültige ID: erwartet 400, war %d", c)
	}
}

func TestSaveComponentTranslationHandler_DatabaseErrorsReturn500(t *testing.T) {
	db, r, base := saveComponentTransSetup(t)
	ok := map[string]string{"title": "API", "status": "draft"}
	db.Exec("CREATE TRIGGER no_insert BEFORE INSERT ON component_translations BEGIN SELECT RAISE(ABORT, 'gesperrt'); END")
	if c := putComponentTrans(r, base+"en", ok); c != http.StatusInternalServerError {
		t.Errorf("Speichern: erwartet 500, war %d", c)
	}
	db.Exec("DROP TRIGGER no_insert")
	if err := db.Migrator().DropTable(&models.ComponentTranslation{}); err != nil {
		t.Fatal(err)
	}
	if c := putComponentTrans(r, base+"en", ok); c != http.StatusInternalServerError {
		t.Errorf("Lesen: erwartet 500, war %d", c)
	}
}
