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

// SummaryV2Provider: Summary-API v2 (viele öffentliche Statusseiten bieten sie
// an), Endpunkt GET {base}/api/v2/summary.json.
type SummaryV2Provider struct {
	Client *http.Client
}

// summaryV2Summary ist der für uns relevante Teil von summary.json.
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

// summaryV2StatusMap: Komponentenstatus der Summary-API → unser Status. Die
// Werte stimmen 1:1 mit unseren Component.Status-Werten überein; die Tabelle
// macht das Mapping trotzdem explizit, damit ein künftiger Unterschied nicht
// unbemerkt durchrutscht.
var summaryV2StatusMap = map[string]string{
	"operational":          componentstatus.Operational,
	"degraded_performance": componentstatus.DegradedPerformance,
	"partial_outage":       componentstatus.PartialOutage,
	"major_outage":         componentstatus.MajorOutage,
	"under_maintenance":    componentstatus.UnderMaintenance,
}

// NormalizeSummaryV2Status bildet einen Status der Summary-API ab. Unbekannte
// Werte ergeben "unknown" (zählt nicht in den effektiven Status, siehe
// componentstatus.Severity) und werden geloggt - nie ein Abbruch.
func NormalizeSummaryV2Status(raw string) string {
	if s, ok := summaryV2StatusMap[raw]; ok {
		return s
	}
	slog.Warn("Externe Statusseite: unbekannter Komponentenstatus", "provider", ProviderSummaryV2, "status", raw)
	return componentstatus.Unknown
}

// FetchSummary implementiert Provider.
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

// safeLinkURL übernimmt eine von der Fremdseite gelieferte URL nur, wenn sie
// https ist - sie landet in Teil D als Link auf unserer Seite, ein
// "javascript:"-Link der Fremdseite darf dort nicht ankommen.
func safeLinkURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return u.String()
}
