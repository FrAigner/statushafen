package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func uintPtr(v uint) *uint { return &v }

func comp(id uint, parent *uint, visible bool) Component {
	c := Component{ParentID: parent, Visible: visible}
	c.ID = id
	return c
}

// Wer eine Gruppe ausblendet oder archiviert, blendet alle Nachfahren mit aus;
// sichtbare Geschwister bleiben sichtbar.
func TestHiddenComponentIDs(t *testing.T) {
	now := time.Now()
	archived := comp(20, nil, true)
	archived.ArchivedAt = &now
	all := []Component{
		comp(1, nil, false),         // ausgeblendete Gruppe
		comp(2, uintPtr(1), true),   // Kind
		comp(3, uintPtr(2), true),   // Enkel
		comp(4, nil, true),          // sichtbar
		comp(5, uintPtr(4), true),   // sichtbar
		archived,                    // archivierte Gruppe
		comp(21, uintPtr(20), true), // Kind der archivierten
	}
	got := HiddenComponentIDs(all)
	assert.Equal(t, map[uint]bool{1: true, 2: true, 3: true, 20: true, 21: true}, got)
}

// Zyklen in Altdaten dürfen die Auswertung nicht endlos laufen lassen.
func TestHiddenComponentIDs_CycleTerminates(t *testing.T) {
	all := []Component{
		comp(1, uintPtr(2), false),
		comp(2, uintPtr(1), true),
	}
	assert.Equal(t, map[uint]bool{1: true, 2: true}, HiddenComponentIDs(all))
}

func TestHiddenComponentIDs_EmptyInput(t *testing.T) {
	assert.Empty(t, HiddenComponentIDs(nil))
}
