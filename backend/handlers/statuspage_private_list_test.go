package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"

	"gorm.io/gorm"
)

func listPublicSlugs(t *testing.T, r http.Handler) []string {
	t.Helper()
	req, _ := http.NewRequest("GET", "/api/public/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	requireStatusCode(t, w, http.StatusOK)
	var pages []models.StatusPage
	if err := json.Unmarshal(w.Body.Bytes(), &pages); err != nil {
		t.Fatalf("Antwort nicht lesbar: %v", err)
	}
	slugs := []string{}
	for _, p := range pages {
		slugs = append(slugs, p.Slug)
	}
	return slugs
}

// Eine private Seite darf nie in der öffentlichen Seitenliste
// erscheinen, auch wenn IsPublicIndexed (Altbestand, Seed-Default) true ist.
func TestGetPublicPagesHandler_ExcludesPrivatePages(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/pages", h.GetPublicPagesHandler)

	db.Create(&models.StatusPage{Name: "Offen", Slug: "offen-319", IsPublicIndexed: true})
	db.Create(&models.StatusPage{Name: "Privat", Slug: "privat-319", IsPublicIndexed: true, IsPrivate: true})

	slugs := listPublicSlugs(t, r)
	if len(slugs) != 1 || slugs[0] != "offen-319" {
		t.Errorf("Erwartet nur offen-319, erhalten %v", slugs)
	}
}

// Beim Speichern mit IsPrivate wird IsPublicIndexed erzwungen
// auf false (Update und Create), sonst bliebe der alte Wert stehen.
func TestStatusPageSave_PrivateForcesNotIndexed(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.Use(mockUserIDMiddleware)
	r.POST("/api/admin/pages", h.CreateStatusPageHandler)
	r.PUT("/api/admin/pages/:page_id", h.UpdateStatusPageHandler)

	t.Run("Update", func(t *testing.T) {
		page := models.StatusPage{Name: "Alt", Slug: "alt-319", IsPublicIndexed: true}
		db.Create(&page)
		w := doStatusPageRequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID)), map[string]interface{}{
			"Name": "Alt", "Slug": "alt-319", "IsPublicIndexed": true, "IsPrivate": true,
		})
		requireStatusCode(t, w, http.StatusOK)
		var reloaded models.StatusPage
		db.First(&reloaded, page.ID)
		if !reloaded.IsPrivate || reloaded.IsPublicIndexed {
			t.Errorf("Erwartet privat und nicht gelistet, erhalten private=%v indexed=%v", reloaded.IsPrivate, reloaded.IsPublicIndexed)
		}
	})

	t.Run("Create", func(t *testing.T) {
		w := doStatusPageRequest(r, "POST", "/api/admin/pages", map[string]interface{}{
			"Name": "Neu", "Slug": "neu-319", "IsPublicIndexed": true, "IsPrivate": true,
		})
		requireStatusCode(t, w, http.StatusCreated)
		var reloaded models.StatusPage
		db.Where("slug = ?", "neu-319").First(&reloaded)
		if !reloaded.IsPrivate || reloaded.IsPublicIndexed {
			t.Errorf("Erwartet privat und nicht gelistet, erhalten private=%v indexed=%v", reloaded.IsPrivate, reloaded.IsPublicIndexed)
		}
	})

	t.Run("Nicht-private Seite bleibt gelistet", func(t *testing.T) {
		page := models.StatusPage{Name: "Offen", Slug: "offen2-319"}
		db.Create(&page)
		w := doStatusPageRequest(r, "PUT", "/api/admin/pages/"+strconv.Itoa(int(page.ID)), map[string]interface{}{
			"Name": "Offen", "Slug": "offen2-319", "IsPublicIndexed": true, "IsPrivate": false,
		})
		requireStatusCode(t, w, http.StatusOK)
		var reloaded models.StatusPage
		db.First(&reloaded, page.ID)
		if !reloaded.IsPublicIndexed {
			t.Error("Nicht-private Seite muss gelistet bleiben können")
		}
	})
}

// Schlägt das explizite Setzen von is_public_indexed=false beim
// Anlegen einer privaten Seite fehl, antwortet Create mit 500.
func TestCreateStatusPageHandler_PrivateIndexUpdateErrorReturns500(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.Use(mockUserIDMiddleware)
	r.POST("/api/admin/pages", h.CreateStatusPageHandler)

	if err := db.Callback().Update().Before("gorm:update").Register("test:fail-update-319", func(tx *gorm.DB) {
		_ = tx.AddError(errors.New("update kaputt"))
	}); err != nil {
		t.Fatal(err)
	}

	w := doStatusPageRequest(r, "POST", "/api/admin/pages", map[string]interface{}{
		"Name": "Fehler", "Slug": "fehler-319", "IsPrivate": true,
	})
	requireStatusCode(t, w, http.StatusInternalServerError)
}

// Schlägt die Abfrage fehl (hier: Tabelle fehlt), antwortet die
// Liste mit 500 statt mit einer leeren oder halben Antwort.
func TestGetPublicPagesHandler_DBErrorReturns500(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/public/pages", h.GetPublicPagesHandler)

	if err := db.Migrator().DropTable(&models.StatusPage{}); err != nil {
		t.Fatalf("Tabelle nicht entfernbar: %v", err)
	}

	req, _ := http.NewRequest("GET", "/api/public/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	requireStatusCode(t, w, http.StatusInternalServerError)
}
