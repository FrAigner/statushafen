package handlers

import (
	"testing"

	"github.com/fraigner/statushafen/models"
	"github.com/fraigner/statushafen/testutils"
)

// soft-gelöschte Join-Zeilen dürfen beim Lesen der Tags nicht
// mehr auftauchen (sonst wirken Replace/Entfernen wie Duplikate bzw. wie ein
// No-Op).
func TestTagsFor_IgnorierenSoftGeloeschteZeilen(t *testing.T) {
	db := testutils.SetupTestDB()
	h := NewHandler(db)
	page := models.StatusPage{Name: "sd", Slug: "sd394"}
	db.Create(&page)
	tag := models.Tag{Name: "T", StatusPageID: &page.ID}
	db.Create(&tag)
	comp := models.Component{StatusPageID: page.ID, Name: "c", Status: "operational"}
	db.Create(&comp)
	inc := models.Incident{StatusPageID: page.ID, Title: "i"}
	db.Create(&inc)

	ct := models.ComponentTag{ComponentID: comp.ID, TagID: tag.ID}
	it := models.IncidentTag{IncidentID: inc.ID, TagID: tag.ID}
	db.Create(&ct)
	db.Create(&it)
	if got := h.tagsForComponent(comp.ID); len(got) != 1 {
		t.Fatalf("aktive Komponenten-Zeile: %d Tags", len(got))
	}
	if got := h.tagsForIncident(inc.ID); len(got) != 1 {
		t.Fatalf("aktive Incident-Zeile: %d Tags", len(got))
	}
	db.Delete(&ct)
	db.Delete(&it)
	if got := h.tagsForComponent(comp.ID); len(got) != 0 {
		t.Errorf("tagsForComponent liefert gelöschte Zeile: %d", len(got))
	}
	if got := h.tagsForIncident(inc.ID); len(got) != 0 {
		t.Errorf("tagsForIncident liefert gelöschte Zeile: %d", len(got))
	}
}
