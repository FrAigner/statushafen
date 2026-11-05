package models

import "gorm.io/gorm"

// UiTranslationOverride überschreibt einen einzelnen
// App-UI-Text (Nav, Buttons, Formularlabels - der statisch aus
// frontend/src/i18n/locales/{de,en}.json kompilierte Text), ohne einen
// Code-Deploy zu benötigen.
//
// StatusPageID nil = globaler Override (Global Admin, wirkt app-weit als
// neuer Standard für alle Besucher). StatusPageID gesetzt = Override, der
// NUR im Admin-Bereich dieser einen Statusseite gilt (Page Admin mit
// pages:write-Berechtigung auf genau dieser Seite) und den globalen
// Override/Bundle-Default dort überschreibt - siehe Merge-Reihenfolge
// Bundle < global < Seite in frontend/src/i18n/overrides.js.
//
// Key ist der "dotted" i18n-Pfad wie er in den JSON-Sprachdateien verwendet
// wird (z.B. "nav.items.sso") - das Backend kennt die Gesamtmenge gültiger
// Keys bewusst nicht (siehe Plan), die Keyliste kommt clientseitig aus dem
// bereits im Frontend-Bundle vorhandenen de.json.
type UiTranslationOverride struct {
	gorm.Model
	StatusPageID *uint       `gorm:"default:null;index"`
	StatusPage   *StatusPage `gorm:"foreignKey:StatusPageID;constraint:OnDelete:CASCADE"`
	LanguageCode string      `gorm:"not null;index"`
	Key          string      `gorm:"not null;index"`
	Value        string      `gorm:"not null"`
}
