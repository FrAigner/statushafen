package models

// the single place in the backend that defines which incident and
// maintenance statuses exist and which count as "resolved". The frontend
// mirrors the rule in frontend/src/utils/incidentStatus.ts
// (RESOLVED_INCIDENT_STATUSES) and has a test that compares both.
const (
	IncidentStatusInvestigating = "investigating"
	IncidentStatusIdentified    = "identified"
	IncidentStatusMonitoring    = "monitoring"
	IncidentStatusResolved      = "resolved"

	MaintenanceStatusScheduled  = "scheduled"
	MaintenanceStatusInProgress = "in_progress"
)

// ResolvedIncidentStatuses are the statuses that mark an incident or a
// maintenance window as resolved/completed.
//
// The legacy value "completed" (maintenance updates before the changeover) is converted to "resolved"
// at start by database.migrateCompletedIncidentStatus
// and no longer applies here.
var ResolvedIncidentStatuses = []string{IncidentStatusResolved}

// IsResolvedIncidentStatus: true for resolved.
func IsResolvedIncidentStatus(status string) bool {
	return status == IncidentStatusResolved
}

// IsValidIncidentStatus: statuses allowed for new writes on normal
// incidents.
func IsValidIncidentStatus(status string) bool {
	switch status {
	case IncidentStatusInvestigating, IncidentStatusIdentified, IncidentStatusMonitoring, IncidentStatusResolved:
		return true
	}
	return false
}

// Severity levels of a normal incident; they correspond to the
// impairment levels of the component status and weight the availability.
const (
	IncidentSeverityDegraded = "degraded_performance"
	IncidentSeverityPartial  = "partial_outage"
	IncidentSeverityMajor    = "major_outage"
)

// IsValidIncidentSeverity: severities allowed for writes.
func IsValidIncidentSeverity(s string) bool {
	switch s {
	case IncidentSeverityDegraded, IncidentSeverityPartial, IncidentSeverityMajor:
		return true
	}
	return false
}

// IsValidMaintenanceStatus: statuses allowed for new writes on
// maintenance windows.
func IsValidMaintenanceStatus(status string) bool {
	switch status {
	case MaintenanceStatusScheduled, MaintenanceStatusInProgress, IncidentStatusResolved:
		return true
	}
	return false
}
