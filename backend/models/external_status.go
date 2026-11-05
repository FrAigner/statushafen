package models

import (
	"time"

	"gorm.io/gorm"
)

// Teil A: externe Statusseiten (z. B. öffentliche
// Statusseiten von Diensten) regelmäßig abfragen und ihre Komponenten an eigene Komponenten
// koppeln. Drei bewusst getrennte Modelle statt Feldern an Component:
//
//   - ExternalSource: die abgefragte Seite, gehört genau einer Statusseite
//     (Mandant). Zwei Mandanten, die dieselbe Fremdseite einbinden, haben je
//     eine eigene Quelle - keine geteilten Daten über Mandantengrenzen.
//   - ExternalComponent: Spiegel einer Komponente der Fremdseite. Wird beim
//     Abruf nie gelöscht, sondern nur als verschwunden markiert, damit eine
//     bestehende Kopplung nicht stillschweigend verloren geht, wenn die
//     Fremdseite eine Komponente kurz ausblendet oder umbenennt.
//   - ExternalComponentLink: explizite n:m-Join-Tabelle eigene ↔ externe
//     Komponente (Muster IncidentComponent), Unique-Index per Raw-SQL
//     (database.createJoinTableUniqueIndexes), soft-delete-fähig.

// ExternalSource: eine eingebundene Fremd-Statusseite.
type ExternalSource struct {
	gorm.Model
	StatusPageID uint        `gorm:"not null;index" json:"status_page_id"`
	StatusPage   *StatusPage `gorm:"foreignKey:StatusPageID;constraint:OnDelete:CASCADE" json:"-"`
	Name         string      `gorm:"not null" json:"name"`
	// BaseURL ohne abschließenden Schrägstrich, z. B. https://www.githubstatus.com.
	BaseURL string `gorm:"not null" json:"base_url"`
	// Provider wählt den Adapter (externalstatus.Provider), bisher nur "summaryv2".
	Provider string `gorm:"not null;default:'summaryv2'" json:"provider"`
	// PollIntervalSeconds: Default 120, Minimum 60 (Schutz der Fremdseite und
	// unseres Servers), erzwungen in handlers/external_source.go.
	PollIntervalSeconds int `gorm:"not null;default:120" json:"poll_interval_seconds"`
	// Active: bewusst ohne gorm-"default:true" (GORM würde ein explizites
	// false beim Insert durch den DB-Default ersetzen, siehe GeneralNotice.IsActive).
	Active bool `gorm:"not null" json:"active"`

	LastFetchAt *time.Time `json:"last_fetch_at"`
	// LastFetchOK: Ergebnis des letzten Abrufs; nil = noch nie abgerufen.
	LastFetchOK *bool  `json:"last_fetch_ok"`
	LastError   string `gorm:"not null;default:''" json:"last_error"`
	// LastSuccessAt: letzter erfolgreicher Abruf (auch 304), damit Teil C/D
	// veraltete Daten kennzeichnen können.
	LastSuccessAt *time.Time `json:"last_success_at"`
	// ETag/LastModified des letzten 200-Abrufs für Conditional Requests.
	ETag         string `gorm:"not null;default:''" json:"-"`
	LastModified string `gorm:"not null;default:''" json:"-"`
	// PageURL: von der Fremdseite gemeldete öffentliche Adresse (Summary-API:
	// page.url), Ziel des Badge-Links in Teil D. Leer = BaseURL.
	PageURL string `gorm:"not null;default:''" json:"page_url"`

	// Ausfall-Erkennung. Ausfall = seit
	// OutageAfterMinutes kein erfolgreicher Abruf (LastSuccessAt, ein 304
	// zählt als Erfolg). Der Zustand liegt in der DB statt im Speicher, damit
	// ein Neustart oder eine zweite Instanz weder Mails doppelt verschickt
	// noch den Ausfall "vergisst". OutageSince = Beginn der Funkstille (letzter
	// Erfolg); solange gesetzt, zählt der letzte bekannte Status nicht mehr.
	// OutageNotifiedAt/OutageRemindedAt = Zeitpunkt der Ausfall-Mail bzw. der
	// 24-h-Erinnerung (gesetzt VOR dem Versand, siehe externalstatus/outage.go).
	OutageAfterMinutes int        `gorm:"not null;default:60" json:"outage_after_minutes"`
	OutageSince        *time.Time `json:"-"`
	OutageNotifiedAt   *time.Time `json:"-"`
	OutageRemindedAt   *time.Time `json:"-"`

	// Teil B2: Spiegelung von Vorfällen und Wartungen.
	// IncidentsSyncedAt/MaintenancesSyncedAt: Zeitpunkt des ersten
	// erfolgreichen Abrufs des jeweiligen Feeds; solange nil, ist der Import
	// still (keine Benachrichtigung). IncidentsError: letzter Fehler der
	// Feeds (leer = ok); ein Feed-Fehler macht den Abruf der Komponenten
	// nicht zum Fehler. Die Validatoren dienen den Conditional Requests.
	IncidentsSyncedAt    *time.Time `json:"incidents_synced_at"`
	MaintenancesSyncedAt *time.Time `json:"maintenances_synced_at"`
	IncidentsError       string     `gorm:"not null;default:''" json:"incidents_error"`
	IncidentsETag        string     `gorm:"not null;default:''" json:"-"`
	IncidentsLastMod     string     `gorm:"not null;default:''" json:"-"`
	MaintenancesETag     string     `gorm:"not null;default:''" json:"-"`
	MaintenancesLastMod  string     `gorm:"not null;default:''" json:"-"`
}

