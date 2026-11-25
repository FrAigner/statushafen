package models

// HiddenComponentIDs returns the IDs of all invisible components
// including their descendants: hiding a group also hides
// its members.
func HiddenComponentIDs(all []Component) map[uint]bool {
	children := map[uint][]uint{}
	for _, comp := range all {
		if comp.ParentID != nil {
			children[*comp.ParentID] = append(children[*comp.ParentID], comp.ID)
		}
	}
	hidden := map[uint]bool{}
	var mark func(id uint)
	mark = func(id uint) {
		if hidden[id] {
			return // protects against cycles in legacy data
		}
		hidden[id] = true
		for _, ch := range children[id] {
			mark(ch)
		}
	}
	for _, comp := range all {
		// archived components count as invisible publicly.
		if !comp.Visible || comp.ArchivedAt != nil {
			mark(comp.ID)
		}
	}
	return hidden
}
