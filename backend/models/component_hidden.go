package models

// HiddenComponentIDs liefert die IDs aller unsichtbaren Komponenten
// einschließlich ihrer Nachfahren: Wer eine Gruppe ausblendet, blendet
// deren Mitglieder mit aus.
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
			return // schützt vor Zyklen in Altdaten
		}
		hidden[id] = true
		for _, ch := range children[id] {
			mark(ch)
		}
	}
	for _, comp := range all {
		// archivierte Komponenten gelten öffentlich wie unsichtbare.
		if !comp.Visible || comp.ArchivedAt != nil {
			mark(comp.ID)
		}
	}
	return hidden
}
