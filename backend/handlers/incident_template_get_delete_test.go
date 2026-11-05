package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"

	"gorm.io/gorm"

	"github.com/fraigner/statushafen/handlers"
	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// Get-/DeleteIncidentTemplateHandler hatten nur Normalfall-Tests.
// Ergänzt: ungültige Parameter, Mandantentrennung, Sortierung, leere Liste
// und Datenbankfehler.
func TestGetDeleteIncidentTemplateHandlers(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.GET("/api/pages/:page_id/incident-templates", h.GetIncidentTemplatesHandler)
	r.DELETE("/api/pages/:page_id/incident-templates/:id", h.DeleteIncidentTemplateHandler)

	page := models.StatusPage{Name: "GD Page", Slug: "gd-page"}
	otherPage := models.StatusPage{Name: "GD Other", Slug: "gd-other"}
	db.Create(&page)
	db.Create(&otherPage)

	listPath := func(pageID string) string { return "/api/pages/" + pageID + "/incident-templates" }
	itemPath := func(pageID uint, id string) string {
		return listPath(strconv.Itoa(int(pageID))) + "/" + id
	}
	newTemplate := func(pageID uint, name string) models.IncidentTemplate {
		tmpl := models.IncidentTemplate{StatusPageID: pageID, Name: name, Kind: "incident", Title: "T", Message: "M"}
		db.Create(&tmpl)
		return tmpl
	}
	failDelete := func(t *testing.T) {
		const cb = "test:fail_template_delete"
		if err := db.Callback().Delete().Before("gorm:delete").Register(cb, func(tx *gorm.DB) {
			_ = tx.AddError(errors.New("simulierter Fehler"))
		}); err != nil {
			t.Fatalf("Callback nicht registriert: %v", err)
		}
		t.Cleanup(func() { _ = db.Callback().Delete().Remove(cb) })
	}

	t.Run("Liste: leere Seite liefert leeres Array", func(t *testing.T) {
		w := doTemplateRequest(r, "GET", listPath(strconv.Itoa(int(otherPage.ID))), nil)
		requireStatusCode(t, w, http.StatusOK)
		if body := w.Body.String(); body != "[]" {
			t.Errorf("Antwort = %s, erwartet []", body)
		}
	})

	t.Run("Liste: nach Name sortiert und nur die eigene Seite", func(t *testing.T) {
		newTemplate(page.ID, "Zeta")
		newTemplate(page.ID, "Alpha")
		newTemplate(otherPage.ID, "Fremd")
		w := doTemplateRequest(r, "GET", listPath(strconv.Itoa(int(page.ID))), nil)
		requireStatusCode(t, w, http.StatusOK)
		var got []models.IncidentTemplate
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("Antwort nicht lesbar: %v", err)
		}
		if len(got) != 2 || got[0].Name != "Alpha" || got[1].Name != "Zeta" {
			t.Errorf("Unerwartete Liste: %+v", got)
		}
	})

	t.Run("Liste: ungültige page_id gibt 400", func(t *testing.T) {
		w := doTemplateRequest(r, "GET", listPath("xyz"), nil)
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Liste: Datenbankfehler gibt 500", func(t *testing.T) {
		const cb = "test:fail_template_list"
		if err := db.Callback().Query().Before("gorm:query").Register(cb, func(tx *gorm.DB) {
			_ = tx.AddError(errors.New("simulierter Fehler"))
		}); err != nil {
			t.Fatalf("Callback nicht registriert: %v", err)
		}
		defer func() { _ = db.Callback().Query().Remove(cb) }()
		w := doTemplateRequest(r, "GET", listPath(strconv.Itoa(int(page.ID))), nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
	})

	countDeleteAudits := func() int64 {
		var n int64
		db.Model(&models.AuditLog{}).Where("action = ?", "incident_template.deleted").Count(&n)
		return n
	}

	t.Run("Löschen: entfernt die Vorlage und schreibt den Audit-Eintrag", func(t *testing.T) {
		tmpl := newTemplate(page.ID, "Weg")
		before := countDeleteAudits()
		w := doTemplateRequest(r, "DELETE", itemPath(page.ID, strconv.Itoa(int(tmpl.ID))), nil)
		requireStatusCode(t, w, http.StatusOK)
		var count int64
		db.Model(&models.IncidentTemplate{}).Where("id = ?", tmpl.ID).Count(&count)
		if count != 0 {
			t.Errorf("Vorlage existiert noch")
		}
		if got := countDeleteAudits(); got != before+1 {
			t.Fatalf("Audit-Einträge = %d, erwartet %d", got, before+1)
		}
		var entry models.AuditLog
		if err := db.Where("action = ? AND target_id = ?", "incident_template.deleted", strconv.Itoa(int(tmpl.ID))).First(&entry).Error; err != nil {
			t.Fatalf("Audit-Eintrag fehlt: %v", err)
		}
		if entry.Category != "config" || entry.TargetType != "IncidentTemplate" || entry.TargetName != "Weg" {
			t.Errorf("Unerwarteter Audit-Eintrag: %+v", entry)
		}
	})

	t.Run("Löschen: ungültige Vorlagen-ID gibt 400", func(t *testing.T) {
		w := doTemplateRequest(r, "DELETE", itemPath(page.ID, "abc"), nil)
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Löschen: ungültige page_id gibt 400", func(t *testing.T) {
		w := doTemplateRequest(r, "DELETE", listPath("xyz")+"/1", nil)
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("Löschen: unbekannte Vorlage gibt 404", func(t *testing.T) {
		w := doTemplateRequest(r, "DELETE", itemPath(page.ID, "99999"), nil)
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Löschen: Vorlage einer anderen Seite gibt 404 und bleibt erhalten", func(t *testing.T) {
		tmpl := newTemplate(page.ID, "Bleibt")
		before := countDeleteAudits()
		w := doTemplateRequest(r, "DELETE", itemPath(otherPage.ID, strconv.Itoa(int(tmpl.ID))), nil)
		requireStatusCode(t, w, http.StatusNotFound)
		if got := countDeleteAudits(); got != before {
			t.Errorf("Audit-Eintrag trotz 404 geschrieben")
		}
		var count int64
		db.Model(&models.IncidentTemplate{}).Where("id = ?", tmpl.ID).Count(&count)
		if count != 1 {
			t.Errorf("Fremde Seite hat die Vorlage gelöscht")
		}
	})

	t.Run("Löschen: Speicherfehler gibt 500", func(t *testing.T) {
		tmpl := newTemplate(page.ID, "Fehler")
		failDelete(t)
		before := countDeleteAudits()
		w := doTemplateRequest(r, "DELETE", itemPath(page.ID, strconv.Itoa(int(tmpl.ID))), nil)
		requireStatusCode(t, w, http.StatusInternalServerError)
		if got := countDeleteAudits(); got != before {
			t.Errorf("Audit-Eintrag trotz 500 geschrieben")
		}
		var count int64
		db.Model(&models.IncidentTemplate{}).Where("id = ?", tmpl.ID).Count(&count)
		if count != 1 {
			t.Errorf("Vorlage wurde trotz Fehler entfernt")
		}
	})
}
