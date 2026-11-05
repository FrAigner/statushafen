package handlers_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"gorm.io/gorm"

	"github.com/fraigner/statushafen/models"
)

func newTestGeneralNotice(db *gorm.DB, pageID uint) models.GeneralNotice {
	notice := models.GeneralNotice{
		StatusPageID: pageID,
		Title:        "Test Notice",
		Message:      "Original message",
		Level:        "info",
		IsActive:     true,
	}
	db.Create(&notice)
	return notice
}

func newGeneralNoticeTranslation(noticeID uint, lang, status string) *models.GeneralNoticeTranslation {
	return &models.GeneralNoticeTranslation{
		GeneralNoticeID: noticeID,
		LanguageCode:    lang,
		Title:           "Translated Title",
		Message:         "Translated Message",
		Status:          status,
		Source:          "manual",
	}
}

func TestGetGeneralNoticeTranslationsHandler(t *testing.T) {
	tests := []struct {
		name              string
		setupTranslations func(*gorm.DB, uint)
		expectedCount     int
		expectedLang      string
	}{
		{
			name: "ReturnsOnlyConfirmedTranslations",
			setupTranslations: func(db *gorm.DB, nid uint) {
				db.Create(newGeneralNoticeTranslation(nid, "de", "confirmed"))
				db.Create(newGeneralNoticeTranslation(nid, "fr", "draft"))
			},
			expectedCount: 1,
			expectedLang:  "de",
		},
		{
			name: "EmptyForNoConfirmed",
			setupTranslations: func(db *gorm.DB, nid uint) {
				db.Create(newGeneralNoticeTranslation(nid, "de", "draft"))
			},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, r, h := newTestSetup()
			r.GET("/api/notices/:notice_id/translations", h.GetGeneralNoticeTranslationsHandler)
			page := newTestPage(db)
			notice := newTestGeneralNotice(db, page.ID)
			if tt.setupTranslations != nil {
				tt.setupTranslations(db, notice.ID)
			}
			nid := strconv.Itoa(int(notice.ID))

			w := doTranslationRequest(r, "GET", "/api/notices/"+nid+"/translations", nil)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
			}

			var resp []models.GeneralNoticeTranslation
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

func TestCreateGeneralNoticeTranslationHandler_Success(t *testing.T) {
	db, r, h := newTestSetup()
	r.POST("/api/notices/:notice_id/translations", h.CreateGeneralNoticeTranslationHandler)

	page := newTestPage(db)
	notice := newTestGeneralNotice(db, page.ID)
	nid := strconv.Itoa(int(notice.ID))

	payload := map[string]string{
		"language_code": "de",
		"title":         "Mitteilung",
		"message":       "Deutsche Nachricht",
		"source":        "manual",
	}
	body, _ := json.Marshal(payload)

	w := doTranslationRequest(r, "POST", "/api/notices/"+nid+"/translations", body)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp models.GeneralNoticeTranslation
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.GeneralNoticeID != notice.ID {
		t.Errorf("Expected notice_id %d, got %d", notice.ID, resp.GeneralNoticeID)
	}
	if resp.Status != "draft" {
		t.Errorf("Expected status=draft, got %q", resp.Status)
	}
}

func TestCreateGeneralNoticeTranslationHandler_DuplicateLanguageReturns409(t *testing.T) {
	db, r, h := newTestSetup()
	r.POST("/api/notices/:notice_id/translations", h.CreateGeneralNoticeTranslationHandler)

	page := newTestPage(db)
	notice := newTestGeneralNotice(db, page.ID)

	db.Create(newGeneralNoticeTranslation(notice.ID, "de", "draft"))

	nid := strconv.Itoa(int(notice.ID))
	payload := map[string]string{
		"language_code": "de",
		"title":         "Different",
		"message":       "Different",
		"source":        "manual",
	}
	body, _ := json.Marshal(payload)

	w := doTranslationRequest(r, "POST", "/api/notices/"+nid+"/translations", body)

	if w.Code != http.StatusConflict {
		t.Fatalf("Expected 409, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestCreateGeneralNoticeTranslationHandler_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		body map[string]string
	}{
		{"MissingLanguageCode", map[string]string{"title": "Title", "message": "Msg"}},
		{"MissingTitle", map[string]string{"language_code": "de", "message": "Msg"}},
		{"MissingMessage", map[string]string{"language_code": "de", "title": "Title"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, r, h := newTestSetup()
			r.POST("/api/notices/:notice_id/translations", h.CreateGeneralNoticeTranslationHandler)

			page := newTestPage(db)
			notice := newTestGeneralNotice(db, page.ID)
			nid := strconv.Itoa(int(notice.ID))

			body, _ := json.Marshal(tt.body)
			w := doTranslationRequest(r, "POST", "/api/notices/"+nid+"/translations", body)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCreateGeneralNoticeTranslationHandler_NoticeNotFound(t *testing.T) {
	_, r, h := newTestSetup()
	r.POST("/api/notices/:notice_id/translations", h.CreateGeneralNoticeTranslationHandler)

	payload := map[string]string{
		"language_code": "de",
		"title":         "Title",
		"message":       "Msg",
		"source":        "manual",
	}
	body, _ := json.Marshal(payload)

	w := doTranslationRequest(r, "POST", "/api/notices/99999/translations", body)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestCreateGeneralNoticeTranslationHandler_InvalidID(t *testing.T) {
	_, r, h := newTestSetup()
	r.POST("/api/notices/:notice_id/translations", h.CreateGeneralNoticeTranslationHandler)

	payload := map[string]string{
		"language_code": "de",
		"title":         "Title",
		"message":       "Msg",
		"source":        "manual",
	}
	body, _ := json.Marshal(payload)

	w := doTranslationRequest(r, "POST", "/api/notices/invalid/translations", body)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestConfirmGeneralNoticeTranslationHandler_Success(t *testing.T) {
	db, r, h := newTestSetup()
	r.PATCH("/api/notice-translations/:translation_id/confirm", h.ConfirmGeneralNoticeTranslationHandler)

	page := newTestPage(db)
	notice := newTestGeneralNotice(db, page.ID)
	trans := newGeneralNoticeTranslation(notice.ID, "de", "draft")
	db.Create(trans)

	tid := strconv.Itoa(int(trans.ID))
	w := doTranslationRequest(r, "PATCH", "/api/notice-translations/"+tid+"/confirm", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp models.GeneralNoticeTranslation
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if resp.Status != "confirmed" {
		t.Errorf("Expected status=confirmed, got %q", resp.Status)
	}
}

func TestConfirmGeneralNoticeTranslationHandler_Idempotent(t *testing.T) {
	db, r, h := newTestSetup()
	r.PATCH("/api/notice-translations/:translation_id/confirm", h.ConfirmGeneralNoticeTranslationHandler)

	page := newTestPage(db)
	notice := newTestGeneralNotice(db, page.ID)
	trans := newGeneralNoticeTranslation(notice.ID, "de", "confirmed")
	db.Create(trans)

	tid := strconv.Itoa(int(trans.ID))
	w := doTranslationRequest(r, "PATCH", "/api/notice-translations/"+tid+"/confirm", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 for idempotent confirm, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestConfirmGeneralNoticeTranslationHandler_NotFound(t *testing.T) {
	_, r, h := newTestSetup()
	r.PATCH("/api/notice-translations/:translation_id/confirm", h.ConfirmGeneralNoticeTranslationHandler)

	w := doTranslationRequest(r, "PATCH", "/api/notice-translations/99999/confirm", nil)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestConfirmGeneralNoticeTranslationHandler_InvalidID(t *testing.T) {
	_, r, h := newTestSetup()
	r.PATCH("/api/notice-translations/:translation_id/confirm", h.ConfirmGeneralNoticeTranslationHandler)

	w := doTranslationRequest(r, "PATCH", "/api/notice-translations/invalid/confirm", nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestDeleteGeneralNoticeTranslationHandler_Success(t *testing.T) {
	db, r, h := newTestSetup()
	r.DELETE("/api/notice-translations/:translation_id", h.DeleteGeneralNoticeTranslationHandler)

	page := newTestPage(db)
	notice := newTestGeneralNotice(db, page.ID)
	trans := newGeneralNoticeTranslation(notice.ID, "de", "draft")
	db.Create(trans)

	tid := strconv.Itoa(int(trans.ID))
	w := doTranslationRequest(r, "DELETE", "/api/notice-translations/"+tid, nil)

	if w.Code != http.StatusNoContent {
		t.Fatalf("Expected 204, got %d. Body: %s", w.Code, w.Body.String())
	}

	var count int64
	db.Model(&models.GeneralNoticeTranslation{}).Where("id = ?", trans.ID).Count(&count)
	if count != 0 {
		t.Errorf("Expected translation to be deleted")
	}
}

func TestDeleteGeneralNoticeTranslationHandler_NotFound(t *testing.T) {
	_, r, h := newTestSetup()
	r.DELETE("/api/notice-translations/:translation_id", h.DeleteGeneralNoticeTranslationHandler)

	w := doTranslationRequest(r, "DELETE", "/api/notice-translations/99999", nil)

	if w.Code != http.StatusNotFound {
		t.Fatalf("Expected 404, got %d. Body: %s", w.Code, w.Body.String())
	}
}

func TestDeleteGeneralNoticeTranslationHandler_InvalidID(t *testing.T) {
	_, r, h := newTestSetup()
	r.DELETE("/api/notice-translations/:translation_id", h.DeleteGeneralNoticeTranslationHandler)

	w := doTranslationRequest(r, "DELETE", "/api/notice-translations/invalid", nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// Speichern je Sprache aus dem Bearbeiten-Formular.
func saveNoticeTransSetup(t *testing.T) (*gorm.DB, http.Handler, string) {
	t.Helper()
	db, r, h := newTestSetup()
	r.PUT("/api/notices/:notice_id/translations/:language_code", h.SaveGeneralNoticeTranslationHandler)
	page := newTestPage(db)
	for _, code := range []string{"de", "en", "fr"} {
		db.FirstOrCreate(&models.Language{Code: code, Name: code, IsActive: true}, models.Language{Code: code})
	}
	db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "de", IsDefault: true})
	db.Create(&models.StatusPageLanguage{StatusPageID: page.ID, LanguageCode: "en"})
	notice := models.GeneralNotice{StatusPageID: page.ID, Title: "Hinweis", Message: "Text", Level: "info"}
	db.Create(&notice)
	return db, r, "/api/notices/" + strconv.Itoa(int(notice.ID)) + "/translations/"
}

func putNoticeTrans(r http.Handler, url, status string) int {
	body, _ := json.Marshal(map[string]string{"title": "Notice", "message": "Text", "status": status})
	return doTranslationRequest(r, "PUT", url, body).Code
}

func TestSaveGeneralNoticeTranslationHandler_CreatesThenUpdates(t *testing.T) {
	db, r, base := saveNoticeTransSetup(t)
	if c := putNoticeTrans(r, base+"en", "draft"); c != http.StatusOK {
		t.Fatalf("anlegen: erwartet 200, war %d", c)
	}
	if c := putNoticeTrans(r, base+"en", "confirmed"); c != http.StatusOK {
		t.Fatalf("ändern: erwartet 200, war %d", c)
	}
	var all []models.GeneralNoticeTranslation
	db.Find(&all)
	if len(all) != 1 || all[0].Status != "confirmed" {
		t.Fatalf("erwartet genau eine bestätigte Übersetzung, war %+v", all)
	}
}

func TestSaveGeneralNoticeTranslationHandler_RejectsBadInput(t *testing.T) {
	_, r, base := saveNoticeTransSetup(t)
	for _, code := range []string{"de", "fr"} { // Standardsprache bzw. nicht aktiv
		if c := putNoticeTrans(r, base+code, "draft"); c != http.StatusBadRequest {
			t.Errorf("%s: erwartet 400, war %d", code, c)
		}
	}
	if c := putNoticeTrans(r, base+"en", "published"); c != http.StatusBadRequest {
		t.Errorf("ungültiger Stand: erwartet 400, war %d", c)
	}
	if c := doTranslationRequest(r, "PUT", "/api/notices/999/translations/en", []byte(`{"title":"a","message":"b","status":"draft"}`)).Code; c != http.StatusNotFound {
		t.Errorf("unbekannte Ankündigung: erwartet 404, war %d", c)
	}
	if c := doTranslationRequest(r, "PUT", "/api/notices/abc/translations/en", []byte(`{}`)).Code; c != http.StatusBadRequest {
		t.Errorf("ungültige ID: erwartet 400, war %d", c)
	}
}

func TestSaveGeneralNoticeTranslationHandler_DatabaseErrorsReturn500(t *testing.T) {
	db, r, base := saveNoticeTransSetup(t)
	db.Exec("CREATE TRIGGER no_insert BEFORE INSERT ON general_notice_translations BEGIN SELECT RAISE(ABORT, 'gesperrt'); END")
	if c := putNoticeTrans(r, base+"en", "draft"); c != http.StatusInternalServerError {
		t.Errorf("Speichern: erwartet 500, war %d", c)
	}
	db.Exec("DROP TRIGGER no_insert")
	if err := db.Migrator().DropTable(&models.GeneralNoticeTranslation{}); err != nil {
		t.Fatal(err)
	}
	if c := putNoticeTrans(r, base+"en", "draft"); c != http.StatusInternalServerError {
		t.Errorf("Lesen: erwartet 500, war %d", c)
	}
}