// ExternalComponent: Spiegel einer Komponente (oder Komponentengruppe) einer
// ExternalSource. (source_id, external_id) ist eindeutig unter den nicht
// gelöschten Zeilen (partieller Index, database.createJoinTableUniqueIndexes).
type ExternalComponent struct {
	gorm.Model
	ExternalSourceID uint            `gorm:"not null;index" json:"external_source_id"`
	ExternalSource   *ExternalSource `gorm:"foreignKey:ExternalSourceID;constraint:OnDelete:CASCADE" json:"-"`
	ExternalID       string          `gorm:"not null" json:"external_id"`
	Name             string          `gorm:"not null" json:"name"`
	// IsGroup: Summary-API "group": true - eine Gruppe hat einen eigenen Status
	// (schlimmstes Kind) und lässt sich ebenfalls koppeln.
	IsGroup bool `gorm:"not null;default:false" json:"is_group"`
	// GroupExternalID/GroupName: Gruppe, zu der die Komponente gehört (leer = keine).
	GroupExternalID string `gorm:"not null;default:''" json:"group_external_id"`
	GroupName       string `gorm:"not null;default:''" json:"group_name"`
	Position        int    `gorm:"not null;default:0" json:"position"`
	// RawStatus wie von der Fremdseite geliefert; Status normalisiert auf
	// unsere Werte (componentstatus) oder "unknown".
	RawStatus  string    `gorm:"not null;default:''" json:"raw_status"`
	Status     string    `gorm:"not null;default:'unknown'" json:"status"`
	URL        string    `gorm:"not null;default:''" json:"url"`
	LastSeenAt time.Time `gorm:"not null" json:"last_seen_at"`
	// Vanished: im letzten erfolgreichen Abruf nicht mehr enthalten. Zählt
	// nicht mehr in den effektiven Status, die Kopplung bleibt bestehen.
	Vanished   bool       `gorm:"not null;default:false" json:"vanished"`
	VanishedAt *time.Time `json:"vanished_at"`
}

// ExternalComponentLink: Kopplung eigene Komponente ↔ externe Komponente.
// StatusPageID ist denormalisiert, damit die Status-Weitergabe pro Seite mit
// einer Abfrage auskommt und die Mandantentrennung direkt prüfbar ist.
type ExternalComponentLink struct {
	gorm.Model
	StatusPageID        uint               `gorm:"not null;index" json:"status_page_id"`
	ComponentID         uint               `gorm:"not null;index" json:"component_id"`
	Component           *Component         `gorm:"foreignKey:ComponentID;constraint:OnDelete:CASCADE" json:"-"`
	ExternalComponentID uint               `gorm:"not null;index" json:"external_component_id"`
	ExternalComponent   *ExternalComponent `gorm:"foreignKey:ExternalComponentID;constraint:OnDelete:CASCADE" json:"-"`
}

// ExternalStatusRef: Herkunft eines effektiven Komponentenstatus,
// wird an Component.ExternalSources ausgeliefert (nicht persistiert).
type ExternalStatusRef struct {
	ExternalComponentID uint   `json:"external_component_id"`
	SourceID            uint   `json:"source_id"`
	SourceName          string `json:"source_name"`
	Name                string `json:"name"`
	GroupName           string `json:"group_name"`
	Status              string `json:"status"`
	URL                 string `json:"url"`
	Vanished            bool   `json:"vanished"`
	// IsGroup: Die Kopplung zeigt auf eine ganze externe Gruppe.
	// Status ist dann das Schlimmste aller aktuellen Mitglieder, Name/GroupName
	// der Gruppenname und Worst der Baustein, der diesen Status liefert.
	IsGroup bool           `json:"is_group"`
	Worst   *ExternalWorst `gorm:"-" json:"worst,omitempty"`
	// SourceOutage: Die Quelle ist ausgefallen; Status ist der
	// zuletzt bekannte, zählt aber nicht mehr in den effektiven Status.
	SourceOutage bool `json:"source_outage"`
}

// ExternalWorst: der Baustein einer gekoppelten externen Gruppe, der deren
// schlechtesten Status liefert (für den Tooltip in Teil D).
type ExternalWorst struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
