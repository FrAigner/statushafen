package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// doTagRequest führt einen Request gegen den Test-Router aus und JSON-kodiert
// payload (falls gesetzt) als Body; reduziert die sonst pro Subtest
// wiederholte Request/Serve-Boilerplate.
func doTagRequest(r http.Handler, method, path string, payload interface{}) *httptest.ResponseRecorder {
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = bytes.NewBuffer(b)
	}
	req, _ := http.NewRequest(method, path, body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestTagHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	r.GET("/api/tags", h.GetTagsHandler)
	r.POST("/api/tags", h.CreateTagHandler)
	r.PUT("/api/tags/:id", h.UpdateTagHandler)
	r.DELETE("/api/tags/:id", h.DeleteTagHandler)

	page := models.StatusPage{Name: "Acme", Slug: "acme"}
	if err := db.Create(&page).Error; err != nil {
		t.Fatalf("failed to seed status page: %v", err)
	}

	var globalTagID uint
	var pageTagID uint

	t.Run("Create global tag", func(t *testing.T) {
		w := doTagRequest(r, "POST", "/api/tags", map[string]interface{}{"name": "eu-west"})

		requireStatusCode(t, w, http.StatusCreated)
		var tag models.Tag
		_ = json.Unmarshal(w.Body.Bytes(), &tag)
		globalTagID = tag.ID
		if tag.Name != "eu-west" || tag.StatusPageID != nil {
			t.Errorf("Global tag created incorrectly: %+v", tag)
		}
	})

	t.Run("Create page-scoped tag", func(t *testing.T) {
		w := doTagRequest(r, "POST", "/api/tags", map[string]interface{}{"name": "internal", "status_page_id": page.ID})

		requireStatusCode(t, w, http.StatusCreated)
		var tag models.Tag
		_ = json.Unmarshal(w.Body.Bytes(), &tag)
		pageTagID = tag.ID
		if tag.StatusPageID == nil || *tag.StatusPageID != page.ID {
			t.Errorf("Page-scoped tag created incorrectly: %+v", tag)
		}
	})

	t.Run("Duplicate tag name in same scope is rejected", func(t *testing.T) {
		w := doTagRequest(r, "POST", "/api/tags", map[string]interface{}{"name": "EU-WEST"})

		requireStatusCode(t, w, http.StatusConflict)
	})

	t.Run("Get all tags", func(t *testing.T) {
		w := doTagRequest(r, "GET", "/api/tags", nil)

		requireStatusCode(t, w, http.StatusOK)
		var tags []models.Tag
		_ = json.Unmarshal(w.Body.Bytes(), &tags)
		if len(tags) != 2 {
			t.Errorf("Expected 2 tags, got %d", len(tags))
		}
	})

	t.Run("Get tags scoped to status page", func(t *testing.T) {
		w := doTagRequest(r, "GET", "/api/tags?status_page_id="+strconv.FormatUint(uint64(page.ID), 10), nil)

		requireStatusCode(t, w, http.StatusOK)
		var tags []models.Tag
		_ = json.Unmarshal(w.Body.Bytes(), &tags)
		if len(tags) != 2 {
			t.Errorf("Expected 2 tags (global + page-scoped), got %d", len(tags))
		}
	})

	t.Run("Rename tag", func(t *testing.T) {
		w := doTagRequest(r, "PUT", "/api/tags/"+strconv.Itoa(int(globalTagID)), map[string]interface{}{"name": "eu-west-1"})

		requireStatusCode(t, w, http.StatusOK)

		var tag models.Tag
		db.First(&tag, globalTagID)
		if tag.Name != "eu-west-1" {
			t.Errorf("Tag was not renamed, got %q", tag.Name)
		}
	})

	t.Run("Renaming to a name already used in the same scope is rejected", func(t *testing.T) {
		other := models.Tag{Name: "us-east"}
		db.Create(&other)

		w := doTagRequest(r, "PUT", "/api/tags/"+strconv.Itoa(int(globalTagID)), map[string]interface{}{"name": "US-EAST"})

		requireStatusCode(t, w, http.StatusConflict)
	})

	t.Run("Renaming to a name used in a different scope is allowed", func(t *testing.T) {
		w := doTagRequest(r, "PUT", "/api/tags/"+strconv.Itoa(int(globalTagID)), map[string]interface{}{"name": "internal"})

		requireStatusCode(t, w, http.StatusOK)
	})

	t.Run("Delete tag cascades related join rows", func(t *testing.T) {
		comp := models.Component{StatusPageID: page.ID, Name: "API", Status: "operational"}
		db.Create(&comp)
		db.Create(&models.ComponentTag{ComponentID: comp.ID, TagID: pageTagID})

		w := doTagRequest(r, "DELETE", "/api/tags/"+strconv.Itoa(int(pageTagID)), nil)

		requireStatusCode(t, w, http.StatusOK)

		var tagCount int64
		db.Model(&models.Tag{}).Where("id = ?", pageTagID).Count(&tagCount)
		if tagCount != 0 {
			t.Errorf("Tag was not deleted")
		}

		var linkCount int64
		db.Model(&models.ComponentTag{}).Where("tag_id = ?", pageTagID).Count(&linkCount)
		if linkCount != 0 {
			t.Errorf("Expected ComponentTag link to be cascade-deleted, found %d", linkCount)
		}
	})

	t.Run("Delete unknown tag returns 404", func(t *testing.T) {
		w := doTagRequest(r, "DELETE", "/api/tags/999999", nil)

		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Update tag with non-numeric ID returns 400", func(t *testing.T) {
		w := doTagRequest(r, "PUT", "/api/tags/not-a-number", map[string]string{"name": "x"})

		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Update unknown tag returns 404", func(t *testing.T) {
		w := doTagRequest(r, "PUT", "/api/tags/999999", map[string]string{"name": "x"})

		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Delete tag with non-numeric ID returns 400", func(t *testing.T) {
		w := doTagRequest(r, "DELETE", "/api/tags/not-a-number", nil)

		requireStatusCode(t, w, http.StatusBadRequest)
	})
}
