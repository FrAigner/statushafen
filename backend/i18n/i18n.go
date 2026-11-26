// Package i18n bundles (stage 1 of 5 of the multilingual
// feature) the shared building blocks that all future localized
// content models (incident translations, component,
// maintenance/notice) share: the draft/confirmed lifecycle of a
// translation and a query scope that shows public endpoints only
// approved translations.
package i18n

import "gorm.io/gorm"

// Status constants for a single localized content row:
//   - StatusDraft: proposed by the translation provider (see backend/translation)
//     or created manually, but not yet confirmed
//     by an admin. Is NOT shown on the public status page.
//   - StatusConfirmed: reviewed/confirmed by an admin. Only from here on
//     publicly visible.
const (
	StatusDraft     = "draft"
	StatusConfirmed = "confirmed"
)

// ScopeConfirmedOnly is a GORM scope that restricts a query to confirmed
// translations only - for public read paths that must never
// deliver unconfirmed (potentially wrong) translation suggestions.
// Admin endpoints read without this scope, so that they also see drafts to
// confirm.
//
// Expects a table with a "status" column that contains one of the two
// values above.
func ScopeConfirmedOnly(db *gorm.DB) *gorm.DB {
	return db.Where("status = ?", StatusConfirmed)
}
