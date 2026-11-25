package externalstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/fraigner/statushafen/models"
)

// Part B2: incidents and maintenances of the Summary API v2 (GET {base}/api/v2/incidents.json and
// scheduled-maintenances.json; both return the latest 50 entries including
// updates).

// Limits against faulty or malicious responses (in addition to
// MaxResponseBytes): anything beyond them is discarded, not stored.
const (
	maxIncidentsPerFeed   = 200
	maxUpdatesPerIncident = 100
	maxComponentsPerItem  = 100
	maxTitleBytes         = 300
	maxUpdateBodyBytes    = 20000
	maxIDBytes            = 100
)

// Normalized statuses of mirrored reports.
const (
	incidentResolved   = "resolved"
	maintenanceDone    = "completed"
	maintenanceRunning = "in_progress"
)

// IncidentRequest describes the fetch of a feed (incidents or maintenances).
type IncidentRequest struct {
	BaseURL string
	// Kind: models.ExternalIncidentKindIncident or ...Maintenance.
	Kind         string
	ETag         string
	LastModified string
}

// Incident is an incident or a maintenance as a provider returns it;
// Status is already normalized, RawStatus is the original value.
type Incident struct {
	ExternalID  string
	Kind        string
	Title       string
	Status      string
	RawStatus   string
	Impact      string
	Closed      bool
	URL         string
	StartedAt   *time.Time
	UpdatedAt   *time.Time
	ResolvedAt  *time.Time
	ScheduledAt *time.Time
	ScheduledTo *time.Time
	Components  []IncidentComponent
	Updates     []IncidentUpdate
}

// IncidentComponent: affected external component (external ID and name).
type IncidentComponent struct {
	ExternalID string
	Name       string
}

// IncidentUpdate is an update of a report.
type IncidentUpdate struct {
	ExternalID string
	Status     string
	Body       string
	DisplayAt  time.Time
}

// IncidentFeed is the result of a feed fetch. NotModified = 304.
type IncidentFeed struct {
	NotModified  bool
	ETag         string
	LastModified string
	Items        []Incident
}

// IncidentProvider is the optional extension of a provider for
// incidents and maintenances. A provider without it only returns components.
type IncidentProvider interface {
	FetchIncidents(ctx context.Context, req IncidentRequest) (*IncidentFeed, error)
}

type summaryV2IncidentsDoc struct {
	Incidents             []summaryV2Incident `json:"incidents"`
	ScheduledMaintenances []summaryV2Incident `json:"scheduled_maintenances"`
}

