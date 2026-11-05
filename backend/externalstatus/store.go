package externalstatus

import (
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// UpsertResult zählt, was ein Abruf an den externen Komponenten geändert hat.
type UpsertResult struct {
	Seen     int `json:"seen"`
	Created  int `json:"created"`
	Vanished int `json:"vanished"`
}

// upsertComponents gleicht die externen Komponenten einer Quelle mit sum ab.
// Idempotent: derselbe Abruf zweimal ändert beim zweiten Mal nur LastSeenAt.
// Komponenten, die fehlen, werden als verschwunden markiert statt gelöscht,
// damit Kopplungen erhalten bleiben; tauchen sie wieder auf, wird die
// Markierung zurückgenommen. Doppelte IDs in einer (fehlerhaften) Antwort
// zählen nur beim ersten Vorkommen.
func upsertComponents(tx *gorm.DB, src models.ExternalSource, sum *Summary, now time.Time) (UpsertResult, error) {
	var existing []models.ExternalComponent
	if err := tx.Where("external_source_id = ?", src.ID).Find(&existing).Error; err != nil {
		return UpsertResult{}, err
	}
	byID := make(map[string]*models.ExternalComponent, len(existing))
	for i := range existing {
		byID[existing[i].ExternalID] = &existing[i]
	}
	link := sum.PageURL
	if link == "" {
		link = src.BaseURL
	}

	var res UpsertResult
	seen := map[string]bool{}
	for _, c := range sum.Components {
		if seen[c.ExternalID] {
			continue
		}
		seen[c.ExternalID] = true
		res.Seen++
		row, ok := byID[c.ExternalID]
		if !ok {
			row = &models.ExternalComponent{ExternalSourceID: src.ID, ExternalID: c.ExternalID}
			res.Created++
		}
		row.Name = c.Name
		row.IsGroup = c.IsGroup
		row.GroupExternalID = c.GroupExternalID
		row.GroupName = c.GroupName
		row.Position = c.Position
		row.RawStatus = c.RawStatus
		row.Status = c.Status
		row.URL = link
		row.LastSeenAt = now
		row.Vanished = false
		row.VanishedAt = nil
		if err := tx.Save(row).Error; err != nil {
			return UpsertResult{}, err
		}
	}
	for i := range existing {
		row := &existing[i]
		if seen[row.ExternalID] || row.Vanished {
			continue
		}
		row.Vanished = true
		row.VanishedAt = &now
		if err := tx.Save(row).Error; err != nil {
			return UpsertResult{}, err
		}
		res.Vanished++
	}
	return res, nil
}

// linkedRefRow ist eine Ergebniszeile von LinkedRefs.
type linkedRefRow struct {
	ComponentID      uint
	ExternalID       string
	ExternalSourceID uint
	models.ExternalStatusRef
}

// groupKey identifiziert eine externe Gruppe innerhalb einer Quelle.
type groupKey struct {
	sourceID   uint
	externalID string
}

// groupMember ist ein aktuelles Mitglied einer externen Gruppe.
type groupMember struct {
	ExternalSourceID uint
	GroupExternalID  string
	Name             string
	Status           string
}

// LinkedRefs liefert je eigener Komponente einer Statusseite die gekoppelten
// externen Komponenten. Nur Quellen, die aktiv sind und zur selben
// Statusseite gehören (Mandantentrennung doppelt abgesichert: Link und
// Quelle müssen auf pageID zeigen). Verschwundene sind mit Vanished=true
// enthalten, zählen aber nicht in Effective.
//
// Zeigt eine Kopplung auf eine Gruppe, steht in Status das
// Schlimmste ihrer aktuellen Mitglieder (siehe groupStatus).
func LinkedRefs(db *gorm.DB, pageID uint) (map[uint][]models.ExternalStatusRef, error) {
	var rows []linkedRefRow
	err := db.Table("external_component_links AS l").
		Select(`l.component_id AS component_id, ec.id AS external_component_id, ec.external_id AS external_id,
			es.id AS source_id, ec.external_source_id AS external_source_id,
			es.name AS source_name, ec.name AS name, ec.group_name AS group_name, ec.status AS status,
			ec.url AS url, ec.vanished AS vanished, ec.is_group AS is_group,
			(es.outage_since IS NOT NULL) AS source_outage`).
		Joins("JOIN external_components ec ON ec.id = l.external_component_id AND ec.deleted_at IS NULL").
		Joins("JOIN external_sources es ON es.id = ec.external_source_id AND es.deleted_at IS NULL").
		Where("l.deleted_at IS NULL AND l.status_page_id = ? AND es.status_page_id = ? AND es.active = ?", pageID, pageID, true).
		Order("es.name, ec.name, ec.id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	members, err := groupMembers(db, groupSourceIDs(rows))
	if err != nil {
		return nil, err
	}
	refs := map[uint][]models.ExternalStatusRef{}
	for _, r := range rows {
		if r.IsGroup {
			r.GroupName = r.Name
			r.Status, r.Worst = groupStatus(members[groupKey{r.ExternalSourceID, r.ExternalID}])
		}
		refs[r.ComponentID] = append(refs[r.ComponentID], r.ExternalStatusRef)
	}
	return refs, nil
}

// groupSourceIDs liefert die Quellen, deren Gruppen in rows gekoppelt sind.
func groupSourceIDs(rows []linkedRefRow) []uint {
	seen := map[uint]bool{}
	var ids []uint
	for _, r := range rows {
		if r.IsGroup && !seen[r.ExternalSourceID] {
			seen[r.ExternalSourceID] = true
			ids = append(ids, r.ExternalSourceID)
		}
	}
	return ids
}

// groupMembers lädt die aktuellen Mitglieder der Gruppen der Quellen
// sourceIDs (eine Abfrage). Mitglieder sind dynamisch über group_external_id
// bestimmt: Neue Bausteine zählen sofort, verschwundene nicht.
func groupMembers(db *gorm.DB, sourceIDs []uint) (map[groupKey][]groupMember, error) {
	out := map[groupKey][]groupMember{}
	if len(sourceIDs) == 0 {
		return out, nil
	}
	var members []groupMember
	err := db.Table("external_components").
		Select("external_source_id, group_external_id, name, status").
		Where("deleted_at IS NULL AND vanished = ? AND is_group = ? AND group_external_id <> '' AND external_source_id IN ?", false, false, sourceIDs).
		Order("name, id").
		Scan(&members).Error
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		k := groupKey{m.ExternalSourceID, m.GroupExternalID}
		out[k] = append(out[k], m)
	}
	return out, nil
}

// GroupWorst liefert Status und schlechtesten Baustein einer externen Gruppe
// (für Listen außerhalb von LinkedRefs, z. B. die Kopplungsliste im Admin).
func GroupWorst(db *gorm.DB, sourceID uint, groupExternalID string) (string, *models.ExternalWorst, error) {
	members, err := groupMembers(db, []uint{sourceID})
	if err != nil {
		return "", nil, err
	}
	status, worst := groupStatus(members[groupKey{sourceID, groupExternalID}])
	return status, worst, nil
}

// groupStatus bestimmt den Status einer Gruppe aus ihren Mitgliedern: das
// Schlimmste nach componentstatus.Severity (bei Gleichstand das erste nach
// Name) samt dem Baustein, der es liefert. Eine leere Gruppe - und eine, deren
// Mitglieder alle "unknown"/"group" sind - liefert "unknown" und kein Worst:
// Sie verschlechtert nichts und löst keinen Fehlalarm aus.
func groupStatus(members []groupMember) (string, *models.ExternalWorst) {
	var worst *groupMember
	for i := range members {
		if componentstatus.Severity(members[i].Status) == 0 {
			continue
		}
		if worst == nil || componentstatus.Severity(members[i].Status) > componentstatus.Severity(worst.Status) {
			worst = &members[i]
		}
	}
	if worst == nil {
		return componentstatus.Unknown, nil
	}
	return worst.Status, &models.ExternalWorst{Name: worst.Name, Status: worst.Status}
}

// Effective ist der effektive Status einer Komponente: das Schlimmste aus
// ihrem eigenen Status (inkl. Überlagerung durch offene Vorfälle) und allen
// gekoppelten, nicht verschwundenen externen Komponenten. Rangfolge und
// Einordnung von Wartung/unknown: componentstatus.Severity. Ist die Quelle
// ausgefallen, gilt ihr letzter bekannter Status als veraltet
// und zählt nicht mehr, damit die öffentliche Seite nicht stundenlang eine
// überholte Störung zeigt.
func Effective(own string, refs []models.ExternalStatusRef) string {
	statuses := []string{own}
	for _, r := range refs {
		if !r.Vanished && !r.SourceOutage {
			statuses = append(statuses, r.Status)
		}
	}
	return componentstatus.Worst(statuses...)
}

// EffectiveStatuses berechnet den effektiven Status aller Komponenten einer
// Statusseite (ohne reine Gruppen, deren Status aus den Kindern folgt).
func EffectiveStatuses(db *gorm.DB, pageID uint) (map[uint]string, error) {
	var comps []models.Component
	if err := db.Select("id", "status").Where("status_page_id = ? AND status <> ? AND archived_at IS NULL", pageID, componentstatus.Group).Find(&comps).Error; err != nil {
		return nil, err
	}
	refs, err := LinkedRefs(db, pageID)
	if err != nil {
		return nil, err
	}
	open := componentstatus.OpenIncidentComponentIDs(db, pageID)
	out := make(map[uint]string, len(comps))
	for _, c := range comps {
		out[c.ID] = Effective(componentstatus.Own(c.Status, open[c.ID]), refs[c.ID])
	}
	return out, nil
}
