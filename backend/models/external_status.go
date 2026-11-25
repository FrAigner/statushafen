package models

import (
	"time"

	"gorm.io/gorm"
)

// Part A: regularly poll external status pages (e.g. public
// status pages of services) and couple their components to own components.
// Three deliberately separate models instead of fields on Component:
//
//   - ExternalSource: the polled page, belongs to exactly one status page
//     (tenant). Two tenants embedding the same foreign page each have
//     their own source - no shared data across tenant boundaries.
//   - ExternalComponent: mirror of a component of the foreign page. Never deleted
//     on fetch, only marked as vanished, so that an
//     existing coupling is not silently lost when the
//     foreign page briefly hides or renames a component.
//   - ExternalComponentLink: explicit n:m join table own ↔ external
//     component (IncidentComponent pattern), unique index via raw SQL
//     (database.createJoinTableUniqueIndexes), soft-delete capable.

// ExternalSource: an embedded foreign status page.
type ExternalSource struct {
	gorm.Model
	StatusPageID uint        `gorm:"not null;index" json:"status_page_id"`
	StatusPage   *StatusPage `gorm:"foreignKey:StatusPageID;constraint:OnDelete:CASCADE" json:"-"`
	Name         string      `gorm:"not null" json:"name"`
	// BaseURL without trailing slash, e.g. https://www.githubstatus.com.
	BaseURL string `gorm:"not null" json:"base_url"`
	// Provider selects the adapter (externalstatus.Provider), so far only "summaryv2".
	Provider string `gorm:"not null;default:'summaryv2'" json:"provider"`
	// PollIntervalSeconds: default 120, minimum 60 (protection of the foreign page and
	// of our server), enforced in handlers/external_source.go.
	PollIntervalSeconds int `gorm:"not null;default:120" json:"poll_interval_seconds"`
	// Active: deliberately without gorm "default:true" (GORM would replace an explicit
	// false on insert with the DB default, see GeneralNotice.IsActive).
	Active bool `gorm:"not null" json:"active"`

	LastFetchAt *time.Time `json:"last_fetch_at"`
	// LastFetchOK: result of the last fetch; nil = never fetched.
	LastFetchOK *bool  `json:"last_fetch_ok"`
	LastError   string `gorm:"not null;default:''" json:"last_error"`
	// LastSuccessAt: last successful fetch (including 304), so that parts C/D
	// can flag stale data.
	LastSuccessAt *time.Time `json:"last_success_at"`
	// ETag/LastModified of the last 200 fetch for conditional requests.
	ETag         string `gorm:"not null;default:''" json:"-"`
	LastModified string `gorm:"not null;default:''" json:"-"`
	// PageURL: public address reported by the foreign page (summary API:
	// page.url), target of the badge link in part D. Empty = BaseURL.
	PageURL string `gorm:"not null;default:''" json:"page_url"`

	// Outage detection. Outage = no successful fetch since
	// OutageAfterMinutes (LastSuccessAt, a 304
	// counts as success). The state lives in the DB instead of memory, so that
	// a restart or a second instance neither sends mails twice
	// nor "forgets" the outage. OutageSince = start of the radio silence (last
	// success); while set, the last known status no longer counts.
	// OutageNotifiedAt/OutageRemindedAt = time of the outage mail and of the
	// 24h reminder (set BEFORE sending, see externalstatus/outage.go).
	OutageAfterMinutes int        `gorm:"not null;default:60" json:"outage_after_minutes"`
	OutageSince        *time.Time `json:"-"`
	OutageNotifiedAt   *time.Time `json:"-"`
	OutageRemindedAt   *time.Time `json:"-"`

	// Part B2: mirroring of incidents and maintenance.
	// IncidentsSyncedAt/MaintenancesSyncedAt: time of the first
	// successful fetch of the respective feed; while nil, the import is
	// silent (no notification). IncidentsError: last error of the
	// feeds (empty = ok); a feed error does not turn the component fetch
	// into an error. The validators serve the conditional requests.
	IncidentsSyncedAt    *time.Time `json:"incidents_synced_at"`
	MaintenancesSyncedAt *time.Time `json:"maintenances_synced_at"`
	IncidentsError       string     `gorm:"not null;default:''" json:"incidents_error"`
	IncidentsETag        string     `gorm:"not null;default:''" json:"-"`
	IncidentsLastMod     string     `gorm:"not null;default:''" json:"-"`
	MaintenancesETag     string     `gorm:"not null;default:''" json:"-"`
	MaintenancesLastMod  string     `gorm:"not null;default:''" json:"-"`
}

