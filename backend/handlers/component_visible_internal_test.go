package handlers

import (
	"testing"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// Randfälle der Sichtbarkeits-Hilfen.
func TestHiddenComponentIDs_ZyklusEndetUndMarkiertNachfahren(t *testing.T) {
	one, two, three := uint(1), uint(2), uint(3)
	comps := []models.Component{
		{Model: gorm.Model{ID: 1}, ParentID: &two, Visible: false}, // Zyklus 1 <-> 2
		{Model: gorm.Model{ID: 2}, ParentID: &one, Visible: true},
		{Model: gorm.Model{ID: 3}, ParentID: &three, Visible: true}, // Selbstbezug, sichtbar
		{Model: gorm.Model{ID: 4}, ParentID: &one, Visible: true},
	}
	hidden := hiddenComponentIDs(comps)
	assert.Equal(t, map[uint]bool{1: true, 2: true, 4: true}, hidden)
	assert.Len(t, withoutHiddenComponents(comps), 1)
}

func TestAllHidden(t *testing.T) {
	hidden := map[uint]bool{1: true, 2: true}
	assert.False(t, allHidden(nil, hidden), "ohne Zuordnung betrifft es die ganze Seite")
	assert.True(t, allHidden([]uint{1, 2}, hidden))
	assert.False(t, allHidden([]uint{1, 3}, hidden))
}

func TestStripHiddenIncidentComponents_OhneVersteckteBleibtUnveraendert(t *testing.T) {
	incs := []models.Incident{{IncidentComponents: []models.IncidentComponent{{ComponentID: 5}}}}
	stripHiddenIncidentComponents(incs, map[uint]bool{})
	assert.Len(t, incs[0].IncidentComponents, 1)
}
