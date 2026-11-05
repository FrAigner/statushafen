package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

func TestStatusPagePublicHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/pages", h.GetStatusPagesHandler)
	r.GET("/api/pages/slug/:slug", h.GetStatusPageBySlugHandler)
	r.GET("/api/public/pages", h.GetPublicPagesHandler)

	page1 := models.StatusPage{Name: "Public Page", Slug: "public", IsPublicIndexed: true}
	db.Create(&page1)

	page2 := models.StatusPage{Name: "Private Page", Slug: "private"}
	db.Create(&page2)
	db.Model(&page2).Update("is_public_indexed", false)

	t.Run("Get All Status Pages", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/pages", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var pages []models.StatusPage
		_ = json.Unmarshal(w.Body.Bytes(), &pages)
		if len(pages) != 2 {
			t.Errorf("Expected 2 pages, got %d", len(pages))
		}
	})

	t.Run("Get Status Page By Slug", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/pages/slug/public", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var page models.StatusPage
		_ = json.Unmarshal(w.Body.Bytes(), &page)
		if page.Slug != "public" {
			t.Errorf("Expected slug 'public', got %s", page.Slug)
		}
	})

	t.Run("Get Status Page By Slug - Not Found", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/pages/slug/invalid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Get Public Pages (Indexed Only)", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/public/pages", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		requireStatusCode(t, w, http.StatusOK)

		var pages []models.StatusPage
		_ = json.Unmarshal(w.Body.Bytes(), &pages)
		if len(pages) != 1 {
			t.Errorf("Expected 1 public indexed page, got %d", len(pages))
		}
		if pages[0].Slug != "public" {
			t.Errorf("Expected public page to be returned")
		}
	})
}
