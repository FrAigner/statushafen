package externalstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/fraigner/statushafen/componentstatus"
)

// SummaryV2Provider: Summary API v2 (many public status pages offer it),
// endpoint GET {base}/api/v2/summary.json.
type SummaryV2Provider struct {
	Client *http.Client
}

// summaryV2Summary is the part of summary.json that is relevant to us.
type summaryV2Summary struct {
	Page struct {
		URL string `json:"url"`
	} `json:"page"`
	Components []struct {
		ID       string  `json:"id"`
		Name     string  `json:"name"`
		Status   string  `json:"status"`
		Position int     `json:"position"`
		Group    bool    `json:"group"`
		GroupID  *string `json:"group_id"`
	} `json:"components"`
}

// summaryV2StatusMap: component status of the summary API → our status. The
// values match our Component.Status values 1:1; the table
// still makes the mapping explicit so a future difference does not slip
// through unnoticed.
var summaryV2StatusMap = map[string]string{
	"operational":          componentstatus.Operational,
	"degraded_performance": componentstatus.DegradedPerformance,
	"partial_outage":       componentstatus.PartialOutage,
	"major_outage":         componentstatus.MajorOutage,
	"under_maintenance":    componentstatus.UnderMaintenance,
}

// NormalizeSummaryV2Status maps a status of the summary API. Unknown
// values yield "unknown" (does not count in the effective status, see
// componentstatus.Severity) and are logged - never an abort.
func NormalizeSummaryV2Status(raw string) string {
	if s, ok := summaryV2StatusMap[raw]; ok {
		return s
	}
	slog.Warn("Externe Statusseite: unbekannter Komponentenstatus", "provider", ProviderSummaryV2, "status", raw)
	return componentstatus.Unknown
}

// FetchSummary implements Provider.
func (p SummaryV2Provider) FetchSummary(ctx context.Context, req FetchRequest) (*Summary, error) {
	res, err := getJSON(ctx, p.Client, req.BaseURL+"/api/v2/summary.json", req)
	if err != nil {
		return nil, err
	}
	if res.NotModified {
		return &Summary{NotModified: true, ETag: res.ETag, LastModified: res.LastModified}, nil
	}
	var raw summaryV2Summary
	if err := json.Unmarshal(res.Body, &raw); err != nil {
		return nil, fmt.Errorf("summary.json nicht lesbar: %w", err)
	}

	groupNames := map[string]string{}
	for _, c := range raw.Components {
		if c.Group {
			groupNames[c.ID] = c.Name
		}
	}
	sum := &Summary{
		ETag:         res.ETag,
		LastModified: res.LastModified,
		PageURL:      safeLinkURL(raw.Page.URL),
		Components:   make([]Component, 0, len(raw.Components)),
	}
	for _, c := range raw.Components {
		if c.ID == "" {
			continue
		}
		comp := Component{
			ExternalID: c.ID,
			Name:       c.Name,
			IsGroup:    c.Group,
			Position:   c.Position,
			RawStatus:  c.Status,
			Status:     NormalizeSummaryV2Status(c.Status),
		}
		if c.GroupID != nil {
			comp.GroupExternalID = *c.GroupID
			comp.GroupName = groupNames[*c.GroupID]
		}
		sum.Components = append(sum.Components, comp)
	}
	return sum, nil
}

// safeLinkURL accepts a URL supplied by the third-party site only if it
// is https - it ends up in part D as a link on our page, a
// "javascript:" link from the third-party site must not arrive there.
func safeLinkURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return u.String()
}
