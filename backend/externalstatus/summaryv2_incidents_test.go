package externalstatus

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fraigner/statushafen/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Provider-Tests für incidents.json / scheduled-maintenances.json.
// Die Fixtures sind erfundene Antworten im Format der API (testdata/summaryv2_*.json),
// darunter die abgeschlossene Wartung fx0000000062.

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return b
}

// feedServer liefert je Pfad einen Body; etag schaltet Conditional Requests ein.
func feedServer(t *testing.T, files map[string]*[]byte, etag string) *testTarget {
	t.Helper()
	return newTestTarget(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
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
		_, _ = w.Write(*body)
	}))
}

func fetchFeed(t *testing.T, tt *testTarget, kind string) (*IncidentFeed, error) {
	t.Helper()
	return SummaryV2Provider{Client: tt.client}.FetchIncidents(context.Background(), IncidentRequest{BaseURL: "https://example.com", Kind: kind})
}

func TestSummaryV2Provider_IncidentsFixture(t *testing.T) {
	body := readFixture(t, "summaryv2_incidents.json")
	tt := feedServer(t, map[string]*[]byte{"/api/v2/incidents.json": &body}, `W/"abc"`)

	feed, err := fetchFeed(t, tt, models.ExternalIncidentKindIncident)
	require.NoError(t, err)
	assert.Equal(t, `W/"abc"`, feed.ETag)
	require.Len(t, feed.Items, 3)

	multi := feed.Items[0]
	assert.Equal(t, "fx0000000019", multi.ExternalID)
	assert.Equal(t, models.ExternalIncidentKindIncident, multi.Kind)
	assert.Equal(t, "resolved", multi.Status)
	assert.True(t, multi.Closed)
	assert.Equal(t, "https://example.com/incidents/fx0000000019", multi.URL, "Link aus Basis-URL und ID, nicht der Shortlink eines Dritten")
	assert.Len(t, multi.Components, 6)
	assert.Len(t, multi.Updates, 5)
	require.NotNil(t, multi.StartedAt)
	assert.Equal(t, time.UTC, multi.StartedAt.Location(), "Zeiten werden als UTC geliefert")

	// postmortem zählt als behoben.
	assert.Equal(t, "fx0000000032", feed.Items[1].ExternalID)
	assert.Equal(t, "resolved", feed.Items[1].Status)
	assert.Equal(t, "postmortem", feed.Items[1].RawStatus)
	assert.True(t, feed.Items[1].Closed)

	// Ohne Komponenten bleibt die Meldung erhalten (Auswertung entscheidet später).
	assert.Empty(t, feed.Items[2].Components)

	// Update-Texte sind Klartext: Die API liefert HTML-Schnipsel.
	for _, it := range feed.Items {
		for _, u := range it.Updates {
			assert.NotContains(t, u.Body, "<br", it.ExternalID)
			assert.NotContains(t, u.Body, "<b>", it.ExternalID)
		}
	}
	assert.Contains(t, feed.Items[2].Updates[0].Body, "Deutsch")
}

func TestSummaryV2Provider_MaintenancesFixture(t *testing.T) {
	body := readFixture(t, "summaryv2_maintenances.json")
	tt := feedServer(t, map[string]*[]byte{"/api/v2/scheduled-maintenances.json": &body}, "")

	feed, err := fetchFeed(t, tt, models.ExternalIncidentKindMaintenance)
	require.NoError(t, err)
	require.Len(t, feed.Items, 2)

	sched := feed.Items[0]
	assert.Equal(t, "scheduled", sched.Status)
	assert.False(t, sched.Closed)
	assert.Equal(t, "maintenance", sched.Impact)
	require.NotNil(t, sched.ScheduledAt)
	require.NotNil(t, sched.ScheduledTo)
	// 2026-11-09T21:00:00.000+01:00 ist 20:00 UTC: die Zeitzone wird umgerechnet.
	assert.Equal(t, time.Date(2026, 11, 9, 20, 0, 0, 0, time.UTC), *sched.ScheduledAt)

	done := feed.Items[1]
	assert.Equal(t, "fx0000000062", done.ExternalID)
	assert.Equal(t, "completed", done.Status)
	assert.True(t, done.Closed)
	assert.Equal(t, "https://example.com/incidents/fx0000000062", done.URL)
}

