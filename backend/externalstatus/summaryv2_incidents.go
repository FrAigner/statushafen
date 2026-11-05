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

// Teil B2: Vorfälle und Wartungen der Summary-API v2 (GET {base}/api/v2/incidents.json und
// scheduled-maintenances.json; beide liefern die jüngsten 50 Einträge samt
// Updates).

// Grenzen gegen fehlerhafte oder bösartige Antworten (zusätzlich zu
// MaxResponseBytes): Was darüber hinausgeht, wird verworfen, nicht gespeichert.
const (
	maxIncidentsPerFeed   = 200
	maxUpdatesPerIncident = 100
	maxComponentsPerItem  = 100
	maxTitleBytes         = 300
	maxUpdateBodyBytes    = 20000
	maxIDBytes            = 100
)

// Normalisierte Status gespiegelter Meldungen.
const (
	incidentResolved   = "resolved"
	maintenanceDone    = "completed"
	maintenanceRunning = "in_progress"
)

// IncidentRequest beschreibt den Abruf eines Feeds (Vorfälle oder Wartungen).
type IncidentRequest struct {
	BaseURL string
	// Kind: models.ExternalIncidentKindIncident oder ...Maintenance.
	Kind         string
	ETag         string
	LastModified string
}

// Incident ist ein Vorfall oder eine Wartung, wie ein Provider sie liefert;
// Status ist bereits normalisiert, RawStatus der Originalwert.
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

// IncidentComponent: betroffene externe Komponente (externe ID und Name).
type IncidentComponent struct {
	ExternalID string
	Name       string
}

// IncidentUpdate ist ein Update einer Meldung.
type IncidentUpdate struct {
	ExternalID string
	Status     string
	Body       string
	DisplayAt  time.Time
}

// IncidentFeed ist das Ergebnis eines Feed-Abrufs. NotModified = 304.
type IncidentFeed struct {
	NotModified  bool
	ETag         string
	LastModified string
	Items        []Incident
}

// IncidentProvider ist die optionale Erweiterung eines Providers um
// Vorfälle und Wartungen. Ein Provider ohne sie liefert nur Komponenten.
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

// NormalizeIncidentStatus bildet einen Status der Summary-API ab. closed = behoben
// bzw. abgeschlossen. Unbekannte Werte bleiben unverändert (klein
// geschrieben), gelten als offen und werden geloggt - nie ein Abbruch.
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

// FetchIncidents implementiert IncidentProvider.
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

// convertIncident wandelt einen Eintrag um; ok=false bei fehlender oder
// überlanger ID (nicht verwertbar).
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

// incidentURL: Original-Meldung auf der Fremdseite, {base}/incidents/{id}
// (gilt auch für Wartungen). Die Basis ist die vom Admin
// geprüfte https-URL; die ID wird als Pfadsegment escaped.
func incidentURL(base, id string) string {
	return safeLinkURL(strings.TrimRight(base, "/") + "/incidents/" + url.PathEscape(id))
}

// cleanText kürzt auf n Bytes, entfernt NUL (Postgres lehnt sie ab) und
// ungültiges UTF-8 und trimmt Leerraum an den Rändern.
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

// plainText macht aus dem Update-Text der Fremdseite Klartext: Die API
// liefert HTML-Schnipsel ("<br>", "<b>Deutsch</b>"). Zeilenumbruch-Tags werden
// zu Zeilenumbrüchen, alle übrigen Tags entfernt, Entities aufgelöst. Gespeichert
// und ausgeliefert wird nur Klartext - damit kann kein Markup der Fremdseite
// auf unserer Seite oder in unseren Mails landen (dort wird zusätzlich escaped).
func plainText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = htmlBreak.ReplaceAllString(s, "\n")
	s = htmlTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = trailingSpace.ReplaceAllString(s, "\n")
	s = blankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// parseTimePtr liest einen RFC-3339-Zeitstempel (mit beliebigem Zonen-Offset,
// auch mit Sekundenbruchteilen) und liefert UTC; unlesbar = nil.
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

// firstTime liefert den ersten lesbaren Zeitstempel.
func firstTime(candidates ...*string) *time.Time {
	for _, c := range candidates {
		if t := parseOptTime(c); t != nil {
			return t
		}
	}
	return nil
}

var _ IncidentProvider = SummaryV2Provider{}
