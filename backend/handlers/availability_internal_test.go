package handlers

import (
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// Randfälle der Intervallbildung und der Gruppen-Auflösung.
func TestIncidentInterval(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	h := func(n int) time.Time { return now.Add(time.Duration(n) * time.Hour) }
	created := h(-10)
	cases := []struct {
		name       string
		inc        models.Incident
		wantOK     bool
		start, end time.Time
		status     string
	}{
		{"offen: bis jetzt, CreatedAt als Beginn", models.Incident{Model: gorm.Model{CreatedAt: created}, Status: "investigating"}, true, created, now, "degraded_performance"},
		{"StartedAt schlägt CreatedAt", models.Incident{Model: gorm.Model{CreatedAt: created}, StartedAt: ptrTime(h(-5)), Status: "identified"}, true, h(-5), now, "degraded_performance"},
		{"ResolvedAt in der Zukunft wird auf jetzt gekappt", models.Incident{Model: gorm.Model{CreatedAt: created}, Status: "resolved", ResolvedAt: ptrTime(h(3))}, true, created, now, "degraded_performance"},
		{"ResolvedAt vor Beginn: Länge 0", models.Incident{Model: gorm.Model{CreatedAt: created}, StartedAt: ptrTime(h(-2)), Status: "resolved", ResolvedAt: ptrTime(h(-4))}, true, h(-2), h(-2), "degraded_performance"},
		{"behoben ohne ResolvedAt: UpdatedAt", models.Incident{Model: gorm.Model{CreatedAt: created, UpdatedAt: h(-8)}, Status: "resolved"}, true, created, h(-8), "degraded_performance"},
		{"Wartung", models.Incident{IsMaintenance: true, ScheduledStartAt: ptrTime(h(-3)), ScheduledEndAt: ptrTime(h(-2))}, true, h(-3), h(-2), "under_maintenance"},
		{"Wartung ohne Ende: nicht auswertbar", models.Incident{IsMaintenance: true, ScheduledStartAt: ptrTime(h(-3))}, false, time.Time{}, time.Time{}, ""},
		{"Wartung ohne Beginn: nicht auswertbar", models.Incident{IsMaintenance: true, ScheduledEndAt: ptrTime(h(-3))}, false, time.Time{}, time.Time{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			iv, ok := incidentInterval(tc.inc, now)
			assert.Equal(t, tc.wantOK, ok)
			if !ok {
				return
			}
			assert.True(t, tc.start.Equal(iv.Start), "Beginn %v", iv.Start)
			assert.True(t, tc.end.Equal(iv.End), "Ende %v", iv.End)
			assert.Equal(t, tc.status, iv.Status)
		})
	}
}

func TestExpandGroup(t *testing.T) {
	g1, g2 := uint(1), uint(2)
	byID := map[uint]models.Component{
		1: {Model: gorm.Model{ID: 1}, Status: "group"},
		2: {Model: gorm.Model{ID: 2}, Status: "group", ParentID: &g1},
		3: {Model: gorm.Model{ID: 3}, Status: "operational", ParentID: &g2},
		4: {Model: gorm.Model{ID: 4}, Status: "major_outage", ParentID: &g1},
	}
	children := map[uint][]uint{1: {2, 4}, 2: {3}}
	assert.ElementsMatch(t, []uint{3, 4}, expandGroup(byID, children, 1), "verschachtelte Gruppe")
	assert.Equal(t, []uint{4}, expandGroup(byID, children, 4))
	assert.Empty(t, expandGroup(byID, children, 99), "unbekannte Komponente")
}

func ptrTime(t time.Time) *time.Time { return &t }
