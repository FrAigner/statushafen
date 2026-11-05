package models

import "gorm.io/gorm"

// Language ist der globale Sprachkatalog (Stage 1 von 5 des
// Mehrsprachigkeits-Features). Anders als die bisherige NotificationTemplate-
// Konvention (Language dort war hartcodiert auf zwei Werte, die
// eigentlich eine Zielgruppe statt einer echten Sprache codierten) ist dieser
// Katalog offen erweiterbar - jede Statusseite aktiviert sich über
// StatusPageLanguage eine Teilmenge davon.
type Language struct {
	gorm.Model
	// Code: ISO-639-1-Kürzel, z.B. "en", "de", "fr". Kleingeschrieben und
	// eindeutig - der fachliche Schlüssel, über den StatusPageLanguage und
	// (später) lokalisierter Content referenzieren.
	Code string `gorm:"uniqueIndex;not null"`
	Name string `gorm:"not null"` // Anzeigename, z.B. "Deutsch"
	// IsActive erlaubt es, eine Sprache aus dem Katalog zu deaktivieren, ohne
	// bereits bestehende StatusPageLanguage-Zuordnungen oder lokalisierten
	// Content zu löschen - neue Zuordnungen/Auswahlen sollen sie dann aber
	// nicht mehr anbieten.
	IsActive bool `gorm:"not null;default:true"`
}

// StatusPageLanguage: pro Statusseite aktivierte Sprachen + genau eine
// Default-Sprache. Join-Tabelle nach demselben Muster wie
// UserStatusPageRole/ComponentTag - bewusst kein GORM many2many-Tag, damit
// IsDefault als zusätzliches Feld mitgeführt werden kann.
//
// Bewusst KEIN gorm "uniqueIndex"-Tag auf (StatusPageID, LanguageCode): ein
// regulärer Unique-Index ignoriert Soft-Deletes (deleted_at) und würde ein
// "Replace" (Deaktivieren + erneutes Aktivieren derselben Sprache) mit dem
// soft-gelöschten Altdatensatz kollidieren lassen (siehe Kommentar bei
// createJoinTableUniqueIndexes in database/db.go, dasselbe Problem wie bei
// ComponentTag/IncidentTag/UserTag). Der partielle Unique-Index (nur
// deleted_at IS NULL) wird stattdessen dort per Raw-SQL angelegt, ebenso wie
// der zusätzliche "genau ein Default pro Seite"-Index
// (createLanguageUniqueIndexes).
type StatusPageLanguage struct {
	gorm.Model
	StatusPageID uint       `gorm:"not null;index"`
	StatusPage   StatusPage `gorm:"foreignKey:StatusPageID;constraint:OnDelete:CASCADE"`
	LanguageCode string     `gorm:"not null;index"`
	Language     Language   `gorm:"foreignKey:LanguageCode;references:Code"`
	IsDefault    bool       `gorm:"not null;default:false"`
}
