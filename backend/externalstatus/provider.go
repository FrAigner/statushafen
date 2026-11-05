// Package externalstatus fragt externe Statusseiten ab
// und spiegelt ihre Komponenten in models.ExternalComponent.
//
// Aufbau:
//   - Provider: Adapter je Anbieter-API hinter einem Interface, damit weitere
//     Anbieter später als eigene Adapter folgen.
//     Erste Implementierung: Summary-API v2 (summaryv2.go).
//   - safeclient.go: HTTP-Client mit SSRF-Schutz, den jeder Provider nutzt.
//   - service.go: Abfrage-Dienst (Takt, Jitter, Backoff, Advisory Lock),
//     store.go: Upsert und effektiver Status.
package externalstatus

import (
	"context"
	"sort"
)

// ProviderSummaryV2 ist der Provider-Schlüssel für Statusseiten mit der
// Summary-API v2 (GET /api/v2/summary.json, von vielen öffentlichen Statusseiten angeboten).
const ProviderSummaryV2 = "summaryv2"

// FetchRequest beschreibt einen Abruf: Basis-URL der Quelle und die
// Validatoren des letzten erfolgreichen Abrufs für Conditional Requests.
type FetchRequest struct {
	BaseURL      string
	ETag         string
	LastModified string
}

// Component ist eine externe Komponente, wie sie ein Provider liefert.
// Status ist bereits auf unsere Werte normalisiert (componentstatus), RawStatus
// der Originalwert.
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

// Summary ist das Ergebnis eines Abrufs. NotModified = 304, dann sind nur
// ETag/LastModified gesetzt und Components leer.
type Summary struct {
	NotModified  bool
	ETag         string
	LastModified string
	// PageURL: öffentliche Adresse der Fremdseite (Link-Ziel), leer = unbekannt.
	PageURL    string
	Components []Component
}

// Provider ruft die Komponentenübersicht einer externen Statusseite ab.
type Provider interface {
	FetchSummary(ctx context.Context, req FetchRequest) (*Summary, error)
}

// SupportedProviders liefert die Provider-Schlüssel, die der Dienst kennt
// (für die Validierung im Admin-Handler), sortiert.
func (s *Service) SupportedProviders() []string {
	keys := make([]string, 0, len(s.providers))
	for k := range s.providers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SupportsProvider meldet, ob es für key einen Adapter gibt.
func (s *Service) SupportsProvider(key string) bool {
	_, ok := s.providers[key]
	return ok
}
