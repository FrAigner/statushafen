package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Zuordnung impact -> Status (Vorschlag).
func TestExternalImpactStatus(t *testing.T) {
	cases := []struct {
		impact string
		want   string
		counts bool
	}{
		{"none", "", false},
		{"minor", "degraded_performance", true},
		{"major", "partial_outage", true},
		{"critical", "major_outage", true},
		{"maintenance", "degraded_performance", true},
		{"", "degraded_performance", true},
		{"neu-von-der-quelle", "degraded_performance", true},
	}
	for _, tc := range cases {
		got, counts := externalImpactStatus(tc.impact)
		assert.Equal(t, tc.want, got, tc.impact)
		assert.Equal(t, tc.counts, counts, tc.impact)
	}
}
