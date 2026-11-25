package componentstatus

import (
	"github.com/fraigner/statushafen/models"

	"gorm.io/gorm"
)

// FromIncidentSeverity maps the severity of an open, assigned
// incident (Incident.Severity) to a component status.
// The severity values are intentionally identical to the
// component statuses (degraded_performance/partial_outage/major_outage); the
// availability bars (handlers/availability.go) and the display use
// THIS one mapping so they do not contradict each other. Empty or
// unknown values (legacy data) are conservatively treated as before as
// degraded_performance.
func FromIncidentSeverity(severity string) string {
	if models.IsValidIncidentSeverity(severity) {
		return severity
	}
	return DegradedPerformance
}

// FromOpenIncident maps an open, assigned incident to a
// component status: incidentSeverity is the worst
// severity of the component's open incidents, empty = no open
// incident (-> "operational"). Since the switch the severity applies instead of
// a blanket "degraded_performance". Since the switch here instead of in
// handlers/component.go, see package doc.
func FromOpenIncident(incidentSeverity string) string {
	if incidentSeverity == "" {
		return Operational
	}
	return FromIncidentSeverity(incidentSeverity)
}

// Own combines the manually set status of a component with the status derived
// from open, assigned incidents: the
// worse value wins (ranking: Severity). So an open
// incident does not swallow an already worse manual status, and a resolved
// incident conversely does not overwrite the manual status - it stays
// unchanged in the DB and is only overlaid for display.
// incidentSeverity: value from OpenIncidentComponentIDs, empty = no incident.
func Own(manualStatus, incidentSeverity string) string {
	return Worst(manualStatus, FromOpenIncident(incidentSeverity))
}

// OpenIncidentComponentIDs returns, for each component of a status page that is
// assigned to at least one open (status not resolved, models.IsResolvedIncidentStatus), normal
// (not maintenance-window) incident, the WORST
// severity of those incidents; components without an open
// incident are missing from the map. Maintenance
// windows are left out, since they already set their component status via
// their own, destructive logic (CreateIncidentHandler/
// maintenance_scheduler.go, "under_maintenance" written directly to the DB)
// - this derivation must not overlay that.
func OpenIncidentComponentIDs(db *gorm.DB, pageID uint) map[uint]string {
	var rows []struct {
		ComponentID uint
		Severity    string
	}
	db.Model(&models.IncidentComponent{}).
		Select("incident_components.component_id AS component_id, incidents.severity AS severity").
		Joins("JOIN incidents ON incidents.id = incident_components.incident_id AND incidents.deleted_at IS NULL").
		Where("incidents.status_page_id = ? AND incidents.is_maintenance = ? AND incidents.status NOT IN ?", pageID, false, models.ResolvedIncidentStatuses).
		Scan(&rows)

	out := make(map[uint]string, len(rows))
	for _, r := range rows {
		st := FromIncidentSeverity(r.Severity)
		if cur, ok := out[r.ComponentID]; !ok || Severity(st) > Severity(cur) {
			out[r.ComponentID] = st
		}
	}
	return out
}