// ExternalComponent: mirror of a component (or component group) of an
// ExternalSource. (source_id, external_id) is unique among the non-deleted
// rows (partial index, database.createJoinTableUniqueIndexes).
type ExternalComponent struct {
	gorm.Model
	ExternalSourceID uint            `gorm:"not null;index" json:"external_source_id"`
	ExternalSource   *ExternalSource `gorm:"foreignKey:ExternalSourceID;constraint:OnDelete:CASCADE" json:"-"`
	ExternalID       string          `gorm:"not null" json:"external_id"`
	Name             string          `gorm:"not null" json:"name"`
	// IsGroup: summary API "group": true - a group has its own status
	// (worst child) and can be coupled as well.
	IsGroup bool `gorm:"not null;default:false" json:"is_group"`
	// GroupExternalID/GroupName: group the component belongs to (empty = none).
	GroupExternalID string `gorm:"not null;default:''" json:"group_external_id"`
	GroupName       string `gorm:"not null;default:''" json:"group_name"`
	Position        int    `gorm:"not null;default:0" json:"position"`
	// RawStatus as delivered by the foreign page; Status normalized to
	// our values (componentstatus) or "unknown".
	RawStatus  string    `gorm:"not null;default:''" json:"raw_status"`
	Status     string    `gorm:"not null;default:'unknown'" json:"status"`
	URL        string    `gorm:"not null;default:''" json:"url"`
	LastSeenAt time.Time `gorm:"not null" json:"last_seen_at"`
	// Vanished: no longer contained in the last successful fetch. No longer counts
	// in the effective status, the coupling remains.
	Vanished   bool       `gorm:"not null;default:false" json:"vanished"`
	VanishedAt *time.Time `json:"vanished_at"`
}

// ExternalComponentLink: coupling of own component ↔ external component.
// StatusPageID is denormalized, so that status propagation per page needs
// only one query and tenant separation is directly verifiable.
type ExternalComponentLink struct {
	gorm.Model
	StatusPageID        uint               `gorm:"not null;index" json:"status_page_id"`
	ComponentID         uint               `gorm:"not null;index" json:"component_id"`
	Component           *Component         `gorm:"foreignKey:ComponentID;constraint:OnDelete:CASCADE" json:"-"`
	ExternalComponentID uint               `gorm:"not null;index" json:"external_component_id"`
	ExternalComponent   *ExternalComponent `gorm:"foreignKey:ExternalComponentID;constraint:OnDelete:CASCADE" json:"-"`
}

// ExternalStatusRef: origin of an effective component status,
// delivered on Component.ExternalSources (not persisted).
type ExternalStatusRef struct {
	ExternalComponentID uint   `json:"external_component_id"`
	SourceID            uint   `json:"source_id"`
	SourceName          string `json:"source_name"`
	Name                string `json:"name"`
	GroupName           string `json:"group_name"`
	Status              string `json:"status"`
	URL                 string `json:"url"`
	Vanished            bool   `json:"vanished"`
	// IsGroup: the coupling points to a whole external group.
	// Status is then the worst of all current members, Name/GroupName
	// the group name and Worst the component that yields this status.
	IsGroup bool           `json:"is_group"`
	Worst   *ExternalWorst `gorm:"-" json:"worst,omitempty"`
	// SourceOutage: the source is down; Status is the
	// last known one but no longer counts in the effective status.
	SourceOutage bool `json:"source_outage"`
}

// ExternalWorst: the component of a coupled external group that yields its
// worst status (for the tooltip in part D).
type ExternalWorst struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
