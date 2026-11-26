package externalstatus

import (
	"time"

	"github.com/fraigner/statushafen/componentstatus"
	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// UpsertResult counts what a fetch changed on the external components.
type UpsertResult struct {
	Seen     int `json:"seen"`
	Created  int `json:"created"`
	Vanished int `json:"vanished"`
}

// upsertComponents reconciles a source's external components with sum.
// Idempotent: the same fetch twice only changes LastSeenAt the second time.
// Components that are missing are marked as vanished instead of deleted,
// so links are preserved; if they reappear, the mark is reverted.
// Duplicate IDs in a (faulty) response count only on first occurrence.
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

// linkedRefRow is a result row of LinkedRefs.
type linkedRefRow struct {
	ComponentID      uint
	ExternalID       string
	ExternalSourceID uint
	models.ExternalStatusRef
}

// groupKey identifies an external group within a source.
type groupKey struct {
	sourceID   uint
	externalID string
}

// groupMember is a current member of an external group.
type groupMember struct {
	ExternalSourceID uint
	GroupExternalID  string
	Name             string
	Status           string
}

// LinkedRefs returns, per own component of a status page, the linked
// external components. Only sources that are active and belong to the same
// status page (tenant separation secured twice: link and
// source must point to pageID). Vanished ones are included with Vanished=true
// but do not count in Effective.
//
// If a link points to a group, Status holds the
// worst of its current members (see groupStatus).
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

// groupSourceIDs returns the sources whose groups are linked in rows.
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

// groupMembers loads the current members of the groups of the sources
// sourceIDs (one query). Members are determined dynamically via group_external_id:
// new components count immediately, vanished ones do not.
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

// GroupWorst returns the status and worst component of an external group
// (for lists outside LinkedRefs, e.g. the link list in the admin).
func GroupWorst(db *gorm.DB, sourceID uint, groupExternalID string) (string, *models.ExternalWorst, error) {
	members, err := groupMembers(db, []uint{sourceID})
	if err != nil {
		return "", nil, err
	}
	status, worst := groupStatus(members[groupKey{sourceID, groupExternalID}])
	return status, worst, nil
}

// groupStatus determines a group's status from its members: the
// worst by componentstatus.Severity (on a tie the first by
// name) together with the component that yields it. An empty group - and one whose
// members are all "unknown"/"group" - yields "unknown" and no Worst:
// it worsens nothing and triggers no false alarm.
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

// Effective is the effective status of a component: the worst of
// its own status (incl. overlay by open incidents) and all
// linked, non-vanished external components. Ranking and
// classification of maintenance/unknown: componentstatus.Severity. If the source
// is down, its last known status is considered stale
// and no longer counts, so the public page does not show an
// outdated disruption for hours.
func Effective(own string, refs []models.ExternalStatusRef) string {
	statuses := []string{own}
	for _, r := range refs {
		if !r.Vanished && !r.SourceOutage {
			statuses = append(statuses, r.Status)
		}
	}
	return componentstatus.Worst(statuses...)
}

// EffectiveStatuses computes the effective status of all components of a
// status page (excluding pure groups, whose status follows from the children).
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