func TestSummaryV2Provider_FeedNotModified(t *testing.T) {
	body := readFixture(t, "summaryv2_incidents.json")
	tt := feedServer(t, map[string]*[]byte{"/api/v2/incidents.json": &body}, `"v1"`)
	p := SummaryV2Provider{Client: tt.client}

	feed, err := p.FetchIncidents(context.Background(), IncidentRequest{BaseURL: "https://example.com", Kind: models.ExternalIncidentKindIncident, ETag: `"v1"`})
	require.NoError(t, err)
	assert.True(t, feed.NotModified)
	assert.Equal(t, `"v1"`, feed.ETag)
	assert.Empty(t, feed.Items)
}

func TestSummaryV2Provider_FeedFehler(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		wantErr string
	}{
		{"ungültiges JSON", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"incidents": [`)) }, "nicht lesbar"},
		{"kein Objekt", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`[1,2]`)) }, "nicht lesbar"},
		{"HTTP 500", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }, "HTTP 500"},
		{"HTTP 404", http.NotFound, "HTTP 404"},
		{"zu groß", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"incidents":[{"id":"a","name":"` + strings.Repeat("x", MaxResponseBytes) + `"}]}`))
		}, "größer als"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tt := newTestTarget(t, tc.handler)
			feed, err := fetchFeed(t, tt, models.ExternalIncidentKindIncident)
			require.Error(t, err)
			assert.Nil(t, feed)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestSummaryV2Provider_FeedRobustgegenSchlechteDaten(t *testing.T) {
	long := strings.Repeat("ä", maxTitleBytes) // doppelt so viele Bytes wie erlaubt
	doc := `{"incidents":[
	 {"id":"","name":"ohne ID","status":"resolved"},
	 {"id":"dup","name":"erster","status":"investigating","impact":"bogus","created_at":"kaputt","updated_at":"2026-10-29T10:00:00Z",
	  "components":[{"id":"c1","name":"A"},{"id":"c1","name":"A doppelt"},{"id":"","name":"leer"}],
	  "incident_updates":[{"id":"u1","status":"investigating","body":"a\u0000b","created_at":"2026-10-29T10:00:00Z"},
	                      {"id":"u1","status":"resolved","body":"doppelt","created_at":"2026-10-29T11:00:00Z"}]},
	 {"id":"dup","name":"zweiter","status":"resolved"},
	 {"id":"` + strings.Repeat("i", maxIDBytes+1) + `","name":"ID zu lang"},
	 {"id":"leer","name":"  ","status":"resolved","started_at":"2026-10-29T10:00:00Z","incident_updates":[{"id":"u","status":"resolved","body":"x"}]},
	 {"id":"neu","name":"` + long + `","status":"etwas-neues","created_at":"2026-10-29T10:00:00-05:00"}
	]}`
	tt := newTestTarget(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(doc)) }))

	feed, err := fetchFeed(t, tt, models.ExternalIncidentKindIncident)
	require.NoError(t, err)
	require.Len(t, feed.Items, 3, "ohne ID, doppelte und zu lange IDs entfallen; das erste Vorkommen gewinnt")

	first := feed.Items[0]
	assert.Equal(t, "erster", first.Title)
	assert.Equal(t, "none", first.Impact, "unbekannte Auswirkung")
	assert.Nil(t, first.StartedAt, "unlesbarer Zeitstempel ist kein Abbruch")
	assert.Len(t, first.Components, 1, "doppelte und leere Komponenten-IDs entfallen")
	require.Len(t, first.Updates, 1, "doppelte Update-IDs zählen einmal")
	assert.Equal(t, "ab", first.Updates[0].Body, "NUL entfernt (Postgres lehnt es ab)")

	blank := feed.Items[1]
	assert.Equal(t, "leer", blank.Title, "ohne Titel dient die ID")
	require.Len(t, blank.Updates, 1)
	assert.Equal(t, *blank.StartedAt, blank.Updates[0].DisplayAt, "Update ohne Zeit nimmt den Beginn der Meldung")

	odd := feed.Items[2]
	assert.LessOrEqual(t, len(odd.Title), maxTitleBytes)
	assert.True(t, strings.HasSuffix(odd.Title, "ä"), "UTF-8-Zeichen nicht zerteilt")
	assert.Equal(t, "etwas-neues", odd.Status, "unbekannter Status bleibt roh")
	assert.False(t, odd.Closed, "und gilt als offen")
	require.NotNil(t, odd.StartedAt)
	assert.Equal(t, time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC), *odd.StartedAt, "-05:00 in UTC umgerechnet")
}

