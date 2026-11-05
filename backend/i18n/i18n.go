// Package i18n bündelt (Stage 1 von 5 des Mehrsprachigkeits-
// Features) die gemeinsamen Bausteine, die alle künftigen lokalisierten
// Content-Modelle (Incident-Übersetzungen, Component,
// Maintenance/Notice) teilen: den Draft/Confirmed-Lebenszyklus einer
// Übersetzung und einen Query-Scope, der öffentlichen Endpunkten nur
// freigegebene Übersetzungen zeigt.
package i18n

import "gorm.io/gorm"

// Status-Konstanten für eine einzelne lokalisierte Content-Zeile:
//   - StatusDraft: vom Übersetzungs-Provider (siehe backend/translation)
//     vorgeschlagen oder manuell angelegt, aber noch nicht von einem Admin
//     bestätigt. Wird NICHT auf der öffentlichen Statusseite angezeigt.
//   - StatusConfirmed: von einem Admin geprüft/bestätigt. Erst ab hier
//     öffentlich sichtbar.
const (
	StatusDraft     = "draft"
	StatusConfirmed = "confirmed"
)

// ScopeConfirmedOnly ist ein GORM-Scope, der eine Query auf ausschließlich
// bestätigte Übersetzungen einschränkt - für öffentliche Lesepfade, die nie
// unbestätigte (potenziell falsche) Übersetzungsvorschläge ausliefern
// dürfen. Admin-Endpunkte lesen ohne diesen Scope, damit sie auch Drafts zum
// Bestätigen sehen.
//
// Erwartet eine Tabelle mit einer "status"-Spalte, die einen der beiden
// obigen Werte enthält.
func ScopeConfirmedOnly(db *gorm.DB) *gorm.DB {
	return db.Where("status = ?", StatusConfirmed)
}
