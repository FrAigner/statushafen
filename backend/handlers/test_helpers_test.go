package handlers_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Test setup helpers
func newTestSetup() (*gorm.DB, *gin.Engine, *handlers.Handler) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	return db, r, h
}

func newTestPage(db *gorm.DB) models.StatusPage {
	page := models.StatusPage{Name: "Test Page", Slug: "test-page"}
	db.Create(&page)
	return page
}

// Request helper
func doTranslationRequest(r http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
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