func TestSummaryV2Provider_FeedBegrenztAnzahl(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"incidents":[`)
	for i := 0; i < maxIncidentsPerFeed+20; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"id":"i` + string(rune('a'+i%26)) + strings.Repeat("x", i/26) + `","name":"n","status":"resolved"}`)
	}
	sb.WriteString(`]}`)
	tt := newTestTarget(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(sb.String())) }))

	feed, err := fetchFeed(t, tt, models.ExternalIncidentKindIncident)
	require.NoError(t, err)
	assert.Len(t, feed.Items, maxIncidentsPerFeed)
}

func TestNormalizeIncidentStatus(t *testing.T) {
	cases := []struct {
		kind, raw, want string
		closed          bool
	}{
		{"incident", "investigating", "investigating", false},
		{"incident", "identified", "identified", false},
		{"incident", "monitoring", "monitoring", false},
		{"incident", "resolved", "resolved", true},
		{"incident", "postmortem", "resolved", true},
		{"incident", " Resolved ", "resolved", true},
		{"incident", "completed", "completed", false}, // Wartungsstatus bei Vorfall: unbekannt, offen
		{"maintenance", "scheduled", "scheduled", false},
		{"maintenance", "in_progress", "in_progress", false},
		{"maintenance", "verifying", "in_progress", false},
		{"maintenance", "completed", "completed", true},
		{"maintenance", "resolved", "resolved", false},
		{"incident", "", "", false},
	}
	for _, tc := range cases {
		got, closed := NormalizeIncidentStatus(tc.kind, tc.raw)
		assert.Equal(t, tc.want, got, tc.kind+"/"+tc.raw)
		assert.Equal(t, tc.closed, closed, tc.kind+"/"+tc.raw)
	}
}

func TestPlainText(t *testing.T) {
	cases := map[string]string{
		"<br>\n<b>Deutsch</b>\nText":               "Deutsch\nText",
		"Zeile1<br/>Zeile2<BR>Zeile3":              "Zeile1\nZeile2\nZeile3",
		"<p>Absatz</p><p>Zwei</p>":                 "Absatz\nZwei",
		"a &amp; b &lt;script&gt;":                 "a & b <script>",
		"<script>alert(1)</script>Rest":            "alert(1)Rest",
		"x\r\ny":                                   "x\ny",
		"viele\n\n\n\n\nLeerzeilen":                "viele\n\nLeerzeilen",
		"<a href=\"javascript:x\">Link</a> danach": "Link danach",
		"": "",
	}
	for in, want := range cases {
		assert.Equal(t, want, plainText(in), in)
	}
}

func TestIncidentURL(t *testing.T) {
	assert.Equal(t, "https://status.example.com/incidents/abc123", incidentURL("https://status.example.com", "abc123"))
	assert.Equal(t, "https://status.example.com/incidents/abc123", incidentURL("https://status.example.com/", "abc123"))
	assert.Equal(t, "https://status.example.com/incidents/a%2Fb%3Fc", incidentURL("https://status.example.com", "a/b?c"), "ID als Pfadsegment escaped")
	assert.Equal(t, "", incidentURL("http://status.example.com", "x"), "nur https")
}

func TestParseTimePtr(t *testing.T) {
	assert.Nil(t, parseTimePtr(""))
	assert.Nil(t, parseTimePtr("gestern"))
	got := parseTimePtr("2026-10-29T11:00:00.123+01:00")
	require.NotNil(t, got)
	assert.Equal(t, time.Date(2026, 10, 29, 10, 0, 0, 123000000, time.UTC), *got)
	assert.Equal(t, time.UTC, got.Location())
	assert.Nil(t, parseOptTime(nil))
	assert.Nil(t, firstTime(nil, ptr("")), "nichts lesbar")
	assert.NotNil(t, firstTime(ptr("kaputt"), ptr("2026-10-29T10:00:00Z")), "erster lesbarer gewinnt")
}

func ptr(s string) *string { return &s }