type summaryV2Incident struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Status         string  `json:"status"`
	Impact         string  `json:"impact"`
	CreatedAt      string  `json:"created_at"`
	StartedAt      *string `json:"started_at"`
	UpdatedAt      string  `json:"updated_at"`
	ResolvedAt     *string `json:"resolved_at"`
	ScheduledFor   *string `json:"scheduled_for"`
	ScheduledUntil *string `json:"scheduled_until"`
	Components     []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"components"`
	IncidentUpdates []struct {
		ID        string  `json:"id"`
		Status    string  `json:"status"`
		Body      string  `json:"body"`
		CreatedAt string  `json:"created_at"`
		DisplayAt *string `json:"display_at"`
	} `json:"incident_updates"`
}

var summaryV2IncidentStatus = map[string]string{
	"investigating": "investigating",
	"identified":    "identified",
	"monitoring":    "monitoring",
	"resolved":      incidentResolved,
	"postmortem":    incidentResolved,
}

var summaryV2MaintenanceStatus = map[string]string{
	"scheduled":   "scheduled",
	"in_progress": maintenanceRunning,
	"verifying":   maintenanceRunning,
	"completed":   maintenanceDone,
}

var summaryV2Impact = map[string]bool{"none": true, "minor": true, "major": true, "critical": true, "maintenance": true}

// NormalizeIncidentStatus maps a status of the summary API. closed = resolved
// or completed. Unknown values stay unchanged (lower-cased),
// count as open and are logged - never an abort.
func NormalizeIncidentStatus(kind, raw string) (status string, closed bool) {
	table := summaryV2IncidentStatus
	done := incidentResolved
	if kind == models.ExternalIncidentKindMaintenance {
		table, done = summaryV2MaintenanceStatus, maintenanceDone
	}
	key := strings.ToLower(strings.TrimSpace(raw))
	if s, ok := table[key]; ok {
		return s, s == done
	}
	slog.Warn("Externe Statusseite: unbekannter Vorfallstatus", "provider", ProviderSummaryV2, "kind", kind, "status", raw)
	return key, false
}

// FetchIncidents implements IncidentProvider.
func (p SummaryV2Provider) FetchIncidents(ctx context.Context, req IncidentRequest) (*IncidentFeed, error) {
	file := "incidents.json"
	if req.Kind == models.ExternalIncidentKindMaintenance {
		file = "scheduled-maintenances.json"
	}
	res, err := getJSON(ctx, p.Client, req.BaseURL+"/api/v2/"+file, FetchRequest{BaseURL: req.BaseURL, ETag: req.ETag, LastModified: req.LastModified})
	if err != nil {
		return nil, err
	}
	if res.NotModified {
		return &IncidentFeed{NotModified: true, ETag: res.ETag, LastModified: res.LastModified}, nil
	}
	var doc summaryV2IncidentsDoc
	if err := json.Unmarshal(res.Body, &doc); err != nil {
		return nil, fmt.Errorf("%s nicht lesbar: %w", file, err)
	}
	raw := doc.Incidents
	if req.Kind == models.ExternalIncidentKindMaintenance {
		raw = doc.ScheduledMaintenances
	}
	feed := &IncidentFeed{ETag: res.ETag, LastModified: res.LastModified, Items: make([]Incident, 0, len(raw))}
	seen := map[string]bool{}
	for _, r := range raw {
		if len(feed.Items) >= maxIncidentsPerFeed {
			slog.Warn("Externe Statusseite: zu viele Einträge, Rest verworfen", "kind", req.Kind, "max", maxIncidentsPerFeed)
			break
		}
		it, ok := convertIncident(req, r)
		if !ok || seen[it.ExternalID] {
			continue
		}
		seen[it.ExternalID] = true
		feed.Items = append(feed.Items, it)
	}
	return feed, nil
}

// convertIncident converts an entry; ok=false for a missing or
// overlong ID (not usable).
func convertIncident(req IncidentRequest, r summaryV2Incident) (Incident, bool) {
	id := cleanText(r.ID, maxIDBytes)
	if id == "" || len(r.ID) > maxIDBytes {
		return Incident{}, false
	}
	status, closed := NormalizeIncidentStatus(req.Kind, r.Status)
	impact := strings.ToLower(strings.TrimSpace(r.Impact))
	if !summaryV2Impact[impact] {
		impact = "none"
	}
	it := Incident{
		ExternalID:  id,
		Kind:        req.Kind,
		Title:       cleanText(r.Name, maxTitleBytes),
		Status:      status,
		RawStatus:   cleanText(r.Status, maxIDBytes),
		Impact:      impact,
		Closed:      closed,
		URL:         incidentURL(req.BaseURL, id),
		StartedAt:   firstTime(r.StartedAt, &r.CreatedAt),
		UpdatedAt:   parseTimePtr(r.UpdatedAt),
		ResolvedAt:  parseOptTime(r.ResolvedAt),
		ScheduledAt: parseOptTime(r.ScheduledFor),
		ScheduledTo: parseOptTime(r.ScheduledUntil),
	}
	if it.Title == "" {
		it.Title = id
	}
	seenComp := map[string]bool{}
	for _, c := range r.Components {
		cid := cleanText(c.ID, maxIDBytes)
		if cid == "" || seenComp[cid] || len(it.Components) >= maxComponentsPerItem {
			continue
		}
		seenComp[cid] = true
		it.Components = append(it.Components, IncidentComponent{ExternalID: cid, Name: cleanText(c.Name, maxTitleBytes)})
	}
	seenUpd := map[string]bool{}
	for _, u := range r.IncidentUpdates {
		uid := cleanText(u.ID, maxIDBytes)
		if uid == "" || seenUpd[uid] || len(it.Updates) >= maxUpdatesPerIncident {
			continue
		}
		seenUpd[uid] = true
		st, _ := NormalizeIncidentStatus(req.Kind, u.Status)
		at := firstTime(u.DisplayAt, &u.CreatedAt)
		var disp time.Time
		if at != nil {
			disp = *at
		} else if it.StartedAt != nil {
			disp = *it.StartedAt
		}
		it.Updates = append(it.Updates, IncidentUpdate{ExternalID: uid, Status: st, Body: cleanText(plainText(u.Body), maxUpdateBodyBytes), DisplayAt: disp})
	}
	return it, true
}

// incidentURL: original report on the third-party site, {base}/incidents/{id}
// (also applies to maintenances). The base is the https URL checked by
// the admin; the ID is escaped as a path segment.
func incidentURL(base, id string) string {
	return safeLinkURL(strings.TrimRight(base, "/") + "/incidents/" + url.PathEscape(id))
}

// cleanText shortens to n bytes, removes NUL (Postgres rejects it) and
// invalid UTF-8, and trims whitespace at the edges.
func cleanText(s string, n int) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.TrimSpace(truncate(strings.ToValidUTF8(s, ""), n))
}

var (
	htmlBreak     = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/h[1-6])\s*/?>`)
	htmlTag       = regexp.MustCompile(`<[^>]*>`)
	blankLines    = regexp.MustCompile(`\n{3,}`)
	trailingSpace = regexp.MustCompile(`[ \t]+\n`)
)

// plainText turns the update text of the third-party site into plain text: the API
// returns HTML snippets ("<br>", "<b>Deutsch</b>"). Line-break tags become
// line breaks, all other tags are removed, entities resolved. Only plain text
// is stored and delivered - so no markup from the third-party site can
// end up on our page or in our mails (which are additionally escaped).
func plainText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = htmlBreak.ReplaceAllString(s, "\n")
	s = htmlTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = trailingSpace.ReplaceAllString(s, "\n")
	s = blankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// parseTimePtr reads an RFC 3339 timestamp (with any zone offset,
// also with fractional seconds) and returns UTC; unreadable = nil.
func parseTimePtr(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		slog.Warn("Externe Statusseite: Zeitstempel nicht lesbar", "value", truncate(s, 40))
		return nil
	}
	t = t.UTC()
	return &t
}

func parseOptTime(s *string) *time.Time {
	if s == nil {
		return nil
	}
	return parseTimePtr(*s)
}

// firstTime returns the first readable timestamp.
func firstTime(candidates ...*string) *time.Time {
	for _, c := range candidates {
		if t := parseOptTime(c); t != nil {
			return t
		}
	}
	return nil
}

var _ IncidentProvider = SummaryV2Provider{}
