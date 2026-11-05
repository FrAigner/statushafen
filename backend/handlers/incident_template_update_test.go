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

// UpdateIncidentTemplateHandler war bisher ungetestet (0 %).
// Abgedeckt: Normalfall, Kind-Fallback, ungültige Parameter, fremde Seite
// (Mandantentrennung), ungültiger Body und Speicherfehler.
func TestUpdateIncidentTemplateHandler(t *testing.T) {
	db := testutils.SetupTestDB()
	h := handlers.NewHandler(db)
	r := testutils.SetupRouter()
	r.PUT("/api/pages/:page_id/incident-templates/:id", h.UpdateIncidentTemplateHandler)

	page := models.StatusPage{Name: "Update Page", Slug: "upd-page"}
	otherPage := models.StatusPage{Name: "Other Page", Slug: "upd-other"}
	db.Create(&page)
	db.Create(&otherPage)

	newTemplate := func() models.IncidentTemplate {
		tmpl := models.IncidentTemplate{
			StatusPageID: page.ID, Name: "Alt", Kind: "incident",
			Title: "Alter Titel", Message: "Alte Nachricht",
		}
		db.Create(&tmpl)
		return tmpl
	}
	path := func(pageID uint, id string) string {
		return "/api/pages/" + strconv.Itoa(int(pageID)) + "/incident-templates/" + id
	}
	validBody := map[string]interface{}{
		"Name": "Neu", "Kind": "maintenance", "Title": "Neuer Titel",
		"Message": "Neue Nachricht", "DefaultStatus": "scheduled", "ComponentIDs": "1,2",
	}

	t.Run("aktualisiert alle Felder", func(t *testing.T) {
		tmpl := newTemplate()
		w := doTemplateRequest(r, "PUT", path(page.ID, strconv.Itoa(int(tmpl.ID))), validBody)
		requireStatusCode(t, w, http.StatusOK)

		var got models.IncidentTemplate
		if err := db.First(&got, tmpl.ID).Error; err != nil {
			t.Fatalf("Vorlage nicht gefunden: %v", err)
		}
		if got.Name != "Neu" || got.Kind != "maintenance" || got.Title != "Neuer Titel" ||
			got.Message != "Neue Nachricht" || got.DefaultStatus != "scheduled" || got.ComponentIDs != "1,2" {
			t.Errorf("Felder nicht übernommen: %+v", got)
		}
		var resp models.IncidentTemplate
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.ID != tmpl.ID || resp.Name != "Neu" {
			t.Errorf("Antwort unerwartet: %+v", resp)
		}
	})

	t.Run("unbekanntes Kind fällt auf incident zurück", func(t *testing.T) {
		tmpl := newTemplate()
		body := map[string]interface{}{"Name": "N", "Kind": "bogus", "Title": "T", "Message": "M"}
		w := doTemplateRequest(r, "PUT", path(page.ID, strconv.Itoa(int(tmpl.ID))), body)
		requireStatusCode(t, w, http.StatusOK)
		var got models.IncidentTemplate
		db.First(&got, tmpl.ID)
		if got.Kind != "incident" {
			t.Errorf("Kind = %q, erwartet incident", got.Kind)
		}
	})

	t.Run("ungültige Vorlagen-ID gibt 400", func(t *testing.T) {
		w := doTemplateRequest(r, "PUT", path(page.ID, "abc"), validBody)
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("ungültige page_id gibt 400", func(t *testing.T) {
		tmpl := newTemplate()
		req := "/api/pages/xyz/incident-templates/" + strconv.Itoa(int(tmpl.ID))
		w := doTemplateRequest(r, "PUT", req, validBody)
		requireStatusCode(t, w, http.StatusBadRequest)
	})

	t.Run("unbekannte Vorlage gibt 404", func(t *testing.T) {
		w := doTemplateRequest(r, "PUT", path(page.ID, "99999"), validBody)
		requireStatusCode(t, w, http.StatusNotFound)
	})

	t.Run("Vorlage einer anderen Seite gibt 404 und bleibt unverändert", func(t *testing.T) {
		tmpl := newTemplate()
		w := doTemplateRequest(r, "PUT", path(otherPage.ID, strconv.Itoa(int(tmpl.ID))), validBody)
		requireStatusCode(t, w, http.StatusNotFound)
		var got models.IncidentTemplate
		db.First(&got, tmpl.ID)
		if got.Name != "Alt" {
			t.Errorf("Fremde Seite hat die Vorlage geändert: %+v", got)
		}
	})

	t.Run("Pflichtfelder fehlen gibt 400", func(t *testing.T) {
		tmpl := newTemplate()
		w := doTemplateRequest(r, "PUT", path(page.ID, strconv.Itoa(int(tmpl.ID))), map[string]interface{}{"Name": "N"})
		requireStatusCode(t, w, http.StatusBadRequest)
		var got models.IncidentTemplate
		db.First(&got, tmpl.ID)
		if got.Name != "Alt" {
			t.Errorf("Vorlage trotz ungültigem Body geändert: %+v", got)
		}
	})

	t.Run("Speicherfehler gibt 500", func(t *testing.T) {
		tmpl := newTemplate()
		const cb = "test:fail_template_update"
		if err := db.Callback().Update().Before("gorm:update").Register(cb, func(tx *gorm.DB) {
			_ = tx.AddError(errors.New("simulierter Fehler"))
		}); err != nil {
			t.Fatalf("Callback nicht registriert: %v", err)
		}
		defer func() { _ = db.Callback().Update().Remove(cb) }()

		w := doTemplateRequest(r, "PUT", path(page.ID, strconv.Itoa(int(tmpl.ID))), validBody)
		requireStatusCode(t, w, http.StatusInternalServerError)
	})
}
