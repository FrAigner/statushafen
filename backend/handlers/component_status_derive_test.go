package handlers

import "testing"

// Ableitung des angezeigten Komponentenstatus aus manuell
// gesetztem Status + offenen zugeordneten Vorfällen. effectiveComponentStatus
// darf den schlechteren der beiden Werte gewinnen lassen, damit weder ein
// offener Vorfall einen bereits schlechteren manuellen Status verschluckt,
// noch - der eigentliche Bugfix - ein fehlender Vorfall einen guten Status
// vorgaukelt.
func TestEffectiveComponentStatus(t *testing.T) {
	cases := []struct {
		name             string
		manualStatus     string
		incidentSeverity string
		want             string
	}{
		{"Betriebsbereit ohne offenen Vorfall bleibt Betriebsbereit", "operational", "", "operational"},
		{"Betriebsbereit mit offenem Vorfall wird beeinträchtigt", "operational", "degraded_performance", "degraded_performance"},
		{"Bereits beeinträchtigt mit offenem Vorfall bleibt beeinträchtigt", "degraded_performance", "degraded_performance", "degraded_performance"},
		{"Teilausfall mit offenem Vorfall bleibt Teilausfall (manueller Status ist schlechter)", "partial_outage", "degraded_performance", "partial_outage"},
		{"Großstörung mit offenem Vorfall bleibt Großstörung (manueller Status ist schlechter)", "major_outage", "degraded_performance", "major_outage"},
		{"Teilausfall ohne offenen Vorfall bleibt Teilausfall (manueller Status bleibt erhalten)", "partial_outage", "", "partial_outage"},
		{"Wartung mit offenem Vorfall wird beeinträchtigt (Vorfall ist schlechter als Wartung)", "under_maintenance", "degraded_performance", "degraded_performance"},
		{"Wartung ohne offenen Vorfall bleibt Wartung", "under_maintenance", "", "under_maintenance"},
		{"Schweregrad Teilausfall überlagert Betriebsbereit", "operational", "partial_outage", "partial_outage"},
		{"Schweregrad Großstörung überlagert Wartung", "under_maintenance", "major_outage", "major_outage"},
		{"Großstörung manuell schlägt milderen Vorfall-Schweregrad", "major_outage", "partial_outage", "major_outage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := effectiveComponentStatus(tc.manualStatus, tc.incidentSeverity)
			if got != tc.want {
				t.Errorf("effectiveComponentStatus(%q, %q) = %q, want %q", tc.manualStatus, tc.incidentSeverity, got, tc.want)
			}
		})
	}
}
