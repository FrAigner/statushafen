package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// serve führt einen vorbereiteten Request gegen den Router aus (für eigene Header).
func serve(r http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// DeleteIncidentHandler hatte nur den Normalfall getestet.
// Ergänzt: ungültige Parameter, Mandantentrennung, Tag-Zugriff, Aufräumen der
// Komponenten-Verknüpfungen, Audit-Eintrag und Datenbankfehler.
func TestDeleteIncidentHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	// Der Aufrufer wird per Header simuliert, wie es die AuthMiddleware mit dem JWT täte.
	r.Use(func(c *gin.Context) {
		if v := c.GetHeader("X-Test-User"); v != "" {
			id, _ := strconv.ParseUint(v, 10, 32)
			c.Set("userID", uint(id))
		}
		c.Next()
	})
	r.DELETE("/api/pages/:page_id/incidents/:id", h.DeleteIncidentHandler)

	page := models.StatusPage{Name: "Del Page", Slug: "del-page"}
	otherPage := models.StatusPage{Name: "Del Other", Slug: "del-other"}
	db.Create(&page)
	db.Create(&otherPage)
	comp := models.Component{StatusPageID: page.ID, Name: "Del Comp"}
	db.Create(&comp)
	deTag := models.Tag{Name: "DE", StatusPageID: &page.ID}
	usTag := models.Tag{Name: "US", StatusPageID: &page.ID}
	db.Create(&deTag)
	db.Create(&usTag)
	deUser := models.User{Email: "del-de@example.com"}
	db.Create(&deUser)
	db.Create(&models.UserTag{UserID: deUser.ID, TagID: deTag.ID})

	path := func(pageID uint, id string) string {
		return "/api/pages/" + strconv.Itoa(int(pageID)) + "/incidents/" + id
	}
	newIncident := func(title string) models.Incident {
		inc := models.Incident{StatusPageID: page.ID, Title: title, Status: "investigating"}
		db.Create(&inc)
		return inc
	}
	idOf := func(inc models.Incident) string { return strconv.Itoa(int(inc.ID)) }
	incidentCount := func(id uint) int64 {
		var n int64
		db.Model(&models.Incident{}).Where("id = ?", id).Count(&n)
		return n
	}
	auditCount := func() int64 {
		var n int64
		db.Model(&models.AuditLog{}).Where("action = ?", "incident.deleted").Count(&n)
		return n
	}
	failOn := func(t *testing.T, name, table string) {
		cb := func(tx *gorm.DB) {
			if tx.Statement.Table == table {
				_ = tx.AddError(errors.New("simulierter Fehler"))
			}
		}
		if err := db.Callback().Update().Before("gorm:update").Register(name+"_u", cb); err != nil {
			t.Fatalf("Callback nicht registriert: %v", err)
		}
		if err := db.Callback().Delete().Before("gorm:delete").Register(name+"_d", cb); err != nil {
			t.Fatalf("Callback nicht registriert: %v", err)
		}
		t.Cleanup(func() {
			_ = db.Callback().Update().Remove(name + "_u")
			_ = db.Callback().Delete().Remove(name + "_d")
		})
	}

	t.Run("ungültige Incident-ID gibt 400", func(t *testing.T) {
		w := doTemplateRequest(r, "DELETE", path(page.ID, "abc"), nil)
		requireStatusCode(t, w, http.StatusBadRequest)
		requireBodyContains(t, w, "Invalid incident ID")
	})

	t.Run("ungültige page_id gibt 400", func(t *testing.T) {
		w := doTemplateRequest(r, "DELETE", "/api/pages/xyz/incidents/1", nil)
		requireStatusCode(t, w, http.StatusBadRequest)
		requireBodyContains(t, w, "Invalid page_id")
	})

	t.Run("unbekannter Incident gibt 404", func(t *testing.T) {
		w := doTemplateRequest(r, "DELETE", path(page.ID, "99999"), nil)
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Incident einer fremden Seite gibt 404 und bleibt erhalten", func(t *testing.T) {
		inc := newIncident("Fremd")
		w := doTemplateRequest(r, "DELETE", path(otherPage.ID, idOf(inc)), nil)
		requireStatusCode(t, w, http.StatusNotFound)
		if incidentCount(inc.ID) != 1 {
			t.Errorf("Incident einer anderen Seite wurde gelöscht")
		}
	})

	t.Run("Tag ohne Überschneidung gibt 403 und löscht nichts", func(t *testing.T) {
		inc := newIncident("US-Incident")
		db.Create(&models.IncidentTag{IncidentID: inc.ID, TagID: usTag.ID})
		before := auditCount()
		req, _ := http.NewRequest("DELETE", path(page.ID, idOf(inc)), nil)
		req.Header.Set("X-Test-User", strconv.Itoa(int(deUser.ID)))
		w := serve(r, req)
		requireStatusCode(t, w, http.StatusForbidden)
		if incidentCount(inc.ID) != 1 {
			t.Errorf("Incident wurde trotz fehlendem Zugriff gelöscht")
		}
		if auditCount() != before {
			t.Errorf("Audit-Eintrag trotz 403")
		}
	})

	t.Run("Tag mit Überschneidung darf löschen", func(t *testing.T) {
		inc := newIncident("DE-Incident")
		db.Create(&models.IncidentTag{IncidentID: inc.ID, TagID: deTag.ID})
		req, _ := http.NewRequest("DELETE", path(page.ID, idOf(inc)), nil)
		req.Header.Set("X-Test-User", strconv.Itoa(int(deUser.ID)))
		w := serve(r, req)
		requireStatusCode(t, w, http.StatusOK)
		if incidentCount(inc.ID) != 0 {
			t.Errorf("Incident existiert noch")
		}
	})

	t.Run("Normalfall: entfernt Verknüpfungen, schreibt Audit-Eintrag", func(t *testing.T) {
		inc := newIncident("Weg")
		db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID})
		before := auditCount()
		w := doTemplateRequest(r, "DELETE", path(page.ID, idOf(inc)), nil)
		requireStatusCode(t, w, http.StatusOK)
		requireBodyContains(t, w, "Incident deleted successfully")
		if incidentCount(inc.ID) != 0 {
			t.Errorf("Incident existiert noch")
		}
		var links int64
		db.Model(&models.IncidentComponent{}).Where("incident_id = ?", inc.ID).Count(&links)
		if links != 0 {
			t.Errorf("Komponenten-Verknüpfungen = %d, erwartet 0", links)
		}
		if got := auditCount(); got != before+1 {
			t.Fatalf("Audit-Einträge = %d, erwartet %d", got, before+1)
		}
		var entry models.AuditLog
		if err := db.Where("action = ? AND target_id = ?", "incident.deleted", idOf(inc)).First(&entry).Error; err != nil {
			t.Fatalf("Audit-Eintrag fehlt: %v", err)
		}
		if entry.TargetType != "Incident" || entry.TargetName != "Weg" {
			t.Errorf("Unerwarteter Audit-Eintrag: %+v", entry)
		}
	})

	t.Run("Fehler beim Aufräumen der Verknüpfungen gibt 500, Incident bleibt", func(t *testing.T) {
		inc := newIncident("Link-Fehler")
		db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID})
		failOn(t, "test:fail_links", "incident_components")
		before := auditCount()
		w := doTemplateRequest(r, "DELETE", path(page.ID, idOf(inc)), nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
		requireBodyContains(t, w, "Failed to clean up component links")
		if incidentCount(inc.ID) != 1 {
			t.Errorf("Incident wurde trotz Fehler gelöscht")
		}
		if auditCount() != before {
			t.Errorf("Audit-Eintrag trotz 500")
		}
	})

	t.Run("Fehler beim Löschen des Incidents gibt 500", func(t *testing.T) {
		inc := newIncident("Delete-Fehler")
		failOn(t, "test:fail_incident", "incidents")
		before := auditCount()
		w := doTemplateRequest(r, "DELETE", path(page.ID, idOf(inc)), nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
		requireBodyContains(t, w, "Failed to delete incident")
		if incidentCount(inc.ID) != 1 {
			t.Errorf("Incident wurde trotz Fehler gelöscht")
		}
		if auditCount() != before {
			t.Errorf("Audit-Eintrag trotz 500")
		}
	})
}
