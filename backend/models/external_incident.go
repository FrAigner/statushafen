package models

import (
	"time"

	"gorm.io/gorm"
)

// Part B2: incidents and maintenance of external status pages
// are mirrored, so that subscribers of the own page can be notified and
// the messages (parts C/D) can be shown with source and link.
//
// Three tables following the Incident/IncidentUpdate/IncidentComponent pattern,
// deliberately separate from the own incidents: mirrored messages belong
// to the foreign page, are only read and are overwritten on every fetch. All
// rows carry the status page (tenant separation directly verifiable) or
// hang off exactly one source via their parent record. Uniqueness among the
// non-deleted rows via a partial index
// (database.createJoinTableUniqueIndexes).

// Kinds of mirrored messages.
const (
	ExternalIncidentKindIncident    = "incident"
	ExternalIncidentKindMaintenance = "maintenance"
)

// ExternalIncident: an incident or maintenance of the foreign page.
// (external_source_id, external_id) is unique.
type ExternalIncident struct {
	gorm.Model
	StatusPageID     uint            `gorm:"not null;index" json:"-"`
	ExternalSourceID uint            `gorm:"not null;index" json:"-"`
	ExternalSource   *ExternalSource `gorm:"foreignKey:ExternalSourceID;constraint:OnDelete:CASCADE" json:"-"`
	ExternalID       string          `gorm:"not null" json:"-"`
	// Kind: "incident" | "maintenance".
	Kind  string `gorm:"not null;default:'incident'" json:"-"`
	Title string `gorm:"not null" json:"-"`
	// Status normalized (incident: investigating|identified|monitoring|resolved,
	// maintenance: scheduled|in_progress|completed); unknown values stay
	// raw and count as open. RawStatus as delivered.
	Status    string `gorm:"not null;default:''" json:"-"`
	RawStatus string `gorm:"not null;default:''" json:"-"`
	// Impact: none|minor|major|critical|maintenance.
	Impact string `gorm:"not null;default:'none'" json:"-"`
	// Closed: fixed or completed. Derived from Status, so that
	// queries do not need to know every status variant.
	Closed bool `gorm:"not null;default:false;index" json:"-"`
	// URL: original message on the foreign page (https, otherwise empty).
	URL               string     `gorm:"not null;default:''" json:"-"`
	StartedAt         *time.Time `json:"-"`
	ExternalUpdatedAt *time.Time `json:"-"`
	ResolvedAt        *time.Time `json:"-"`
	// ScheduledFor/ScheduledUntil: maintenance only.
	ScheduledFor   *time.Time `json:"-"`
	ScheduledUntil *time.Time `json:"-"`
	LastSeenAt     time.Time  `gorm:"not null" json:"-"`
	// NotifyKey: fingerprint of the most recently processed state (status +
	// latest update). The notification is claimed via a conditional UPDATE on
	// this value (externalstatus/incidents.go), so that neither a
	// second fetch nor a second instance sends the same mail twice.
	NotifyKey string `gorm:"not null;default:''" json:"-"`

	Components []ExternalIncidentComponent `gorm:"foreignKey:ExternalIncidentID" json:"-"`
	Updates    []ExternalIncidentUpdate    `gorm:"foreignKey:ExternalIncidentID" json:"-"`
}

// ExternalIncidentUpdate: an update of a mirrored message.
// (external_incident_id, external_id) is unique.
type ExternalIncidentUpdate struct {
	gorm.Model
	ExternalIncidentID uint              `gorm:"not null;index" json:"-"`
	ExternalIncident   *ExternalIncident `gorm:"foreignKey:ExternalIncidentID;constraint:OnDelete:CASCADE" json:"-"`
	ExternalID         string            `gorm:"not null" json:"-"`
	Status             string            `gorm:"not null;default:''" json:"-"`
	Body               string            `gorm:"not null;default:''" json:"-"`
	DisplayAt          time.Time         `gorm:"not null" json:"-"`
}

// ExternalIncidentComponent: affected external component of a message.
// Deliberately via the external ID instead of ExternalComponent.ID: the message
// may name a component that the component fetch does not (yet or
// no longer) know; the relation to our components arises only in the
// evaluation via ExternalComponentLink.
type ExternalIncidentComponent struct {
	gorm.Model
	ExternalIncidentID  uint              `gorm:"not null;index" json:"-"`
	ExternalIncident    *ExternalIncident `gorm:"foreignKey:ExternalIncidentID;constraint:OnDelete:CASCADE" json:"-"`
	ComponentExternalID string            `gorm:"not null" json:"-"`
	Name                string            `gorm:"not null;default:''" json:"-"`
}
