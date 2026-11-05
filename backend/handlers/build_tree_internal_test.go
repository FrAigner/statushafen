package handlers

import (
	"testing"

	"github.com/fraigner/statushafen/models"
)

// buildTree darf bei leerer Eingabe keine nil-Liste liefern
// (würde als JSON "null" serialisiert).
func TestBuildTree_EmptyInputReturnsNonNil(t *testing.T) {
	got := buildTree(nil, nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("erwartet leere, nicht-nil Liste, erhalten %#v", got)
	}
}

func TestBuildTree_ChildrenUnchanged(t *testing.T) {
	parent := uint(1)
	all := []models.Component{
		{Status: "group"},
		{ParentID: &parent, Status: "major_outage"},
	}
	all[0].ID = 1
	all[1].ID = 2
	got := buildTree(all, nil)
	if len(got) != 1 || len(got[0].Children) != 1 {
		t.Fatalf("Baum falsch: %#v", got)
	}
	if got[0].Status != "major_outage" {
		t.Errorf("Gruppenstatus nicht aggregiert: %s", got[0].Status)
	}
	if len(got[0].Children[0].Children) != 0 {
		t.Errorf("Blatt sollte keine Kinder haben")
	}
}
