package handlers_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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

// Update-/DeleteComponentHandler hatten nur den Normalfall getestet.
// Ergänzt: ungültige Parameter, Mandantentrennung, Tag-Zugriff, Validierung,
// Aufräumen der Incident-Verknüpfungen, Audit-Einträge und Datenbankfehler.
func TestUpdateAndDeleteComponentHandler(t *testing.T) {
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
	r.PUT("/api/pages/:page_id/components/:id", h.UpdateComponentHandler)
	r.DELETE("/api/pages/:page_id/components/:id", h.DeleteComponentHandler)

	page := models.StatusPage{Name: "Comp Page", Slug: "comp-ud"}
	otherPage := models.StatusPage{Name: "Comp Other", Slug: "comp-ud-other"}
	db.Create(&page)
	db.Create(&otherPage)
	deTag := models.Tag{Name: "DE", StatusPageID: &page.ID}
	usTag := models.Tag{Name: "US", StatusPageID: &page.ID}
	db.Create(&deTag)
	db.Create(&usTag)
	deUser := models.User{Email: "comp-de@example.com"}
	db.Create(&deUser)
	db.Create(&models.UserTag{UserID: deUser.ID, TagID: deTag.ID})

	path := func(pageID uint, id string) string {
		return "/api/pages/" + strconv.Itoa(int(pageID)) + "/components/" + id
	}
	newComponent := func(name string, tag *models.Tag) models.Component {
		comp := models.Component{StatusPageID: page.ID, Name: name, Status: "operational"}
		db.Create(&comp)
		if tag != nil {
			db.Create(&models.ComponentTag{ComponentID: comp.ID, TagID: tag.ID})
		}
		return comp
	}
	idOf := func(c models.Component) string { return strconv.Itoa(int(c.ID)) }
	reload := func(id uint) models.Component {
		var c models.Component
		db.First(&c, id)
		return c
	}
	componentCount := func(id uint) int64 {
		var n int64
		db.Model(&models.Component{}).Where("id = ?", id).Count(&n)
		return n
	}
	auditCount := func(action string) int64 {
		var n int64
		db.Model(&models.AuditLog{}).Where("action = ?", action).Count(&n)
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
		if err := db.Callback().Create().Before("gorm:create").Register(name+"_c", cb); err != nil {
			t.Fatalf("Callback nicht registriert: %v", err)
		}
		t.Cleanup(func() {
			_ = db.Callback().Update().Remove(name + "_u")
			_ = db.Callback().Delete().Remove(name + "_d")
			_ = db.Callback().Create().Remove(name + "_c")
		})
	}
	asDE := func(method, p string, payload interface{}) *httptest.ResponseRecorder {
		var body io.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			body = bytes.NewBuffer(b)
		}
		req, _ := http.NewRequest(method, p, body)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("X-Test-User", strconv.Itoa(int(deUser.ID)))
		return serve(r, req)
	}

	t.Run("Update: ungültige Komponenten-ID gibt 400", func(t *testing.T) {
		w := doTemplateRequest(r, "PUT", path(page.ID, "abc"), map[string]string{"status": "operational"})
		requireStatusCode(t, w, http.StatusBadRequest)
		requireBodyContains(t, w, "Invalid component ID")
	})

	t.Run("Update: ungültige page_id gibt 400", func(t *testing.T) {
		w := doTemplateRequest(r, "PUT", "/api/pages/xyz/components/1", map[string]string{"status": "operational"})
		requireStatusCode(t, w, http.StatusBadRequest)
		requireBodyContains(t, w, "Invalid page_id")
	})

	t.Run("Update: unbekannte Komponente gibt 404", func(t *testing.T) {
		w := doTemplateRequest(r, "PUT", path(page.ID, "99999"), map[string]string{"status": "operational"})
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Update: Komponente einer fremden Seite gibt 404 und bleibt unverändert", func(t *testing.T) {
		comp := newComponent("Fremd-Update", nil)
		w := doTemplateRequest(r, "PUT", path(otherPage.ID, idOf(comp)), map[string]string{"name": "Gekapert", "status": "major_outage"})
		requireStatusCode(t, w, http.StatusNotFound)
		if got := reload(comp.ID); got.Name != "Fremd-Update" || got.Status != "operational" {
			t.Errorf("Komponente einer anderen Seite wurde geändert: %+v", got)
		}
	})

	t.Run("Update: Tag ohne Überschneidung gibt 403 und ändert nichts", func(t *testing.T) {
		comp := newComponent("US-Komponente", &usTag)
		before := auditCount("component.updated")
		w := asDE("PUT", path(page.ID, idOf(comp)), map[string]string{"name": "Neu", "status": "major_outage"})
		requireStatusCode(t, w, http.StatusForbidden)
		if got := reload(comp.ID); got.Name != "US-Komponente" || got.Status != "operational" {
			t.Errorf("Komponente wurde trotz fehlendem Zugriff geändert: %+v", got)
		}
		if auditCount("component.updated") != before {
			t.Errorf("Audit-Eintrag trotz 403")
		}
	})

	t.Run("Update: Tag mit Überschneidung darf ändern", func(t *testing.T) {
		comp := newComponent("DE-Komponente", &deTag)
		w := asDE("PUT", path(page.ID, idOf(comp)), map[string]string{"status": "degraded_performance"})
		requireStatusCode(t, w, http.StatusOK)
		if got := reload(comp.ID); got.Status != "degraded_performance" {
			t.Errorf("Status = %q, erwartet degraded_performance", got.Status)
		}
	})

	t.Run("Update: ohne Status gibt 400 und ändert nichts", func(t *testing.T) {
		comp := newComponent("Ohne-Status", nil)
		w := doTemplateRequest(r, "PUT", path(page.ID, idOf(comp)), map[string]string{"name": "Neu"})
		requireStatusCode(t, w, http.StatusBadRequest)
		requireBodyContains(t, w, "Invalid request fields")
		if got := reload(comp.ID); got.Name != "Ohne-Status" {
			t.Errorf("Name = %q, erwartet unverändert", got.Name)
		}
	})

	t.Run("Update: ungültiger Status gibt 400", func(t *testing.T) {
		comp := newComponent("Falscher-Status", nil)
		w := doTemplateRequest(r, "PUT", path(page.ID, idOf(comp)), map[string]string{"status": "kaputt"})
		requireStatusCode(t, w, http.StatusBadRequest)
		if got := reload(comp.ID); got.Status != "operational" {
			t.Errorf("Status = %q, erwartet operational", got.Status)
		}
	})

	t.Run("Update: setzt Name, Beschreibung und Status, schreibt Audit-Eintrag", func(t *testing.T) {
		comp := newComponent("Alt", nil)
		before := auditCount("component.updated")
		w := doTemplateRequest(r, "PUT", path(page.ID, idOf(comp)),
			map[string]string{"name": "Neu", "description": "Beschreibung", "status": "partial_outage"})
		requireStatusCode(t, w, http.StatusOK)
		got := reload(comp.ID)
		if got.Name != "Neu" || got.Description != "Beschreibung" || got.Status != "partial_outage" {
			t.Errorf("Unerwarteter Stand: %+v", got)
		}
		if n := auditCount("component.updated"); n != before+1 {
			t.Errorf("Audit-Einträge = %d, erwartet %d", n, before+1)
		}
	})

	t.Run("Update: leere Tag-Liste entfernt die Tags", func(t *testing.T) {
		comp := newComponent("Mit-Tag", &deTag)
		w := doTemplateRequest(r, "PUT", path(page.ID, idOf(comp)),
			map[string]interface{}{"status": "operational", "tag_ids": []uint{}})
		requireStatusCode(t, w, http.StatusOK)
		var n int64
		db.Model(&models.ComponentTag{}).Where("component_id = ?", comp.ID).Count(&n)
		if n != 0 {
			t.Errorf("Tag-Zuordnungen = %d, erwartet 0", n)
		}
	})

	t.Run("Update: Tag-Wechsel auf fremden Tag gibt 403", func(t *testing.T) {
		comp := newComponent("Wechsel", &deTag)
		w := asDE("PUT", path(page.ID, idOf(comp)),
			map[string]interface{}{"status": "operational", "tag_ids": []uint{usTag.ID}})
		requireStatusCode(t, w, http.StatusForbidden)
		requireBodyContains(t, w, "move components")
	})

	t.Run("Update: Fehler beim Speichern der Tags gibt 500", func(t *testing.T) {
		comp := newComponent("Tag-Fehler", &deTag)
		failOn(t, "test:fail_comp_tags", "component_tags")
		w := doTemplateRequest(r, "PUT", path(page.ID, idOf(comp)),
			map[string]interface{}{"status": "operational", "tag_ids": []uint{deTag.ID}})
		requireStatusCode(t, w, http.StatusInternalServerError)
		requireBodyContains(t, w, "Failed to update tags")
	})

	t.Run("Delete: ungültige Komponenten-ID gibt 400", func(t *testing.T) {
		w := doTemplateRequest(r, "DELETE", path(page.ID, "abc"), nil)
		requireStatusCode(t, w, http.StatusBadRequest)
		requireBodyContains(t, w, "Invalid component ID")
	})

	t.Run("Delete: ungültige page_id gibt 400", func(t *testing.T) {
		w := doTemplateRequest(r, "DELETE", "/api/pages/xyz/components/1", nil)
		requireStatusCode(t, w, http.StatusBadRequest)
		requireBodyContains(t, w, "Invalid page_id")
	})

	t.Run("Delete: unbekannte Komponente gibt 404", func(t *testing.T) {
		w := doTemplateRequest(r, "DELETE", path(page.ID, "99999"), nil)
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Delete: Komponente einer fremden Seite gibt 404 und bleibt erhalten", func(t *testing.T) {
		comp := newComponent("Fremd-Delete", nil)
		w := doTemplateRequest(r, "DELETE", path(otherPage.ID, idOf(comp)), nil)
		requireStatusCode(t, w, http.StatusNotFound)
		if componentCount(comp.ID) != 1 {
			t.Errorf("Komponente einer anderen Seite wurde gelöscht")
		}
	})

	t.Run("Delete: Tag ohne Überschneidung gibt 403 und löscht nichts", func(t *testing.T) {
		comp := newComponent("US-Delete", &usTag)
		before := auditCount("component.deleted")
		w := asDE("DELETE", path(page.ID, idOf(comp)), nil)
		requireStatusCode(t, w, http.StatusForbidden)
		if componentCount(comp.ID) != 1 {
			t.Errorf("Komponente wurde trotz fehlendem Zugriff gelöscht")
		}
		if auditCount("component.deleted") != before {
			t.Errorf("Audit-Eintrag trotz 403")
		}
	})

	t.Run("Delete: Tag mit Überschneidung darf löschen", func(t *testing.T) {
		comp := newComponent("DE-Delete", &deTag)
		w := asDE("DELETE", path(page.ID, idOf(comp)), nil)
		requireStatusCode(t, w, http.StatusOK)
		if componentCount(comp.ID) != 0 {
			t.Errorf("Komponente existiert noch")
		}
	})

	t.Run("Delete: entfernt nur die Incident-Zuordnung, der Incident bleibt", func(t *testing.T) {
		comp := newComponent("Mit-Incident", nil)
		inc := models.Incident{StatusPageID: page.ID, Title: "Bleibt", Status: "investigating"}
		db.Create(&inc)
		db.Create(&models.IncidentComponent{IncidentID: inc.ID, ComponentID: comp.ID})
		before := auditCount("component.deleted")
		w := doTemplateRequest(r, "DELETE", path(page.ID, idOf(comp)), nil)
		requireStatusCode(t, w, http.StatusOK)
		requireBodyContains(t, w, "Component deleted successfully")
		var links, incidents int64
		db.Model(&models.IncidentComponent{}).Where("component_id = ?", comp.ID).Count(&links)
		db.Model(&models.Incident{}).Where("id = ?", inc.ID).Count(&incidents)
		if links != 0 {
			t.Errorf("Zuordnungen = %d, erwartet 0", links)
		}
		if incidents != 1 {
			t.Errorf("Incident wurde mitgelöscht")
		}
		if n := auditCount("component.deleted"); n != before+1 {
			t.Errorf("Audit-Einträge = %d, erwartet %d", n, before+1)
		}
	})

	t.Run("Delete: Fehler beim Löschen gibt 500, ohne Audit-Eintrag", func(t *testing.T) {
		comp := newComponent("Delete-Fehler", nil)
		failOn(t, "test:fail_components", "components")
		before := auditCount("component.deleted")
		w := doTemplateRequest(r, "DELETE", path(page.ID, idOf(comp)), nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
		requireBodyContains(t, w, "Failed to delete component")
		if componentCount(comp.ID) != 1 {
			t.Errorf("Komponente wurde trotz Fehler gelöscht")
		}
		if auditCount("component.deleted") != before {
			t.Errorf("Audit-Eintrag trotz 500")
		}
	})
}
