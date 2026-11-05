package externalstatus

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/fraigner/statushafen/componentstatus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// summaryFixture: erfundene Antwort im Format von /api/v2/summary.json mit drei
// Gruppen samt Kindern (testdata/summaryv2_summary.json).
func summaryFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/summaryv2_summary.json")
	require.NoError(t, err)
	return b
}

// withStatuses ersetzt in der Fixture die Status einzelner Komponenten
// (die Fixture ist komplett "operational").
func withStatuses(t *testing.T, raw []byte, statuses map[string]string) []byte {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	for _, c := range doc["components"].([]any) {
		comp := c.(map[string]any)
		if s, ok := statuses[comp["id"].(string)]; ok {
			comp["status"] = s
		}
	}
	out, err := json.Marshal(doc)
	require.NoError(t, err)
	return out
}

func summaryServer(t *testing.T, body *[]byte, etag string) *testTarget {
	t.Helper()
	return newTestTarget(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/summary.json" {
			http.NotFound(w, r)
			return
		}
		if etag != "" && r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if etag != "" {
			w.Header().Set("ETag", etag)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(*body)
	}))
}

func TestSummaryV2Provider_SummaryFixture(t *testing.T) {
	body := summaryFixture(t)
	tt := summaryServer(t, &body, `W/"b29160354272ac0f4b86ff79a17c1e02"`)
	p := SummaryV2Provider{Client: tt.client}

	sum, err := p.FetchSummary(context.Background(), FetchRequest{BaseURL: "https://example.com"})
	require.NoError(t, err)
	assert.False(t, sum.NotModified)
	assert.Equal(t, `W/"b29160354272ac0f4b86ff79a17c1e02"`, sum.ETag)
	assert.Equal(t, "https://status.example.com", sum.PageURL)
	require.Len(t, sum.Components, 17)

	byID := map[string]Component{}
	groups := 0
	for _, c := range sum.Components {
		byID[c.ExternalID] = c
		if c.IsGroup {
			groups++
		}
		assert.Equal(t, componentstatus.Operational, c.Status, c.Name)
		assert.Equal(t, "operational", c.RawStatus, c.Name)
	}
	assert.Equal(t, 3, groups)
	vm := byID["fx0000000006"]
	assert.Equal(t, "Service 03", vm.Name)
	assert.Equal(t, "fx0000000003", vm.GroupExternalID)
	assert.Equal(t, "Group A", vm.GroupName)
	assert.False(t, vm.IsGroup)
	g := byID["fx0000000003"]
	assert.True(t, g.IsGroup)
	assert.Empty(t, g.GroupExternalID)
	assert.Equal(t, 1, g.Position)

	// Conditional Request: gleicher ETag → 304.
	sum, err = p.FetchSummary(context.Background(), FetchRequest{BaseURL: "https://example.com", ETag: `W/"b29160354272ac0f4b86ff79a17c1e02"`})
	require.NoError(t, err)
	assert.True(t, sum.NotModified)
	assert.Empty(t, sum.Components)
	assert.Equal(t, `W/"b29160354272ac0f4b86ff79a17c1e02"`, sum.ETag)
}

func TestSummaryV2Provider_StatusMappingAusFixture(t *testing.T) {
	body := withStatuses(t, summaryFixture(t), map[string]string{
		"fx0000000002": "degraded_performance",
		"fx0000000006": "partial_outage",
		"fx0000000007": "major_outage",
		"fx0000000010": "under_maintenance",
		"fx0000000016": "brand_new_status",
	})
	tt := summaryServer(t, &body, "")
	sum, err := SummaryV2Provider{Client: tt.client}.FetchSummary(context.Background(), FetchRequest{BaseURL: "https://example.com"})
	require.NoError(t, err)
	got := map[string]string{}
	raw := map[string]string{}
	for _, c := range sum.Components {
		got[c.ExternalID] = c.Status
		raw[c.ExternalID] = c.RawStatus
	}
	assert.Equal(t, componentstatus.DegradedPerformance, got["fx0000000002"])
	assert.Equal(t, componentstatus.PartialOutage, got["fx0000000006"])
	assert.Equal(t, componentstatus.MajorOutage, got["fx0000000007"])
	assert.Equal(t, componentstatus.UnderMaintenance, got["fx0000000010"])
	assert.Equal(t, componentstatus.Unknown, got["fx0000000016"], "unbekannt statt Abbruch")
	assert.Equal(t, "brand_new_status", raw["fx0000000016"], "Rohwert bleibt erhalten")
}

func TestNormalizeSummaryV2Status_Vollstaendig(t *testing.T) {
	cases := map[string]string{
		"operational":          componentstatus.Operational,
		"degraded_performance": componentstatus.DegradedPerformance,
		"partial_outage":       componentstatus.PartialOutage,
		"major_outage":         componentstatus.MajorOutage,
		"under_maintenance":    componentstatus.UnderMaintenance,
		"":                     componentstatus.Unknown,
		"Operational":          componentstatus.Unknown,
		"group":                componentstatus.Unknown,
	}
	for in, want := range cases {
		assert.Equal(t, want, NormalizeSummaryV2Status(in), in)
	}
}

func TestSummaryV2Provider_Fehler(t *testing.T) {
	body := []byte(`{"page":{"url":"javascript:alert(1)"},"components":[{"id":"","name":"ohne ID","status":"operational"},{"id":"a","name":"A","status":"operational","group_id":"fehlt"}]}`)
	tt := summaryServer(t, &body, "")
	p := SummaryV2Provider{Client: tt.client}
	sum, err := p.FetchSummary(context.Background(), FetchRequest{BaseURL: "https://example.com"})
	require.NoError(t, err)
	assert.Empty(t, sum.PageURL, "javascript:-Link wird verworfen")
	require.Len(t, sum.Components, 1, "Komponente ohne ID wird übersprungen")
	assert.Equal(t, "fehlt", sum.Components[0].GroupExternalID)
	assert.Empty(t, sum.Components[0].GroupName, "unbekannte Gruppe ohne Namen, kein Crash")

	body = []byte(`kein json`)
	_, err = p.FetchSummary(context.Background(), FetchRequest{BaseURL: "https://example.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nicht lesbar")

	_, err = p.FetchSummary(context.Background(), FetchRequest{BaseURL: "https://example.com/falsch"})
	require.Error(t, err, "404 der Fremdseite")
}

func TestSafeLinkURL(t *testing.T) {
	assert.Equal(t, "https://status.example.com", safeLinkURL("https://status.example.com"))
	assert.Equal(t, "", safeLinkURL("http://status.example.com"))
	assert.Equal(t, "", safeLinkURL("javascript:alert(1)"))
	assert.Equal(t, "", safeLinkURL("https://"))
	assert.Equal(t, "", safeLinkURL("https://[::1"))
	assert.Equal(t, "", safeLinkURL(""))
}
