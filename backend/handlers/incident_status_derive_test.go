package handlers

import (
	"testing"
	"time"
)

// Ableitung des Wartungsstatus aus den Zeiten, inkl. Grenzfälle.
func TestDeriveMaintenanceStatus(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		start, end time.Time
		want       string
	}{
		{"Vergangenheit", now.Add(-3 * time.Hour), now.Add(-2 * time.Hour), "resolved"},
		{"laufend", now.Add(-time.Hour), now.Add(time.Hour), "in_progress"},
		{"Zukunft", now.Add(time.Hour), now.Add(2 * time.Hour), "scheduled"},
		{"Start = jetzt", now, now.Add(time.Hour), "in_progress"},
		{"Ende = jetzt", now.Add(-time.Hour), now, "resolved"},
		{"Start eine Sekunde in der Zukunft", now.Add(time.Second), now.Add(time.Hour), "scheduled"},
		{"Ende eine Sekunde in der Zukunft", now.Add(-time.Hour), now.Add(time.Second), "in_progress"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveMaintenanceStatus(tc.start, tc.end, now); got != tc.want {
				t.Errorf("erwartet %s, bekam %s", tc.want, got)
			}
		})
	}
}
