package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSubscriberMeHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.Use(func(c *gin.Context) {
		email := c.GetHeader("X-Mock-Email")
		if email != "" {
			c.Set("email", email)
		}
		c.Next()
	})

	r.GET("/api/me/subscription", h.GetMySubscriptionHandler)
	r.PUT("/api/me/subscription", h.UpdateMySubscriptionHandler)
	r.DELETE("/api/me/subscription", h.DeleteMySubscriptionHandler)
	r.POST("/api/subscribe/test-webhook", h.TestWebhookHandler)

	page := models.StatusPage{Name: "Page 1", Slug: "page-1"}
	db.Create(&page)

	sub := models.Subscriber{
		StatusPageID:           page.ID,
		Email:                  "sub@example.com",
		Channel:                "email",
		SubscribedComponentIDs: "1,2",
	}
	db.Create(&sub)

	t.Run("Get My Subscription - Valid", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/me/subscription?slug=page-1", nil)
		req.Header.Set("X-Mock-Email", "sub@example.com")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["component_ids"] != "1,2" {
			t.Errorf("Expected 1,2 got %v", resp["component_ids"])
		}
	})

	t.Run("Get My Subscription - Unauthenticated", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/me/subscription?slug=page-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401, got %d", w.Code)
		}
	})

	t.Run("Update My Subscription - Valid", func(t *testing.T) {
		payload := map[string]string{
			"component_ids": "all",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("PUT", "/api/me/subscription?slug=page-1", bytes.NewBuffer(body))
		req.Header.Set("X-Mock-Email", "sub@example.com")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", w.Code)
		}

		var dbSub models.Subscriber
		db.First(&dbSub, sub.ID)
		if dbSub.SubscribedComponentIDs != "all" {
			t.Errorf("Subscription not updated")
		}
	})

	// "Abonnement beenden" braucht einen eigenen Endpunkt
	// mit derselben Besitzprüfung wie GET/PUT (findMySubscriber).
	t.Run("Delete My Subscription - Valid", func(t *testing.T) {
		delSub := models.Subscriber{
			StatusPageID:           page.ID,
			Email:                  "todelete@example.com",
			Channel:                "email",
			SubscribedComponentIDs: "all",
		}
		db.Create(&delSub)

		req, _ := http.NewRequest("DELETE", "/api/me/subscription?slug=page-1", nil)
		req.Header.Set("X-Mock-Email", "todelete@example.com")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", w.Code)
		}

		var count int64
		db.Model(&models.Subscriber{}).Where("id = ?", delSub.ID).Count(&count)
		if count != 0 {
			t.Errorf("Expected subscriber to be deleted, still found %d", count)
		}
	})

	t.Run("Delete My Subscription - Unauthenticated", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/me/subscription?slug=page-1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Expected 401, got %d", w.Code)
		}
	})

	t.Run("Delete My Subscription - Not Found", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/api/me/subscription?slug=page-1", nil)
		req.Header.Set("X-Mock-Email", "nobody@example.com")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected 404, got %d", w.Code)
		}
	})

	t.Run("Test Webhook - Missing URL", func(t *testing.T) {
		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", "/api/subscribe/test-webhook", bytes.NewBuffer(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d", w.Code)
		}
	})
}

// TestDeleteMySubscriptionHandler_DBErrorReturns500 deckt den Fehlerpfad von
// h.DB.Delete ab: die Besitzprüfung (findMySubscriber)
// muss noch lesen können, aber das anschließende Delete (Schreiben) schlägt
// fehl - analog zu TestUpdateStatusPageHandler_SaveErrorReturns500 wird dafür
// die DB-Datei nach dem Seeden read-only gemacht (ein einfaches Schließen der
// Connection würde auch die vorherigen Lese-Queries scheitern lassen und
// damit fälschlich ein 404 statt 500 liefern).
func TestDeleteMySubscriptionHandler_DBErrorReturns500(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "readonly-delete.db")
	writeDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open write DB: %v", err)
	}
	if err := writeDB.AutoMigrate(&models.StatusPage{}, &models.Subscriber{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	page := models.StatusPage{Name: "Page 2", Slug: "page-2"}
	if err := writeDB.Create(&page).Error; err != nil {
		t.Fatalf("failed to seed page: %v", err)
	}
	sub := models.Subscriber{
		StatusPageID:           page.ID,
		Email:                  "closed-db@example.com",
		Channel:                "email",
		SubscribedComponentIDs: "all",
	}
	if err := writeDB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to seed subscriber: %v", err)
	}
	sqlWriteDB, _ := writeDB.DB()
	_ = sqlWriteDB.Close()

	if err := os.Chmod(dbPath, 0o444); err != nil {
		t.Fatalf("failed to chmod db file read-only: %v", err)
	}
	defer func() {
		_ = os.Chmod(dbPath, 0o644)
		_ = os.Remove(dbPath)
	}()

	roDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open read-only DB: %v", err)
	}

	h := handlers.NewHandler(roDB)
	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) {
		c.Set("email", "closed-db@example.com")
		c.Next()
	})
	r.DELETE("/api/me/subscription", h.DeleteMySubscriptionHandler)

	req, _ := http.NewRequest("DELETE", "/api/me/subscription?slug=page-2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500, got %d", w.Code)
	}
}
