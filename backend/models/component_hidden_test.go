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

// Whoever hides or archives a group also hides all descendants;
// visible siblings stay visible.
func TestHiddenComponentIDs(t *testing.T) {
	now := time.Now()
	archived := comp(20, nil, true)
	archived.ArchivedAt = &now
	all := []Component{
		comp(1, nil, false),         // hidden group
		comp(2, uintPtr(1), true),   // child
		comp(3, uintPtr(2), true),   // grandchild
		comp(4, nil, true),          // visible
		comp(5, uintPtr(4), true),   // visible
		archived,                    // archived group
		comp(21, uintPtr(20), true), // child of the archived one
	}
	got := HiddenComponentIDs(all)
	assert.Equal(t, map[uint]bool{1: true, 2: true, 3: true, 20: true, 21: true}, got)
}

// Cycles in legacy data must not let the evaluation run endlessly.
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
