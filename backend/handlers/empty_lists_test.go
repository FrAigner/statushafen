package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Listen-Endpunkte müssen bei leerem Ergebnis "[]" statt "null"
// liefern, sonst stürzen die Admin-Views (.length/.map auf null) ab.
func TestListEndpoints_EmptyResultIsEmptyArray(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()

	page := models.StatusPage{Name: "Leer", Slug: "leer"}
	db.Create(&page)
	pid := strconv.Itoa(int(page.ID))
	// Alle Sprachen entfernen, damit auch der Sprachkatalog leer ist.
	db.Unscoped().Where("1 = 1").Delete(&models.Language{})

	r.GET("/api/pages/:page_id/components", h.GetComponentsHandler)
	r.GET("/api/pages/:page_id/incidents", h.GetIncidentsHandler)
	r.GET("/api/pages/:page_id/notices", h.GetNoticesHandler)
	r.GET("/api/pages/:page_id/subscribers", h.GetSubscribersHandler)
	r.GET("/api/pages/:page_id/notification-templates", h.GetNotificationTemplatesHandler)
	r.GET("/api/pages/:page_id/languages", h.GetPageLanguagesHandler)
	r.GET("/api/pages/:page_id/users", h.GetStatusPageUsersHandler)
	r.GET("/api/languages", h.GetLanguagesHandler)
	r.GET("/api/tags", h.GetTagsHandler)

	paths := map[string]string{
		"Komponenten":            "/api/pages/" + pid + "/components",
		"Vorfälle":               "/api/pages/" + pid + "/incidents",
		"Wartungen":              "/api/pages/" + pid + "/incidents?is_maintenance=true",
		"Hinweise":               "/api/pages/" + pid + "/notices",
		"Abonnenten":             "/api/pages/" + pid + "/subscribers",
		"Benachrichtigungsvorl.": "/api/pages/" + pid + "/notification-templates",
		"Seitensprachen":         "/api/pages/" + pid + "/languages",
		"Seitenbenutzer":         "/api/pages/" + pid + "/users",
		"Sprachkatalog":          "/api/languages",
		"Tags":                   "/api/tags",
	}
	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", path, nil)
			r.ServeHTTP(w, req)
			requireStatusCode(t, w, http.StatusOK)
			if got := strings.TrimSpace(w.Body.String()); got != "[]" {
				t.Errorf("erwartet [], erhalten %s", got)
			}
		})
	}

	t.Run("Komponenten: nur gelöschte", func(t *testing.T) {
		c := models.Component{StatusPageID: page.ID, Name: "weg", Status: "operational"}
		db.Create(&c)
		db.Delete(&c)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", paths["Komponenten"], nil)
		r.ServeHTTP(w, req)
		if got := strings.TrimSpace(w.Body.String()); got != "[]" {
			t.Errorf("erwartet [], erhalten %s", got)
		}
	})
}

func TestAuditLogs_EmptyItemsIsEmptyArray(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/admin/audit", h.GetAuditLogsHandler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/admin/audit", nil)
	r.ServeHTTP(w, req)
	requireStatusCode(t, w, http.StatusOK)
	var body map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if string(body["items"]) != "[]" {
		t.Errorf("items: erwartet [], erhalten %s", body["items"])
	}
}

func TestGetComponents_NoEmptyNestedStatusPage(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/:page_id/components", h.GetComponentsHandler)

	page := models.StatusPage{Name: "P", Slug: "p"}
	db.Create(&page)
	db.Create(&models.Component{StatusPageID: page.ID, Name: "A", Status: "operational"})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/pages/"+strconv.Itoa(int(page.ID))+"/components", nil)
	r.ServeHTTP(w, req)
	requireStatusCode(t, w, http.StatusOK)
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("unerwartete Antwort: %s", w.Body.String())
	}
	if _, ok := list[0]["StatusPage"]; ok {
		t.Errorf("leeres StatusPage-Objekt sollte nicht ausgeliefert werden: %s", list[0]["StatusPage"])
	}
}
