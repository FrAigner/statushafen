package handlers_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"gorm.io/gorm"

	"github.com/fraigner/statushafen/models"
)

func newTestIncident(db *gorm.DB, pageID uint) models.Incident {
	incident := models.Incident{
		StatusPageID: pageID,
		Title:        "API Down",
		Message:      "The API is currently unavailable.",
		Status:       "investigating",
	}
	db.Create(&incident)
	return incident
}

func newTranslation(incidentID uint, lang, status string) *models.IncidentTranslation {
	return &models.IncidentTranslation{
		IncidentID:   incidentID,
		LanguageCode: lang,
		Title:        "Translated Title",
		Message:      "Translated Message",
		Status:       status,
		Source:       "manual",
	}
}

// TestGetIncidentTranslationsHandler tests the handler with various states
func TestGetIncidentTranslationsHandler(t *testing.T) {
	tests := []struct {
		name              string
		setupTranslations func(*gorm.DB, uint)
		expectedCount     int
		expectedLang      string
	}{
		{
			name: "ReturnsOnlyConfirmedTranslations",
			setupTranslations: func(db *gorm.DB, iid uint) {
				db.Create(newTranslation(iid, "de", "confirmed"))
				db.Create(newTranslation(iid, "fr", "draft"))
			},
			expectedCount: 1,
			expectedLang:  "de",
		},
		{
			name: "EmptyForNoConfirmed",
			setupTranslations: func(db *gorm.DB, iid uint) {
				db.Create(newTranslation(iid, "de", "draft"))
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, r, h := newTestSetup()
			r.GET("/api/incidents/:incident_id/translations", h.GetIncidentTranslationsHandler)
			page := newTestPage(db)
			incident := newTestIncident(db, page.ID)
			if tt.setupTranslations != nil {
				tt.setupTranslations(db, incident.ID)
			}
			iid := strconv.Itoa(int(incident.ID))

			w := doTranslationRequest(r, "GET", "/api/incidents/"+iid+"/translations", nil)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
			}

			var resp []models.IncidentTranslation
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

// TestCreateIncidentTranslationHandler_Success verifies that an admin can create
// a new incident translation with status="draft" by default.
func TestCreateIncidentTranslationHandler_Success(t *testing.T) {
	db, r, h := newTestSetup()
	r.POST("/api/incidents/:incident_id/translations", h.CreateIncidentTranslationHandler)

	page := newTestPage(db)
	incident := newTestIncident(db, page.ID)
	iid := strconv.Itoa(int(incident.ID))

	payload := map[string]string{
		"language_code": "de",
		"title":         "API ausgefallen",
		"message":       "Die API ist derzeit nicht verfügbar.",
		"source":        "manual",
	}
	body, _ := json.Marshal(payload)

	w := doTranslationRequest(r, "POST", "/api/incidents/"+iid+"/translations", body)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp models.IncidentTranslation
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.IncidentID != incident.ID {
		t.Errorf("Expected incident_id %d, got %d", incident.ID, resp.IncidentID)
	}
	if resp.Status != "draft" {
		t.Errorf("Expected status=draft, got %q", resp.Status)
	}
}

// TestCreateIncidentTranslationHandler_DuplicateLanguageReturns409 ensures
// that creating a translation for a language that already has one returns 409.
func TestCreateIncidentTranslationHandler_DuplicateLanguageReturns409(t *testing.T) {
	db, r, h := newTestSetup()
	r.POST("/api/incidents/:incident_id/translations", h.CreateIncidentTranslationHandler)

	page := newTestPage(db)
	incident := newTestIncident(db, page.ID)

	db.Create(newTranslation(incident.ID, "de", "draft"))

	iid := strconv.Itoa(int(incident.ID))
	payload := map[string]string{
		"language_code": "de",
		"title":         "Different",
		"message":       "Different",
		"source":        "manual",
	}
	body, _ := json.Marshal(payload)

	w := doTranslationRequest(r, "POST", "/api/incidents/"+iid+"/translations", body)

	if w.Code != http.StatusConflict {
		t.Fatalf("Expected 409, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestConfirmIncidentTranslationHandler_Success verifies that an admin can
// confirm a draft translation (transitions status from draft to confirmed).
func TestConfirmIncidentTranslationHandler_Success(t *testing.T) {
	db, r, h := newTestSetup()
	r.PATCH("/api/incident-translations/:translation_id/confirm", h.ConfirmIncidentTranslationHandler)

	page := newTestPage(db)
	incident := newTestIncident(db, page.ID)

	trans := newTranslation(incident.ID, "de", "draft")
	db.Create(trans)
	tid := strconv.Itoa(int(trans.ID))

	w := doTranslationRequest(r, "PATCH", "/api/incident-translations/"+tid+"/confirm", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp models.IncidentTranslation
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.Status != "confirmed" {
		t.Errorf("Expected status=confirmed, got %q", resp.Status)
	}
}

// TestConfirmIncidentTranslationHandler_NotFoundReturns404 verifies that
// confirming a non-existent translation returns 404.
func TestConfirmIncidentTranslationHandler_NotFoundReturns404(t *testing.T) {
	_, r, h := newTestSetup()
	r.PATCH("/api/incident-translations/:translation_id/confirm", h.ConfirmIncidentTranslationHandler)

	w := doTranslationRequest(r, "PATCH", "/api/incident-translations/9999/confirm", nil)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestDeleteIncidentTranslationHandler_Success verifies that an admin can delete
// a translation.
func TestDeleteIncidentTranslationHandler_Success(t *testing.T) {
	db, r, h := newTestSetup()
	r.DELETE("/api/incident-translations/:translation_id", h.DeleteIncidentTranslationHandler)

	page := newTestPage(db)
	incident := newTestIncident(db, page.ID)

	trans := newTranslation(incident.ID, "de", "confirmed")
	db.Create(trans)
	tid := strconv.Itoa(int(trans.ID))

	w := doTranslationRequest(r, "DELETE", "/api/incident-translations/"+tid, nil)

	if w.Code != http.StatusNoContent {
		t.Fatalf("Expected 204, got %d. Body: %s", w.Code, w.Body.String())
	}

	var dbTrans models.IncidentTranslation
	result := db.First(&dbTrans, trans.ID)
	if result.Error == nil {
		t.Errorf("Expected translation to be deleted, but found it in DB")
	}
}

// TestDeleteIncidentTranslationHandler_NotFoundReturns404 verifies that
// deleting a non-existent translation returns 404.
func TestDeleteIncidentTranslationHandler_NotFoundReturns404(t *testing.T) {
	_, r, h := newTestSetup()
	r.DELETE("/api/incident-translations/:translation_id", h.DeleteIncidentTranslationHandler)

	w := doTranslationRequest(r, "DELETE", "/api/incident-translations/9999", nil)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestGetIncidentTranslationsHandler_InvalidIncidentID verifies that an
// invalid incident ID returns 400.
func TestGetIncidentTranslationsHandler_InvalidIncidentID(t *testing.T) {
	_, r, h := newTestSetup()
	r.GET("/api/incidents/:incident_id/translations", h.GetIncidentTranslationsHandler)

	w := doTranslationRequest(r, "GET", "/api/incidents/invalid/translations", nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestGetIncidentTranslationsHandler_IncidentNotFound verifies that
// a non-existent incident returns 404.
func TestGetIncidentTranslationsHandler_IncidentNotFound(t *testing.T) {
	_, r, h := newTestSetup()
	r.GET("/api/incidents/:incident_id/translations", h.GetIncidentTranslationsHandler)

	w := doTranslationRequest(r, "GET", "/api/incidents/9999/translations", nil)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestCreateIncidentTranslationHandler_IncidentNotFound verifies that
// creating a translation for a non-existent incident returns 404.
func TestCreateIncidentTranslationHandler_IncidentNotFound(t *testing.T) {
	_, r, h := newTestSetup()
	r.POST("/api/incidents/:incident_id/translations", h.CreateIncidentTranslationHandler)

	payload := map[string]string{
		"language_code": "de",
		"title":         "Title",
		"message":       "Message",
		"source":        "manual",
	}
	body, _ := json.Marshal(payload)

	w := doTranslationRequest(r, "POST", "/api/incidents/9999/translations", body)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestCreateIncidentTranslationHandler_MissingFields verifies that
// missing required fields returns 400.
func TestCreateIncidentTranslationHandler_MissingFields(t *testing.T) {
	db, r, h := newTestSetup()
	r.POST("/api/incidents/:incident_id/translations", h.CreateIncidentTranslationHandler)

	page := newTestPage(db)
	incident := newTestIncident(db, page.ID)
	iid := strconv.Itoa(int(incident.ID))

	payload := map[string]string{
		"language_code": "de",
		// missing title, message, source
	}
	body, _ := json.Marshal(payload)

	w := doTranslationRequest(r, "POST", "/api/incidents/"+iid+"/translations", body)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestConfirmIncidentTranslationHandler_InvalidTranslationID verifies that
// an invalid translation ID returns 400.
func TestConfirmIncidentTranslationHandler_InvalidTranslationID(t *testing.T) {
	_, r, h := newTestSetup()
	r.PATCH("/api/incident-translations/:translation_id/confirm", h.ConfirmIncidentTranslationHandler)

	w := doTranslationRequest(r, "PATCH", "/api/incident-translations/invalid/confirm", nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestDeleteIncidentTranslationHandler_InvalidTranslationID verifies that
// an invalid translation ID returns 400.
func TestDeleteIncidentTranslationHandler_InvalidTranslationID(t *testing.T) {
	_, r, h := newTestSetup()
	r.DELETE("/api/incident-translations/:translation_id", h.DeleteIncidentTranslationHandler)

	w := doTranslationRequest(r, "DELETE", "/api/incident-translations/invalid", nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// TestGetIncidentTranslationsHandler_MultipleTranslations verifies that
// multiple confirmed translations are all returned.
func TestGetIncidentTranslationsHandler_MultipleTranslations(t *testing.T) {
	db, r, h := newTestSetup()
	r.GET("/api/incidents/:incident_id/translations", h.GetIncidentTranslationsHandler)

	page := newTestPage(db)
	incident := newTestIncident(db, page.ID)
	db.Create(newTranslation(incident.ID, "de", "confirmed"))
	db.Create(newTranslation(incident.ID, "fr", "confirmed"))
	db.Create(newTranslation(incident.ID, "es", "draft"))
	iid := strconv.Itoa(int(incident.ID))

	w := doTranslationRequest(r, "GET", "/api/incidents/"+iid+"/translations", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp []models.IncidentTranslation
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(resp) != 2 {
		t.Errorf("Expected 2 confirmed translations, got %d", len(resp))
	}
	// Verify no draft translations are included
	for _, trans := range resp {
		if trans.Status == "draft" {
			t.Errorf("Draft translation should not be included in public API")
		}
	}
}

// TestConfirmIncidentTranslationHandler_IdempotentConfirm verifies that
// confirming an already-confirmed translation still works.
func TestConfirmIncidentTranslationHandler_IdempotentConfirm(t *testing.T) {
	db, r, h := newTestSetup()
	r.PATCH("/api/incident-translations/:translation_id/confirm", h.ConfirmIncidentTranslationHandler)

	page := newTestPage(db)
	incident := newTestIncident(db, page.ID)
	trans := newTranslation(incident.ID, "de", "confirmed")
	db.Create(trans)
	tid := strconv.Itoa(int(trans.ID))

	w := doTranslationRequest(r, "PATCH", "/api/incident-translations/"+tid+"/confirm", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp models.IncidentTranslation
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.Status != "confirmed" {
		t.Errorf("Expected status=confirmed, got %q", resp.Status)
	}
}

// TestCreateIncidentTranslationHandler_InvalidIncidentID verifies that
// an invalid incident ID returns 400.
func TestCreateIncidentTranslationHandler_InvalidIncidentID(t *testing.T) {
	_, r, h := newTestSetup()
	r.POST("/api/incidents/:incident_id/translations", h.CreateIncidentTranslationHandler)

	payload := map[string]string{
		"language_code": "de",
		"title":         "Title",
		"message":       "Message",
		"source":        "manual",
	}
	body, _ := json.Marshal(payload)

	w := doTranslationRequest(r, "POST", "/api/incidents/invalid/translations", body)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// Speichern je Sprache aus dem Bearbeiten-Formular.
func saveTransSetup(t *testing.T) (*gorm.DB, http.Handler, string) {
	t.Helper()
	db, r, h := newTestSetup()
	r.PUT("/api/incidents/:incident_id/translations/:language_code", h.SaveIncidentTranslationHandler)
	page := newTestPage(db)
	for _, code := range []string{"de", "en", "fr"} {
		db.FirstOrCreate(&models.Language{Code: code, Name: code, IsActive: true}, models.Language{Code: code})
	}
	db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true})
	db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "en"})
	incident := newTestIncident(db, page.ID)
	return db, r, "/api/incidents/" + strconv.Itoa(int(incident.ID)) + "/translations/"
}

func putTrans(r http.Handler, url string, status string) int {
	body, _ := json.Marshal(map[string]string{"title": "Outage", "message": "API is down", "status": status})
	return doTranslationRequest(r, "PUT", url, body).Code
}

func TestSaveIncidentTranslationHandler_CreatesThenUpdates(t *testing.T) {
	db, r, base := saveTransSetup(t)
	if c := putTrans(r, base+"en", "draft"); c != http.StatusOK {
		t.Fatalf("anlegen: erwartet 200, war %d", c)
	}
	if c := putTrans(r, base+"en", "confirmed"); c != http.StatusOK {
		t.Fatalf("ändern: erwartet 200, war %d", c)
	}
	var all []models.IncidentTranslation
	db.Find(&all)
	if len(all) != 1 || all[0].Status != "confirmed" || all[0].Title != "Outage" {
		t.Fatalf("erwartet genau eine bestätigte Übersetzung, war %+v", all)
	}
}

func TestSaveIncidentTranslationHandler_RejectsDefaultAndInactiveLanguage(t *testing.T) {
	_, r, base := saveTransSetup(t)
	for _, code := range []string{"de", "fr"} { // Standardsprache bzw. nicht aktiv auf der Seite
		if c := putTrans(r, base+code, "draft"); c != http.StatusBadRequest {
			t.Errorf("%s: erwartet 400, war %d", code, c)
		}
	}
}

func TestSaveIncidentTranslationHandler_RejectsInvalidInput(t *testing.T) {
	_, r, base := saveTransSetup(t)
	if c := putTrans(r, base+"en", "published"); c != http.StatusBadRequest {
		t.Errorf("ungültiger Stand: erwartet 400, war %d", c)
	}
	if c := doTranslationRequest(r, "PUT", base+"en", []byte(`{"title":"","message":"x","status":"draft"}`)).Code; c != http.StatusBadRequest {
		t.Errorf("leerer Titel: erwartet 400, war %d", c)
	}
	if c := doTranslationRequest(r, "PUT", "/api/incidents/999/translations/en", []byte(`{"title":"a","message":"b","status":"draft"}`)).Code; c != http.StatusNotFound {
		t.Errorf("unbekannter Vorfall: erwartet 404, war %d", c)
	}
	if c := doTranslationRequest(r, "PUT", "/api/incidents/abc/translations/en", []byte(`{}`)).Code; c != http.StatusBadRequest {
		t.Errorf("ungültige ID: erwartet 400, war %d", c)
	}
}

func TestSaveIncidentTranslationHandler_DatabaseErrorsReturn500(t *testing.T) {
	db, r, base := saveTransSetup(t)
	// Speichern scheitert: ein Trigger weist das Einfügen ab.
	db.Exec("CREATE TRIGGER no_insert BEFORE INSERT ON incident_translations BEGIN SELECT RAISE(ABORT, 'gesperrt'); END")
	if c := putTrans(r, base+"en", "draft"); c != http.StatusInternalServerError {
		t.Errorf("Speichern: erwartet 500, war %d", c)
	}
	// Lesen scheitert: Tabelle fehlt.
	db.Exec("DROP TRIGGER no_insert")
	if err := db.Migrator().DropTable(&models.IncidentTranslation{}); err != nil {
		t.Fatal(err)
	}
	if c := putTrans(r, base+"en", "draft"); c != http.StatusInternalServerError {
		t.Errorf("Lesen: erwartet 500, war %d", c)
	}
}
