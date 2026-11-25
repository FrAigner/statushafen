package models

import (
	"time"

	"gorm.io/gorm"
)

// Extension of the incident model by a history (timeline) and a post-mortem.
// These models are deliberately kept separate, so that models.go stays unchanged.
//
// Migration: add to the AutoMigrate list in database.InitDB():
//   db.AutoMigrate(&models.IncidentUpdate{}, &models.PostMortem{}, &models.ActionItem{})

// IncidentUpdate is a single entry in the incident timeline.
// Every status change or communication to subscribers produces an update.
type IncidentUpdate struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	IncidentID  uint      `gorm:"not null;index" json:"incident_id"`
	Status      string    `gorm:"not null" json:"status"` // investigating | identified | monitoring | resolved
	Message     string    `gorm:"not null" json:"message"`
	AuthorEmail string    `json:"author_email"`
}

// IncidentUpdateTranslation is the translation of the message of an
// update into a further language of the page. An update has no title, hence
// only Message. Status as with the other translations: draft (admin only) or
// confirmed (public). At most one active row per (update, language)
// (partial unique index idx_incident_update_translation_active in
// database.createJoinTableUniqueIndexes); with the update, the translations
// also disappear via FK (OnDelete:CASCADE).
type IncidentUpdateTranslation struct {
	gorm.Model
	UpdateID     uint           `gorm:"not null;index" json:"UpdateID"`
	Update       IncidentUpdate `gorm:"foreignKey:UpdateID;constraint:OnDelete:CASCADE" json:"-"`
	LanguageCode string         `gorm:"not null" json:"LanguageCode"`
	Message      string         `gorm:"not null" json:"Message"`
	Status       string         `gorm:"not null;default:'draft'" json:"Status"` // draft | confirmed
	Source       string         `gorm:"not null;default:'manual'" json:"Source"`
}

// PostMortem is the structured retrospective of a resolved incident.
// 1:1 relationship to the incident (an incident has at most one post-mortem).
type PostMortem struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	IncidentID  uint       `gorm:"not null;uniqueIndex" json:"incident_id"`
	Summary     string     `gorm:"type:text" json:"summary"`
	RootCause   string     `gorm:"type:text" json:"root_cause"`
	Impact      string     `gorm:"type:text" json:"impact"`
	Resolution  string     `gorm:"type:text" json:"resolution"`
	Detection   string     `gorm:"type:text" json:"detection"`
	PublishedAt *time.Time `json:"published_at"` // nil = draft, set = published

	Actions []ActionItem `gorm:"foreignKey:PostMortemID" json:"actions"`
}

// ActionItem is a follow-up action from a post-mortem.
type ActionItem struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	PostMortemID uint   `gorm:"not null;index" json:"post_mortem_id"`
	Text         string `gorm:"not null" json:"text"`
	Owner        string `json:"owner"`
	State        string `gorm:"not null;default:'open'" json:"state"` // open | in_progress | done
}
