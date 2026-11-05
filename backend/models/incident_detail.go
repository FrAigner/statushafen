package models

import (
	"time"

	"gorm.io/gorm"
)

// Erweiterung des Incident-Modells um einen Verlauf (Timeline) und ein Post-Mortem.
// Diese Modelle liegen bewusst getrennt, damit models.go unverändert bleibt.
//
// Migration: In database.InitDB() zur AutoMigrate-Liste hinzufügen:
//   db.AutoMigrate(&models.IncidentUpdate{}, &models.PostMortem{}, &models.ActionItem{})

// IncidentUpdate ist ein einzelner Eintrag in der Incident-Timeline.
// Jeder Statuswechsel oder jede Kommunikation an Abonnenten erzeugt einen Update.
type IncidentUpdate struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	IncidentID  uint      `gorm:"not null;index" json:"incident_id"`
	Status      string    `gorm:"not null" json:"status"` // investigating | identified | monitoring | resolved
	Message     string    `gorm:"not null" json:"message"`
	AuthorEmail string    `json:"author_email"`
}

// IncidentUpdateTranslation ist die Übersetzung der Nachricht eines
// Updates in eine weitere Sprache der Seite. Ein Update hat keinen Titel, daher
// nur Message. Stand wie bei den übrigen Übersetzungen: draft (nur Admin) oder
// confirmed (öffentlich). Je (Update, Sprache) höchstens eine aktive Zeile
// (partieller Unique-Index idx_incident_update_translation_active in
// database.createJoinTableUniqueIndexes); mit dem Update verschwinden per FK
// (OnDelete:CASCADE) auch die Übersetzungen.
type IncidentUpdateTranslation struct {
	gorm.Model
	UpdateID     uint           `gorm:"not null;index" json:"UpdateID"`
	Update       IncidentUpdate `gorm:"foreignKey:UpdateID;constraint:OnDelete:CASCADE" json:"-"`
	LanguageCode string         `gorm:"not null" json:"LanguageCode"`
	Message      string         `gorm:"not null" json:"Message"`
	Status       string         `gorm:"not null;default:'draft'" json:"Status"` // draft | confirmed
	Source       string         `gorm:"not null;default:'manual'" json:"Source"`
}

// PostMortem ist die strukturierte Nachbetrachtung eines behobenen Incidents.
// 1:1-Beziehung zum Incident (ein Incident hat höchstens ein Post-Mortem).
type PostMortem struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	IncidentID  uint       `gorm:"not null;uniqueIndex" json:"incident_id"`
	Summary     string     `gorm:"type:text" json:"summary"`
	RootCause   string     `gorm:"type:text" json:"root_cause"`
	Impact      string     `gorm:"type:text" json:"impact"`
	Resolution  string     `gorm:"type:text" json:"resolution"`
	Detection   string     `gorm:"type:text" json:"detection"`
	PublishedAt *time.Time `json:"published_at"` // nil = Entwurf, gesetzt = veröffentlicht

	Actions []ActionItem `gorm:"foreignKey:PostMortemID" json:"actions"`
}

// ActionItem ist eine Folgemaßnahme aus einem Post-Mortem.
type ActionItem struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	PostMortemID uint   `gorm:"not null;index" json:"post_mortem_id"`
	Text         string `gorm:"not null" json:"text"`
	Owner        string `json:"owner"`
	State        string `gorm:"not null;default:'open'" json:"state"` // open | in_progress | done
}
