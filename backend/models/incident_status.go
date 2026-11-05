package models

// einzige Stelle im Backend, die festlegt, welche Vorfall- und
// Wartungs-Status es gibt und welche als „behoben" gelten. Das Frontend
// spiegelt die Regel in frontend/src/utils/incidentStatus.js
// (RESOLVED_INCIDENT_STATUSES) und hat einen Test, der beide abgleicht.
const (
	IncidentStatusInvestigating = "investigating"
	IncidentStatusIdentified    = "identified"
	IncidentStatusMonitoring    = "monitoring"
	IncidentStatusResolved      = "resolved"

	MaintenanceStatusScheduled  = "scheduled"
	MaintenanceStatusInProgress = "in_progress"
)

// ResolvedIncidentStatuses sind die Status, die einen Vorfall bzw. ein
// Wartungsfenster als behoben/abgeschlossen ausweisen.
//
// Der Altwert "completed" (Wartungs-Updates vor der Umstellung) wird beim
// Start von database.migrateCompletedIncidentStatus auf "resolved" umgestellt
// und gilt hier nicht mehr.
var ResolvedIncidentStatuses = []string{IncidentStatusResolved}

// IsResolvedIncidentStatus: true für resolved.
func IsResolvedIncidentStatus(status string) bool {
	return status == IncidentStatusResolved
}

// IsValidIncidentStatus: erlaubte Status für neue Schreibvorgänge bei normalen
// Vorfällen.
func IsValidIncidentStatus(status string) bool {
	switch status {
	case IncidentStatusInvestigating, IncidentStatusIdentified, IncidentStatusMonitoring, IncidentStatusResolved:
		return true
	}
	return false
}

// Schweregrade eines normalen Vorfalls; entsprechen den
// Beeinträchtigungs-Stufen des Komponentenstatus und gewichten die Verfügbarkeit.
const (
	IncidentSeverityDegraded = "degraded_performance"
	IncidentSeverityPartial  = "partial_outage"
	IncidentSeverityMajor    = "major_outage"
)

// IsValidIncidentSeverity: erlaubte Schweregrade für Schreibvorgänge.
func IsValidIncidentSeverity(s string) bool {
	switch s {
	case IncidentSeverityDegraded, IncidentSeverityPartial, IncidentSeverityMajor:
		return true
	}
	return false
}

// IsValidMaintenanceStatus: erlaubte Status für neue Schreibvorgänge bei
// Wartungsfenstern.
func IsValidMaintenanceStatus(status string) bool {
	switch status {
	case MaintenanceStatusScheduled, MaintenanceStatusInProgress, IncidentStatusResolved:
		return true
	}
	return false
}
