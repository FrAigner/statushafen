package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// doNoticeRequest führt einen Request gegen den Test-Router aus; body wird
// nur bei Nicht-Leere als Content-Type: application/json gesetzt.
func doNoticeRequest(r http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestGetActiveNoticesHandler ist der geforderte Regressionstest:
// eine aktive Notice (ohne Zeitfenster) muss über den öffentlichen Lesepfad
// erscheinen, eine abgelaufene (EndAt in der Vergangenheit) oder inaktive
// (IsActive=false) Notice darf dort NICHT auftauchen.
func TestGetActiveNoticesHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/:page_id/notices", h.GetActiveNoticesHandler)

	page := models.StatusPage{Name: "Notice Page", Slug: "notice-page"}
	db.Create(&page)
	pid := strconv.Itoa(int(page.ID))

	past := time.Now().Add(-48 * time.Hour)
	recentPast := time.Now().Add(-1 * time.Hour)

	active := models.GeneralNotice{
		StatusPageID: page.ID,
		Title:        "Aktive Ankündigung",
		Message:      "Alles wie gewohnt.",
		Level:        "info",
		IsActive:     true,
	}
	db.Create(&active)

	expired := models.GeneralNotice{
		StatusPageID: page.ID,
		Title:        "Abgelaufene Ankündigung",
		Message:      "Das hier sollte nicht mehr sichtbar sein.",
		Level:        "warning",
		IsActive:     true,
		StartAt:      &past,
		EndAt:        &recentPast,
	}
	db.Create(&expired)

	inactive := models.GeneralNotice{
		StatusPageID: page.ID,
		Title:        "Inaktive Ankündigung",
		Message:      "Manuell deaktiviert.",
		Level:        "critical",
		IsActive:     false,
	}
	db.Create(&inactive)

	// Notice einer anderen Statusseite darf nicht mit auftauchen (Mandantentrennung).
	otherPage := models.StatusPage{Name: "Other Page", Slug: "other-page"}
	db.Create(&otherPage)
	otherPageNotice := models.GeneralNotice{
		StatusPageID: otherPage.ID,
		Title:        "Fremde Ankündigung",
		Message:      "Gehört zu einer anderen Seite.",
		Level:        "info",
		IsActive:     true,
	}
	db.Create(&otherPageNotice)

	req, _ := http.NewRequest("GET", "/api/pages/"+pid+"/notices", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp []models.GeneralNotice
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(resp) != 1 {
		t.Fatalf("Expected exactly 1 active notice, got %d: %+v", len(resp), resp)
	}
	if resp[0].Title != "Aktive Ankündigung" {
		t.Errorf("Expected 'Aktive Ankündigung' to be the only visible notice, got %q", resp[0].Title)
	}
}

// TestGetActiveNoticesHandler_FutureStartNotYetVisible: eine Notice, deren
// StartAt in der Zukunft liegt, darf noch nicht erscheinen.
func TestGetActiveNoticesHandler_FutureStartNotYetVisible(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/:page_id/notices", h.GetActiveNoticesHandler)

	page := models.StatusPage{Name: "Future Notice Page", Slug: "future-notice-page"}
	db.Create(&page)
	pid := strconv.Itoa(int(page.ID))

	future := time.Now().Add(24 * time.Hour)
	notice := models.GeneralNotice{
		StatusPageID: page.ID,
		Title:        "Noch nicht gestartet",
		Message:      "Startet erst morgen.",
		Level:        "info",
		IsActive:     true,
		StartAt:      &future,
	}
	db.Create(&notice)

	req, _ := http.NewRequest("GET", "/api/pages/"+pid+"/notices", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var resp []models.GeneralNotice
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp) != 0 {
		t.Fatalf("Expected 0 notices (StartAt in the future), got %d", len(resp))
	}
}

// TestNoticeCRUDHandlers deckt die Admin-CRUD-Endpunkte ab.
func TestNoticeCRUDHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/:page_id/notices/all", h.GetNoticesHandler)
	r.POST("/api/pages/:page_id/notices", h.CreateNoticeHandler)
	r.PUT("/api/pages/:page_id/notices/:id", h.UpdateNoticeHandler)
	r.DELETE("/api/pages/:page_id/notices/:id", h.DeleteNoticeHandler)

	page := models.StatusPage{Name: "CRUD Notice Page", Slug: "crud-notice-page"}
	db.Create(&page)
	pid := strconv.Itoa(int(page.ID))

	var createdID uint

	t.Run("Create", func(t *testing.T) {
		w := doNoticeRequest(r, "POST", "/api/pages/"+pid+"/notices",
			[]byte(`{"Title":"Wartungsrichtlinie geändert","Message":"Details **hier**.","Level":"warning","Collapsed":true}`))
		requireStatusCode(t, w, http.StatusCreated)
		var created models.GeneralNotice
		_ = json.Unmarshal(w.Body.Bytes(), &created)
		createdID = created.ID
		if !created.IsActive {
			t.Errorf("Expected default IsActive=true")
		}
		if created.Level != "warning" {
			t.Errorf("Expected Level=warning, got %s", created.Level)
		}
	})

	t.Run("Invalid level rejected", func(t *testing.T) {
		w := doNoticeRequest(r, "POST", "/api/pages/"+pid+"/notices", []byte(`{"Title":"x","Message":"y","Level":"bogus"}`))
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("List all (admin) includes inactive", func(t *testing.T) {
		w := doNoticeRequest(r, "GET", "/api/pages/"+pid+"/notices/all", nil)
		requireStatusCode(t, w, http.StatusOK)
		var list []models.GeneralNotice
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 1 {
			t.Fatalf("Expected 1 notice, got %d", len(list))
		}
	})

	t.Run("Update", func(t *testing.T) {
		w := doNoticeRequest(r, "PUT", "/api/pages/"+pid+"/notices/"+strconv.Itoa(int(createdID)),
			[]byte(`{"Title":"Aktualisiert","Message":"Neuer Text.","Level":"critical","IsActive":false}`))
		requireStatusCode(t, w, http.StatusOK)
		var updated models.GeneralNotice
		db.First(&updated, createdID)
		if updated.Title != "Aktualisiert" || updated.IsActive {
			t.Errorf("Notice not updated correctly: %+v", updated)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		w := doNoticeRequest(r, "DELETE", "/api/pages/"+pid+"/notices/"+strconv.Itoa(int(createdID)), nil)
		requireStatusCode(t, w, http.StatusOK)
		var count int64
		db.Model(&models.GeneralNotice{}).Where("id = ?", createdID).Count(&count)
		if count != 0 {
			t.Errorf("Notice was not deleted")
		}
	})
}
