package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Gruppen lassen sich per API anlegen (is_group), haben keinen
// eigenen Status und lassen sich nur löschen, wenn sie leer sind.
func TestComponentGroups_CreateAndDeleteGuard(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.Use(func(c *gin.Context) {
		if v := c.GetHeader("X-Test-User"); v != "" {
			id, _ := strconv.ParseUint(v, 10, 32)
			c.Set("userID", uint(id))
		}
		c.Next()
	})
	r.GET("/api/pages/:page_id/components", h.GetComponentsHandler)
	r.POST("/api/pages/:page_id/components", h.CreateComponentHandler)
	r.DELETE("/api/pages/:page_id/components/:id", h.DeleteComponentHandler)

	page := models.StatusPage{Name: "Gruppen", Slug: "grp-579"}
	other := models.StatusPage{Name: "Fremd", Slug: "grp-579-other"}
	db.Create(&page)
	db.Create(&other)
	deTag := models.Tag{Name: "DE", StatusPageID: &page.ID}
	usTag := models.Tag{Name: "US", StatusPageID: &page.ID}
	db.Create(&deTag)
	db.Create(&usTag)
	deUser := models.User{Email: "grp-de@example.com"}
	db.Create(&deUser)
	db.Create(&models.UserTag{UserID: deUser.ID, TagID: deTag.ID})

	base := func(p uint) string { return "/api/pages/" + strconv.Itoa(int(p)) + "/components" }
	idStr := func(id uint) string { return "/" + strconv.Itoa(int(id)) }
	asDE := func(method, p string, payload interface{}) *httptest.ResponseRecorder {
		var body io.Reader
		if payload != nil {
			b, _ := json.Marshal(payload)
			body = bytes.NewBuffer(b)
		}
		req, _ := http.NewRequest(method, p, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-User", strconv.Itoa(int(deUser.ID)))
		return serve(r, req)
	}
	create := func(payload map[string]interface{}) (models.Component, *httptest.ResponseRecorder) {
		w := doTemplateRequest(r, "POST", base(page.ID), payload)
		var c models.Component
		_ = json.Unmarshal(w.Body.Bytes(), &c)
		return c, w
	}
	audits := func(action string) int64 {
		var n int64
		db.Model(&models.AuditLog{}).Where("action = ?", action).Count(&n)
		return n
	}

	t.Run("Gruppe anlegen: Status group, is_group, Audit, kein Anfangsstatus", func(t *testing.T) {
		before := audits("component.created")
		c, w := create(map[string]interface{}{"name": "Kern", "is_group": true, "tag_ids": []uint{deTag.ID}})
		requireStatusCode(t, w, http.StatusCreated)
		if c.Status != "group" || !c.IsGroup || len(c.Tags) != 1 {
			t.Errorf("Gruppe falsch angelegt: %+v", c)
		}
		var stored models.Component
		db.First(&stored, c.ID)
		if stored.Status != "group" || stored.StatusPageID != page.ID {
			t.Errorf("gespeichert: %+v", stored)
		}
		if audits("component.created") != before+1 {
			t.Error("Audit-Eintrag component.created fehlt")
		}
	})

	t.Run("Gruppe mit eigenem Status gibt 400", func(t *testing.T) {
		_, w := create(map[string]interface{}{"name": "X", "is_group": true, "status": "major_outage"})
		requireStatusCode(t, w, http.StatusBadRequest)
		requireBodyContains(t, w, "no status of its own")
	})

	t.Run("Gruppe verschachtelt unter Gruppe, GET kennzeichnet IsGroup", func(t *testing.T) {
		parent, _ := create(map[string]interface{}{"name": "Eltern", "is_group": true})
		child, w := create(map[string]interface{}{"name": "Kind-Gruppe", "is_group": true, "parent_id": parent.ID})
		requireStatusCode(t, w, http.StatusCreated)
		if child.ParentID == nil || *child.ParentID != parent.ID {
			t.Errorf("parent_id fehlt: %+v", child)
		}
		g := doTemplateRequest(r, "GET", base(page.ID), nil)
		var tree []models.Component
		_ = json.Unmarshal(g.Body.Bytes(), &tree)
		found := false
		for _, n := range tree {
			if n.ID == parent.ID {
				found = n.IsGroup && len(n.Children) == 1 && n.Children[0].IsGroup
			}
		}
		if !found {
			t.Errorf("Baum falsch: %s", g.Body.String())
		}
	})

	t.Run("Gruppe unter Eltern einer fremden Seite gibt 400", func(t *testing.T) {
		foreign := models.Component{StatusPageID: other.ID, Name: "Fremd", Status: "group"}
		db.Create(&foreign)
		_, w := create(map[string]interface{}{"name": "X", "is_group": true, "parent_id": foreign.ID})
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Gruppe mit fremdem Tag gibt 403 und legt nichts an", func(t *testing.T) {
		w := asDE("POST", base(page.ID), map[string]interface{}{"name": "US-Gruppe", "is_group": true, "tag_ids": []uint{usTag.ID}})
		requireStatusCode(t, w, http.StatusForbidden)
		var n int64
		db.Model(&models.Component{}).Where("name = ?", "US-Gruppe").Count(&n)
		if n != 0 {
			t.Error("Gruppe trotz 403 angelegt")
		}
	})

	t.Run("Komponente ohne is_group bleibt unverändert, ungültiger Status gibt 400", func(t *testing.T) {
		c, w := create(map[string]interface{}{"name": "Normal"})
		requireStatusCode(t, w, http.StatusCreated)
		if c.Status != "operational" || c.IsGroup {
			t.Errorf("Komponente: %+v", c)
		}
		c, w = create(map[string]interface{}{"name": "Normal3", "status": "degraded_performance"})
		requireStatusCode(t, w, http.StatusCreated)
		if c.Status != "degraded_performance" {
			t.Errorf("Status: %+v", c)
		}
		_, w = create(map[string]interface{}{"name": "Normal2", "status": "bogus"})
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Leere Gruppe löschen: 200 und Audit", func(t *testing.T) {
		g, _ := create(map[string]interface{}{"name": "Leer", "is_group": true})
		before := audits("component.deleted")
		w := doTemplateRequest(r, "DELETE", base(page.ID)+idStr(g.ID), nil)
		requireStatusCode(t, w, http.StatusOK)
		var n int64
		db.Model(&models.Component{}).Where("id = ?", g.ID).Count(&n)
		if n != 0 || audits("component.deleted") != before+1 {
			t.Errorf("Gruppe nicht gelöscht (%d) oder Audit fehlt", n)
		}
	})

	t.Run("Gruppe mit Kindern löschen gibt 409 mit code und ändert nichts", func(t *testing.T) {
		g, _ := create(map[string]interface{}{"name": "Voll", "is_group": true})
		kid, _ := create(map[string]interface{}{"name": "Mitglied", "parent_id": g.ID})
		before := audits("component.deleted")
		w := doTemplateRequest(r, "DELETE", base(page.ID)+idStr(g.ID), nil)
		requireStatusCode(t, w, http.StatusConflict)
		requireBodyContains(t, w, `"code":"group_not_empty"`)
		var n int64
		db.Model(&models.Component{}).Where("id IN ?", []uint{g.ID, kid.ID}).Count(&n)
		if n != 2 || audits("component.deleted") != before {
			t.Errorf("Gruppe/Kind verändert (%d) oder Audit geschrieben", n)
		}
		// Nach dem Entfernen des letzten Kindes ist die Gruppe löschbar.
		requireStatusCode(t, doTemplateRequest(r, "DELETE", base(page.ID)+idStr(kid.ID), nil), http.StatusOK)
		requireStatusCode(t, doTemplateRequest(r, "DELETE", base(page.ID)+idStr(g.ID), nil), http.StatusOK)
	})

	t.Run("Gruppe einer fremden Seite löschen gibt 404", func(t *testing.T) {
		g, _ := create(map[string]interface{}{"name": "Meine", "is_group": true})
		w := doTemplateRequest(r, "DELETE", base(other.ID)+idStr(g.ID), nil)
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Gruppe mit fremdem Tag löschen gibt 403", func(t *testing.T) {
		g := models.Component{StatusPageID: page.ID, Name: "US-Gruppe2", Status: "group"}
		db.Create(&g)
		db.Create(&models.ComponentTag{ComponentID: g.ID, TagID: usTag.ID})
		w := asDE("DELETE", base(page.ID)+idStr(g.ID), nil)
		requireStatusCode(t, w, http.StatusForbidden)
	})

	t.Run("Löschen: Datenbankfehler beim Zählen der Kinder gibt 500", func(t *testing.T) {
		g, _ := create(map[string]interface{}{"name": "Fehler", "is_group": true})
		db.Exec("ALTER TABLE components RENAME COLUMN parent_id TO parent_id_x")
		defer db.Exec("ALTER TABLE components RENAME COLUMN parent_id_x TO parent_id")
		w := doTemplateRequest(r, "DELETE", base(page.ID)+idStr(g.ID), nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
	})
}
