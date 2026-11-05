package models

import "testing"

// Die Statusregeln (behoben, erlaubte Mengen) stehen an einer
// Stelle; das Frontend spiegelt sie (incidentStatus.js).
func TestIncidentStatusRules(t *testing.T) {
	for status, want := range map[string]bool{"resolved": true, "completed": false, "investigating": false, "identified": false, "monitoring": false, "scheduled": false, "in_progress": false, "": false, "bogus": false} {
		if got := IsResolvedIncidentStatus(status); got != want {
			t.Errorf("IsResolvedIncidentStatus(%q) = %v, want %v", status, got, want)
		}
	}
	for status, want := range map[string]bool{"investigating": true, "identified": true, "monitoring": true, "resolved": true, "completed": false, "scheduled": false, "": false, "bogus": false} {
		if got := IsValidIncidentStatus(status); got != want {
			t.Errorf("IsValidIncidentStatus(%q) = %v, want %v", status, got, want)
		}
	}
	for status, want := range map[string]bool{"scheduled": true, "in_progress": true, "resolved": true, "completed": false, "investigating": false, "": false} {
		if got := IsValidMaintenanceStatus(status); got != want {
			t.Errorf("IsValidMaintenanceStatus(%q) = %v, want %v", status, got, want)
		}
	}
}

func TestIsValidIncidentSeverity(t *testing.T) {
	for _, ok := range []string{IncidentSeverityDegraded, IncidentSeverityPartial, IncidentSeverityMajor} {
		if !IsValidIncidentSeverity(ok) {
			t.Errorf("%q muss ein gültiger Schweregrad sein", ok)
		}
	}
	// Ein Komponentenstatus ohne Beeinträchtigung ist kein Schweregrad.
	for _, bad := range []string{"", "operational", "MAJOR_OUTAGE", "major_outage "} {
		if IsValidIncidentSeverity(bad) {
			t.Errorf("%q darf kein gültiger Schweregrad sein", bad)
		}
	}
}
