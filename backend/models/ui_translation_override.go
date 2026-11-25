package models

import "gorm.io/gorm"

// UiTranslationOverride overrides a single
// app UI text (nav, buttons, form labels - the text compiled statically from
// frontend/src/i18n/locales/{de,en}.json) without needing
// a code deploy.
//
// StatusPageID nil = global override (global admin, applies app-wide as the
// new default for all visitors). StatusPageID set = override that applies
// ONLY in the admin area of this one status page (page admin with
// pages:write permission on exactly this page) and overrides the global
// override/bundle default there - see the merge order
// bundle < global < page in frontend/src/i18n/overrides.js.
//
// Key is the "dotted" i18n path as used in the JSON language files
// (e.g. "nav.items.sso") - the backend deliberately does not know the full set of valid
// keys (see plan), the key list comes client-side from the
// de.json already present in the frontend bundle.
type UiTranslationOverride struct {
	gorm.Model
	StatusPageID *uint       `gorm:"default:null;index"`
	StatusPage   *StatusPage `gorm:"foreignKey:StatusPageID;constraint:OnDelete:CASCADE"`
	LanguageCode string      `gorm:"not null;index"`
	Key          string      `gorm:"not null;index"`
	Value        string      `gorm:"not null"`
}
