package models

import "time"

// Quellen einer Statusänderung.
const (
	StatusSourceManual  = "manual"  // Admin-Oberfläche / API
	StatusSourceWebhook = "webhook" // Monitoring-Webhook
	// StatusSourceWebhookNachlauf markiert die Endstand-Zeile eines verdichteten Webhook-Fensters
	// Anker (webhook) trägt den schlimmsten Status, der Nachlauf den Endstand.
	StatusSourceWebhookNachlauf = "webhook_nachlauf"
	StatusSourceMaintenance     = "maintenance" // Wartungsfenster (Start/Ende)
	StatusSourceSelfMonitor     = "selfmonitor" // Selbstüberwachung
	StatusSourceCreated         = "created"     // Anfangsstatus beim Anlegen
	StatusSourceBackfill        = "backfill"    // einmalig nachgetragener Bestand
)

// ComponentStatusChange: ein Eintrag der Statushistorie einer
// Komponente. Component.Status ist nur der aktuelle Wert; erst diese Tabelle
// macht den Verlauf für die 90-Tage-Verfügbarkeit auswertbar. Reine
// Anhängetabelle (kein Soft-Delete, keine Änderung): Bereinigung nur über die
// Aufbewahrungsfrist (statushistory.Prune). Bewusst ohne Fremdschlüssel und
// ohne StatusPageID: Komponenten werden nur weich gelöscht, und die Seite wird
// immer über die Komponente aufgelöst (Mandantentrennung in der Abfrage).
type ComponentStatusChange struct {
	ID          uint `gorm:"primaryKey"`
	ComponentID uint `gorm:"not null;index:idx_csc_component_time,priority:1"`
	// OldStatus ist leer beim ersten Eintrag einer Komponente.
	OldStatus string    `gorm:"not null;default:''"`
	NewStatus string    `gorm:"not null"`
	Source    string    `gorm:"not null;default:''"`
	ChangedAt time.Time `gorm:"not null;index:idx_csc_component_time,priority:2;index:idx_csc_changed_at"`
	CreatedAt time.Time
}
