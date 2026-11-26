// Package componentstatus contains the ranking of component statuses.
//
// Previously it lived as statusSeverity/worstStatus only in
// handlers/component.go. The polling service for external status pages
// (package externalstatus) needs the same ranking to detect status
// changes, but must not import handlers (handlers imports
// externalstatus). Hence a separate package (only models/gorm as a
// dependency); handlers delegates there, so there is only one ranking and
// only one incident overlay.
package componentstatus

// Status values of a component (Component.Status) plus "unknown" for
// external components with a status we do not know.
const (
	Operational         = "operational"
	DegradedPerformance = "degraded_performance"
	PartialOutage       = "partial_outage"
	MajorOutage         = "major_outage"
	UnderMaintenance    = "under_maintenance"
	Group               = "group"
	Unknown             = "unknown"
)

// Severity gives each status a numeric severity (higher = worse).
//
// Maintenance sits between "operational" and the real outages:
// planned, but noticeable to users. This also applies to coupled external
// components: a maintenance at the provider turns the own
// component to "under_maintenance", any real outage (own or
// external) takes precedence. "group" and "unknown" have rank 0 and never
// make anything worse: an unknown external status must not trigger a false alarm.
func Severity(s string) int {
	switch s {
	case MajorOutage:
		return 5
	case PartialOutage:
		return 4
	case DegradedPerformance:
		return 3
	case UnderMaintenance:
		return 2
	case Operational:
		return 1
	default:
		return 0
	}
}

// Worst returns the worst status from statuses, at least "operational".
func Worst(statuses ...string) string {
	worst := Operational
	for _, s := range statuses {
		if Severity(s) > Severity(worst) {
			worst = s
		}
	}
	return worst
}
