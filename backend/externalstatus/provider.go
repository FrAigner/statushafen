// Package externalstatus polls external status pages
// and mirrors their components into models.ExternalComponent.
//
// Structure:
//   - Provider: one adapter per vendor API behind an interface, so further
//     vendors can follow later as separate adapters.
//     First implementation: Summary API v2 (summaryv2.go).
//   - safeclient.go: HTTP client with SSRF protection that every provider uses.
//   - service.go: polling service (tick, jitter, backoff, advisory lock),
//     store.go: upsert and effective status.
package externalstatus

import (
	"context"
	"sort"
)

// ProviderSummaryV2 is the provider key for status pages with the
// Summary API v2 (GET /api/v2/summary.json, offered by many public status pages).
const ProviderSummaryV2 = "summaryv2"

// FetchRequest describes a fetch: base URL of the source and the
// validators of the last successful fetch for conditional requests.
type FetchRequest struct {
	BaseURL      string
	ETag         string
	LastModified string
}

// Component is an external component as a provider returns it.
// Status is already normalized to our values (componentstatus), RawStatus
// is the original value.
type Component struct {
	ExternalID      string
	Name            string
	IsGroup         bool
	GroupExternalID string
	GroupName       string
	Position        int
	RawStatus       string
	Status          string
}

// Summary is the result of a fetch. NotModified = 304, in which case only
// ETag/LastModified are set and Components is empty.
type Summary struct {
	NotModified  bool
	ETag         string
	LastModified string
	// PageURL: public address of the third-party site (link target), empty = unknown.
	PageURL    string
	Components []Component
}

// Provider fetches the component overview of an external status page.
type Provider interface {
	FetchSummary(ctx context.Context, req FetchRequest) (*Summary, error)
}

// SupportedProviders returns the provider keys the service knows
// (for validation in the admin handler), sorted.
func (s *Service) SupportedProviders() []string {
	keys := make([]string, 0, len(s.providers))
	for k := range s.providers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SupportsProvider reports whether there is an adapter for key.
func (s *Service) SupportsProvider(key string) bool {
	_, ok := s.providers[key]
	return ok
}
