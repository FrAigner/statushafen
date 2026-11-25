package models

import "gorm.io/gorm"

// Language is the global language catalog (stage 1 of 5 of the
// multilingual feature). Unlike the previous NotificationTemplate
// convention (Language there was hard-coded to two values that
// actually encoded an audience instead of a real language), this
// catalog is openly extensible - each status page activates a subset
// of it via StatusPageLanguage.
type Language struct {
	gorm.Model
	// Code: ISO 639-1 abbreviation, e.g. "en", "de", "fr". Lowercase and
	// unique - the business key by which StatusPageLanguage and
	// (later) localized content reference.
	Code string `gorm:"uniqueIndex;not null"`
	Name string `gorm:"not null"` // display name, e.g. "Deutsch"
	// IsActive allows deactivating a language in the catalog without deleting
	// existing StatusPageLanguage assignments or localized
	// content - new assignments/selections should then no longer
	// offer it, though.
	IsActive bool `gorm:"not null;default:true"`
}

// StatusPageLanguage: languages activated per status page + exactly one
// default language. Join table following the same pattern as
// UserStatusPageRole/ComponentTag - deliberately no GORM many2many tag, so that
// IsDefault can be carried as an additional field.
//
// Deliberately NO gorm "uniqueIndex" tag on (StatusPageID, LanguageCode): a
// regular unique index ignores soft deletes (deleted_at) and would make a
// "replace" (deactivating + re-activating the same language) collide with the
// soft-deleted old record (see the comment at
// createJoinTableUniqueIndexes in database/db.go, same problem as with
// ComponentTag/IncidentTag/UserTag). The partial unique index (only
// deleted_at IS NULL) is created there via raw SQL instead, as is
// the additional "exactly one default per page" index
// (createLanguageUniqueIndexes).
type StatusPageLanguage struct {
	gorm.Model
	StatusPageID uint       `gorm:"not null;index"`
	StatusPage   StatusPage `gorm:"foreignKey:StatusPageID;constraint:OnDelete:CASCADE"`
	LanguageCode string     `gorm:"not null;index"`
	Language     Language   `gorm:"foreignKey:LanguageCode;references:Code"`
	IsDefault    bool       `gorm:"not null;default:false"`
}
