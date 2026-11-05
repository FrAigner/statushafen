package models

import (
	"time"

	"gorm.io/gorm"
)

// Teil B2: Vorfälle und Wartungen externer Statusseiten
// werden gespiegelt, damit Abonnenten der eigenen Seite benachrichtigt und
// die Meldungen (Teil C/D) mit Quelle und Link angezeigt werden können.
//
// Drei Tabellen nach dem Muster Incident/IncidentUpdate/IncidentComponent,
// bewusst getrennt von den eigenen Vorfällen: Gespiegelte Meldungen gehören
// der Fremdseite, werden nur gelesen und bei jedem Abruf überschrieben. Alle
// Zeilen tragen die Statusseite (Mandantentrennung direkt prüfbar) bzw.
// hängen über ihren Elternsatz an genau einer Quelle. Eindeutigkeit unter den
// nicht gelöschten Zeilen per partiellem Index
// (database.createJoinTableUniqueIndexes).

// Arten gespiegelter Meldungen.
const (
	ExternalIncidentKindIncident    = "incident"
	ExternalIncidentKindMaintenance = "maintenance"
)

// ExternalIncident: ein Vorfall oder eine Wartung der Fremdseite.
// (external_source_id, external_id) ist eindeutig.
type ExternalIncident struct {
	gorm.Model
	StatusPageID     uint            `gorm:"not null;index" json:"-"`
	ExternalSourceID uint            `gorm:"not null;index" json:"-"`
	ExternalSource   *ExternalSource `gorm:"foreignKey:ExternalSourceID;constraint:OnDelete:CASCADE" json:"-"`
	ExternalID       string          `gorm:"not null" json:"-"`
	// Kind: "incident" | "maintenance".
	Kind  string `gorm:"not null;default:'incident'" json:"-"`
	Title string `gorm:"not null" json:"-"`
	// Status normalisiert (incident: investigating|identified|monitoring|resolved,
	// maintenance: scheduled|in_progress|completed); unbekannte Werte bleiben
	// roh stehen und gelten als offen. RawStatus wie geliefert.
	Status    string `gorm:"not null;default:''" json:"-"`
	RawStatus string `gorm:"not null;default:''" json:"-"`
	// Impact: none|minor|major|critical|maintenance.
	Impact string `gorm:"not null;default:'none'" json:"-"`
	// Closed: behoben bzw. abgeschlossen. Abgeleitet aus Status, damit
	// Abfragen nicht jede Statusvariante kennen müssen.
	Closed bool `gorm:"not null;default:false;index" json:"-"`
	// URL: Original-Meldung auf der Fremdseite (https, sonst leer).
	URL               string     `gorm:"not null;default:''" json:"-"`
	StartedAt         *time.Time `json:"-"`
	ExternalUpdatedAt *time.Time `json:"-"`
	ResolvedAt        *time.Time `json:"-"`
	// ScheduledFor/ScheduledUntil: nur Wartungen.
	ScheduledFor   *time.Time `json:"-"`
	ScheduledUntil *time.Time `json:"-"`
	LastSeenAt     time.Time  `gorm:"not null" json:"-"`
	// NotifyKey: Fingerabdruck des zuletzt verarbeiteten Stands (Status +
	// jüngstes Update). Die Benachrichtigung wird per bedingtem UPDATE auf
	// diesen Wert beansprucht (externalstatus/incidents.go), damit weder ein
	// zweiter Abruf noch eine zweite Instanz dieselbe Mail doppelt verschickt.
	NotifyKey string `gorm:"not null;default:''" json:"-"`

	Components []ExternalIncidentComponent `gorm:"foreignKey:ExternalIncidentID" json:"-"`
	Updates    []ExternalIncidentUpdate    `gorm:"foreignKey:ExternalIncidentID" json:"-"`
}

// ExternalIncidentUpdate: ein Update einer gespiegelten Meldung.
// (external_incident_id, external_id) ist eindeutig.
type ExternalIncidentUpdate struct {
	gorm.Model
	ExternalIncidentID uint              `gorm:"not null;index" json:"-"`
	ExternalIncident   *ExternalIncident `gorm:"foreignKey:ExternalIncidentID;constraint:OnDelete:CASCADE" json:"-"`
	ExternalID         string            `gorm:"not null" json:"-"`
	Status             string            `gorm:"not null;default:''" json:"-"`
	Body               string            `gorm:"not null;default:''" json:"-"`
	DisplayAt          time.Time         `gorm:"not null" json:"-"`
}

// ExternalIncidentComponent: betroffene externe Komponente einer Meldung.
// Bewusst über die externe ID statt über ExternalComponent.ID: Die Meldung
// kann eine Komponente nennen, die der Komponenten-Abruf noch nicht (oder
// nicht mehr) kennt; der Bezug zu unseren Komponenten entsteht erst bei der
// Auswertung über ExternalComponentLink.
type ExternalIncidentComponent struct {
	gorm.Model
	ExternalIncidentID  uint              `gorm:"not null;index" json:"-"`
	ExternalIncident    *ExternalIncident `gorm:"foreignKey:ExternalIncidentID;constraint:OnDelete:CASCADE" json:"-"`
	ComponentExternalID string            `gorm:"not null" json:"-"`
	Name                string            `gorm:"not null;default:''" json:"-"`
}
